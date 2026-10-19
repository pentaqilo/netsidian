import * as api from '../api/client';

export type CanvasState = Pick<api.MindMapConfig, 'nodes' | 'edges'>;
export type CanvasStatus = 'saved' | 'saving' | 'unsaved' | 'conflict' | 'error';

// A canvas has the same concurrency requirement as an editor draft: an older
// response must never replace a newer local edit. This controller survives
// asynchronous requests without relying on React render timing.
export class MindMapDraft {
  server: api.MindMap;
  canvas: CanvasState;
  status: CanvasStatus = 'saved';
  error = '';
  private generation = 0;
  private committed = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private flight: Promise<void> | null = null;
  private past: CanvasState[] = [];
  private future: CanvasState[] = [];
  get canUndo(){return this.past.length>0}
  get canRedo(){return this.future.length>0}
  undo(){const previous=this.past.pop();if(!previous)return;this.future.push(this.canvas);this.change(previous,true,false)}
  redo(){const next=this.future.pop();if(!next)return;this.past.push(this.canvas);this.change(next,true,false)}
  private listeners = new Set<() => void>();
  constructor(private space: string, private path: string, server: api.MindMap, private recoveryKey: string) {
    this.server = server;
    this.canvas = {nodes: server.config.nodes, edges: server.config.edges};
    try {
      const recovery = JSON.parse(localStorage.getItem(recoveryKey) || 'null');
      if (recovery && Array.isArray(recovery.nodes) && Array.isArray(recovery.edges)) {
        this.canvas = {nodes: recovery.nodes, edges: recovery.edges};
        this.generation = 1;
        this.status = recovery.revision === server.revision ? 'unsaved' : 'conflict';
        this.error = this.status === 'conflict' ? 'Recovered layout differs from the server. Review it before saving.' : 'Recovered unsaved layout.';
      }
    } catch { /* recovery storage may be unavailable */ }
  }
  get dirty() { return this.generation !== this.committed; }
  subscribe(fn: () => void) { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; }
  private emit() { this.listeners.forEach(fn => fn()); }
  private recover() {
    try {
      if (this.dirty) localStorage.setItem(this.recoveryKey, JSON.stringify({...this.canvas, revision: this.server.revision}));
      else localStorage.removeItem(this.recoveryKey);
    } catch { /* keep the live draft even if storage is full */ }
  }
  change(next: CanvasState, autosave = true, remember = true) {
    if(remember){this.past.push(this.canvas);if(this.past.length>100)this.past.shift();this.future=[]}
    this.canvas = next;
    this.generation++;
    if (this.status !== 'conflict' && this.status !== 'error') this.status = 'unsaved';
    this.recover(); this.emit();
    if (this.timer) clearTimeout(this.timer);
    if (autosave && this.status !== 'conflict' && this.status !== 'error') this.timer = setTimeout(() => { this.timer = null; void this.flush().catch(() => {}); }, 450);
  }
  // Background live refresh may update metadata only when no layout is pending.
  acceptRemote(server: api.MindMap) {
    if (this.dirty || this.flight) return false;
    this.server = server;
    this.canvas = {nodes: server.config.nodes, edges: server.config.edges};
    this.emit(); return true;
  }
  discard(server: api.MindMap) {
    if (this.flight) throw new Error('Wait for the current save before loading another layout.');
    if (this.timer) clearTimeout(this.timer); this.timer = null;
    this.server = server;
    this.past=[];this.future=[];
    this.canvas = {nodes: server.config.nodes, edges: server.config.edges};
    this.committed = this.generation;
    this.status = 'saved'; this.error = ''; this.recover(); this.emit();
  }
  async saveOver(remote: api.MindMap) {
    if (this.flight) await this.flight;
    this.server = remote;
    this.status = 'unsaved'; this.error = '';
    return this.flush();
  }
  flush(): Promise<void> {
    if (this.timer) clearTimeout(this.timer); this.timer = null;
    if (this.flight) return this.flight;
    if (this.status === 'conflict') return Promise.reject(new Error(this.error || 'Resolve the canvas conflict first.'));
    if (!this.dirty) return Promise.resolve();
    this.flight = this.saveLoop().finally(() => { this.flight = null; });
    return this.flight;
  }
  private async saveLoop() {
    while (this.dirty) {
      const generation = this.generation;
      const next = this.canvas;
      this.status = 'saving'; this.error = ''; this.emit();
      try {
        const server = await api.updateMindMapConfig(this.space, this.path, next, this.server.revision);
        this.server = server;
        this.committed = generation;
        // Retain newer local changes; the loop saves them with the new revision.
        if (!this.dirty) this.canvas = {nodes: server.config.nodes, edges: server.config.edges};
        this.status = this.dirty ? 'unsaved' : 'saved';
        this.recover(); this.emit();
      } catch (error) {
        this.status = api.ApiError.isConflict(error) ? 'conflict' : 'error';
        this.error = error instanceof Error ? error.message : 'Canvas could not be saved.';
        this.recover(); this.emit(); throw error;
      }
    }
  }
  dispose() {
    if (this.timer) clearTimeout(this.timer); this.timer = null;
    this.listeners.clear();
    if (this.dirty && this.status !== 'conflict' && this.status !== 'error') void this.flush().catch(() => {});
  }
}

// Workspace tab closure and file moves flush the same canvas controller that
// owns the view, including a draft still finishing a save after a tab switch.
const canvasDrafts=new Map<string,MindMapDraft>();
const canvasKey=(principal:string,space:string,path:string)=>`netsidian:mindmap-draft:${principal}:${space}:${path}`;
export function sharedMindMapDraft(principal:string,space:string,path:string,server:api.MindMap){
 const key=canvasKey(principal,space,path);let draft=canvasDrafts.get(key);
 if(!draft){draft=new MindMapDraft(space,path,server,key);canvasDrafts.set(key,draft)}else draft.acceptRemote(server);
 return draft;
}
export async function flushMindMapDraft(principal:string,space:string,path:string){await canvasDrafts.get(canvasKey(principal,space,path))?.flush()}
export async function flushMindMapDrafts(principal:string,space:string){const prefix=canvasKey(principal,space,'');for(const [key,draft]of canvasDrafts)if(key.startsWith(prefix))await draft.flush()}
