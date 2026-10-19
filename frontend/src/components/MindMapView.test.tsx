// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {DRAG_KEY} from './FileTree';
import * as api from '../api/client';
import {Draft,loadSharedDraft} from '../lib/drafts';
import type {NoteContent} from '../api/types';
import {MindMapView} from './MindMapView';
vi.mock('../api/client',async original=>({...await original<typeof api>(),getMindMap:vi.fn(),getDocument:vi.fn(),saveContent:vi.fn(),updateMindMapConfig:vi.fn(),deleteMindMapNode:vi.fn(),createMindMapNode:vi.fn(),addMindMapReference:vi.fn(),moveFileIntoMindMapNode:vi.fn(),linkFileToMindMapNode:vi.fn()}));
vi.mock('./VisualEditor',()=>({VisualEditor:({draft}:{draft:Draft})=><textarea aria-label="Idea text" readOnly={!draft.canEdit} value={draft.text} onChange={e=>draft.change(e.target.value)}/>}));
if(!HTMLDialogElement.prototype.showModal)HTMLDialogElement.prototype.showModal=function(){this.setAttribute('open','')};
const config=[{id:'a',x:40,y:80},{id:'b',x:400,y:80}];
const node=(id:string)=>({...config.find(n=>n.id===id)!,title:id==='a'?'First':'Second',path:`Maps/Plan/${id}.md`,revision:'1',owned:true});
const map:api.MindMap={path:'Maps/Plan',title:'Plan',revision:'1',config:{version:1,nodes:config,edges:[]},nodes:config.map(n=>node(n.id)),edges:[]};
const note=(id:string):NoteContent=>({markdown:`# ${id}`,path:`Maps/Plan/${id}.md`,revision:'1',canEdit:true,kind:'note',title:id});
let root:Root,host:HTMLDivElement,cache:Map<string,Draft>;
const open=vi.fn(),flush=vi.fn();
const load=(id:string)=>loadSharedDraft(cache,'vault',id,'alice',true);
const docs=['a','b','external'].map(id=>({id,path:id==='external'?'Inbox/Evidence.md':`Maps/Plan/${id}.md`,title:id==='external'?'Evidence':id,kind:'note',currentRevision:'1',updatedAt:''}));
const mount=(writable=true,epoch=0)=>act(async()=>root.render(<MindMapView space="vault" mapPath={map.path} writable={writable} principal="alice" docs={docs} epoch={epoch} onPreviewDraft={load} onResolveConflict={async()=>{}} onOpenDoc={open} onFlushDraft={flush} onInvalidateDoc={()=>{}} onChanged={()=>{}} onRevealFolder={()=>{}}/>));
const click=(el:Element)=>act(async()=>{el.dispatchEvent(new MouseEvent('click',{bubbles:true}))});
const key=(el:Element,key:string,extra:KeyboardEventInit={})=>act(async()=>{el.dispatchEvent(new KeyboardEvent('keydown',{key,bubbles:true,...extra}))});
const action=(label:string)=>host.querySelector(`[aria-label="${label}"]`)!;
const first=()=>host.querySelector('[data-node-id="a"]')!;
beforeEach(()=>{vi.clearAllMocks();localStorage.clear();cache=new Map();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true,CSS:{escape:(s:string)=>s}});host=document.createElement('div');document.body.append(host);root=createRoot(host);vi.mocked(api.getMindMap).mockResolvedValue(map);vi.mocked(api.getDocument).mockImplementation(async(_s,id)=>note(id));vi.mocked(api.saveContent).mockResolvedValue({revision:'2',committedAt:''});vi.mocked(api.updateMindMapConfig).mockImplementation(async(_s,_p,change)=>({...map,revision:'2',config:{...map.config,...change}}));vi.mocked(api.linkFileToMindMapNode).mockResolvedValue(map);vi.mocked(api.deleteMindMapNode).mockResolvedValue({...map,nodes:[node('b')],config:{...map.config,nodes:[config[1]]}})});
afterEach(async()=>{await act(async()=>{for(const draft of cache.values())draft.useServer(draft.text,draft.revision);root.unmount();await Promise.resolve()});host.remove();localStorage.clear()});
it('opens an editable preview and retains a failed document draft when closing',async()=>{
 await mount();await click(first());await act(async()=>{await new Promise(r=>setTimeout(r,0))});expect(action('Idea text')).not.toBeNull();
 const draft=cache.get('vault/a')!;vi.mocked(api.saveContent).mockRejectedValue(new Error('Offline'));await act(async()=>draft.change('Keep this idea'));await click(action('Close node preview'));expect(host.querySelector('.mindmap-preview')).not.toBeNull();expect(draft.text).toBe('Keep this idea');
});
it('provides keyboard resizing and closes to the selected node',async()=>{
 await mount();await click(first());const sep=action('Resize node preview');await key(sep,'End');expect(sep.getAttribute('aria-valuenow')).toBe('60');await click(action('Close node preview'));expect(document.activeElement).toBe(first());
});
it('shows real note-kind documents in the reference picker',async()=>{
 await mount();await click(action('Link an existing document'));expect(document.querySelector('dialog')?.textContent).toContain('Evidence');
});
it('connects existing ideas and exposes connectors through accessible controls',async()=>{
 await mount();await click(first());await click(action('Connect selected idea'));await click(host.querySelector('[data-node-id="b"]')!);expect(action('Select connector')).not.toBeNull();expect(host.querySelectorAll('.mindmap-edge')).toHaveLength(1);
});
it('removes a canvas node non-destructively with the latest layout revision',async()=>{
 await mount();await click(action('Actions for First'));const remove=[...host.querySelectorAll('[role="menuitem"]')].find(el=>el.textContent==='Remove from canvas')!;await click(remove);expect(api.deleteMindMapNode).toHaveBeenCalledWith('vault',map.path,{nodeId:'a',trash:false},'1');
});
it('offers explicit file collection with a visible destination before moving',async()=>{
 await mount();await click(action('Actions for First'));await click([...host.querySelectorAll('[role="menuitem"]')].find(el=>el.textContent==='Move another file into this node…')!);
 const evidence=[...document.querySelectorAll('[role="option"]')].find(el=>el.textContent?.includes('Evidence'))!;await click(evidence);expect(document.querySelector('dialog')?.textContent).toContain('Maps/Plan/Nodes/a/Evidence.md');expect(api.moveFileIntoMindMapNode).not.toHaveBeenCalled();
});
it('read-only maps have no mutation controls and do not save keyboard edits',async()=>{
 vi.mocked(api.getDocument).mockImplementation(async(_s,id)=>({...note(id),canEdit:false}));await mount(false);expect(action('Add node')).toBeNull();expect(action('Connect selected idea')).toBeNull();await key(first(),'ArrowRight',{altKey:true});expect(api.updateMindMapConfig).not.toHaveBeenCalled();await click(first());expect((action('Idea text')as HTMLTextAreaElement).readOnly).toBe(true);
 // Parent workspace and backend supply canEdit=false for a viewer. Canvas controls
 // must still refuse writes independently of that document contract.
});
it('keyboard branch creation opens a labeled creation dialog',async()=>{
 await mount();await key(first(),'Tab');expect(document.querySelector('dialog')?.textContent).toContain('New child node');
});
it('resizes a narrow preview vertically with keyboard controls',async()=>{
 const width=innerWidth;Object.defineProperty(window,'innerWidth',{value:500,configurable:true});
 try{await mount();await click(first());const separator=action('Resize node preview');expect(separator.getAttribute('aria-orientation')).toBe('horizontal');await key(separator,'End');expect(separator.getAttribute('aria-valuenow')).toBe('80')}finally{Object.defineProperty(window,'innerWidth',{value:width,configurable:true})}
});

it('exposes keyboard connection points and persists the selected border sides',async()=>{
 await mount();await click(action('Connect First from bottom'));expect(host.querySelector('.mindmap-connection-ghost')).not.toBeNull();await click(action('Connect Second from top'));
 expect(host.querySelectorAll('.mindmap-edge')).toHaveLength(1);
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(api.updateMindMapConfig).toHaveBeenLastCalledWith('vault',map.path,expect.objectContaining({edges:[expect.objectContaining({from:'a',to:'b',fromSide:'bottom',toSide:'top'})]}),'1');
});
it('Escape cancels a connector without writing or creating a document',async()=>{
 await mount();await click(action('Connect First from right'));await key(action('Connect First from right'),'Escape');expect(host.querySelector('.mindmap-connection-ghost')).toBeNull();expect(api.updateMindMapConfig).not.toHaveBeenCalled();expect(api.createMindMapNode).not.toHaveBeenCalled();
});
it('resizes a card by keyboard and renders its Markdown body',async()=>{
 await mount();await key(action('Resize First'),'ArrowDown');await key(action('Resize First'),'ArrowDown');expect(first().getAttribute('style')).toContain('height: 144px');expect(first().querySelector('.mindmap-card-body')).not.toBeNull();
});
it('dropping a document onto a node links it without invoking file moves',async()=>{
 await mount();await act(async()=>{const event=new Event('drop',{bubbles:true});Object.defineProperty(event,'dataTransfer',{value:{types:[DRAG_KEY],getData:()=>JSON.stringify({docs:['external'],folders:[],atts:[]})}});first().dispatchEvent(event);await new Promise(r=>setTimeout(r,0))});
 expect(api.linkFileToMindMapNode).toHaveBeenCalledWith('vault',map.path,{nodeId:'a',docId:'external',attachmentPath:undefined},'1');expect(api.moveFileIntoMindMapNode).not.toHaveBeenCalled();expect(api.createMindMapNode).not.toHaveBeenCalled();expect(flush).toHaveBeenCalledWith('a');
});
it('offers a new connected node on empty-canvas release and cancel creates no files',async()=>{
 await mount();const surface=host.querySelector('.mindmap-surface')!;
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 const point=document.elementFromPoint;document.elementFromPoint=()=>surface;
 const pointer=async(el:Element,type:string,x:number,y:number)=>act(async()=>el.dispatchEvent(new MouseEvent(type,{bubbles:true,button:0,clientX:x,clientY:y})));
 try{
  await pointer(action('Connect First from right'),'pointerdown',10,10);await pointer(surface,'pointermove',300,200);await pointer(surface,'pointerup',300,200);
  expect(document.querySelector('dialog')?.textContent).toContain('New child node');
  await click([...document.querySelectorAll('dialog button')].find(el=>el.textContent==='Cancel')!);
  expect(api.createMindMapNode).not.toHaveBeenCalled();expect(api.updateMindMapConfig).not.toHaveBeenCalled();expect(host.querySelector('.mindmap-connection-ghost')).toBeNull();
 }finally{HTMLElement.prototype.setPointerCapture=capture;document.elementFromPoint=point}
});
