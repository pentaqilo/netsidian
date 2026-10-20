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
// Record the props each VisualEditor instance receives, keyed by document id,
// so wiring like the chrome-free inline `bare` mode is directly assertable.
const visualProps=vi.hoisted(()=>new Map<string,Record<string,unknown>>());
vi.mock('./VisualEditor',()=>({VisualEditor:(props:{draft:Draft})=>{visualProps.set(props.draft.id,props);return <textarea aria-label="Idea text" readOnly={!props.draft.canEdit} value={props.draft.text} onChange={e=>props.draft.change(e.target.value)}/>}}));
if(!HTMLDialogElement.prototype.showModal)HTMLDialogElement.prototype.showModal=function(){this.setAttribute('open','')};
const config=[{id:'a',x:40,y:80},{id:'b',x:400,y:80},{id:'c',x:220,y:300}];
const node=(id:string)=>({...config.find(n=>n.id===id)!,title:id==='a'?'First':id==='b'?'Second':'Third',path:`Maps/Plan/${id}.md`,revision:'1',owned:true});
const map:api.MindMap={path:'Maps/Plan',title:'Plan',revision:'1',config:{version:1,nodes:config,edges:[]},nodes:config.map(n=>node(n.id)),edges:[]};
const edgeMap:api.MindMap={...map,config:{...map.config,edges:[{id:'e1',from:'a',to:'b',label:'why'}]},edges:[{id:'e1',from:'a',to:'b',label:'why'}]};
const note=(id:string):NoteContent=>({markdown:`# ${id}`,path:`Maps/Plan/${id}.md`,revision:'1',canEdit:true,kind:'note',title:id});
let root:Root,host:HTMLDivElement,cache:Map<string,Draft>;
const open=vi.fn(),flush=vi.fn();
const load=(id:string)=>loadSharedDraft(cache,'vault',id,'alice',true);
const docs=[...['a','b','c','external'].map(id=>({id,path:id==='external'?'Inbox/Evidence.md':`Maps/Plan/${id}.md`,title:id==='external'?'Evidence':id,kind:'note',currentRevision:'1',updatedAt:''})),{id:'mm',path:'Maps/Plan/mindmap.md',title:'mindmap',kind:'note',currentRevision:'1',updatedAt:''}];
const mount=(writable=true,epoch=0,server:api.MindMap=map)=>{vi.mocked(api.getMindMap).mockResolvedValue(server);return act(async()=>root.render(<MindMapView space="vault" mapPath={map.path} writable={writable} principal="alice" docs={docs} epoch={epoch} onPreviewDraft={load} onResolveConflict={async()=>{}} onOpenDoc={open} onFlushDraft={flush} onInvalidateDoc={()=>{}} onChanged={()=>{}} onRevealFolder={()=>{}}/>))};
const click=(el:Element)=>act(async()=>{el.dispatchEvent(new MouseEvent('click',{bubbles:true}))});
const dblclick=(el:Element)=>act(async()=>{el.dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))});
const key=(el:Element,key:string,extra:KeyboardEventInit={})=>act(async()=>{el.dispatchEvent(new KeyboardEvent('keydown',{key,bubbles:true,...extra}))});
const pointer=(el:Element,type:string,x:number,y:number)=>act(async()=>{el.dispatchEvent(new MouseEvent(type,{bubbles:true,button:0,clientX:x,clientY:y}))});
const selectEdgeByClick=async(el:Element)=>{await act(async()=>{el.dispatchEvent(new MouseEvent('pointerdown',{bubbles:true,button:0,clientX:200,clientY:150}))});await click(el)};
const action=(label:string)=>host.querySelector(`[aria-label="${label}"]`)!;
const first=()=>host.querySelector('[data-node-id="a"]')!;
const outside=()=>{let button=host.querySelector<HTMLButtonElement>('.review-outside');if(!button){button=document.createElement('button');button.className='review-outside';button.textContent='Outside';host.append(button)}return button};
const focusInline=()=>act(async()=>{(first().querySelector('.mindmap-node-edit textarea') as HTMLElement).focus()});
const leaveInline=async()=>{await focusInline();await act(async()=>{outside().focus();await new Promise(r=>setTimeout(r,10))})};

beforeEach(()=>{visualProps.clear();window.getSelection()?.removeAllRanges();vi.clearAllMocks();localStorage.clear();cache=new Map();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true,CSS:{escape:(s:string)=>s}});host=document.createElement('div');document.body.append(host);root=createRoot(host);vi.mocked(api.getMindMap).mockResolvedValue(map);vi.mocked(api.getDocument).mockImplementation(async(_s,id)=>note(id));vi.mocked(api.saveContent).mockResolvedValue({revision:'2',committedAt:''});vi.mocked(api.updateMindMapConfig).mockImplementation(async(_s,_p,change)=>({...map,revision:'2',config:{...map.config,...change}}));vi.mocked(api.linkFileToMindMapNode).mockResolvedValue(map);vi.mocked(api.deleteMindMapNode).mockResolvedValue({...map,nodes:[node('b')],config:{...map.config,nodes:[config[1]]}})});
afterEach(async()=>{await act(async()=>{for(const draft of cache.values())draft.useServer(draft.text,draft.revision);root.unmount();await Promise.resolve()});host.remove();localStorage.clear()});
it('opens an editable preview and retains a failed document draft when closing',async()=>{
 await mount();await dblclick(first());await act(async()=>{await new Promise(r=>setTimeout(r,0))});expect(action('Idea text')).not.toBeNull();
 const draft=cache.get('vault/a')!;vi.mocked(api.saveContent).mockRejectedValue(new Error('Offline'));await act(async()=>draft.change('Keep this idea'));await click(action('Close node preview'));expect(host.querySelector('.mindmap-preview')).not.toBeNull();expect(draft.text).toBe('Keep this idea');
});
it('provides keyboard resizing and closes to the selected node',async()=>{
 await mount();await dblclick(first());const sep=action('Resize node preview');await key(sep,'End');expect(sep.getAttribute('aria-valuenow')).toBe('60');await click(action('Close node preview'));expect(document.activeElement).toBe(first());
});
it('shows real note-kind documents in the reference picker',async()=>{
 await mount();await click(action('Link an existing document'));expect(document.querySelector('dialog')?.textContent).toContain('Evidence');
});
it('connects existing ideas and exposes connectors through accessible controls',async()=>{
 await mount();await click(first());await click(action('Connect selected idea'));await click(host.querySelector('[data-node-id="b"]')!);
 // No dropdown: the connector itself is the accessible control.
 const hit=host.querySelector('.mindmap-edge-hit')!;
 expect(hit.getAttribute('role')).toBe('button');expect(hit.getAttribute('tabindex')).toBe('0');
 expect(hit.getAttribute('aria-label')).toContain('First');expect(hit.getAttribute('aria-label')).toContain('Second');
 expect(host.querySelectorAll('.mindmap-edge')).toHaveLength(1);
});
it('removes a canvas node non-destructively with the latest layout revision',async()=>{
 await mount();await click(action('Actions for First'));const remove=[...host.querySelectorAll('[role="menuitem"]')].find(el=>el.textContent==='Remove from canvas')!;await click(remove);expect(api.deleteMindMapNode).toHaveBeenCalledWith('vault',map.path,{nodeId:'a',trash:false},'1');
});
it('offers explicit file collection with a visible destination before moving',async()=>{
 await mount();await click(action('Actions for First'));await click([...host.querySelectorAll('[role="menuitem"]')].find(el=>el.textContent==='Move another file into this node…')!);
 const evidence=[...document.querySelectorAll('[role="option"]')].find(el=>el.textContent?.includes('Evidence'))!;await click(evidence);expect(document.querySelector('dialog')?.textContent).toContain('Maps/Plan/Nodes/a/Evidence.md');expect(api.moveFileIntoMindMapNode).not.toHaveBeenCalled();
});
it('read-only maps have no mutation controls and do not save keyboard edits',async()=>{
 vi.mocked(api.getDocument).mockImplementation(async(_s,id)=>({...note(id),canEdit:false}));await mount(false);expect(action('Add node')).toBeNull();expect(action('Connect selected idea')).toBeNull();await key(first(),'ArrowRight',{altKey:true});expect(api.updateMindMapConfig).not.toHaveBeenCalled();await dblclick(first());expect((action('Idea text')as HTMLTextAreaElement).readOnly).toBe(true);
 // Parent workspace and backend supply canEdit=false for a viewer. Canvas controls
 // must still refuse writes independently of that document contract.
});
it('keyboard branch creation opens a labeled creation dialog',async()=>{
 await mount();await key(first(),'Tab');expect(document.querySelector('dialog')?.textContent).toContain('New child node');
});
it('resizes a narrow preview vertically with keyboard controls',async()=>{
 const width=innerWidth;Object.defineProperty(window,'innerWidth',{value:500,configurable:true});
 try{await mount();await dblclick(first());const separator=action('Resize node preview');expect(separator.getAttribute('aria-orientation')).toBe('horizontal');await key(separator,'End');expect(separator.getAttribute('aria-valuenow')).toBe('80')}finally{Object.defineProperty(window,'innerWidth',{value:width,configurable:true})}
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
it('keeps floating navigation outside the transformed canvas and supports read-only zoom',async()=>{
 await mount(false);const nav=host.querySelector('[aria-label="Mind map navigation"]')!;
 expect(host.querySelector('.mindmap-world')?.contains(nav)).toBe(false);
 await click(action('Zoom in'));expect(host.querySelector('.mindmap-world')?.getAttribute('style')).toContain('scale(1.2)');
 await click(action('Reset zoom to 100%'));expect(host.querySelector('.mindmap-world')?.getAttribute('style')).toContain('scale(1)');
 await key(action('Zoom out'),'End');expect(document.activeElement).toBe(action('Show minimap'));
 await act(async()=>nav.dispatchEvent(new MouseEvent('dblclick',{bubbles:true,clientX:100,clientY:100})));
 expect(document.querySelector('dialog')).toBeNull();expect(api.createMindMapNode).not.toHaveBeenCalled();expect(api.updateMindMapConfig).not.toHaveBeenCalled();
});

it('connection state adds no banner and never shifts, refits or pans the canvas',async()=>{
 await mount();const world=host.querySelector('.mindmap-world')!;const before=world.getAttribute('style');
 await click(action('Connect First from right'));
 expect(host.querySelector('.mindmap-connect-hint')).toBeNull();
 expect(host.querySelector('.mindmap-connection-ghost')).not.toBeNull();
 expect(world.getAttribute('style')).toBe(before);
 await click(action('Connect First from right'));expect(host.querySelector('.mindmap-connection-ghost')).toBeNull();
 expect(world.getAttribute('style')).toBe(before);
 await click(action('Connect First from right'));
 await key(document.body,'Escape');
 expect(host.querySelector('.mindmap-connection-ghost')).toBeNull();
 expect(world.getAttribute('style')).toBe(before);
 expect(api.updateMindMapConfig).not.toHaveBeenCalled();
});

it('selects a connection by clicking its visible line without panning or clearing',async()=>{
 await mount(false,0,edgeMap);
 const hit=host.querySelector('.mindmap-edge-hit')!;
 await act(async()=>{hit.dispatchEvent(new MouseEvent('pointerdown',{bubbles:true,button:0,clientX:200,clientY:150}))});
 await click(hit);
 expect(host.querySelector('.mindmap-edge.selected')).not.toBeNull();
 expect(host.querySelector('.mindmap-edge-inspector')).not.toBeNull();
 // The hit target follows the curve and stays generous at any zoom.
 expect((host.querySelector('.mindmap-edge-hit') as HTMLElement).getAttribute('style')).toContain('stroke-width: 18');
});

it('Delete removes only the selected connection, never while typing in its label',async()=>{
 await mount(true,0,edgeMap);
 await selectEdgeByClick(host.querySelector('.mindmap-edge-hit')!);
 expect(host.querySelector('.mindmap-edge-inspector')).not.toBeNull();
 const label=action('Connector label') as HTMLInputElement;
 await act(async()=>{label.focus();label.dispatchEvent(new KeyboardEvent('keydown',{key:'Delete',bubbles:true,cancelable:true}))});
 expect(api.updateMindMapConfig).not.toHaveBeenCalled();
 await key(document.body,'Delete');expect(api.updateMindMapConfig).not.toHaveBeenCalled();
 await key(host.querySelector('.mindmap-surface')!,'Delete');
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(api.updateMindMapConfig).toHaveBeenLastCalledWith('vault',map.path,expect.objectContaining({edges:[]}),'1');
 // Deleting the connection leaves both documents untouched.
 expect(api.deleteMindMapNode).not.toHaveBeenCalled();
});

it('reconnects an endpoint to another node keeping id and label, and Escape cancels',async()=>{
 await mount(true,0,edgeMap);
 await selectEdgeByClick(host.querySelector('.mindmap-edge-hit')!);
 await click([...host.querySelectorAll('.mindmap-edge-inspector button')].find(el=>el.textContent==='Move start')!);
 expect(host.querySelector('.mindmap-connection-ghost')).not.toBeNull();
 await click(host.querySelector('[data-node-id="c"]')!);
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(api.updateMindMapConfig).toHaveBeenLastCalledWith('vault',map.path,expect.objectContaining({edges:[expect.objectContaining({id:'e1',from:'c',to:'b',label:'why'})]}),'1');
  // Escape during a reconnect changes nothing.
  await selectEdgeByClick(host.querySelector('.mindmap-edge-hit')!);
  await click([...host.querySelectorAll('.mindmap-edge-inspector button')].find(el=>el.textContent==='Move end')!);
 await key(document.body,'Escape');
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(api.updateMindMapConfig).toHaveBeenCalledTimes(1);
});

it('read-only viewers can select and inspect a connection but cannot mutate it',async()=>{
 await mount(false,0,edgeMap);
 const hit=host.querySelector('.mindmap-edge-hit')!;
 await act(async()=>{hit.dispatchEvent(new MouseEvent('pointerdown',{bubbles:true,button:0,clientX:200,clientY:150}))});
 await click(hit);
 const inspector=host.querySelector('.mindmap-edge-inspector');
 expect(inspector).not.toBeNull();
 expect((action('Connector label') as HTMLInputElement).readOnly).toBe(true);
 expect([...inspector!.querySelectorAll('button')].some(el=>el.textContent?.includes('Delete'))).toBe(false);
 expect([...inspector!.querySelectorAll('button')].some(el=>el.textContent==='Move start')).toBe(false);
 expect(host.querySelector('.mindmap-edge-handle')).toBeNull();
 await key(host.querySelector('.mindmap-surface')!,'Delete');
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(api.updateMindMapConfig).not.toHaveBeenCalled();
});

it('keeps one compact header: path and configuration left, action cluster right, no save chip or connector dropdown',async()=>{
 await mount();
 const header=host.querySelector('.mindmap-header')!;
 expect(header.querySelector('.mindmap-heading h1')?.textContent).toContain('Plan');
 // Left side: folder path and Configuration stay in the heading under the title.
 const sub=header.querySelector('.mindmap-sub')!;
 expect(sub.querySelector('.mindmap-path')?.textContent).toContain('Maps/Plan');
 expect(sub.querySelector('.mindmap-config')?.textContent).toContain('Configuration');
 // Right side: every action lives in the same header row, not a second bar.
 const tools=header.querySelector('[role="toolbar"][aria-label="Mind map tools"]')!;
 for(const label of ['Add node','Link an existing document','Undo canvas edit','Redo canvas edit','Arrange branches','Connect selected idea','Reload mind map'])expect(tools.querySelector(`[aria-label="${label}"]`)).not.toBeNull();
 expect([...header.children].indexOf(header.querySelector('.mindmap-heading')!)).toBeLessThan([...header.children].indexOf(tools));
 // Removed controls: no ordinary Saved/Saving chip and no Connections selector.
 expect(host.querySelector('.mindmap-save')).toBeNull();
 expect(host.querySelector('.mindmap-connector-select')).toBeNull();
 expect(host.querySelector('select')).toBeNull();
});

it('reaches a connector from the keyboard: labelled focusable path, Enter/Space inspect, Delete removes',async()=>{
 await mount(true,0,edgeMap);
 const hit=host.querySelector('.mindmap-edge-hit')!;
 expect(hit.getAttribute('tabindex')).toBe('0');expect(hit.getAttribute('role')).toBe('button');
 expect(hit.getAttribute('aria-label')).toBe('Connection from First to Second, labelled why. Press Enter to inspect.');
 await key(hit,' ');
 expect(host.querySelector('.mindmap-edge.selected')).not.toBeNull();
 expect(host.querySelector('.mindmap-edge-inspector')).not.toBeNull();
 await key(hit,'Enter');expect(host.querySelector('.mindmap-edge-inspector')).not.toBeNull();
 // Delete works while the connector itself holds focus, and only removes the edge.
 await key(hit,'Delete');
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(api.updateMindMapConfig).toHaveBeenLastCalledWith('vault',map.path,expect.objectContaining({edges:[]}),'1');
 expect(api.deleteMindMapNode).not.toHaveBeenCalled();
});

it('drags a node by its frame even when DOM text is selected, clearing only the stale selection',async()=>{
 await mount();
 await key(action('Resize First'),'ArrowDown');await key(action('Resize First'),'ArrowDown');
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 const body=first().querySelector('.mindmap-card-body')!;
 const selection=window.getSelection()!;const range=document.createRange();range.selectNodeContents(body);selection.addRange(range);
 expect(selection.rangeCount).toBe(1);
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 try{
  await pointer(first(),'pointerdown',100,100);
  // A press alone preserves the selection; moving clears it exactly once.
  expect(selection.rangeCount).toBe(1);
  await pointer(host.querySelector('.mindmap-surface')!,'pointermove',164,148);
  expect(selection.toString()).toBe('');
  await pointer(host.querySelector('.mindmap-surface')!,'pointerup',164,148);
 }finally{HTMLElement.prototype.setPointerCapture=capture}
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 // The keyboard resize already saved once, so the drag save carries revision '2'.
 expect(api.updateMindMapConfig).toHaveBeenLastCalledWith('vault',map.path,expect.objectContaining({nodes:[expect.objectContaining({id:'a',x:104,y:128}),expect.objectContaining({id:'b'}),expect.objectContaining({id:'c'})]}),'2');
});

it('leaves card body text gestures alone: body drag never moves the node and keeps the selection',async()=>{
 await mount();
 await key(action('Resize First'),'ArrowDown');await key(action('Resize First'),'ArrowDown');
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 const scroll=first().querySelector('.mindmap-card-scroll')!;
 const selection=window.getSelection()!;const range=document.createRange();range.selectNodeContents(scroll.querySelector('.mindmap-card-body')!);selection.addRange(range);
 const saves=vi.mocked(api.updateMindMapConfig).mock.calls.length;
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 try{
  await pointer(scroll,'pointerdown',120,150);
  await pointer(host.querySelector('.mindmap-surface')!,'pointermove',180,190);
  await pointer(host.querySelector('.mindmap-surface')!,'pointerup',180,190);
 }finally{HTMLElement.prototype.setPointerCapture=capture}
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(vi.mocked(api.updateMindMapConfig).mock.calls.length).toBe(saves);
 expect(selection.rangeCount).toBe(1);
 // Native text dragging stays allowed inside the selectable body but is
 // suppressed on the header/frame drag surface.
 const fromBody=new Event('dragstart',{bubbles:true,cancelable:true});scroll.dispatchEvent(fromBody);expect(fromBody.defaultPrevented).toBe(false);
 const fromFrame=new Event('dragstart',{bubbles:true,cancelable:true});first().querySelector('.mindmap-node-title')!.dispatchEvent(fromFrame);expect(fromFrame.defaultPrevented).toBe(true);
});

it('read-only maps never move nodes by pointer drag',async()=>{
 await mount(false);
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 try{
  await pointer(first(),'pointerdown',100,100);
  await pointer(host.querySelector('.mindmap-surface')!,'pointermove',164,148);
  await pointer(host.querySelector('.mindmap-surface')!,'pointerup',164,148);
 }finally{HTMLElement.prototype.setPointerCapture=capture}
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(api.updateMindMapConfig).not.toHaveBeenCalled();
});

it('node mini-card body reflects saved preview edits after switching selection',async()=>{
 vi.mocked(api.getDocument).mockImplementation(async(_s,id)=>({...note(id),markdown:cache.get('vault/'+id)?.text??note(id).markdown}));
 await mount();
 await key(action('Resize First'),'ArrowDown');await key(action('Resize First'),'ArrowDown');
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 await dblclick(first());await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 const boardSaves=vi.mocked(api.updateMindMapConfig).mock.calls.length;
 const draft=cache.get('vault/a')!;
 await act(async()=>{draft.change('Fresh body from the preview editor');await new Promise(r=>setTimeout(r,1000))});
 expect(first().querySelector('.mindmap-card-body')?.textContent).toContain('Fresh body from the preview editor');
 await dblclick(host.querySelector('[data-node-id="b"]')!);await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 expect(first().querySelector('.mindmap-card-body')?.textContent).toContain('Fresh body from the preview editor');
 // Node Markdown edits never save board metadata (only the earlier resize did).
 expect(vi.mocked(api.updateMindMapConfig).mock.calls.length).toBe(boardSaves);
});

// ---- edge auto-pan while dragging ----
const stubSurfaceRect=(surface:Element)=>{surface.getBoundingClientRect=()=>({left:0,top:0,right:800,bottom:600,width:800,height:600,x:0,y:0} as DOMRect)};
const stubFrames=()=>{
 const raf=globalThis.requestAnimationFrame,cancel=globalThis.cancelAnimationFrame;
 const frames=new Map<number,FrameRequestCallback>();let id=0,now=0;
 vi.stubGlobal('requestAnimationFrame',(cb:FrameRequestCallback)=>{frames.set(++id,cb);return id});
 vi.stubGlobal('cancelAnimationFrame',(id:number)=>frames.delete(id));
 return Object.assign(()=>{frames.clear();vi.stubGlobal('requestAnimationFrame',raf);vi.stubGlobal('cancelAnimationFrame',cancel)},
 {advance:async(dt=16)=>{now+=dt;const pending=[...frames.values()];frames.clear();await act(async()=>pending.forEach(cb=>cb(now)))} });
};
const worldStyle=()=>host.querySelector('.mindmap-world')!.getAttribute('style')!;

it('auto-pans the camera when a dragged node reaches the canvas edge, keeps it under the pointer and never saves camera moves',async()=>{
 await mount();
 const surface=host.querySelector('.mindmap-surface')!;
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 const restoreFrames=stubFrames();
 try{
  stubSurfaceRect(surface);
  await pointer(first(),'pointerdown',100,100);
  await pointer(surface,'pointermove',790,590); // bottom-right activation band
  const before=worldStyle();
  await restoreFrames.advance();await restoreFrames.advance();
  const after=worldStyle();
  expect(after).not.toBe(before);
  expect(Number(/translate\(([-\d.]+)/.exec(after)![1])).toBeLessThan(0); // right edge reveals space to the right
  // the camera panned while the pointer held still
  expect(api.updateMindMapConfig).not.toHaveBeenCalled(); // camera-only movement never touches the layout draft
  // Coordinate continuity: the card keeps its original offset under the pointer
  // even though both it and the camera moved (drag offset was 60/20).
  const [,vx,vy]=/translate\(([-\d.]+)px, ([-\d.]+)px\)/.exec(after)!;
  const [,nx,ny]=/left: ([-\d.]+)px; top: ([-\d.]+)px/.exec(first().getAttribute('style')!)!;
  expect(Math.abs(Number(vx)+Number(nx)-730)).toBeLessThanOrEqual(10);
  expect(Math.abs(Number(vy)+Number(ny)-570)).toBeLessThanOrEqual(10);
  await pointer(surface,'pointerup',790,590);
  const settled=worldStyle();
  await restoreFrames.advance();await restoreFrames.advance();
  expect(worldStyle()).toBe(settled); // release stops the pan loop
 }finally{HTMLElement.prototype.setPointerCapture=capture;restoreFrames()}
});

it('never auto-pans for hovering, background panning or resizing',async()=>{
 await mount();
 const surface=host.querySelector('.mindmap-surface')!;
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 const restoreFrames=stubFrames();
 try{
  stubSurfaceRect(surface);
  await pointer(surface,'pointermove',790,590); // hover into the band: nothing pans
  await restoreFrames.advance();await restoreFrames.advance();
  expect(worldStyle()).toContain('translate(0px, 0px) scale(1)');
  await pointer(surface,'pointerdown',100,100);
  await pointer(surface,'pointermove',790,590); // background pan: only the manual delta applies
  await restoreFrames.advance();await restoreFrames.advance();
  expect(worldStyle()).toContain('translate(690px, 490px) scale(1)');
  await pointer(surface,'pointerup',790,590);
  await pointer(action('Resize First'),'pointerdown',780,590);
  await pointer(surface,'pointermove',795,595);
  await restoreFrames.advance();await restoreFrames.advance();
  expect(worldStyle()).toContain('translate(690px, 490px) scale(1)'); // resizing never pans
  await pointer(surface,'pointerup',795,595);
 }finally{HTMLElement.prototype.setPointerCapture=capture;restoreFrames()}
});

it('Escape stops edge auto-pan mid-drag without saving the camera',async()=>{
 await mount();
 const surface=host.querySelector('.mindmap-surface')!;
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 const restoreFrames=stubFrames();
 try{
  stubSurfaceRect(surface);
  await pointer(first(),'pointerdown',100,100);
  await pointer(surface,'pointermove',790,590);
  await restoreFrames.advance();
  const panned=worldStyle();
  expect(panned).not.toContain('translate(0px, 0px)');
  await key(document.body,'Escape');
  await restoreFrames.advance();await restoreFrames.advance();
  expect(worldStyle()).toBe(panned); // loop stopped; camera frozen
  expect(api.updateMindMapConfig).not.toHaveBeenCalled();
 }finally{HTMLElement.prototype.setPointerCapture=capture;restoreFrames()}
});

it('auto-pans while dragging a connector and cancels cleanly on Escape',async()=>{
 await mount();
 const surface=host.querySelector('.mindmap-surface')!;
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 const point=document.elementFromPoint;document.elementFromPoint=()=>null;
 const restoreFrames=stubFrames();
 try{
  stubSurfaceRect(surface);
  await pointer(action('Connect First from right'),'pointerdown',10,10);
  await pointer(surface,'pointermove',790,590);
  expect(host.querySelector('.mindmap-connection-ghost')).not.toBeNull();
  await restoreFrames.advance();await restoreFrames.advance();
  expect(worldStyle()).not.toBe('translate(0px, 0px) scale(1)'); // camera followed the ghost to the edge
  expect(api.updateMindMapConfig).not.toHaveBeenCalled();
  await key(document.body,'Escape');
  expect(host.querySelector('.mindmap-connection-ghost')).toBeNull();
  const settled=worldStyle();
  await restoreFrames.advance();await restoreFrames.advance();
  expect(worldStyle()).toBe(settled);
 }finally{HTMLElement.prototype.setPointerCapture=capture;document.elementFromPoint=point;restoreFrames()}
});

// ---- inline text editing on the card ----
it('opens the inline editor from F2 and shares one draft with the preview',async()=>{
 vi.mocked(api.getDocument).mockImplementation(async(_s,id)=>({...note(id),markdown:cache.get('vault/'+id)?.text??note(id).markdown,revision:cache.get('vault/'+id)?.revision??'1'}));
 await mount();
 await key(first(),'F2');
 await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 const editor=first().querySelector('.mindmap-node-edit textarea') as HTMLTextAreaElement;
 expect(editor).not.toBeNull();
 const draft=cache.get('vault/a')!;
 await act(async()=>{draft.change('Inline thought')});
 expect((first().querySelector('.mindmap-node-edit textarea') as HTMLTextAreaElement).value).toBe('Inline thought');
 // Opening the preview on the same node reuses the very same Draft instance.
 await dblclick(first());await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 const previewArea=host.querySelector('.mindmap-preview textarea') as HTMLTextAreaElement;
 expect(previewArea.value).toBe('Inline thought');
 expect(cache.size).toBe(1);
 expect(first().querySelector('.mindmap-node-edit')).toBeNull();
 expect(host.querySelectorAll('textarea')).toHaveLength(1);
 expect(host.querySelector('.mindmap-preview')).not.toBeNull();
 expect(api.saveContent).toHaveBeenCalled();
});

it('double-click body edits inline, header previews, and Enter explicitly opens the document',async()=>{
 await mount();
 await key(action('Resize First'),'ArrowDown');await key(action('Resize First'),'ArrowDown');
 await act(async()=>{await new Promise(r=>setTimeout(r,500))});
 expect(first().querySelector('.mindmap-card-scroll')).not.toBeNull();
 await act(async()=>{first().querySelector('.mindmap-card-scroll')!.dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))});
 await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 expect(first().querySelector('.mindmap-node-edit textarea')).not.toBeNull();
 expect(open).not.toHaveBeenCalled();
 await key(first().querySelector('.mindmap-node-edit textarea')!,'Escape');
 await dblclick(first().querySelector('.mindmap-node-title')!);expect(host.querySelector('.mindmap-preview')).not.toBeNull();
 expect(open).not.toHaveBeenCalled();await key(first(),'Enter');expect(open).toHaveBeenCalledWith('a');
});

it('the card edit button opens a bare editor and losing focus saves and closes without stealing focus',async()=>{
 await mount();
 await click(action('Edit text of First'));
 await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 expect(first().querySelector('.mindmap-node-edit textarea')).not.toBeNull();
 await leaveInline();
 expect(first().querySelector('.mindmap-node-edit')).toBeNull();
});

it('read-only maps expose no inline editing entry points',async()=>{
 vi.mocked(api.getDocument).mockImplementation(async(_s,id)=>({...note(id),canEdit:false}));
 await mount(false);
 expect(action('Edit text of First')).toBeNull();
 await key(first(),'F2');
 await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 expect(first().querySelector('.mindmap-node-edit')).toBeNull();
});


it('retains a failed inline draft on blur and node switch, then saves successfully on retry',async()=>{
 await mount();await click(action('Edit text of First'));
 const draft=cache.get('vault/a')!;vi.mocked(api.saveContent).mockRejectedValue(new Error('Offline'));
 await act(async()=>draft.change('Keep this inline edit'));
 await leaveInline();
 expect(first().querySelector('.mindmap-node-edit')).not.toBeNull();expect(host.textContent).toContain('Offline');
 await click(action('Edit text of Second'));
 expect(first().querySelector('.mindmap-node-edit')).not.toBeNull();expect(cache.has('vault/b')).toBe(false);
 expect(localStorage.getItem('netsidian:draft:alice:vault:a')).toContain('Keep this inline edit');
 vi.mocked(api.saveContent).mockResolvedValue({revision:'2',committedAt:''});
 await key(first().querySelector('textarea')!,'Escape');
 expect(first().querySelector('.mindmap-node-edit')).toBeNull();expect(draft.dirty).toBe(false);
 expect(api.updateMindMapConfig).not.toHaveBeenCalled();
});
it('mounts one editor, preserves saved size, and isolates editor keys from canvas actions',async()=>{
 await mount();await dblclick(first());await click(action('Edit text of First'));
 expect(host.querySelectorAll('textarea')).toHaveLength(1);
 expect(first().getAttribute('style')).toContain('height: 112px');
 expect(visualProps.get('a')?.bare).toBe(true);
 expect(first().querySelector('.mindmap-node-edit-bar')).toBeNull();
 expect(first().querySelector('.mindmap-node-edit h1')).toBeNull();
 expect(first().querySelector('.mindmap-card-body')?.getAttribute('aria-hidden')).toBe('true');
 const editor=first().querySelector('textarea')!;
 await key(editor,'Tab');await key(editor,'Delete');await key(editor,'Enter');await key(editor,'ArrowRight',{altKey:true});
 expect(api.createMindMapNode).not.toHaveBeenCalled();expect(api.updateMindMapConfig).not.toHaveBeenCalled();expect(open).not.toHaveBeenCalled();
 await key(editor,'Escape');expect(first().getAttribute('style')).toContain('height: 112px');
 expect(host.querySelectorAll('textarea')).toHaveLength(1);
});
it('cancels and restores an auto-panned node on lost capture, without saving its intermediate position',async()=>{
 await mount();const surface=host.querySelector('.mindmap-surface')!,capture=HTMLElement.prototype.setPointerCapture;
 HTMLElement.prototype.setPointerCapture=vi.fn();const frames=stubFrames();
 try{stubSurfaceRect(surface);await pointer(first(),'pointerdown',100,100);await pointer(surface,'pointermove',790,590);await frames.advance();
 await pointer(surface,'lostpointercapture',790,590);const stopped=worldStyle();await frames.advance();
 expect(worldStyle()).toBe(stopped);expect(first().getAttribute('style')).toContain('left: 40px; top: 80px');
 await pointer(surface,'pointerup',790,590);expect(api.updateMindMapConfig).not.toHaveBeenCalled();
 }finally{HTMLElement.prototype.setPointerCapture=capture;frames()}
});


it('refuses inline edits when the document is read-only even on an editable map',async()=>{
 vi.mocked(api.getDocument).mockImplementation(async(_s,id)=>({...note(id),canEdit:false}));
 await mount();await click(action('Edit text of First'));
 expect(first().querySelector('.mindmap-node-edit')).toBeNull();expect(host.textContent).toContain('This document is read-only');
 expect(api.saveContent).not.toHaveBeenCalled();expect(api.updateMindMapConfig).not.toHaveBeenCalled();
});
it('guards leaving the page while an inline document draft is unsaved',async()=>{
 await mount();await key(first(),'F2');const draft=cache.get('vault/a')!;
 await act(async()=>draft.change('Retain on reload'));
 const event=new Event('beforeunload',{cancelable:true});window.dispatchEvent(event);
 expect(event.defaultPrevented).toBe(true);
});

it('closed formatting menus do not block Escape from finishing an inline edit',async()=>{
 await mount();await key(first(),'F2');const inline=first().querySelector('.mindmap-node-edit')!;
 const details=document.createElement('details');details.innerHTML='<summary>Table</summary><div role="menu">Insert</div>';inline.append(details);
 inline.querySelector('textarea')!.addEventListener('keydown',e=>e.preventDefault());
 await key(inline.querySelector('textarea')!,'Escape');expect(first().querySelector('.mindmap-node-edit')).toBeNull();
});


it('captures pending visual text synchronously before a blur save',async()=>{
 await mount();await key(first(),'F2');await focusInline();
 const draft=cache.get('vault/a')!;
 const flush=visualProps.get('a')!.flushRef as {current:(()=>string)|null};
 flush.current=()=>{draft.change('The final keystroke');return draft.text};
 await leaveInline();
 expect(api.saveContent).toHaveBeenCalledWith('vault','a','The final keystroke','1');
 expect(first().querySelector('.mindmap-node-edit')).toBeNull();expect(document.activeElement).toBe(outside());
});
it('keeps editing through keyboard resizing, then finishes when focus leaves the grip',async()=>{
 await mount();await key(first(),'F2');await focusInline();
 const grip=action('Resize First') as HTMLElement;
 await act(async()=>{grip.focus();await new Promise(r=>setTimeout(r,10))});
 await key(grip,'ArrowDown');
 expect(first().querySelector('.mindmap-node-edit')).not.toBeNull();expect(first().getAttribute('style')).toContain('height: 128px');
 await act(async()=>{outside().focus();await new Promise(r=>setTimeout(r,10))});
 expect(first().querySelector('.mindmap-node-edit')).toBeNull();expect(document.activeElement).toBe(outside());
});
it('keeps editing and focus during pointer resizing from current dimensions',async()=>{
 await mount();await key(first(),'F2');await focusInline();
 const capture=HTMLElement.prototype.setPointerCapture;HTMLElement.prototype.setPointerCapture=vi.fn();
 try{const surface=host.querySelector('.mindmap-surface')!;
 await pointer(action('Resize First'),'pointerdown',100,100);await pointer(surface,'pointermove',164,148);await pointer(surface,'pointerup',164,148);
 expect(first().getAttribute('style')).toContain('width: 256px; height: 160px');
 expect(first().querySelector('.mindmap-node-edit')).not.toBeNull();expect(document.activeElement).toBe(first().querySelector('textarea'));
 }finally{HTMLElement.prototype.setPointerCapture=capture}
});
it('keeps the editor open when focus returns during a delayed blur save',async()=>{
 let release!:(r:{revision:string;committedAt:string})=>void;
 vi.mocked(api.saveContent).mockImplementation(()=>new Promise(resolve=>{release=resolve}));
 await mount();await key(first(),'F2');await focusInline();
 await act(async()=>cache.get('vault/a')!.change('Still editing this idea'));
 await leaveInline();expect(api.saveContent).toHaveBeenCalled();
 await focusInline();await act(async()=>release({revision:'2',committedAt:''}));
 expect(first().querySelector('.mindmap-node-edit')).not.toBeNull();
 await leaveInline();expect(first().querySelector('.mindmap-node-edit')).toBeNull();
});
it('keeps its link picker portal in the edit session and finishes when leaving the portal',async()=>{
 await mount();await key(first(),'F2');await focusInline();
 const portal=document.createElement('div');portal.className='document-link-picker';const input=document.createElement('input');portal.append(input);document.body.append(portal);
 try{await act(async()=>{input.focus();await new Promise(r=>setTimeout(r,10))});
 expect(first().querySelector('.mindmap-node-edit')).not.toBeNull();
 await act(async()=>{outside().focus();await new Promise(r=>setTimeout(r,10))});
 expect(first().querySelector('.mindmap-node-edit')).toBeNull();
 }finally{portal.remove()}
});


it('single clicks select without opening or switching preview; double-click and Space preview explicitly',async()=>{
 await mount();await click(first());expect(first().classList.contains('selected')).toBe(true);
 expect(host.querySelector('.mindmap-preview')).toBeNull();expect(cache.size).toBe(0);
 await dblclick(first());expect(host.querySelector('.mindmap-preview-heading strong')?.textContent).toBe('First');
 const second=host.querySelector('[data-node-id="b"]')!;
 await click(second);expect(second.classList.contains('selected')).toBe(true);
 expect(host.querySelector('.mindmap-preview-heading strong')?.textContent).toBe('First');expect(cache.has('vault/b')).toBe(false);
 await key(second,' ');expect(host.querySelector('.mindmap-preview-heading strong')?.textContent).toBe('Second');
 expect(open).not.toHaveBeenCalled();expect(api.updateMindMapConfig).not.toHaveBeenCalled();
});
it('preview open, switch, resize and close preserve zoom, revealing only an obscured node with an idempotent pan',async()=>{
 const original=globalThis.ResizeObserver;const observers:{callback:()=>void;target?:Element}[]=[];
 class Observer{entry:{callback:()=>void;target?:Element};constructor(callback:()=>void){this.entry={callback};observers.push(this.entry)}observe(target:Element){this.entry.target=target}unobserve(){}disconnect(){}}
 vi.stubGlobal('ResizeObserver',Observer);let width=800;
 const rect=(left:number,top:number,w:number,h:number)=>({left,top,right:left+w,bottom:top+h,width:w,height:h,x:left,y:top,toJSON:()=>({})} as DOMRect);
 const bounds=vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockImplementation(function(this:HTMLElement){
   if(this.classList.contains('mindmap-surface'))return rect(0,0,width,600);
   if(this.classList.contains('mindmap-preview'))return rect(width,0,800-width,600);
   if(this.classList.contains('mindmap-content'))return rect(0,0,800,600);
   return rect(0,0,0,0);
 });
 const notify=()=>act(async()=>observers.filter(o=>o.target?.classList.contains('mindmap-surface')).forEach(o=>o.callback()));
 try{
   await mount();await click(action('Zoom in'));const initial=worldStyle();expect(initial).toContain('scale(1.2)');
   width=500;await dblclick(first());await notify();expect(worldStyle()).toBe(initial); // First already fits.
   await dblclick(host.querySelector('[data-node-id="b"]')!);const revealed=worldStyle();
   expect(revealed).not.toBe(initial);expect(revealed).toContain('scale(1.2)');
   await notify();expect(worldStyle()).toBe(revealed); // repeated callback cannot add the correction twice
   width=320;await key(action('Resize node preview'),'End');await notify();
   const resized=worldStyle();expect(resized).not.toBe(revealed);expect(resized).toContain('scale(1.2)');
   width=800;await click(action('Close node preview'));await notify();expect(worldStyle()).toBe(resized);
   expect(api.updateMindMapConfig).not.toHaveBeenCalled();expect(api.saveContent).not.toHaveBeenCalled();
 }finally{bounds.mockRestore();vi.stubGlobal('ResizeObserver',original)}
});
