// BoardView: the Kanban board tab. Columns are folders, cards are Markdown
// documents. Drag, keyboard and the card status menu all funnel through the
// board API (move + reorder with If-Match); folder mechanics stay hidden from
// the normal UX, while each card keeps its vault path visible for orientation.

import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import * as api from '../api/client';
import type { BoardCard, BoardColumnView } from '../api/client';
import type { DocMeta, NoteContent } from '../api/types';
import type {Draft, DraftDocument} from '../lib/drafts';
import { Dialog } from './Dialog';
import { Icon, Tool } from './Icon';
import {NoteEditor} from './NoteEditor';
import {DRAG_KEY,parseDragPayload} from './FileTree';
import {
  BOARD_DRAG_KEY, type BoardColumnDraft, type BoardDragPayload,
  columnSlug, insertOrdered, parseBoardDrag,
  populatedColumnRemovalErrors, randomIdemKey, swapOrdered, validateBoardColumns,
} from '../lib/boards';

const errMsg = (e: unknown) => e instanceof Error ? e.message : 'Something went wrong.';
const VisualEditor=lazy(()=>import('./VisualEditor').then(m=>({default:m.VisualEditor})));
const clamp=(n:number,min:number,max:number)=>Math.max(min,Math.min(max,n));
function storedSize(key:string,fallback:number,min:number,max:number){try{const raw=localStorage.getItem(key),n=raw===null?fallback:Number(raw);return Number.isFinite(n)?clamp(n,min,max):fallback}catch{return fallback}}
function exportDraft(path:string,text:string){const url=URL.createObjectURL(new Blob([text],{type:'text/markdown;charset=utf-8'})),link=document.createElement('a');link.href=url;link.download=path.split('/').pop()||'task.md';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}

export function BoardView({ space, boardPath, writable, docs, attachments = [], epoch, onOpenDoc, onFlushDraft, onInvalidateDoc, onChanged, onMoveToBoard, onRevealFolder, onPreviewDraft, onResolveConflict, onUploadImages, principal }: {
  space: string; boardPath: string; writable: boolean; docs: DocMeta[]; epoch: number;
  attachments?: {path: string}[];
  principal:string;
  onPreviewDraft:(id:string)=>Promise<DraftDocument>;
  onResolveConflict:(id:string)=>Promise<void>;
  onUploadImages?:(files:File[],notePath:string)=>Promise<string[]>;
  onOpenDoc: (id: string) => void;
  onFlushDraft: (docId: string) => Promise<void>;
  onInvalidateDoc: (docId: string) => void;
  onChanged: () => void;
  onMoveToBoard: (docId: string) => void;
  onRevealFolder: (path: string) => void;
}) {
  const [board, setBoard] = useState<api.Board | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
 const [operationWarnings,setOperationWarnings]=useState<string[]>([]);
 const busyRef=useRef(new Set<string>());
  const [busy, setBusy] = useState<Set<string>>(() => new Set());
  const [adding, setAdding] = useState<Set<string>>(() => new Set());
  const [titles, setTitles] = useState<Record<string, string>>({});
  const [drag, setDrag] = useState<{ cardId: string } | null>(null);
  const [dropTarget, setDropTarget] = useState<{ columnId: string; cardId?: string; before?: boolean } | null>(null);
  const [cardMenu, setCardMenu] = useState<{ card: BoardCard; columnId: string; x: number; y: number } | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [focusCard, setFocusCard] = useState('');
  const idemRef = useRef<Record<string,{signature:string;key:string}>>({});
  // Preview model mirrors the graph: a single click selects a card and shows an
  // inline preview without leaving the board; a double click (or Enter) opens.
  const [previewId, setPreviewId] = useState('');
  const [preview, setPreview] = useState<NoteContent | null>(null);
  const [previewError, setPreviewError] = useState('');
  const [previewDraft,setPreviewDraft]=useState<Draft|null>(null);
  const draftRef=useRef<Draft|null>(null),selection=useRef(0);
  const [,renderDraft]=useState(0);
  const [previewMode,setPreviewMode]=useState<'visual'|'source'>('visual');
  const content=useRef<HTMLDivElement>(null),resize=useRef<{right:number;bottom:number;width:number;height:number}|null>(null);
  const sizeKey=`netsidian:task-pane:${principal}:${space}:${boardPath}`;
  const [width,setWidth]=useState(()=>storedSize(sizeKey+':width',40,20,60)),[height,setHeight]=useState(()=>storedSize(sizeKey+':height',60,25,75));
  const [narrow,setNarrow]=useState(()=>typeof matchMedia==='function'?matchMedia('(max-width:700px)').matches:innerWidth<=700);
  const [bodyWidth,setBodyWidth]=useState(0),[bodyHeight,setBodyHeight]=useState(0);
  const minWidth=bodyWidth?Math.min(60,Math.max(20,240/bodyWidth*100)):20;
  const effectiveWidth=clamp(width,minWidth,60);
  const minHeight=bodyHeight?Math.min(75,Math.max(25,240/bodyHeight*100)):25;
  const effectiveHeight=clamp(height,minHeight,75);
  useEffect(()=>{setWidth(storedSize(sizeKey+':width',40,20,60));setHeight(storedSize(sizeKey+':height',60,25,75))},[sizeKey]);
  useEffect(()=>{const media=typeof matchMedia==='function'?matchMedia('(max-width:700px)'):null;const update=()=>setNarrow(media?media.matches:innerWidth<=700);media?.addEventListener('change',update);window.addEventListener('resize',update);return()=>{media?.removeEventListener('change',update);window.removeEventListener('resize',update)}},[]);
  useEffect(()=>{const el=content.current;if(!el)return;const update=()=>{const r=el.getBoundingClientRect();setBodyWidth(r.width);setBodyHeight(r.height)};update();if(typeof ResizeObserver==='undefined')return;const observer=new ResizeObserver(update);observer.observe(el);return()=>observer.disconnect()},[!!board]);
  useEffect(()=>()=>{selection.current++;resize.current=null},[]);
  const changeSize=(value:number)=>{const n=narrow?clamp(value,minHeight,75):clamp(value,minWidth,60);narrow?setHeight(n):setWidth(n);try{localStorage.setItem(sizeKey+(narrow?':height':':width'),String(n))}catch{}};
  const [previewRetry,setPreviewRetry]=useState(0);
  const draggedUntil=useRef(0);
  const previewCard=board?.columns.flatMap(c=>c.cards).find(c=>c.id===previewId)||board?.overflow?.find(c=>c.id===previewId);
  const flushPreview=useCallback(async()=>{const d=draftRef.current;if(!d)return true;try{await d.flush();if(d.dirty)throw new Error('Save or export this draft before closing it or selecting another task.');return true}catch(e){setPreviewError(errMsg(e));return false}},[]);
  const selectCard=async(id:string)=>{if(busyRef.current.has(id)||Date.now()<draggedUntil.current||id===previewId)return;const seq=++selection.current;if(!await flushPreview()||seq!==selection.current)return;draftRef.current=null;setPreviewDraft(null);setPreview(null);setPreviewError('');setPreviewMode('visual');setPreviewId(id)};
  const closePreview=useCallback(async()=>{const seq=++selection.current;if(!await flushPreview()||seq!==selection.current)return;const id=previewId;draftRef.current=null;setPreviewDraft(null);setPreviewId('');setPreview(null);setFocusCard(id)},[previewId,flushPreview]);
  useEffect(()=>{if(!previewId)return;const escape=(e:KeyboardEvent)=>{if(e.key!=='Escape'||e.defaultPrevented||cardMenu||settingsOpen||document.querySelector('dialog[open]')||(e.target instanceof Element&&e.target.closest('[role="dialog"],[role="menu"],[role="listbox"]')))return;const menu=content.current?.querySelector<HTMLDetailsElement>('.visual-more[open],.visual-format-more[open]');if(menu){menu.open=false;menu.querySelector<HTMLElement>('summary')?.focus();e.preventDefault();return}e.preventDefault();void closePreview()};document.addEventListener('keydown',escape);return()=>document.removeEventListener('keydown',escape)},[previewId,cardMenu,settingsOpen,closePreview]);
  useEffect(()=>previewDraft?.subscribe(()=>renderDraft(n=>n+1)),[previewDraft]);

  const reloadBoard = useCallback(() => {
    let active = true;
    api.getBoard(space, boardPath).then((b) => { if (active) { setBoard(b); setError(''); } }).catch((e) => { if (active) setError(errMsg(e)); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [space, boardPath]);
  useEffect(() => reloadBoard(), [reloadBoard, epoch]);

  useEffect(() => {
    if (!cardMenu) return;
    const close = (e: KeyboardEvent) => { if (e.key === 'Escape') setCardMenu(null); };
    document.addEventListener('keydown', close);
    return () => document.removeEventListener('keydown', close);
  }, [cardMenu]);

  useEffect(() => {
    if (!focusCard || !board) return;
    const el = document.querySelector<HTMLElement>(`[data-card-id="${CSS.escape(focusCard)}"]`);
    el?.focus();
    setFocusCard('');
  }, [board, focusCard]);

  // The parent owns this same draft when the task opens in a document tab.
  useEffect(() => {
    if (!previewId) { setPreview(null); setPreviewError(''); return; }
    let active = true;
    setPreviewError('');
    onPreviewDraft(previewId).then(({draft,note})=>{if(active){draftRef.current=draft;setPreviewDraft(draft);setPreview(note)}}).catch((e)=>{if(active)setPreviewError(errMsg(e))});
    return () => { active = false; };
  }, [space, previewId, epoch, previewRetry, previewCard?.path, previewCard?.revision,onPreviewDraft]);

  // Drop the preview if the card leaves the board (moved, trashed, reloaded).
  useEffect(() => {
    if (!board || !previewId) return;
    const present = board.columns.some((c) => c.cards.some((card) => card.id === previewId)) || (board.overflow || []).some((c) => c.id === previewId);
    if (!present) {if(draftRef.current?.dirty){setPreviewError('This task is no longer on the board. Your unsaved draft is retained; resolve or export it before closing.')}else{selection.current++;draftRef.current=null;setPreviewDraft(null);setPreviewId('');setPreview(null)}}
  }, [board, previewId]);

  const columns = board?.columns ?? [];
  const overflow = board?.overflow ?? [];
  const boardMD = useMemo(() => docs.find((d) => !d.tombstoneAt && d.path === boardPath + '/board.md'), [docs, boardPath]);

  const runBusy = async (cardId: string, fn: () => Promise<void>) => {
    if(busyRef.current.has(cardId))return;busyRef.current.add(cardId);
 setBusy((old) => new Set(old).add(cardId));
    try { await fn(); } catch (e) { setError(errMsg(e)); api.getBoard(space, boardPath).then((b) => setBoard(b)).catch(() => {}); }
    finally { busyRef.current.delete(cardId);setBusy((old) => { const next = new Set(old); next.delete(cardId); return next; }); }
  };

  const sameOrder = (a: string[], b: string[]) => a.length === b.length && a.every((id, i) => id === b[i]);

  // Cross-column move: flush any open draft, take the document's current
  // path+revision (a rename elsewhere must not stale the move), then move.
  const moveCardToColumn = async (cardId: string, docId: string, fromColumnId: string, toColumnId: string, beforeCardId: string | null = null, before = true,expectedPath?:string) => {
    if (!board || !writable || (toColumnId === fromColumnId && board.columns.find(c=>c.id===toColumnId)?.cards.some(c=>c.id===docId))) return;
    await runBusy(cardId, async () => {
      await onFlushDraft(docId);
      const fresh = await api.getDocument(space, docId);
      const displayed=board.columns.flatMap(c=>c.cards).find(c=>c.id===docId);const from=expectedPath||displayed?.path||fresh.path;
      if(fresh.path!==from)throw new Error('This task moved elsewhere. Reload the board before moving it again.');
      const moved=await api.boardMoveTask(space, boardPath, { docId, columnId: toColumnId, fromPath:from }, fresh.revision);
      setOperationWarnings(moved.warnings||[]);
      let next = await api.getBoard(space, boardPath);
      const col = next.columns.find((c) => c.id === toColumnId);
      if (col && beforeCardId) {
        const ordered = insertOrdered(col.cards.map((c) => c.id), cardId, beforeCardId, before);
        if (!sameOrder(ordered, col.cards.map((c) => c.id))) next = await api.boardReorder(space, boardPath, { columnId: toColumnId, orderedIds: ordered }, next.revision);
      }
      setBoard(next);
      onInvalidateDoc(docId);
      onChanged();
      setFocusCard(cardId);
    });
  };

  const reorderWithin = async (col: BoardColumnView, cardId: string, ordered: string[]) => {
    if (!board || !writable || sameOrder(ordered, col.cards.map((c) => c.id))) return;
    await runBusy(cardId, async () => {
      await onFlushDraft(cardId);
      setBoard(await api.boardReorder(space, boardPath, { columnId: col.id, orderedIds: ordered }, board.revision));
      onChanged();
      setFocusCard(cardId);
    });
  };

  const applyDrop = async (payload: BoardDragPayload | null, columnId: string, beforeCardId: string | null, before: boolean) => {
    setDrag(null);
    setDropTarget(null);
    if (!payload || !board || !writable) return;
    if (payload.columnId === columnId && columns.find(c=>c.id===columnId)?.cards.some(c=>c.id===payload.docId&&c.path===payload.fromPath)) {
      const col = columns.find((c) => c.id === columnId);
      if (!col) return;
      await reorderWithin(col, payload.cardId, insertOrdered(col.cards.map((c) => c.id), payload.cardId, beforeCardId, before));
      return;
    }
    await moveCardToColumn(payload.cardId, payload.docId, payload.columnId, columnId, beforeCardId, before,payload.fromPath);
  };

  const acceptsBoardDrag = (e: React.DragEvent) => writable && (e.dataTransfer.types.includes(BOARD_DRAG_KEY)||e.dataTransfer.types.includes(DRAG_KEY));
  const handleDrop=async(e:React.DragEvent,columnId:string,beforeCardId:string|null,before:boolean)=>{
    if(e.dataTransfer.types.includes(BOARD_DRAG_KEY)){await applyDrop(parseBoardDrag(e.dataTransfer.getData(BOARD_DRAG_KEY)),columnId,beforeCardId,before);return}
    const payload=parseDragPayload(e.dataTransfer.getData(DRAG_KEY));if(!payload)return;
    if(payload.atts.length||payload.folders.length){setError('Move Markdown documents onto a board column; attachments and folders stay in the explorer.');return}
    for(const id of payload.docs){const doc=docs.find(d=>d.id===id&&!d.tombstoneAt);if(doc)await moveCardToColumn(id,id,'external',columnId,beforeCardId,before,doc.path)}
  };

  const cardKeys = (e: React.KeyboardEvent, card: BoardCard, col: BoardColumnView) => {
    if (!board||e.target!==e.currentTarget) return;
    if (busyRef.current.has(card.id)) return;
    if (e.key === ' ') { e.preventDefault(); e.stopPropagation(); selectCard(card.id); return; }
    if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); onOpenDoc(card.id); return; }
    if (e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10') || e.key.toLowerCase() === 'm') {
      e.preventDefault(); e.stopPropagation();
      const r = e.currentTarget.getBoundingClientRect();
      setCardMenu({ card, columnId: col.id, x: r.left, y: r.bottom });
      return;
    }
    if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) { e.preventDefault(); e.stopPropagation(); void reorderWithin(col, card.id, swapOrdered(col.cards.map((c) => c.id), card.id, e.key === 'ArrowDown' ? 1 : -1)); return; }
    if (e.altKey && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
      e.preventDefault(); e.stopPropagation();
      const i = columns.findIndex((c) => c.id === col.id);
      const j = Math.max(0, Math.min(columns.length - 1, i + (e.key === 'ArrowRight' ? 1 : -1)));
      if (j !== i) void moveCardToColumn(card.id, card.id, col.id, columns[j].id);
      return;
    }
    if(['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(e.key)){
      e.preventDefault();e.stopPropagation();const ci=columns.findIndex(c=>c.id===col.id),i=col.cards.findIndex(c=>c.id===card.id);
      const target=e.key==='ArrowUp'||e.key==='ArrowDown'?col.cards[Math.max(0,Math.min(col.cards.length-1,i+(e.key==='ArrowDown'?1:-1)))]:columns[ci+(e.key==='ArrowRight'?1:-1)]?.cards[0];
      if(target)setFocusCard(target.id);return;
    }
    if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); e.stopPropagation(); setCardMenu({ card, columnId: col.id, x: e.currentTarget.getBoundingClientRect().left, y: e.currentTarget.getBoundingClientRect().bottom }); return; }
  };

  const addTask = async (col: BoardColumnView) => {
    if (!board || !writable) return;
    const title = (titles[col.id] || '').trim();
    if (!title || adding.has(col.id)) return;
    const signature=JSON.stringify({title,content:board.config.createDefaults?.content||''});
    let attempt=idemRef.current[col.id];if(attempt?.signature!==signature){attempt={signature,key:randomIdemKey('board-task')};idemRef.current[col.id]=attempt}
    const key=attempt.key;
    setAdding((old) => new Set(old).add(col.id));
    try {
      const content = board.config.createDefaults?.content;
      const card = await api.createBoardTask(space, boardPath, { columnId: col.id, title, ...(content ? { content } : {}) }, key);
      delete idemRef.current[col.id];
      setTitles((t) => ({ ...t, [col.id]: '' }));
      setBoard(await api.getBoard(space, boardPath));
      onChanged();
      setFocusCard(card.id);
    } catch (e) { setError(errMsg(e)); api.getBoard(space, boardPath).then((b) => setBoard(b)).catch(() => {}); }
    finally { setAdding((old) => { const next = new Set(old); next.delete(col.id); return next; }); }
  };

  if (!board && loading) return <div className="board-view"><div className="board-header"><h1>Loading board…</h1></div></div>;
  if (!board) return <div className="board-view"><div className="board-banner" role="alert">{error || 'Board could not be loaded.'}</div><div className="board-header"><button className="btn" onClick={() => { setLoading(true); void reloadBoard(); }}>Retry</button></div></div>;

  return <div className="board-view">
   <header className="board-header">
    <div className="board-heading">
     <h1>{board.title}</h1>
     <div className="board-sub">
      <button className="board-path" title="Board folder — reveal it in the explorer" onClick={() => onRevealFolder(board.path)}><Icon name="folder" size={13}/>{board.path}</button>
      {boardMD && <button className="board-config" title="board.md holds this board's configuration" onClick={() => onOpenDoc(boardMD.id)}><Icon name="settings" size={13}/>Configuration</button>}
       <span className="board-hint" id="board-card-help">Click or Space previews · Double-click or Enter opens · Alt + ←/→ status · Alt + ↑/↓ order</span>
     </div>
    </div>
    <div className="spacer"/>
    {writable && <Tool icon="settings" label="Board settings: columns, labels, WIP limits, defaults" onClick={() => setSettingsOpen(true)}/>}
    <Tool icon="undoArrow" label="Reload board" onClick={() => { setLoading(true); void reloadBoard(); }}/>
   </header>
   {error && <div className="board-banner" role="alert">{error}</div>}
   {operationWarnings.length>0&&<div className="board-banner warning" role="alert">{operationWarnings.join(' · ')}</div>}
   {board.warnings?.length ? <div className="board-banner warning">{board.warnings.join(' · ')}</div> : null}
   <div className="board-content" ref={content} style={{'--task-preview-width':effectiveWidth+'%','--task-preview-height':effectiveHeight+'%'} as React.CSSProperties}><div className="board-surface">
   <div className="board-columns" role="list" aria-label="Board columns">
    {columns.map((col) => {
     const over = !!col.wipLimit && col.cards.length > col.wipLimit;
     const isDefault = board.config.createDefaults?.column === col.id;
     return <section key={col.id} className={'board-column ' + (col.completion ? 'completion ' : '') + (dropTarget?.columnId === col.id && !dropTarget.cardId ? 'drop' : '')} role="listitem" aria-label={col.label}
      onDragOver={(e) => { if (!acceptsBoardDrag(e)) return; e.preventDefault(); e.stopPropagation(); e.dataTransfer.dropEffect = 'move'; setDropTarget({ columnId: col.id }); }}
      onDragLeave={(e) => { if (e.currentTarget.contains(e.relatedTarget as Node | null)) return; setDropTarget((t) => t?.columnId === col.id && !t.cardId ? null : t); }}
      onDrop={(e) => { if (!acceptsBoardDrag(e)) return; e.preventDefault(); e.stopPropagation(); void handleDrop(e,col.id,null,true); }}>
      <header>
       {col.completion && <Icon name="check" size={14}/>}
       <strong>{col.label}</strong>
       {isDefault && <span className="board-default" title="New tasks start here">default</span>}
       <span className={'board-count' + (over ? ' over' : '')} title={col.wipLimit ? `Soft WIP limit: ${col.wipLimit}` : 'No WIP limit'}>{col.cards.length}{col.wipLimit ? ` / ${col.wipLimit}` : ''}</span>
      </header>
      {over && <small className="board-wip-note">WIP limit exceeded. Finish active work before starting more.</small>}
      <div className="board-cards">
       {col.cards.map((card) => <div role="button" tabIndex={0} key={card.id} data-card-id={card.id}
        className={'board-card ' + (col.completion ? 'completed ' : '') + (drag?.cardId === card.id ? 'dragging ' : '') + (dropTarget?.columnId === col.id && dropTarget.cardId === card.id ? (dropTarget.before ? 'drop-before' : 'drop-after') : '') + (busy.has(card.id) ? 'busy' : '') + (previewId === card.id ? ' selected' : '')}
        aria-label={`${card.title} — ${col.label}`} aria-describedby="board-card-help" aria-pressed={previewId===card.id} title={card.path}
        draggable={writable}
        onDragStart={(e) => { if (!writable||busyRef.current.has(card.id)||(e.target as HTMLElement).closest('button')) { e.preventDefault(); e.stopPropagation(); return; } draggedUntil.current=Infinity; e.dataTransfer.setData(BOARD_DRAG_KEY, JSON.stringify({ cardId: card.id, docId: card.id, columnId: col.id, fromPath: card.path, revision: card.revision } satisfies BoardDragPayload)); e.dataTransfer.effectAllowed = 'move'; setDrag({ cardId: card.id }); }}
        onDragEnd={() => { draggedUntil.current=Date.now()+250;setDrag(null); setDropTarget(null); }}
        onDragOver={(e) => { if (!acceptsBoardDrag(e)) return; e.preventDefault(); e.stopPropagation(); e.dataTransfer.dropEffect = 'move'; const r = e.currentTarget.getBoundingClientRect(); setDropTarget({ columnId: col.id, cardId: card.id, before: e.clientY < r.top + r.height / 2 }); }}
        onDrop={(e) => { if (!acceptsBoardDrag(e)) return; e.preventDefault(); e.stopPropagation(); const r = e.currentTarget.getBoundingClientRect(); void handleDrop(e,col.id,card.id,e.clientY < r.top + r.height / 2); }}
        onClick={(e) => { if(!(e.target as HTMLElement).closest('button'))selectCard(card.id); }}
        onDoubleClick={(e) => { if (!(e.target as HTMLElement).closest('button')&&!busyRef.current.has(card.id)&&Date.now()>=draggedUntil.current) onOpenDoc(card.id); }}
        onContextMenu={e=>{e.preventDefault();e.stopPropagation();setCardMenu({card,columnId:col.id,x:e.clientX,y:e.clientY})}}
        onKeyDown={(e) => cardKeys(e, card, col)}>
        <button className="board-card-menu" aria-label={'Actions for '+card.title} onClick={e=>{e.stopPropagation();const r=e.currentTarget.getBoundingClientRect();setCardMenu({card,columnId:col.id,x:r.left,y:r.bottom})}} onKeyDown={e=>e.stopPropagation()}><Icon name="more" size={16}/></button>
        <span className="board-card-title">{card.title}</span>
        <span className="board-card-path">{card.path}</span>
        {card.warnings?.length ? <span className="board-card-warn" title={card.warnings.join(' · ')}><Icon name="info" size={12}/></span> : null}
        {busy.has(card.id) ? <span className="board-card-busy" aria-hidden="true">↻</span> : null}
       </div>)}
       {!col.cards.length && <div className="board-empty-card">Drop a card here</div>}
      </div>
      {writable && <form className="board-add" onSubmit={(e) => { e.preventDefault(); e.stopPropagation(); void addTask(col); }}>
       <input aria-label={'New task in ' + col.label} placeholder="Add a task…" value={titles[col.id] || ''} onChange={(e) => setTitles((t) => ({ ...t, [col.id]: e.target.value }))}/>
       <button className="btn small" disabled={adding.has(col.id) || !titles[col.id]?.trim()}>{adding.has(col.id) ? 'Adding…' : 'Add'}</button>
      </form>}
     </section>;
    })}
   </div>
   {overflow.length > 0 && <section className="board-supporting">
    <header><strong>Supporting documents</strong><small>Files inside the board folder that are not in a column — never counted as task cards.</small></header>
    {overflow.map((card) => <button key={card.id} type="button" data-card-id={card.id} aria-pressed={previewId===card.id} aria-describedby="board-card-help" className={'board-card supporting' + (previewId === card.id ? ' selected' : '')} onClick={() => selectCard(card.id)} onDoubleClick={() => onOpenDoc(card.id)} onKeyDown={e=>{if(e.key==='Enter'){e.preventDefault();onOpenDoc(card.id)}}} title={card.path}><span className="board-card-title">{card.title}</span><span className="board-card-path">{card.path}</span></button>)}
   </section>}
   </div>
   {previewId&&<aside className="board-preview" aria-label="Task preview">
    <div className="board-preview-resizer" role="separator" tabIndex={0} aria-label="Resize task preview" aria-orientation={narrow?'horizontal':'vertical'} aria-valuemin={Math.round(narrow?minHeight:minWidth)} aria-valuemax={narrow?75:60} aria-valuenow={Math.round(narrow?effectiveHeight:effectiveWidth)}
     onKeyDown={e=>{const increase=narrow?'ArrowUp':'ArrowLeft',decrease=narrow?'ArrowDown':'ArrowRight';if(![increase,decrease,'Home','End'].includes(e.key))return;e.preventDefault();changeSize(e.key==='Home'?(narrow?minHeight:minWidth):e.key==='End'?(narrow?75:60):(narrow?effectiveHeight:effectiveWidth)+(e.key===increase?1:-1)*(narrow?5:2))}}
     onPointerDown={e=>{e.preventDefault();e.currentTarget.focus();e.currentTarget.setPointerCapture(e.pointerId);const rect=content.current!.getBoundingClientRect();resize.current={right:rect.right,bottom:rect.bottom,width:rect.width,height:rect.height}}}
     onPointerMove={e=>{const rect=resize.current;if(rect)changeSize(narrow?(rect.bottom-e.clientY)/rect.height*100:(rect.right-e.clientX)/rect.width*100)}}
     onPointerUp={()=>{resize.current=null}} onPointerCancel={()=>{resize.current=null}} onLostPointerCapture={()=>{resize.current=null}}/>
    <header className="board-preview-header">
     <div className="board-preview-heading"><strong>{previewCard?.title||preview?.title||'Task'}</strong><small className="board-path" title={preview?.path||previewCard?.path}>{preview?.path||previewCard?.path}</small><div className="board-preview-meta"><span>{columns.find(c=>c.cards.some(card=>card.id===previewId))?.label||(previewCard?'Supporting document':'No longer on this board')}</span>{previewDraft&&<small role="status">{!previewDraft.canEdit?'Read only':({saved:'Saved',saving:'Saving…',unsaved:'Unsaved changes',error:'Save failed',conflict:'Save conflict'}[previewDraft.status])}</small>}</div></div>
     <Tool icon="close" label="Close task preview" onClick={()=>void closePreview()}/>
    </header>
    {previewError&&<div className="board-banner" role="alert"><span>{previewError}</span><button className="btn small" onClick={()=>setPreviewRetry(n=>n+1)}>Retry preview</button>{previewDraft&&<button className="btn small" onClick={()=>exportDraft(preview?.path||'task.md',previewDraft.text)}>Export draft</button>}</div>}
    {previewDraft&&(previewDraft.status==='error'||previewDraft.status==='conflict')&&<div className="save-error" role="alert"><span>{previewDraft.error}</span><button className="btn small" onClick={()=>previewDraft.status==='conflict'?void onResolveConflict(previewId):void previewDraft.flush()}>{previewDraft.status==='conflict'?'Resolve conflict':'Retry save'}</button><button className="btn small" onClick={()=>exportDraft(preview?.path||'task.md',previewDraft.text)}>Export draft</button></div>}
    {!previewDraft&&!previewError&&<p className="muted" role="status">Loading task…</p>}
    {previewDraft&&<div className="board-preview-body" inert={busy.has(previewId)} aria-busy={busy.has(previewId)}>
     {previewMode==='source'?<><div className="board-preview-mode"><span>Markdown source</span><button className="btn small" onClick={()=>setPreviewMode('visual')}>Visual editor</button></div><NoteEditor draft={previewDraft} docs={docs} notePath={preview?.path||previewCard?.path} onUploadImages={onUploadImages?files=>onUploadImages(files,preview?.path||previewCard?.path||''):undefined} onImageError={setPreviewError}/></>:<Suspense fallback={<p className="muted" role="status">Loading Visual editor…</p>}><VisualEditor compact draft={previewDraft} docs={docs} notePath={preview?.path||previewCard?.path} attachments={attachments.map(a=>({path:a.path,url:api.attachmentURL(space,a.path)}))} onUploadImages={onUploadImages?files=>onUploadImages(files,preview?.path||previewCard?.path||''):undefined} onSourceMode={()=>setPreviewMode('source')}/></Suspense>}
    </div>}
   </aside>}
   </div>
   {cardMenu && <div className="board-menu-backdrop" onClick={() => setCardMenu(null)} onContextMenu={(e) => { e.preventDefault(); e.stopPropagation(); setCardMenu(null); }}>
    <div className="board-menu" role="menu" style={{ left: Math.max(0,Math.min(cardMenu.x, window.innerWidth - 250)), top: Math.max(0,Math.min(cardMenu.y, window.innerHeight - 300)) }} onClick={(e) => e.stopPropagation()}>
     <button role="menuitem" onClick={() => { const id = cardMenu.card.id; setCardMenu(null); onOpenDoc(id); }}>Open document</button>
     {writable && columns.filter((c) => c.id !== cardMenu.columnId).map((c) => <button key={c.id} role="menuitem" onClick={() => { const card = cardMenu.card, from = cardMenu.columnId; setCardMenu(null); void moveCardToColumn(card.id, card.id, from, c.id); }}>Move to {c.label}</button>)}
     {writable && <button role="menuitem" onClick={() => { const id = cardMenu.card.id; setCardMenu(null); onMoveToBoard(id); }}>Move to another board…</button>}
     <button role="menuitem" onClick={() => { void navigator.clipboard?.writeText(cardMenu.card.path).catch(() => {}); setCardMenu(null); }}>Copy path</button>
    </div>
   </div>}
   {settingsOpen && <BoardSettings space={space} board={board} onClose={() => setSettingsOpen(false)} onSaved={(b) => { setBoard(b); setSettingsOpen(false); onChanged(); }}/>}
  </div>;
}

function BoardSettings({ space, board, onClose, onSaved }: { space: string; board: api.Board; onClose: () => void; onSaved: (board: api.Board) => void }) {
  const [title, setTitle] = useState(board.config.title || board.title);
  const [columns, setColumns] = useState<BoardColumnDraft[]>(board.columns.map((c) => ({ id: c.id, label: c.label, folder: c.folder, completion: !!c.completion, wipLimit: c.wipLimit ?? 0 })));
  const [createColumn, setCreateColumn] = useState(board.config.createDefaults?.column || board.columns[0]?.id || '');
  const [createContent, setCreateContent] = useState(board.config.createDefaults?.content || '');
  const [errors, setErrors] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);

  const patch = (id: string, part: Partial<BoardColumnDraft>) => setColumns((old) => old.map((c) => c.id === id ? { ...c, ...part } : c));
  const shift = (index: number, dir: -1 | 1) => setColumns((old) => { const j = index + dir; if (j < 0 || j >= old.length) return old; const next = [...old]; const [c] = next.splice(index, 1); next.splice(j, 0, c); return next; });
  const addColumn = () => setColumns(old=>{const used=new Set(old.flatMap(c=>[c.id,c.folder]));const id=columnSlug('new column',used);return [...old,{id,label:'New column',folder:id,completion:false,wipLimit:0}]});

  const save = async () => {
    const problems = validateBoardColumns(columns, createColumn);
    problems.push(...populatedColumnRemovalErrors(board.columns.map((c) => ({ id: c.id, label: c.label, folder: c.folder, cards: c.cards.length })), columns));
    setErrors(problems);
    if (problems.length) return;
    setSaving(true);
    try {
      const next = await api.updateBoardConfig(space, board.path, {
        title: title.trim() || undefined,
        columns: columns.map((c) => ({ id: c.id.trim(), label: c.label.trim() || c.id, folder: c.folder.trim(), completion: c.completion, wipLimit: c.wipLimit })),
        createDefaults: { column: createColumn, content: createContent },
      }, board.revision);
      onSaved(next);
    } catch (e) {
      setErrors([api.ApiError.isConflict(e) ? 'The board configuration changed elsewhere. Close this dialog, reopen it to load the latest version, then apply your edits again.' : errMsg(e)]);
    } finally { setSaving(false); }
  };

  return <Dialog title="Board settings" close={onClose}>
   <p className="muted">Columns are folders inside <code>{board.path}</code>. A column folder cannot be removed or renamed while it still holds cards — move those cards first. The <code>board.md</code> file in the board folder is this configuration, not a task.</p>
   <label className="field"><span>Board title</span><input value={title} onChange={(e) => setTitle(e.target.value)}/></label>
   <div className="board-settings-columns">
    <strong>Columns</strong>
    {columns.map((c, i) => <div className="board-column-row" key={c.id}>
     <input aria-label="Column label" value={c.label} onChange={(e) => patch(c.id, { label: e.target.value })} placeholder="Label"/>
     <input aria-label="Column folder" value={c.folder} onChange={(e) => patch(c.id, { folder: e.target.value })} placeholder="folder"/>
     <label className="board-completion"><input type="checkbox" checked={c.completion} onChange={(e) => patch(c.id, { completion: e.target.checked })}/>Done</label>
     <input type="number" min={0} aria-label="Soft WIP limit" title="Soft WIP limit (0 = none)" value={c.wipLimit} onChange={(e) => patch(c.id, { wipLimit: Number(e.target.value) })}/>
     <button className="btn small" aria-label="Move column earlier" disabled={i === 0} onClick={() => shift(i, -1)}>↑</button>
     <button className="btn small" aria-label="Move column later" disabled={i === columns.length - 1} onClick={() => shift(i, 1)}>↓</button>
     <button className="btn small danger" aria-label={'Remove column ' + c.label} disabled={columns.length <= 1} onClick={() => setColumns((old) => old.filter((x) => x.id !== c.id))}><Icon name="trash" size={13}/></button>
    </div>)}
    <button className="btn small" onClick={addColumn}><Icon name="plus" size={13}/>Add column</button>
   </div>
   <div className="board-defaults">
    <strong>New task defaults</strong>
    <label className="field"><span>Default column</span>
     <select value={createColumn} onChange={(e) => setCreateColumn(e.target.value)}>
      {columns.map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
     </select>
    </label>
    <label className="field"><span>Default task content (Markdown)</span>
     <textarea rows={4} value={createContent} onChange={(e) => setCreateContent(e.target.value)} placeholder="Every new task starts with this content…"/>
    </label>
   </div>
   {errors.length > 0 && <div className="board-banner" role="alert">{errors.map((e) => <p key={e}>{e}</p>)}</div>}
   <footer className="dialog-actions"><button className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={saving} onClick={() => void save()}>{saving ? 'Saving…' : 'Save board'}</button></footer>
  </Dialog>;
}
