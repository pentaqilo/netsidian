import {describe,it,expect,vi} from 'vitest';
import {Draft,loadSharedDraft} from './drafts';
import {ApiError} from '../api/client';
describe('durable document draft controller',()=>{
 it('saves edits made while an older save is in flight without acknowledging them early',async()=>{
  let finish!:(v:{revision:string;committedAt:string})=>void;
  const save=vi.fn().mockImplementationOnce(()=>new Promise(r=>{finish=r})).mockResolvedValue({revision:'r3',committedAt:''});
  const d=new Draft('space','doc','base','r1',true,save);d.change('first');const pending=d.flush();d.change('second');expect(d.dirty).toBe(true);finish({revision:'r2',committedAt:''});await pending;
  expect(save).toHaveBeenCalledTimes(2);expect(save.mock.calls[1]).toEqual(['space','doc','second','r2']);expect(d.dirty).toBe(false);expect(d.text).toBe('second');expect(d.revision).toBe('r3');
 });
 it('serializes repeated explicit saves',async()=>{let finish!:(v:any)=>void;const save=vi.fn(()=>new Promise<{revision:string;committedAt:string}>(r=>finish=r));const d=new Draft('s','d','','r1',true,save);d.change('x');const a=d.flush(),b=d.flush();expect(save).toHaveBeenCalledTimes(1);finish({revision:'r2',committedAt:''});await Promise.all([a,b]);expect(d.status).toBe('saved')});
 it('retains a failed draft and retries',async()=>{const save=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue({revision:'r2'});const d=new Draft('s','d','base','r1',true,save);d.change('unsaved');await d.flush();expect(d.status).toBe('error');expect(d.dirty).toBe(true);expect(d.text).toBe('unsaved');await d.flush();expect(d.dirty).toBe(false)});
 it('preserves conflict base and does not retry blindly',async()=>{const save=vi.fn().mockRejectedValue(new ApiError(412,'precondition_failed','Changed',false,'r2'));const d=new Draft('s','d','base','r1',true,save);d.change('mine');await d.flush();await d.flush();expect(save).toHaveBeenCalledTimes(1);expect(d.base).toBe('base');expect(d.text).toBe('mine');expect(d.status).toBe('conflict')});
 it('does not edit viewer documents',()=>{const save=vi.fn();const d=new Draft('s','d','base','r1',false,save);d.change('forbidden');expect(d.text).toBe('base');expect(d.dirty).toBe(false)});
});

describe('shared task/document drafts',()=>{
 const note=(markdown='base',revision='r1',canEdit=true)=>({markdown,revision,canEdit,path:'Tasks/todo/Task.md',title:'Task',kind:'markdown'});
 it('coalesces overlapping loads and gives both editors the same save controller',async()=>{
  const cache=new Map<string,Draft>();let finish!:(n:ReturnType<typeof note>)=>void;
  const read=vi.fn(()=>new Promise<ReturnType<typeof note>>(resolve=>{finish=resolve})),save=vi.fn().mockResolvedValue({revision:'r2',committedAt:''});
  const preview=loadSharedDraft(cache,'s','task','alice',true,read,save),document=loadSharedDraft(cache,'s','task','alice',false,read,save);
  finish(note());const [a,b]=await Promise.all([preview,document]);expect(a.draft).toBe(b.draft);expect(read).toHaveBeenCalledTimes(1);
  a.draft.change('shared edit');await b.draft.flush();expect(save).toHaveBeenCalledTimes(1);expect(b.draft.text).toBe('shared edit');
 });
 it('refreshes clean content without replacing the controller or overwriting an unsaved draft',async()=>{
  const cache=new Map<string,Draft>(),read=vi.fn().mockResolvedValueOnce(note()).mockResolvedValueOnce(note('remote','r2')).mockResolvedValueOnce(note('other edit','r3'));
  const save=vi.fn().mockRejectedValue(new Error('Offline'));
  const first=await loadSharedDraft(cache,'s','task','alice',false,read,save);
  const fresh=await loadSharedDraft(cache,'s','task','alice',true,read,save);expect(fresh.draft).toBe(first.draft);expect(first.draft.text).toBe('remote');
  first.draft.change('mine');await first.draft.flush();await loadSharedDraft(cache,'s','task','alice',true,read,save);expect(first.draft.text).toBe('mine');expect(first.draft.revision).toBe('r2');expect(first.draft.dirty).toBe(true);
 });
 it('updates read-only permissions and retries after a failed initial read',async()=>{
  const cache=new Map<string,Draft>(),read=vi.fn().mockRejectedValueOnce(new Error('Offline')).mockResolvedValueOnce(note()).mockResolvedValueOnce(note('base','r1',false));
  await expect(loadSharedDraft(cache,'s','task','alice',false,read)).rejects.toThrow('Offline');
  const first=await loadSharedDraft(cache,'s','task','alice',false,read);await loadSharedDraft(cache,'s','task','alice',true,read);
  first.draft.change('forbidden');expect(first.draft.text).toBe('base');expect(first.draft.canEdit).toBe(false);
 });
});

describe('draft recovery across reloads',()=>{
 const installStorage=()=>{const values=new Map<string,string>();vi.stubGlobal('localStorage',{getItem:(k:string)=>values.get(k)??null,setItem:(k:string,v:string)=>values.set(k,v),removeItem:(k:string)=>values.delete(k)});return values};
 it('recovers failed edits and removes the device copy after successful save',async()=>{const values=installStorage();try{const fail=vi.fn().mockRejectedValue(new Error('offline'));const old=new Draft('s','recovery','base','r1',true,fail,'alice');old.change('retained');await old.flush();expect(values.size).toBe(1);const save=vi.fn().mockResolvedValue({revision:'r2'});const next=new Draft('s','recovery','base','r1',true,save,'alice');expect(next.text).toBe('retained');expect(next.dirty).toBe(true);await next.flush();expect(save).toHaveBeenCalledWith('s','recovery','retained','r1');expect(values.size).toBe(0)}finally{vi.unstubAllGlobals()}});
 it('keeps divergent recovered content in conflict instead of overwriting the server',async()=>{installStorage();try{const old=new Draft('s','conflict','base','r1',true,vi.fn().mockRejectedValue(new Error('offline')),'alice');old.change('mine');await old.flush();const save=vi.fn();const next=new Draft('s','conflict','theirs','r2',true,save,'alice');expect(next.text).toBe('mine');expect(next.base).toBe('base');expect(next.status).toBe('conflict');await next.flush();expect(save).not.toHaveBeenCalled();next.useServer('theirs','r2')}finally{vi.unstubAllGlobals()}});
 it('separates accounts and recognizes a save whose response was lost',async()=>{installStorage();try{const old=new Draft('s','isolated','base','r1',true,vi.fn().mockRejectedValue(new Error('lost response')),'alice');old.change('already committed');await old.flush();const other=new Draft('s','isolated','base','r1',true,vi.fn(),'bob');expect(other.text).toBe('base');const next=new Draft('s','isolated','already committed','r2',true,vi.fn(),'alice');expect(next.dirty).toBe(false);expect(next.text).toBe('already committed')}finally{vi.unstubAllGlobals()}});
});
