import * as api from '../api/client';
import type {NoteContent} from '../api/types';
export type DraftStatus = 'saved'|'unsaved'|'saving'|'error'|'conflict';
export class Draft {
 text: string; base: string; revision: string; status: DraftStatus='saved'; error=''; version=0; acknowledged=0;
 private timer?: ReturnType<typeof setTimeout>; private pending?: Promise<void>; private listeners=new Set<()=>void>();
 constructor(public space:string,public id:string,text:string,revision:string,public canEdit:boolean,private save=api.saveContent,private principal='local'){
  this.text=text;this.base=text;this.revision=revision;
  try{const saved=JSON.parse(localStorage.getItem(this.storageKey)||'null');
   if(saved?.version===1&&typeof saved.text==='string'&&typeof saved.base==='string'&&typeof saved.revision==='string'){
    if(saved.text===text){localStorage.removeItem(this.storageKey);return}
    this.text=saved.text;this.base=saved.base;this.version=1;
    if(!canEdit||saved.revision!==revision){this.status='conflict';this.error=canEdit?'Recovered draft has a different base revision. Review both versions before saving.':'Recovered draft is retained, but this document is now read-only.'}
    else{this.status='unsaved';this.timer=setTimeout(()=>void this.flush(),750)}
   }
  }catch{/* Browser storage may be disabled. In-memory drafts still work. */}
 }
 private get storageKey(){return 'netsidian:draft:'+this.principal+':'+this.space+':'+this.id}
 private persist(){try{if(!this.dirty){localStorage.removeItem(this.storageKey);return}localStorage.setItem(this.storageKey,JSON.stringify({version:1,text:this.text,base:this.base,revision:this.revision}))}catch{/* Do not interrupt editing when storage is unavailable. */}}

 subscribe=(fn:()=>void)=>{this.listeners.add(fn);return ()=>{this.listeners.delete(fn)}};
 emit=()=>this.listeners.forEach(f=>f());
 change=(text:string)=>{if(!this.canEdit)return;this.text=text;this.version++;if(this.status!=='conflict')this.status='unsaved';this.persist();this.emit();clearTimeout(this.timer);this.timer=setTimeout(()=>void this.flush(),750)};
 async flush():Promise<void>{
  clearTimeout(this.timer); if(this.pending)return this.pending;
  if(!this.canEdit||this.status==='conflict'||this.acknowledged===this.version)return;
  this.pending=this.drain();try{await this.pending}finally{this.pending=undefined}
 }
 private async drain(){
  while(this.acknowledged<this.version){
   const generation=this.version,text=this.text,revision=this.revision;
   this.status='saving';this.emit();
   try{const r=await this.save(this.space,this.id,text,revision);this.revision=r.revision;this.base=text;this.acknowledged=generation;this.status=this.version===generation?'saved':'unsaved';this.error='';this.persist();this.emit();}
   catch(e){this.status=e instanceof api.ApiError&&[409,412,428].includes(e.status)?'conflict':'error';this.error=e instanceof Error?e.message:String(e);this.persist();this.emit();return;}
  }
 }
 async keepLocal(currentRevision:string){this.revision=currentRevision;this.status='unsaved';this.persist();await this.flush()}
 useServer(text:string,revision:string){clearTimeout(this.timer);this.text=text;this.base=text;this.revision=revision;this.version++;this.acknowledged=this.version;this.status='saved';this.error='';this.persist();this.emit()}
 get dirty(){return this.version!==this.acknowledged}
}

export interface DraftDocument {draft:Draft;note:NoteContent}
// Structural subset the Visual editor needs. The composer uses a lightweight
// local draft (no server autosave) while the document tabs use full Drafts.
export type DraftLike = Pick<Draft, 'text' | 'canEdit' | 'status' | 'error' | 'change' | 'subscribe'>;
export interface LocalDraft extends DraftLike { emit: () => void; }
export function localDraft(text: string, canEdit: boolean, onChange: (text: string) => void): LocalDraft {
  const listeners = new Set<() => void>();
  const draft: LocalDraft = {
    text, canEdit, status: 'saved', error: '',
    change: (next: string) => { if (!draft.canEdit) return; draft.text = next; draft.status = 'unsaved'; onChange(next); },
    subscribe: (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn); }; },
    emit: () => { listeners.forEach((fn) => fn()); },
  };
  return draft;
}
// Both the document tab and task pane use this loader. Coalescing reads and
// rechecking the cache after a read guarantees one save controller per file.
const sharedLoads=new WeakMap<Map<string,Draft>,Map<string,Promise<DraftDocument>>>();
const sharedNotes=new WeakMap<Map<string,Draft>,Map<string,NoteContent>>();
export function loadSharedDraft(cache:Map<string,Draft>,space:string,id:string,principal:string,refresh=false,read=api.getDocument,save=api.saveContent):Promise<DraftDocument>{
 const key=space+'/'+id;
 let notes=sharedNotes.get(cache);if(!notes){notes=new Map();sharedNotes.set(cache,notes)}
 let loads=sharedLoads.get(cache);if(!loads){loads=new Map();sharedLoads.set(cache,loads)}
 const pending=loads.get(key);if(pending)return pending;
 const existing=cache.get(key),known=notes.get(key);
 if(existing&&known&&!refresh)return Promise.resolve({draft:existing,note:known});
 const request=read(space,id).then(note=>{
  let draft=cache.get(key);
  if(!draft){draft=new Draft(space,id,note.markdown,note.revision,note.canEdit,save,principal);cache.set(key,draft)}
  else{draft.canEdit=note.canEdit;if(!draft.dirty&&draft.revision!==note.revision)draft.useServer(note.markdown,note.revision)}
  notes!.set(key,note);return {draft,note};
 }).finally(()=>{if(loads!.get(key)===request)loads!.delete(key)});
 loads.set(key,request);return request;
}
