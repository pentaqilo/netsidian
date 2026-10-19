// @vitest-environment jsdom
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import * as api from '../api/client';
import {MindMapDraft} from './mindmapDraft';
vi.mock('../api/client', async importOriginal => ({...await importOriginal<typeof api>(),updateMindMapConfig:vi.fn()}));
const server=(revision='1',x=0):api.MindMap=>({path:'Maps/Plan',title:'Plan',revision,config:{version:1,nodes:[{id:'a',x,y:0}],edges:[]},nodes:[],edges:[]});
const edit=(x:number)=>({nodes:[{id:'a',x,y:0}],edges:[]});
beforeEach(()=>{localStorage.clear();vi.clearAllMocks()});
afterEach(()=>vi.useRealTimers());
it('retains edits made while an earlier request is in flight and saves them in order',async()=>{
 const draft=new MindMapDraft('vault','Maps/Plan',server(),'recovery');
 let finish!:(m:api.MindMap)=>void;
 vi.mocked(api.updateMindMapConfig).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce(server('3',200));
 draft.change(edit(100),false);const saving=draft.flush();draft.change(edit(200),false);finish(server('2',100));await saving;
 expect(draft.canvas.nodes[0].x).toBe(200);expect(draft.dirty).toBe(false);
 expect(api.updateMindMapConfig).toHaveBeenNthCalledWith(2,'vault','Maps/Plan',edit(200),'2');
 expect(localStorage.getItem('recovery')).toBeNull();draft.dispose();
});
it('live refresh does not discard unsaved canvas edits',()=>{
 const draft=new MindMapDraft('vault','Maps/Plan',server(),'recovery');draft.change(edit(120),false);
 expect(draft.acceptRemote(server('2',500))).toBe(false);expect(draft.canvas.nodes[0].x).toBe(120);
 // Keep offline draft for recovery without invoking a background save in this test.
 draft.status='error';draft.dispose();
});
it('retains failed saves across reload and requires explicit conflict resolution',async()=>{
 const draft=new MindMapDraft('vault','Maps/Plan',server(),'recovery');vi.mocked(api.updateMindMapConfig).mockRejectedValue(new Error('Offline'));
 draft.change(edit(120),false);await expect(draft.flush()).rejects.toThrow('Offline');draft.dispose();
 const recovered=new MindMapDraft('vault','Maps/Plan',server('2',40),'recovery');expect(recovered.canvas.nodes[0].x).toBe(120);expect(recovered.status).toBe('conflict');
 await expect(recovered.flush()).rejects.toThrow();vi.mocked(api.updateMindMapConfig).mockResolvedValue(server('3',120));await recovered.saveOver(server('2',40));expect(recovered.dirty).toBe(false);recovered.dispose();
});
it('discarding a recovered draft clears local recovery storage',()=>{
 localStorage.setItem('recovery',JSON.stringify({...edit(120),revision:'1'}));const draft=new MindMapDraft('vault','Maps/Plan',server('2'),'recovery');draft.discard(server('2'));
 expect(draft.canvas.nodes[0].x).toBe(0);expect(localStorage.getItem('recovery')).toBeNull();draft.dispose();
});
it('undoes and redoes layout edits without touching document contents',()=>{
 vi.useFakeTimers();const draft=new MindMapDraft('vault','Maps/Plan',server(),'recovery');draft.change(edit(120),false);draft.change(edit(240),false);draft.undo();expect(draft.canvas.nodes[0].x).toBe(120);draft.redo();expect(draft.canvas.nodes[0].x).toBe(240);draft.undo();draft.change(edit(300),false);expect(draft.canRedo).toBe(false);draft.status='error';draft.dispose();
});
