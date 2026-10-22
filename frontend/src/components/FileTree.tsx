import {useEffect,useMemo,useRef,useState,type ReactNode} from 'react';
import {createPortal} from 'react-dom';
import type {DocMeta} from '../api/types';
import {overlayPoint,uiZoom} from '../lib/overlayPosition';
import {Icon} from './Icon';
import {indexFileTree,flattenFileTree,fileTreeGuides,type TreeRow as Row} from '../lib/fileTreeRows';

export type Selection={docs:string[];folders:string[]};
// A drag carries notes, folders and attachments; every drop parses this through
// parseDragPayload so a foreign or malformed payload can never reach a mutation.
export type DragPayload={docs:string[];folders:string[];atts:string[]};
export const DRAG_KEY='application/netsidian-move';
export const parseDragPayload=(raw:string|null):DragPayload|null=>{
 if(!raw||raw.length>65536)return null;let value:unknown;try{value=JSON.parse(raw)}catch{return null}
 if(!value||typeof value!=='object'||Array.isArray(value))return null;
 const p=value as Partial<Record<keyof DragPayload,unknown>>;
 const lists:Record<keyof DragPayload,string[]>={docs:[],folders:[],atts:[]};
 for(const key of ['docs','folders','atts'] as const){
  const list=p[key];if(list===undefined)continue;
  if(!Array.isArray(list)||list.length>256||list.some(s=>typeof s!=='string'||!s||s.length>1024))return null;
  lists[key]=[...new Set(list)] as string[];
 }
 return lists.docs.length+lists.folders.length+lists.atts.length?lists:null;
};
const noAttachments:{path:string}[]=[];

const parentOf=(p:string)=>p.includes('/')?p.slice(0,p.lastIndexOf('/')):'';
export const PREVIEWABLE=/\.(png|jpe?g|gif|webp|avif|ico|bmp|svg|tiff?|pdf|mp4|webm|ogv|mov|m4v|mp3|wav|ogg|m4a|flac|aac)$/i;

export function FileTree({onCreateNote,onCreateFolder,onCreateBoard,onCreateMap,onCreateMemo,canCreate=false,docs,folders,selected,selection,onSelect,onPreview,onOpen,onFolder,onMenu,onMenuFolder,onMoveDocs,onMoveFolder,initialCollapsed,resetKey,onCollapse,sortBy,attachments,onOpenAttachment,onMenuAttachment,onMoveAtts,revealTick,collapseTick,filterQuery,onRename,onSelMenu,onBlank,canImport=false,onImportFiles,boardPaths=[],onOpenBoard,mindMapPaths=[],onOpenMindMap,emptyState}:{emptyState?:ReactNode;onCreateNote?:(folder:string)=>void;onCreateFolder?:(folder:string)=>void;onCreateBoard?:(folder:string)=>void;onCreateMap?:(folder:string)=>void;onCreateMemo?:(folder:string)=>void;canCreate?:boolean;docs:DocMeta[];folders:string[];selected:string;selection:Selection;onSelect:(s:Selection)=>void;onPreview:(id:string)=>void;onOpen:(id:string)=>void;onFolder:(path:string)=>void;onMenu:(d:DocMeta,x:number,y:number)=>void;onMenuFolder:(path:string,x:number,y:number)=>void;onMoveDocs:(ids:string[],folder:string)=>void;onMoveFolder:(from:string,to:string)=>void;initialCollapsed?:string[];resetKey?:string;onCollapse?:(collapsed:string[])=>void;sortBy?:'name'|'mtime';attachments?:{path:string}[];onOpenAttachment?:(path:string)=>void;onMenuAttachment?:(path:string,x:number,y:number)=>void;onMoveAtts?:(paths:string[],folder:string)=>void;revealTick?:number;collapseTick?:number;filterQuery?:string;onRename?:(doc:DocMeta|undefined,folder?:string)=>void;onSelMenu?:(x:number,y:number)=>void;onBlank?:()=>void;mindMapPaths?:string[];onOpenMindMap?:(path:string)=>void;boardPaths?:string[];onOpenBoard?:(path:string)=>void;canImport?:boolean;onImportFiles?:(files:FileList|File[],folder:string)=>void}){
 const [createMenu,setCreateMenu]=useState<{path:string;left:number;top:number;owner:HTMLElement}|null>(null);const createHost=useRef<HTMLDivElement>(null);
 useEffect(()=>{if(!createMenu)return;const outside=(e:PointerEvent)=>{if(!createHost.current?.contains(e.target as Node)&&!createMenu.owner.contains(e.target as Node))setCreateMenu(null)};const escape=(e:KeyboardEvent)=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();setCreateMenu(null);createMenu.owner.focus()}};document.addEventListener('pointerdown',outside);document.addEventListener('keydown',escape,true);return()=>{document.removeEventListener('pointerdown',outside);document.removeEventListener('keydown',escape,true)}},[createMenu]);
  const createInFolder=(kind:'note'|'folder'|'board'|'map'|'memo')=>{if(!createMenu||!canCreate)return;const path=createMenu.path;setCreateMenu(null);setCollapsed(old=>{const next=new Set(old);const parts=path.split('/');for(let i=1;i<=parts.length;i++)next.delete(parts.slice(0,i).join('/'));emit(next);return next});if(kind==='note')onCreateNote?.(path);else if(kind==='folder')onCreateFolder?.(path);else if(kind==='board')onCreateBoard?.(path);else if(kind==='map')onCreateMap?.(path);else onCreateMemo?.(path)};
  // Board/map configuration files identify their folder's specialized UI; the
  // tree shows the same recognizable icon and quick-open affordance for both
  // the folder and its configuration document. Matching is exact and
  // case-sensitive so it agrees with the Workspace's own
  // `path === folder + '/board.md'` / `'/mindmap.md'` checks.
  const isBoardConfig=(path:string)=>path.endsWith('/board.md')&&boardPaths.includes(parentOf(path));
  const isMapConfig=(path:string)=>path.endsWith('/mindmap.md')&&mindMapPaths.includes(parentOf(path));
  // Rows whose double click opens a board/map also get a single-click,
  // keyboard-focusable open button next to the ellipsis.
  const openTarget=(row:Row):{kind:'board'|'map';path:string}|null=>{
   if(row.kind==='folder'){if(mindMapPaths.includes(row.path))return{kind:'map',path:row.path};if(boardPaths.includes(row.path))return{kind:'board',path:row.path};return null}
   if(row.kind==='doc'){if(isMapConfig(row.doc.path))return{kind:'map',path:parentOf(row.doc.path)};if(isBoardConfig(row.doc.path))return{kind:'board',path:parentOf(row.doc.path)}}
   return null;
  };
  const rowOpenFor=(i:number,row:Row)=>{const target=openTarget(row);if(!target||(target.kind==='board'?!onOpenBoard:!onOpenMindMap))return null;
   return <button type="button" className="row-open" tabIndex={focus===i?0:-1} aria-label={(target.kind==='board'?'Open board ':'Open map ')+row.name} aria-description={target.path} title={target.kind==='board'?'Open board':'Open map'} draggable={false} onPointerDown={e=>e.stopPropagation()} onKeyDown={e=>e.stopPropagation()} onDoubleClick={e=>e.stopPropagation()} onClick={e=>{e.stopPropagation();if(target.kind==='board')onOpenBoard?.(target.path);else onOpenMindMap?.(target.path)}}><Icon name={target.kind==='board'?'group':'mindmap'} size={13}/></button>;};
 const [collapsed,setCollapsed]=useState<Set<string>>(()=>new Set(initialCollapsed||[]));
 const [dropPath,setDropPath]=useState<string|null>(null);
 const [dragKey,setDragKey]=useState<string|null>(null);
 const dragChip=useRef<HTMLDivElement|null>(null);
 const [focus,setFocus]=useState(-1);
 const anchor=useRef(-1);
 const navRef=useRef<HTMLElement>(null);
 const focusRequest=useRef({index:-1,pending:false});
 const [view,setView]=useState({top:0,height:600,rowHeight:innerWidth<=700?30:22});
 useEffect(()=>{const nav=navRef.current;if(!nav)return;const measure=()=>setView({top:nav.scrollTop,height:nav.clientHeight,rowHeight:matchMedia('(max-width:700px)').matches?30:22});const observer=new ResizeObserver(measure);observer.observe(nav);measure();return()=>observer.disconnect()},[]);
 const hoverExpand=useRef<{t:ReturnType<typeof setTimeout>;path:string}|null>(null);

 // Per-vault workspace restoration: re-seed collapsed folders when the
 // active vault changes so one vault's tree expansion never paints over
 // another's. Guarded on resetKey (space id) only.
 useEffect(()=>{setCollapsed(new Set(initialCollapsed||[]))/* seed */ // eslint-disable-line react-hooks/exhaustive-deps
  // eslint-disable-next-line react-hooks/exhaustive-deps
 },[resetKey]);
 const emit=(next:Set<string>)=>{onCollapse?.([...next])};

 const treeIndex=useMemo(()=>indexFileTree(docs,folders,attachments||noAttachments,sortBy),[docs,folders,attachments,sortBy]);
 const rows=useMemo(()=>flattenFileTree(treeIndex,collapsed,!!filterQuery),[treeIndex,collapsed,filterQuery]);
 const guideRows=useMemo(()=>fileTreeGuides(rows),[rows]);
 const guidesFor=(i:number)=><span className="tree-guides" aria-hidden="true">{guideRows[i].guides.map(g=><span key={g.level} className={'tree-guide'+(g.end?' tree-guide-end':'')} style={{left:14+g.level*18}}/>)}{guideRows[i].stem&&<span className="tree-guide tree-guide-stem" style={{left:14+rows[i].depth*18}}/>}</span>;
 const virtual=rows.length>250,visibleCount=Math.ceil(view.height/view.rowHeight)+16;
 const start=virtual?Math.max(0,Math.min(rows.length-visibleCount,Math.floor(view.top/view.rowHeight)-8)):0;
 const end=virtual?Math.min(rows.length,start+visibleCount):rows.length;
 const scrollToRow=(index:number)=>{const nav=navRef.current;if(!nav||!virtual)return;const top=index*view.rowHeight;if(top<nav.scrollTop)nav.scrollTop=top;else if(top+view.rowHeight>nav.scrollTop+nav.clientHeight)nav.scrollTop=top+view.rowHeight-nav.clientHeight;setView(old=>({...old,top:nav.scrollTop}))};
 const rowsRef=useRef(rows);rowsRef.current=rows;

 // Reveal active document: expand its ancestors, then focus and scroll it once the expanded rows exist.
 useEffect(()=>{if(!revealTick||!selected)return;const key='d:'+selected;const doc=docs.find(d=>d.id===selected);
  if(doc){const parts=doc.path.split('/');const anc:string[]=[];for(let i=1;i<parts.length;i++)anc.push(parts.slice(0,i).join('/'));
   if(anc.some(p=>collapsed.has(p)))setCollapsed(old=>{const next=new Set(old);anc.forEach(p=>next.delete(p));emit(next);return next})}
  const settle=(tries:number)=>{const i=rowsRef.current.findIndex(r=>r.key===key);if(i>=0){setFocus(i);anchor.current=i;requestAnimationFrame(()=>{scrollToRow(i);const el=navRef.current?.querySelector(`[data-key="${key}"]`);el?.scrollIntoView({block:'nearest'})});return}if(tries>0)setTimeout(()=>settle(tries-1),50)};
  requestAnimationFrame(()=>setTimeout(()=>settle(8),40));
  // eslint-disable-next-line react-hooks/exhaustive-deps
 },[revealTick]);
 // Re-anchor keyboard focus only when the tree itself (or nothing) holds it; rows refresh on every
 // autosave, so blindly focusing here would steal the caret from the editor while typing (issue #8).
 useEffect(()=>{const changed=focusRequest.current.index!==focus;focusRequest.current.index=focus;
  if(focus<0||(!changed&&!focusRequest.current.pending))return;const a=document.activeElement;if(a&&a!==document.body&&!navRef.current?.contains(a))return;const r=rows[focus];if(!r)return;
  const el=navRef.current?.querySelector(`[data-key="${r.key}"]`)as HTMLElement|null;
  if(el){focusRequest.current.pending=false;if(a!==el)el.focus({preventScroll:true})}else{focusRequest.current.pending=true;scrollToRow(focus)}
 },[focus,rows,start,end]);
 // Collapse all folders currently visible in the tree.
 useEffect(()=>{if(!collapseTick)return;const all=new Set<string>();
  treeIndex.folders.forEach(path=>all.add(path));setCollapsed(all);emit(all);
  // eslint-disable-next-line react-hooks/exhaustive-deps
 },[collapseTick]);

 const keysOf=(s:Selection)=>new Set([...s.docs.map(id=>'d:'+id),...s.folders.map(p=>'f:'+p)]);
 const selectionFrom=(keys:Set<string>):Selection=>({docs:rows.filter(r=>keys.has(r.key)&&r.kind==='doc').map(r=>(r as Extract<Row,{kind:'doc'}>).doc.id),folders:rows.filter(r=>keys.has(r.key)&&r.kind==='folder').map(r=>(r as Extract<Row,{kind:'folder'}>).path)});
 const selectRange=(a:number,b:number)=>{const [lo,hi]=a<b?[a,b]:[b,a];const keys=new Set(rows.slice(lo,hi+1).map(r=>r.key));onSelect(selectionFrom(keys))};
 const toggleRow=(i:number)=>{const keys=keysOf(selection);const k=rows[i].key;if(keys.has(k))keys.delete(k);else keys.add(k);onSelect(selectionFrom(keys))};
  // Every row exposes its own actions through an ellipsis that appears on hover or keyboard
  // focus — no selection required. When several items are selected and the row is part of that
  // selection, the ellipsis opens the shared selection menu instead of a single-item menu.
  function openRowMenu(i:number,e:React.MouseEvent){e.stopPropagation();e.preventDefault();const r=e.currentTarget.getBoundingClientRect();const row=rows[i];
   const totalSel=selection.docs.length+selection.folders.length;
   const isSel=row.kind==='doc'?selection.docs.includes(row.doc.id):row.kind==='folder'?selection.folders.includes(row.path):false;
    if(totalSel>1&&isSel){onSelMenu?.(r.right,r.bottom);return}
    if(row.kind==='doc')onMenu(row.doc,r.right,r.bottom);else if(row.kind==='folder')onMenuFolder(row.path,r.right,r.bottom);else if(row.kind==='att')onMenuAttachment?.(row.path,r.right,r.bottom)}
  const rowMenuFor=(i:number,row:Row)=>(<button className="row-menu" tabIndex={focus===i?0:-1} aria-label={'Actions for '+row.name} aria-haspopup="menu" title="Actions" draggable={false} onPointerDown={e=>e.stopPropagation()} onKeyDown={e=>e.stopPropagation()} onClick={e=>openRowMenu(i,e)}><Icon name="more" size={14}/></button>);

 function clickRow(i:number,e:React.MouseEvent){const row=rows[i];setFocus(i);
  if(row.kind==='att'){onSelect({docs:[],folders:[]});onOpenAttachment?.(row.path);return}
  if(e.shiftKey&&anchor.current>=0){selectRange(anchor.current,i);return}
  anchor.current=i;
  if(e.ctrlKey||e.metaKey){toggleRow(i);return}
  onSelect(row.kind==='doc'?{docs:[row.doc.id],folders:[]}:row.kind==='folder'?{docs:[],folders:[row.path]}:{docs:[],folders:[]});
  if(row.kind==='doc')onPreview(row.doc.id);
  else if(row.kind==='folder'){onFolder(row.path);if(treeIndex.branches.has(row.path))toggle(row.path)}
 }
 function toggle(path:string){setCollapsed(old=>{const next=new Set(old);if(next.has(path))next.delete(path);else next.add(path);emit(next);return next})}
 function keyNav(e:React.KeyboardEvent){if(!rows.length)return;
  const move=(i:number,extend:boolean)=>{i=Math.max(0,Math.min(rows.length-1,i));setFocus(i);if(anchor.current<0)anchor.current=i;extend?selectRange(anchor.current,i):onSelect(selectionFrom(new Set([rows[i].key])))};
  const row=rows[Math.max(focus,0)];
  switch(e.key){
   case 'ArrowDown':e.preventDefault();move(focus+1,e.shiftKey);break;
   case 'ArrowUp':e.preventDefault();move(focus<0?0:focus-1,e.shiftKey);break;
   case 'Home':e.preventDefault();move(0,false);break;
   case 'End':e.preventDefault();move(rows.length-1,false);break;
   case 'ArrowRight':e.preventDefault();if(row?.kind==='folder'&&treeIndex.branches.has(row.path)&&collapsed.has(row.path)){toggle(row.path)}else if(row?.kind==='folder'&&rows[focus+1]?.depth>row.depth)move(focus+1,false);break;
   case 'ArrowLeft':e.preventDefault();if(row&&row.kind==='folder'&&treeIndex.branches.has(row.path)&&!collapsed.has(row.path)){toggle(row.path)}else{const cur=rows[focus];const cp=cur?(cur.kind==='folder'?cur.path:cur.kind==='doc'?cur.doc.path:cur.path):'';const parent=cp.includes('/')?cp.slice(0,cp.lastIndexOf('/')):'';const pi=rows.findIndex(r=>r.kind==='folder'&&r.path===parent);if(pi>=0)move(pi,false)}break;
   case 'Enter':e.preventDefault();if(row?.kind==='doc'){onOpen(row.doc.id)}else if(row?.kind==='att'){onOpenAttachment?.(row.path)}else if(row?.kind==='folder'){if(mindMapPaths.includes(row.path))onOpenMindMap?.(row.path);else if(boardPaths.includes(row.path))onOpenBoard?.(row.path);else{onFolder(row.path);if(treeIndex.branches.has(row.path))toggle(row.path)}}break;
   case 'F2':e.preventDefault();if(row?.kind==='doc')onRename?.(row.doc);else if(row?.kind==='folder')onRename?.(undefined,row.path);break;
    case 'ContextMenu':case 'F10':{e.preventDefault();const r=rows[Math.max(focus,0)];if(!r)break;const el=navRef.current?.querySelector(`[data-key="${r.key}"]`)as HTMLElement|null;const x=(el?.getBoundingClientRect().left||100)+24,y=(el?.getBoundingClientRect().top||100)+20;if(r.kind==='doc')onMenu(r.doc,x,y);else if(r.kind==='folder')onMenuFolder(r.path,x,y);else if(r.kind==='att')onMenuAttachment?.(r.path,x,y);break}
   case ' ':e.preventDefault();if(focus>=0)toggleRow(focus);break;
   case 'Escape':onSelect({docs:[],folders:[]});break;
  }
 }

  function dragStart(i:number,e:React.DragEvent){const row=rows[i];const inSel=keysOf(selection).has(row.key);
  const payload:DragPayload=inSel?{docs:selection.docs,folders:selection.folders,atts:[]}:row.kind==='doc'?{docs:[row.doc.id],folders:[],atts:[]}:row.kind==='att'?{docs:[],folders:[],atts:[row.path]}:{docs:[],folders:[row.path],atts:[]};
  e.dataTransfer.setData(DRAG_KEY,JSON.stringify(payload));
  if(payload.docs.length===1&&!payload.folders.length&&!payload.atts.length)e.dataTransfer.setData('application/netsidian-document',payload.docs[0]);
  // File/folder targets move; canvas targets link without moving the source.
  e.dataTransfer.effectAllowed='copyMove';
  // Cursor-anchored chip: the default ghost snapshots the whole full-width row, which
  // parks the label far from the pointer. A compact pill put under the cursor reads far better.
  setDragKey(row.key);
  const chip=document.createElement('div');chip.className='drag-chip';
  const count=payload.docs.length+payload.folders.length+payload.atts.length;
  chip.textContent=count>1?`${count} items`:row.name;
  document.body.appendChild(chip);dragChip.current=chip;
  e.dataTransfer.setDragImage(chip,10,14);
  setTimeout(()=>{if(dragChip.current===chip){chip.remove();if(dragChip.current===chip)dragChip.current=null}},0);
 }
 function dragEnd(){clearHover();setDropPath(null);setDragKey(null);if(dragChip.current){dragChip.current.remove();dragChip.current=null}}
 function clearHover(){if(hoverExpand.current){clearTimeout(hoverExpand.current.t);hoverExpand.current=null}}
 function scheduleExpand(path:string){if(!path||!collapsed.has(path))return;if(hoverExpand.current?.path===path)return;clearHover();const t=setTimeout(()=>{hoverExpand.current=null;toggle(path)},800);hoverExpand.current={t,path}}
 // Rows claim the drag first (folder, or a file's parent folder); nav accepts only what
 // no row took — otherwise bubbling made every drop land in the root. The highlight is
 // sticky during the drag and clears only on drop/dragend: per-row dragleave handlers made
 // child nodes (icon, span) re-fire enter/leave ~60Hz and the drop target blinked.
  function accept(e:React.DragEvent,path:string,allowFiles:boolean){if(e.defaultPrevented)return;const t=[...e.dataTransfer.types];
   if(t.includes(DRAG_KEY)&&canCreate){e.preventDefault();e.dataTransfer.dropEffect='move';if(dropPath!==path)setDropPath(path);if(path)scheduleExpand(path);return}
   if(allowFiles&&canImport&&t.includes('Files')){e.preventDefault();e.dataTransfer.dropEffect='copy';if(dropPath!==path)setDropPath(path);if(path)scheduleExpand(path)}}
   function drop(e:React.DragEvent,path:string,allowFiles:boolean){const payload=parseDragPayload(e.dataTransfer.getData(DRAG_KEY));
    if(payload){e.preventDefault();e.stopPropagation();clearHover();setDropPath(null);
     if(payload.docs.length)onMoveDocs(payload.docs,path);
     if(payload.folders.length)for(const f of payload.folders)if(f!==path&&!path.startsWith(f+'/'))onMoveFolder(f,path);
     if(payload.atts.length)onMoveAtts?.(payload.atts,path);return}
    if(allowFiles&&canImport&&e.dataTransfer.files&&e.dataTransfer.files.length){e.preventDefault();e.stopPropagation();clearHover();setDropPath(null);onImportFiles?.(e.dataTransfer.files,path)}
   }

  return <nav ref={navRef} role="tree" aria-multiselectable="true" className={'file-tree'+(!rows.length?' file-tree-empty':'')+(virtual?' virtual':'')+(dropPath===''?' dropping':'')+(dragKey?' dragging-tree':'')} style={{'--tree-row-height':view.rowHeight+'px'} as React.CSSProperties} data-total-rows={rows.length} tabIndex={0} aria-label="Documents and folders" onScroll={e=>{const top=e.currentTarget.scrollTop;setView(old=>({...old,top}))}} onKeyDown={keyNav} onClick={e=>{if(e.target===navRef.current){onSelect({docs:[],folders:[]});onBlank?.()}}} onDragOver={e=>accept(e,'',false)} onDrop={e=>{if(e.dataTransfer.getData(DRAG_KEY))drop(e,'',false)}} onPaste={e=>{if(!canImport)return;const files=e.clipboardData?.files;if(files&&files.length){e.preventDefault();const row=focus>=0?rows[focus]:undefined;const target=row?(row.kind==='folder'?row.path:row.kind==='doc'?parentOf(row.doc.path):row.kind==='att'?parentOf(row.path):''):'';onImportFiles?.(files,target)}}}>
   {!rows.length&&emptyState}
   {virtual&&<div aria-hidden="true" className="tree-spacer" style={{height:start*view.rowHeight}}/>}
   {rows.slice(start,end).map((row,offset)=>{const i=start+offset;return row.kind==='folder'?
     <div key={row.key} data-key={row.key} role="treeitem" tabIndex={-1} aria-level={row.depth+1} aria-expanded={treeIndex.branches.has(row.path)?!!filterQuery||!collapsed.has(row.path):undefined} aria-selected={selection.folders.includes(row.path)} className={['file-row','folder-row',(boardPaths.includes(row.path)||mindMapPaths.includes(row.path))&&'board-folder',openTarget(row)&&'has-open',selection.folders.includes(row.path)&&'selected',focus===i&&'focused',dragKey===row.key&&'dragging',dropPath===row.path&&'drop-target'].filter(Boolean).join(' ')} style={{paddingLeft:8+row.depth*18,'--tree-depth':row.depth} as React.CSSProperties} onClick={e=>clickRow(i,e)} onDoubleClick={()=>{if(mindMapPaths.includes(row.path))onOpenMindMap?.(row.path);else if(boardPaths.includes(row.path))onOpenBoard?.(row.path)}} onKeyDown={e=>{if(e.ctrlKey||e.metaKey)e.stopPropagation()}} onContextMenu={e=>{e.preventDefault();setFocus(i);onMenuFolder(row.path,e.clientX,e.clientY)}} draggable={canCreate} onDragStart={e=>dragStart(i,e)} onDragEnd={dragEnd} onDragOver={e=>accept(e,row.path,true)} onDrop={e=>drop(e,row.path,true)} title={row.path}>
    {guidesFor(i)}<Icon name="chevron" size={12} style={{visibility:treeIndex.branches.has(row.path)?undefined:'hidden',transform:collapsed.has(row.path)&&!filterQuery?'':'rotate(90deg)'}}/><span className="folder-icon"><Icon name={mindMapPaths.includes(row.path)?'mindmap':boardPaths.includes(row.path)||row.path==='Tasks'?'group':'folder'} size={14}/></span><span className="file-name">{row.name}</span>{rowOpenFor(i,row)}{rowMenuFor(i,row)}{canCreate&&<button type="button" className="folder-create" tabIndex={focus===i?0:-1} aria-label={'Create in '+row.path} aria-haspopup="menu" aria-expanded={createMenu?.path===row.path} title={'Add a document, folder, board, map or memo in '+row.path} draggable={false} onPointerDown={e=>e.stopPropagation()} onKeyDown={e=>e.stopPropagation()} onDoubleClick={e=>e.stopPropagation()} onClick={e=>{e.stopPropagation();const rect=e.currentTarget.getBoundingClientRect();setCreateMenu(createMenu?.path===row.path?null:{path:row.path,left:rect.right-208*uiZoom(),top:rect.bottom+4,owner:e.currentTarget})}}><Icon name="plus" size={14}/></button>}</div>
  :row.kind==='doc'?
     <div key={row.key} data-key={row.key} role="treeitem" tabIndex={-1} aria-level={row.depth+1} aria-selected={selection.docs.includes(row.doc.id)} className={['file-row','doc-row',openTarget(row)&&'has-open',selected===row.doc.id&&'current',selection.docs.includes(row.doc.id)&&'selected',focus===i&&'focused',dragKey===row.key&&'dragging'].filter(Boolean).join(' ')} style={{paddingLeft:25+row.depth*18,'--tree-depth':row.depth} as React.CSSProperties} onClick={e=>clickRow(i,e)} onDoubleClick={()=>onOpen(row.doc.id)} onKeyDown={e=>{if(e.ctrlKey||e.metaKey)e.stopPropagation()}} onContextMenu={e=>{e.preventDefault();setFocus(i);if(!selection.docs.includes(row.doc.id))onSelect({docs:[row.doc.id],folders:[]});onMenu(row.doc,e.clientX,e.clientY)}} draggable={canCreate} onDragStart={e=>dragStart(i,e)} onDragEnd={dragEnd} onDragOver={e=>accept(e,parentOf(row.doc.path),true)} onDrop={e=>drop(e,parentOf(row.doc.path),true)} title={row.doc.path}>
    {guidesFor(i)}<Icon name={row.doc.tags?.includes('memo')?'text':isBoardConfig(row.doc.path)?'group':isMapConfig(row.doc.path)?'mindmap':'file'} size={14}/><span className="file-name">{row.name}</span>{row.doc.tags?.includes('memo')&&<small className="tree-memo-badge">memo</small>}{rowOpenFor(i,row)}{rowMenuFor(i,row)}</div>
  :<div key={row.key} data-key={row.key} role="treeitem" tabIndex={-1} aria-level={row.depth+1} className={['file-row','attachment-row',focus===i&&'focused',dragKey===row.key&&'dragging'].filter(Boolean).join(' ')} style={{paddingLeft:25+row.depth*18,'--tree-depth':row.depth} as React.CSSProperties} onClick={e=>clickRow(i,e)} onKeyDown={e=>{if(e.ctrlKey||e.metaKey)e.stopPropagation()}} onContextMenu={e=>{e.preventDefault();setFocus(i);onMenuAttachment?.(row.path,e.clientX,e.clientY)}} draggable={canCreate} onDragStart={e=>dragStart(i,e)} onDragEnd={dragEnd} onDragOver={e=>accept(e,parentOf(row.path),true)} onDrop={e=>drop(e,parentOf(row.path),true)} title={row.path+(PREVIEWABLE.test(row.path)?' (attachment — click to preview)':' (attachment — opens in a new tab)')}>
    {guidesFor(i)}<Icon name={/\.(png|jpe?g|gif|webp|svg)$/i.test(row.path)?'image':'file'} size={14}/><span className="file-name">{row.name}</span>{rowMenuFor(i,row)}</div>})}
   {virtual&&<div aria-hidden="true" className="tree-spacer" style={{height:(rows.length-end)*view.rowHeight}}/>}
   {createMenu&&createPortal(<div className="folder-create-menu" role="menu" aria-label={'Create in '+createMenu.path} ref={createHost} style={overlayPoint(createMenu.left,createMenu.top,208,240)} onClick={e=>e.stopPropagation()} onKeyDown={e=>{e.stopPropagation();if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();e.stopPropagation();const buttons=[...e.currentTarget.querySelectorAll<HTMLButtonElement>('button')],index=buttons.indexOf(document.activeElement as HTMLButtonElement);buttons[(index+(e.key==='ArrowDown'?1:buttons.length-1))%buttons.length]?.focus()}}}><small>{createMenu.path}</small><button type="button" role="menuitem" autoFocus onClick={()=>createInFolder('note')}><Icon name="fileplus" size={16}/>New document</button><button type="button" role="menuitem" onClick={()=>createInFolder('folder')}><Icon name="folderplus" size={16}/>New folder</button>{onCreateBoard&&<button type="button" role="menuitem" onClick={()=>createInFolder('board')}><Icon name="group" size={16}/>New board</button>}{onCreateMap&&<button type="button" role="menuitem" onClick={()=>createInFolder('map')}><Icon name="mindmap" size={16}/>New map</button>}{onCreateMemo&&<button type="button" role="menuitem" onClick={()=>createInFolder('memo')}><Icon name="text" size={16}/>New memo</button>}</div>,document.body)}
  </nav>;
}
