// MindMapView: the mind map canvas tab. Nodes are ordinary Markdown documents
// (owned inside the map folder or referenced from elsewhere), connectors and
// placement live in mindmap.md. Canvas edits autosave with If-Match. File
// drops add ordinary Markdown links; moving files is a separate explicit action.

import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import * as api from '../api/client';
import type { MindMapEdgeConfig, MindMapNodeConfig, MindMapNodeView } from '../api/client';
import type { DocMeta, NoteContent } from '../api/types';
import type { Draft, DraftDocument } from '../lib/drafts';
import { DRAG_KEY, parseDragPayload } from './FileTree';
import {MindMapCardContent} from './MindMapCardContent';
import {NoteEditor} from './NoteEditor';
import { Dialog } from './Dialog';
import { Icon, Tool } from './Icon';
import {uiZoom,overlayPoint} from '../lib/overlayPosition';
import {MindMapDraft,sharedMindMapDraft} from '../lib/mindmapDraft';
import { bindGraphZoom } from '../lib/graphZoom';
import { randomIdemKey } from '../lib/boards';
import {
  availablePosition, arrangeMindMap, borderPoint, sidePoint, NODE_SIDES, type NodeSide, childPosition, clampZoom, curvePath, documentsInNodeFolder, fitView,
  mindMapFileTarget, mindMapNodeFolder, newEdgeId, nodeSize, siblingPosition, snapCoord,
  validateCanvas, worldPoint, type Viewport,
} from '../lib/mindmaps';

const errMsg = (e: unknown) => e instanceof Error ? e.message : 'Something went wrong.';
const VisualEditor = lazy(() => import('./VisualEditor').then((m) => ({ default: m.VisualEditor })));
const clamp = (n: number, min: number, max: number) => Math.max(min, Math.min(max, n));
function storedSize(key: string, fallback: number, min: number, max: number) { try { const raw = localStorage.getItem(key), n = raw === null ? fallback : Number(raw); return Number.isFinite(n) ? clamp(n, min, max) : fallback; } catch { return fallback; } }
function exportDraft(path: string, text: string) { const url = URL.createObjectURL(new Blob([text], { type: 'text/markdown;charset=utf-8' })), link = document.createElement('a'); link.href = url; link.download = path.split('/').pop() || 'node.md'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); }

const PALETTE = ['', '#e07a5f', '#81b29a', '#f2cc8f', '#6d9dc5', '#9b7ede', '#d16ba5'];

type SaveState = 'saved' | 'saving' | 'unsaved' | 'conflict' | 'error';
type Selection = { kind: 'node' | 'edge'; id: string } | null;

export function MindMapView({ space, mapPath, writable, docs, attachments = [], epoch, principal, onPreviewDraft, onResolveConflict, onOpenDoc, onFlushDraft, onInvalidateDoc, onChanged, onRevealFolder, onUploadImages }: {
  space: string; mapPath: string; writable: boolean; docs: DocMeta[]; epoch: number;
  attachments?: { path: string }[];
  principal: string;
  onPreviewDraft: (id: string) => Promise<DraftDocument>;
  onResolveConflict: (id: string) => Promise<void>;
  onOpenDoc: (id: string) => void;
  onFlushDraft: (docId: string) => Promise<void>;
  onInvalidateDoc: (docId: string) => void;
  onChanged: () => void;
  onRevealFolder: (path: string) => void;
  onUploadImages?:(files:File[],notePath:string)=>Promise<string[]>;
}) {
  const [map, setMap] = useState<api.MindMap | null>(null);
  const [canvas, setCanvas] = useState<{ nodes: MindMapNodeConfig[]; edges: MindMapEdgeConfig[] } | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [operationWarnings, setOperationWarnings] = useState<string[]>([]);
  const [saveState, setSaveState] = useState<SaveState>('saved');
  const [saveError, setSaveError] = useState('');
  const [renaming,setRenaming]=useState(false),[mapTitle,setMapTitle]=useState('');
  const [connectFrom, setConnectFrom] = useState('');
  const [connection,setConnection]=useState<{from:string;side:NodeSide;point:{x:number;y:number};target?:string;targetSide?:NodeSide}|null>(null);
  const connectionRef=useRef<typeof connection>(null);
  const connectionStart=useRef<{x:number;y:number;moved:boolean}|null>(null);
  const nodeResize=useRef<{id:string;px:number;py:number;w:number;h:number}|null>(null);
  const [resizing,setResizing]=useState('');
  const pendingDrops=useRef(new Map<string,{key:string;nodeId?:string}>());
  const updateConnection=(next:typeof connection)=>{connectionRef.current=next;setConnection(next)};
  useEffect(()=>{const cancel=(e:KeyboardEvent)=>{if(e.key==='Escape'&&(connectionRef.current||connectFrom)){e.preventDefault();e.stopImmediatePropagation();updateConnection(null);connectionStart.current=null;setConnectFrom('')}};document.addEventListener('keydown',cancel,true);return()=>document.removeEventListener('keydown',cancel,true)},[connectFrom]);

  const [view, setView] = useState<Viewport>({ x: 0, y: 0, scale: 1 });
  const [selected, setSelected] = useState<Selection>(null);
  const [dragNode, setDragNode] = useState('');
  const [dropNode, setDropNode] = useState('');
  const [nodeMenu, setNodeMenu] = useState<{ node: MindMapNodeView; x: number; y: number } | null>(null);
  const [newNode, setNewNode] = useState<{ x: number; y: number; parent?: string; fromSide?:NodeSide; siblings?: string[] } | null>(null);
  const [newTitle, setNewTitle] = useState('');
  const createRequest=useRef<{signature:string;key:string}|null>(null),createInFlight=useRef(false);
  const [creatingNode,setCreatingNode]=useState(false);
  const [picker, setPicker] = useState<{ mode: 'link' | 'collect'; nodeId?: string } | null>(null);
  const [pickQuery, setPickQuery] = useState('');
  const [confirm, setConfirm] = useState<
    | { kind: 'trash'; node: MindMapNodeView }
    | { kind: 'collect'; nodeId: string; docs: { id: string; path: string; target: string }[] }
    | { kind: 'moveToMap'; node: MindMapNodeView; target: string }
    | null>(null);
  const [busyIds, setBusyIds] = useState<Set<string>>(() => new Set());
  const busyRef = useRef(new Set<string>());
  const [focusNode, setFocusNode] = useState('');

  // Preview mirrors BoardView: single click selects and previews, double click opens.
  const [previewId, setPreviewId] = useState('');
  const [preview, setPreview] = useState<NoteContent | null>(null);
  const [previewError, setPreviewError] = useState('');
  const [previewDraft, setPreviewDraft] = useState<Draft | null>(null);
  const draftRef = useRef<Draft | null>(null), selection = useRef(0);
  const [, renderDraft] = useState(0);
  const [previewMode,setPreviewMode]=useState<'visual'|'source'>('visual');
  const [previewRetry, setPreviewRetry] = useState(0);
  const content = useRef<HTMLDivElement>(null);
  const sizeKey = `netsidian:mindmap-preview:${principal}:${space}:${mapPath}`;
  const [width, setWidth] = useState(() => storedSize(sizeKey + ':width', 40, 20, 60));
  const [narrow,setNarrow]=useState(innerWidth<=700),[height,setHeight]=useState(()=>storedSize(sizeKey+':height',58,30,80));
  useEffect(()=>{const resize=()=>setNarrow(innerWidth<=700);window.addEventListener('resize',resize);return()=>window.removeEventListener('resize',resize)},[]);
  const [bodyWidth, setBodyWidth] = useState(0);
  const minWidth = bodyWidth ? Math.min(60, Math.max(20, 240 / bodyWidth * 100)) : 20;
  const effectiveWidth = clamp(width, minWidth, 60);
  useEffect(() => setWidth(storedSize(sizeKey + ':width', 40, 20, 60)), [sizeKey]);
  useEffect(() => { const el = content.current; if (!el) return; const update = () => setBodyWidth(el.getBoundingClientRect().width); update(); if (typeof ResizeObserver === 'undefined') return; const o = new ResizeObserver(update); o.observe(el); return () => o.disconnect(); }, [!!map]);
  const changeSize = (value: number) => { const n = narrow?clamp(value,30,80):clamp(value,minWidth,60); narrow?setHeight(n):setWidth(n); try { localStorage.setItem(sizeKey + (narrow?':height':':width'), String(n)); } catch { /* private mode */ } };

  // A serialized draft protects against stale responses, live refresh and reload.
  const layoutDraft = useRef<MindMapDraft | null>(null);
  const unsubscribeLayout = useRef<(() => void) | null>(null);
  const revisionRef = useRef('');
  const syncLayout = useCallback(() => {
    const d=layoutDraft.current;if(!d)return;
    setMap(d.server);setCanvas(d.canvas);revisionRef.current=d.server.revision;
    setSaveState(d.status);setSaveError(d.error);
  },[]);
  const applyServer = useCallback((m:api.MindMap) => {
    if(!layoutDraft.current){
      layoutDraft.current=sharedMindMapDraft(principal,space,mapPath,m);
      unsubscribeLayout.current=layoutDraft.current.subscribe(syncLayout);
      syncLayout();
    }else layoutDraft.current.discard(m);
  },[space,mapPath,principal,syncLayout]);
  const queueSave = useCallback((next:{nodes:MindMapNodeConfig[];edges:MindMapEdgeConfig[]})=>{
    const problems=validateCanvas(next.nodes,next.edges);
    if(problems.length){setError(problems[0]);return}
    layoutDraft.current?.change(next);
  },[]);
  const flushSave = useCallback(async()=>{await layoutDraft.current?.flush()},[]);
  const retrySave = async()=>{
    const d=layoutDraft.current;if(!d)return;
    try{if(d.status==='conflict')await d.saveOver(await api.getMindMap(space,mapPath));else await d.flush()}catch{/* draft exposes the error */}
  };
  const adoptRemote = async()=>{try{layoutDraft.current?.discard(await api.getMindMap(space,mapPath))}catch(e){setError(errMsg(e))}};
  const reload = useCallback(()=>{
    let active=true;
    api.getMindMap(space,mapPath).then(m=>{
      if(!active||nodeDrag.current||nodeResize.current)return;
      if(!layoutDraft.current)applyServer(m);else layoutDraft.current.acceptRemote(m);
      setError('');
    }).catch(e=>{if(active)setError(errMsg(e))}).finally(()=>{if(active)setLoading(false)});
    return()=>{active=false};
  },[space,mapPath,applyServer]);
  useEffect(()=>reload(),[reload,epoch]);
  useEffect(()=>{
    const guard=(e:BeforeUnloadEvent)=>{if(layoutDraft.current?.dirty){e.preventDefault();e.returnValue=''}};
    window.addEventListener('beforeunload',guard);
    return()=>{window.removeEventListener('beforeunload',guard);unsubscribeLayout.current?.();layoutDraft.current?.dispose();layoutDraft.current=null};
  },[]);

  // ---- viewport: pan, wheel/pinch zoom, fit ----
  const surfaceRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = surfaceRef.current; if (!el) return;
    return bindGraphZoom(el, (factor, anchor) => {
      const rect = el.getBoundingClientRect();
      setView((v) => {
        const scale = clampZoom(v.scale * factor);
        const ax = (anchor.clientX - rect.left)/uiZoom(), ay = (anchor.clientY - rect.top)/uiZoom();
        return { scale, x: ax - (ax - v.x) * (scale / v.scale), y: ay - (ay - v.y) * (scale / v.scale) };
      });
    });
  }, [!!map]);
  const zoomAtCenter = (factor: number) => {
    const el = surfaceRef.current; if (!el || !layoutDraft.current?.canvas.nodes.length) return;
    const rect = el.getBoundingClientRect();
    setView((v) => { const scale = clampZoom(v.scale * factor), ax = rect.width / (2*uiZoom()), ay = rect.height / (2*uiZoom()); return { scale, x: ax - (ax - v.x) * (scale / v.scale), y: ay - (ay - v.y) * (scale / v.scale) }; });
  };
  const fit = () => {
    const el = surfaceRef.current; if (!el || !canvas) return;
    const rect = el.getBoundingClientRect();
    const next = fitView(canvas.nodes, rect.width/uiZoom(), rect.height/uiZoom());
    if (next) setView(next);
  };
  useEffect(()=>{const el=surfaceRef.current;if(!el)return;const resize=()=>{const rect=el.getBoundingClientRect(),nodes=layoutDraft.current?.canvas.nodes;if(nodes?.length){const fitted=fitView(nodes,rect.width/uiZoom(),rect.height/uiZoom());if(fitted)setView(fitted)}};resize();if(typeof ResizeObserver==='undefined')return;const observer=new ResizeObserver(resize);observer.observe(el);return()=>observer.disconnect()},[!!map]);
  const centerWorld = () => {
    const el = surfaceRef.current; if (!el) return { x: 0, y: 0 };
    const rect = el.getBoundingClientRect();
    return worldPoint(rect.left + rect.width / 2, rect.top + rect.height / 2, rect, view,uiZoom());
  };

  // ---- node dragging (pointer events, not HTML5 DnD) ----
  const panRef = useRef<{ px: number; py: number; vx: number; vy: number } | null>(null);
  const nodeDrag = useRef<{ id: string; px: number; py: number; ox: number; oy: number; moved: boolean } | null>(null);
  const draggedUntil = useRef(0);
  const onSurfacePointerDown = (e: React.PointerEvent) => {
    if (connectionRef.current) { e.preventDefault(); finishConnection(e.clientX,e.clientY,true); return; }
    if (!canvas?.nodes.length || e.button !== 0 || (e.target as HTMLElement).closest('[data-node-id],button,[role="menu"],[role="separator"]')) return;
    panRef.current = { px: e.clientX, py: e.clientY, vx: view.x, vy: view.y };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setSelected(null); setNodeMenu(null);
  };
  const onSurfacePointerMove = (e: React.PointerEvent) => {
    const c=connectionRef.current;
    if(c&&surfaceRef.current){
      const point=worldPoint(e.clientX,e.clientY,surfaceRef.current.getBoundingClientRect(),view,uiZoom());
      const target=document.elementFromPoint(e.clientX,e.clientY)?.closest<HTMLElement>('[data-node-id]');
      const id=target?.dataset.nodeId,side=document.elementFromPoint(e.clientX,e.clientY)?.closest<HTMLElement>('[data-connection-side]')?.dataset.connectionSide as NodeSide|undefined;
      const start=connectionStart.current;if(start&&Math.hypot(e.clientX-start.x,e.clientY-start.y)>4)start.moved=true;
      updateConnection({...c,point,target:id&&id!==c.from?id:undefined,targetSide:side});return;
    }
    const resize=nodeResize.current;
    if(resize&&canvas){const w=clamp(snapCoord(resize.w+(e.clientX-resize.px)/(view.scale*uiZoom())),160,1200),h=clamp(snapCoord(resize.h+(e.clientY-resize.py)/(view.scale*uiZoom())),96,1200);setCanvas({...canvas,nodes:canvas.nodes.map(n=>n.id===resize.id?{...n,w,h}:n)});return}
    const p = panRef.current;
    if (p) setView((v) => ({ ...v, x: p.vx + (e.clientX - p.px)/uiZoom(), y: p.vy + (e.clientY - p.py)/uiZoom() }));
    const d = nodeDrag.current;
    if (d && canvas && (d.moved || Math.abs(e.clientX - d.px) + Math.abs(e.clientY - d.py) > 3)) {
      d.moved = true; setDragNode(d.id);
      const nodes = canvas.nodes.map((n) => n.id === d.id ? { ...n, x: snapCoord(d.ox + (e.clientX - d.px) / (view.scale*uiZoom())), y: snapCoord(d.oy + (e.clientY - d.py) / (view.scale*uiZoom())) } : n);
      setCanvas({ ...canvas, nodes });
    }
  };
  const onSurfacePointerUp = (e:React.PointerEvent) => {
    if(connectionRef.current){if(e.type==='pointercancel'){updateConnection(null);connectionStart.current=null;return}if(connectionStart.current?.moved)finishConnection(e.clientX,e.clientY,true);connectionStart.current=null;return}
    if(nodeResize.current){nodeResize.current=null;setResizing('');draggedUntil.current=Date.now()+250;if(canvas&&writable)queueSave(canvas);return}
    panRef.current = null;
    const d = nodeDrag.current;
    nodeDrag.current = null; setDragNode('');
    if (d?.moved && canvas) { draggedUntil.current = Date.now() + 250; if (writable) queueSave({ ...canvas }); }
  };
  const onNodePointerDown = (e: React.PointerEvent, node: MindMapNodeConfig) => {
    if (connectionRef.current || !writable || e.button !== 0 || (e.target as HTMLElement).closest('button,a,.mindmap-card-scroll,[data-resize]')) return;
    nodeDrag.current = { id: node.id, px: e.clientX, py: e.clientY, ox: node.x, oy: node.y, moved: false };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  };

  const finishConnection=(clientX:number,clientY:number,createOnEmpty:boolean)=>{
    const c=connectionRef.current,current=layoutDraft.current?.canvas;
    if(!c||!current||!writable)return;
    const element=document.elementFromPoint(clientX,clientY),target=element?.closest<HTMLElement>('[data-node-id]')?.dataset.nodeId;
    const targetSide=element?.closest<HTMLElement>('[data-connection-side]')?.dataset.connectionSide as NodeSide|undefined;
    if(target&&target!==c.from){
      if(!current.edges.some(e=>e.from===c.from&&e.to===target))queueSave({...current,edges:[...current.edges,{id:newEdgeId(current.edges.map(e=>e.id)),from:c.from,to:target,fromSide:c.side,toSide:targetSide}]});
    }else if(!target&&createOnEmpty&&element&&surfaceRef.current?.contains(element)){
      const pos=worldPoint(clientX,clientY,surfaceRef.current.getBoundingClientRect(),view,uiZoom());setNewTitle('');setNewNode({x:pos.x,y:pos.y,parent:c.from,fromSide:c.side});
    }
    updateConnection(null);setConnectFrom('');draggedUntil.current=Date.now()+250;
  };
  const startConnection=(e:React.PointerEvent,id:string,side:NodeSide)=>{
    e.stopPropagation();if(!writable||e.button!==0)return;
    if(connectionRef.current){finishConnection(e.clientX,e.clientY,false);return}
    const node=layoutDraft.current?.canvas.nodes.find(n=>n.id===id);if(!node)return;
    setConnectFrom('');updateConnection({from:id,side,point:sidePoint(node,side)});connectionStart.current={x:e.clientX,y:e.clientY,moved:false};
    surfaceRef.current?.setPointerCapture(e.pointerId);
  };
  const resizeNode=(e:React.PointerEvent,node:MindMapNodeConfig)=>{e.stopPropagation();e.preventDefault();if(!writable||e.button!==0)return;const size=nodeSize(node);nodeResize.current={id:node.id,px:e.clientX,py:e.clientY,...size};setResizing(node.id);surfaceRef.current?.setPointerCapture(e.pointerId)};

  // ---- selection, preview, keyboard ----
  const mapNode = (id: string) => map?.nodes.find((n) => n.id === id);
  const flushPreview = useCallback(async () => { const d = draftRef.current; if (!d) return true; try { await d.flush(); if (d.dirty) throw new Error('Save or export this draft before closing it or selecting another node.'); return true; } catch (e) { setPreviewError(errMsg(e)); return false; } }, []);
  const selectNode = async (id: string) => {
    if (busyRef.current.has(id) || Date.now() < draggedUntil.current) return;
    const seq = ++selection.current;
    if(connectionRef.current&&writable){const c=connectionRef.current;if(c.from!==id&&canvas&&!canvas.edges.some(e=>e.from===c.from&&e.to===id))queueSave({...canvas,edges:[...canvas.edges,{id:newEdgeId(canvas.edges.map(e=>e.id)),from:c.from,to:id,fromSide:c.side}]});updateConnection(null);return}
    if(connectFrom && writable){
      if(connectFrom!==id && canvas){
        if(!canvas.edges.some(e=>e.from===connectFrom&&e.to===id))queueSave({nodes:canvas.nodes,edges:[...canvas.edges,{id:newEdgeId(canvas.edges.map(e=>e.id)),from:connectFrom,to:id}]});
      }
      setConnectFrom('');return;
    }
    setSelected({ kind: 'node', id });
    if (id === previewId || !mapNode(id)?.path) return;
    if (!await flushPreview() || seq !== selection.current) return;
    draftRef.current = null; setPreviewDraft(null); setPreview(null); setPreviewError('');setPreviewMode('visual'); setPreviewId(id);
  };
  const closePreview = useCallback(async () => {
    const seq = ++selection.current;
    if (!await flushPreview() || seq !== selection.current) return;
    const id = previewId; draftRef.current = null; setPreviewDraft(null); setPreviewId(''); setPreview(null); setFocusNode(id);
  }, [previewId, flushPreview]);
  useEffect(() => {
    if (!previewId) { setPreview(null); setPreviewError(''); return; }
    let active = true; setPreviewError('');
    onPreviewDraft(previewId).then(({ draft, note }) => { if (active) { draftRef.current = draft; setPreviewDraft(draft); setPreview(note); } }).catch((e) => { if (active) setPreviewError(errMsg(e)); });
    return () => { active = false; };
  }, [space, previewId, epoch, previewRetry, mapNode(previewId)?.path, mapNode(previewId)?.revision, onPreviewDraft]);
  useEffect(() => previewDraft?.subscribe(() => renderDraft((n) => n + 1)), [previewDraft]);
  useEffect(() => {
    if (!map || !previewId) return;
    if (!map.nodes.some((n) => n.id === previewId)) {
      if (draftRef.current?.dirty) setPreviewError('This node is no longer on the canvas. Your unsaved draft is retained; resolve or export it before closing.');
      else { selection.current++; draftRef.current = null; setPreviewDraft(null); setPreviewId(''); setPreview(null); }
    }
  }, [map, previewId]);
  useEffect(() => {
    if (!previewId) return;
    const escape = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented || nodeMenu || newNode || picker || confirm || document.querySelector('dialog[open]') || (e.target instanceof Element && e.target.closest('[role="dialog"],[role="menu"],[role="listbox"]'))) return;
      e.preventDefault(); void closePreview();
    };
    document.addEventListener('keydown', escape);
    return () => document.removeEventListener('keydown', escape);
  }, [previewId, nodeMenu, newNode, picker, confirm, closePreview]);
  useEffect(() => {
    if (!nodeMenu) return;
    const last=document.activeElement as HTMLElement;
    content.current?.closest('.mindmap-view')?.querySelector<HTMLElement>('.mindmap-menu [role="menuitem"]')?.focus();
    const close = (e: KeyboardEvent) => { if (e.key === 'Escape') setNodeMenu(null); };
    document.addEventListener('keydown', close);
    return () => {document.removeEventListener('keydown', close);if(last?.isConnected)last.focus()};
  }, [nodeMenu]);
  useEffect(() => {
    if (!focusNode || !map) return;
    document.querySelector<HTMLElement>(`[data-node-id="${CSS.escape(focusNode)}"]`)?.focus();
    setFocusNode('');
  }, [map, focusNode]);

  const nodeKeys = (e: React.KeyboardEvent, node: MindMapNodeConfig) => {
    if (e.target !== e.currentTarget || !canvas) return;
    const viewNode = mapNode(node.id);
    if(e.key==='Escape'){
      e.preventDefault();setConnectFrom('');setSelected(null);void closePreview();
      content.current?.closest('.mindmap-view')?.querySelector<HTMLElement>('.mindmap-header button')?.focus();return;
    }
    if(['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(e.key)){
      e.preventDefault();const dx=e.key==='ArrowLeft'?-1:e.key==='ArrowRight'?1:0,dy=e.key==='ArrowUp'?-1:e.key==='ArrowDown'?1:0;
      if(e.altKey&&writable){queueSave({nodes:canvas.nodes.map(n=>n.id===node.id?{...n,x:n.x+dx*16,y:n.y+dy*16}:n),edges:canvas.edges})}
      else{const candidates=canvas.nodes.filter(n=>n.id!==node.id&&((n.x-node.x)*dx+(n.y-node.y)*dy)>0).sort((a,b)=>Math.hypot(a.x-node.x,a.y-node.y)-Math.hypot(b.x-node.x,b.y-node.y));if(candidates[0])setFocusNode(candidates[0].id)}return;
    }
    if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='z'&&writable){e.preventDefault();e.shiftKey?layoutDraft.current?.redo():layoutDraft.current?.undo();return}

    if (e.key === ' ') { e.preventDefault(); void selectNode(node.id); return; }
    if (e.key === 'Enter') { e.preventDefault(); if (viewNode?.path && !viewNode.missing) onOpenDoc(node.id); return; }
    if (e.key === 'Tab' && writable) {
      e.preventDefault();
      const pos = availablePosition(e.shiftKey ? siblingPosition(node) : childPosition(node),canvas.nodes);
      const parents = e.shiftKey ? canvas.edges.filter((ed) => ed.to === node.id).map((ed) => ed.from) : [node.id];
      setNewTitle(''); setNewNode({ x: pos.x, y: pos.y, parent: e.shiftKey ? undefined : node.id, siblings: e.shiftKey ? parents : undefined });
      return;
    }
    if (e.key === 'Delete' || e.key === 'Backspace' || e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10')) {
      e.preventDefault();
      const r = e.currentTarget.getBoundingClientRect();
      if (viewNode) setNodeMenu({ node: viewNode, x: r.left, y: r.bottom });
      return;
    }
  };

  // ---- structural operations (each flushes pending layout saves first) ----
  const runBusy = async (id: string, fn: () => Promise<void>) => {
    if (busyRef.current.has(id)) return;
    busyRef.current.add(id); setBusyIds((old) => new Set(old).add(id));
    setError('');
    try { await fn(); } catch (e) { setError(errMsg(e)); }
    finally { busyRef.current.delete(id); setBusyIds((old) => { const n = new Set(old); n.delete(id); return n; }); }
  };

  const createNode = async () => {
    if (!newNode || !canvas || !writable || createInFlight.current) return;
    const title = newTitle.trim();
    if (!title) return;
    createInFlight.current=true;setCreatingNode(true);setError('');
    try {
      await flushSave();
      const signature=JSON.stringify({space,mapPath,title,x:newNode.x,y:newNode.y});
      if(createRequest.current?.signature!==signature)createRequest.current={signature,key:randomIdemKey('mindmap-node')};
      const key=createRequest.current.key;
      const created = await api.createMindMapNode(space, mapPath, { title, x: snapCoord(newNode.x), y: snapCoord(newNode.y) }, key);
      const before = new Set(canvas.nodes.map((n) => n.id));
      const added = created.nodes.find((n) => !before.has(n.id));
      let final = created;
      const links = newNode.parent ? [newNode.parent] : (newNode.siblings || []);
      if (added && links.length) {
        const ids = new Set(created.config.edges.map((ed) => ed.id));
        const edges = [...created.config.edges, ...links.map((p) => { const id=newEdgeId(ids);ids.add(id);return {id,from:p,to:added.id,fromSide:newNode.fromSide} })];
        final = await api.updateMindMapConfig(space, mapPath, { edges }, created.revision);
      }
      applyServer(final);
      const surface=surfaceRef.current;
      if(added&&surface){const rect=surface.getBoundingClientRect(),w=rect.width/uiZoom(),h=rect.height/uiZoom(),size=nodeSize(added);if(added.x*view.scale+view.x<20||added.y*view.scale+view.y<20||(added.x+size.w)*view.scale+view.x>w-20||(added.y+size.h)*view.scale+view.y>h-20){const fit=fitView(final.config.nodes,w,h);if(fit)setView(fit)}}
      createRequest.current=null;setNewNode(null); setNewTitle(''); setOperationWarnings(final.warnings || []);
      if (added) setFocusNode(added.id);
      onChanged();
    } catch (e) { setError(errMsg(e)); }finally{createInFlight.current=false;setCreatingNode(false)}
  };

  const linkDocument = async (docId: string) => {
    if (!writable) return;
    try {
      await flushSave();
      const center=centerWorld(),pos=availablePosition({x:center.x-95,y:center.y-32},layoutDraft.current?.canvas.nodes||[]);
      const m = await api.addMindMapReference(space, mapPath, { docId, x: snapCoord(pos.x), y: snapCoord(pos.y) });
      applyServer(m); setPicker(null); setPickQuery(''); setOperationWarnings(m.warnings || []); setFocusNode(docId);
      onChanged();
    } catch (e) { setError(errMsg(e)); }
  };

  const removeNode = async (nodeId: string, trash: boolean) => {
    if (!map || !writable) return;
    await runBusy(nodeId, async () => {
      await flushSave();
      if(!await flushPreview())throw new Error("Save or export the document draft before removing its node.");
      const m = await api.deleteMindMapNode(space, mapPath, { nodeId, trash }, revisionRef.current);
      applyServer(m);
      if (previewId === nodeId) { selection.current++; draftRef.current = null; setPreviewDraft(null); setPreviewId(''); setPreview(null); }
      setSelected(null); setOperationWarnings(m.warnings || []);
      onChanged();
    });
  };

  const moveDocToMap = async (node: MindMapNodeView) => {
    if (!map || !writable || !node.path) return;
    await runBusy(node.id, async () => {
      await flushSave();
      await onFlushDraft(node.id);
      const fresh = await api.getDocument(space, node.id);
      if (fresh.path !== node.path) throw new Error('This document moved elsewhere. Reload the canvas before moving it again.');
      const res = await api.moveDocIntoMindMap(space, mapPath, { docId: node.id, fromPath: fresh.path }, fresh.revision);
      applyServer(res.mindMap); setOperationWarnings(res.warnings || []);
      onInvalidateDoc(node.id); onChanged();
    });
  };

  const collectFiles = async (items: { id: string; path: string }[], nodeId: string) => {
    if (!map || !writable) return;
    for (const item of items) {
      await runBusy(item.id, async () => {
        await flushSave();
        await onFlushDraft(nodeId);
        await onFlushDraft(item.id);
        const fresh = await api.getDocument(space, item.id);
        if (fresh.path !== item.path) throw new Error(`${item.path} moved elsewhere. Reload before moving it again.`);
        const res = await api.moveFileIntoMindMapNode(space, mapPath, { nodeId, docId: item.id, fromPath: fresh.path }, fresh.revision);
        applyServer(res.mindMap); setOperationWarnings(res.warnings || []);
        onInvalidateDoc(item.id); onInvalidateDoc(nodeId); onChanged();
      });
    }
    setConfirm(null);
  };

  // Drops add portable Markdown links; source files never move implicitly.
  const acceptsDrop=(e:React.DragEvent)=>writable&&e.dataTransfer.types.includes(DRAG_KEY);
  const handleSurfaceDrop=async(e:React.DragEvent)=>{
    if(!acceptsDrop(e))return;e.preventDefault();e.stopPropagation();setDropNode('');
    const payload=parseDragPayload(e.dataTransfer.getData(DRAG_KEY));if(!payload||!canvas||!map)return;
    if(payload.folders.length){setError('Drop individual files to link them. Folders remain in the explorer.');return}
    const target=(e.target as Element).closest<HTMLElement>('[data-node-id]')?.dataset.nodeId;
    const files=[...payload.docs.flatMap(id=>{const d=docs.find(d=>d.id===id&&!d.tombstoneAt);return d?[{docId:d.id,attachmentPath:undefined as string|undefined,title:d.title||d.path.split('/').pop()!,path:d.path}]:[]}),...payload.atts.filter(path=>attachments.some(a=>a.path===path)).map(path=>({docId:undefined as string|undefined,attachmentPath:path,title:path.split('/').pop()!,path}))];
    const pos=worldPoint(e.clientX,e.clientY,surfaceRef.current!.getBoundingClientRect(),view,uiZoom());
    await runBusy('drop',async()=>{
      await flushSave();let m=await api.getMindMap(space,mapPath);applyServer(m);
      for(const file of files){
        if(target===file.docId)continue;
        let id=target;const signature=JSON.stringify([target,file.path]);
        if(!id){
          let pending=pendingDrops.current.get(signature);if(!pending){pending={key:randomIdemKey('mindmap-drop')};pendingDrops.current.set(signature,pending)}
          if(pending.nodeId&&m.nodes.some(n=>n.id===pending.nodeId))id=pending.nodeId;
          else{
            const location=availablePosition(pos,m.config.nodes),before=new Set(m.nodes.map(n=>n.id));
            const base=file.title.replace(/\.md$/i,'').replace(/[\\/:*?"<>|%\x00-\x1f]/g,' ').trim()||'Linked file';
            let title=base,number=2;while(docs.some(d=>d.path.toLowerCase()===`${mapPath}/${title}.md`.toLowerCase())||m.nodes.some(n=>n.path?.toLowerCase()===`${mapPath}/${title}.md`.toLowerCase()))title=`${base} ${number++}`;
            m=await api.createMindMapNode(space,mapPath,{title,x:snapCoord(location.x),y:snapCoord(location.y)},pending.key);applyServer(m);
            id=m.nodes.find(n=>!before.has(n.id))?.id;if(!id)throw new Error('The linked card was created. Reload the map before retrying.');pending.nodeId=id;
          }
        }
        await onFlushDraft(id);const note=await api.getDocument(space,id);
        m=await api.linkFileToMindMapNode(space,mapPath,{nodeId:id,docId:file.docId,attachmentPath:file.attachmentPath},note.revision);
        onInvalidateDoc(id);applyServer(m);pendingDrops.current.delete(signature);onChanged();
      }
    });
  };

  // ---- derived render data ----
  const nodes = canvas?.nodes ?? [];
  const edges = canvas?.edges ?? [];
  const nodeById = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const mapMD = useMemo(() => docs.find((d) => !d.tombstoneAt && d.path === mapPath + '/mindmap.md'), [docs, mapPath]);
  const pickerCandidates = useMemo(() => {
    if (!picker) return [];
    const onCanvas = new Set(nodes.map((n) => n.id));
    const q = pickQuery.trim().toLowerCase();
    return docs.filter((d) => !d.tombstoneAt && (d.kind === 'note'||d.kind==='memory') && d.path !== mapPath + '/mindmap.md' &&
      (picker.mode === 'collect' ? d.id !== picker.nodeId : !onCanvas.has(d.id)) &&
      (!q || d.title.toLowerCase().includes(q) || d.path.toLowerCase().includes(q))).slice(0, 30);
  }, [picker, pickQuery, docs, nodes, mapPath]);

  if (!map && loading) return <div className="mindmap-view"><div className="mindmap-header"><h1>Loading mind map…</h1></div></div>;
  if (!map) return <div className="mindmap-view"><div className="mindmap-banner" role="alert">{error || 'Mind map could not be loaded.'}</div><div className="mindmap-header"><button className="btn" onClick={() => { setLoading(true); void reload(); }}>Retry</button></div></div>;

  const statusText = { saved: 'Saved', saving: 'Saving…', unsaved: 'Unsaved changes', conflict: 'Save conflict', error: 'Save failed' }[saveState];

  return <div className="mindmap-view">
    <header className="mindmap-header">
      <div className="mindmap-heading">
        <h1>{writable?<button className="mindmap-title-button" title="Rename mind map" onClick={()=>{setMapTitle(map.title);setRenaming(true)}}>{map.title}<Icon name="edit" size={12}/></button>:map.title}</h1>
        <div className="mindmap-sub">
          <button className="mindmap-path" title="Mind map folder — reveal it in the explorer" onClick={() => onRevealFolder(map.path)}><Icon name="folder" size={13}/>{map.path}</button>
          {mapMD && <button className="mindmap-config" title="mindmap.md holds this canvas configuration" onClick={() => onOpenDoc(mapMD.id)}><Icon name="settings" size={13}/>Configuration</button>}
          <span className="mindmap-hint">Click previews · Double-click opens · Tab child · Shift+Tab sibling · Drag documents to link, drop on a node to link files</span>
        </div>
      </div>
      <div className="spacer"/>
      <span className={'mindmap-save ' + saveState} role="status">{statusText}</span>
      <div className="mindmap-tools" role="toolbar" aria-label="Mind map tools">
      {writable && <Tool caption="Add idea" icon="plus" label="Add node" onClick={() => { const c = centerWorld(); setNewTitle(''); setNewNode(availablePosition({x:c.x-95,y:c.y-32},nodes)); }}/>}
      {writable && <Tool caption="Link note" icon="link" label="Link an existing document" onClick={() => { setPicker({ mode: 'link' }); setPickQuery(''); }}/>}
      {writable&&<><Tool icon="undoArrow" label="Undo canvas edit" disabled={!layoutDraft.current?.canUndo} onClick={()=>layoutDraft.current?.undo()}/><Tool icon="redoArrow" label="Redo canvas edit" disabled={!layoutDraft.current?.canRedo} onClick={()=>layoutDraft.current?.redo()}/></>}
      <Tool icon="minus" label="Zoom out" onClick={() => zoomAtCenter(1 / 1.2)}/>
      <button className="mindmap-zoom" title="Zoom level" onClick={fit}>{Math.round(view.scale * 100)}%</button>
      <Tool icon="plus" label="Zoom in" onClick={() => zoomAtCenter(1.2)}/>
      {writable&&<Tool icon="graph" label="Arrange branches" disabled={!nodes.length} onClick={()=>{queueSave({nodes:arrangeMindMap(nodes,edges),edges});requestAnimationFrame(()=>{const el=surfaceRef.current;if(el){const r=el.getBoundingClientRect();const fitted=fitView(arrangeMindMap(nodes,edges),r.width/uiZoom(),r.height/uiZoom());if(fitted)setView(fitted)}})}}/>}
      {writable&&<Tool icon="link" label="Connect selected idea" disabled={selected?.kind!=='node'} active={!!connectFrom} onClick={()=>{if(selected?.kind==='node'){updateConnection(null);setConnectFrom(selected.id)}}}/>}
      {!!edges.length&&<select className="mindmap-connector-select" aria-label="Select connector" value={selected?.kind==='edge'?selected.id:''} onChange={e=>setSelected(e.target.value?{kind:'edge',id:e.target.value}:null)}><option value="">Connections ({edges.length})</option>{edges.map(edge=><option key={edge.id} value={edge.id}>{mapNode(edge.from)?.title||'Missing idea'} → {mapNode(edge.to)?.title||'Missing idea'}</option>)}</select>}
      <Tool icon="expand" label="Fit canvas to content" onClick={fit}/>
      <Tool icon="history" label="Reload mind map" onClick={() => { setLoading(true); void reload(); }}/></div>
    </header>
    {(connectFrom||connection) && <div className="mindmap-connect-hint" role="status">Choose a node, or release on the canvas to add an idea. <button className="btn small" onClick={()=>{setConnectFrom('');updateConnection(null);connectionStart.current=null}}>Cancel</button></div>}
    {error && <div className="mindmap-banner" role="alert">{error}</div>}
    {operationWarnings.length > 0 && <div className="mindmap-banner warning" role="alert">{operationWarnings.join(' · ')}</div>}
    {map.warnings?.length ? <div className="mindmap-banner warning">{map.warnings.join(' · ')}</div> : null}
    {saveState === 'conflict' && <div className="mindmap-banner" role="alert"><span>{saveError || 'The canvas changed elsewhere. Your layout is retained locally.'}</span><div><button className="btn small" onClick={retrySave}>Save my version</button><button className="btn small" onClick={adoptRemote}>Load theirs</button></div></div>}
    {saveState === 'error' && <div className="mindmap-banner" role="alert"><span>{saveError}</span><button className="btn small" onClick={retrySave}>Retry save</button></div>}
    <div className="mindmap-content" ref={content} style={{ '--map-preview-width': effectiveWidth + '%', '--map-preview-height':height+'%' } as React.CSSProperties}>
      <div className={'mindmap-surface'+(connection?' connecting':'')} ref={surfaceRef} role="application" aria-label="Mind map canvas" style={{backgroundPosition:`${view.x}px ${view.y}px`,backgroundSize:`${24*view.scale}px ${24*view.scale}px`}}
        onPointerDown={onSurfacePointerDown} onPointerMove={onSurfacePointerMove} onPointerUp={onSurfacePointerUp} onPointerCancel={onSurfacePointerUp}
        onDoubleClick={(e) => { if (!writable || connectionRef.current || (e.target as HTMLElement).closest('[data-node-id]')) return; const el = surfaceRef.current!; const rect = el.getBoundingClientRect(); const p = worldPoint(e.clientX, e.clientY, rect, view,uiZoom()); setNewTitle(''); setNewNode({ x: p.x - 95, y: p.y - 32 }); }}
        onDragOver={(e) => { if (!acceptsDrop(e)) return; e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; }}
        onDrop={(e) => void handleSurfaceDrop(e)}>
        <div className="mindmap-world" style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}>
          <svg className="mindmap-edges" aria-hidden="true"><defs><marker id="mindmap-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="var(--accent)"/></marker></defs>
            {edges.map((edge) => {
              const a = nodeById.get(edge.from), b = nodeById.get(edge.to);
              if (!a || !b) return null;
              const as = nodeSize(a), bs = nodeSize(b);
              const pa = edge.fromSide?sidePoint(a,edge.fromSide,4):borderPoint(a, { x: b.x + bs.w / 2, y: b.y + bs.h / 2 });
              const pb = edge.toSide?sidePoint(b,edge.toSide,4):borderPoint(b, { x: a.x + as.w / 2, y: a.y + as.h / 2 });
              const sel = selected?.kind === 'edge' && selected.id === edge.id;
              const mid = { x: (pa.x + pb.x) / 2, y: (pa.y + pb.y) / 2 };
              return <g key={edge.id} className={'mindmap-edge' + (sel ? ' selected' : '') + (edge.dashed ? ' dashed' : '')}>
                <path d={curvePath(pa, pb)} className="mindmap-edge-hit" onClick={(e) => { e.stopPropagation(); setSelected({ kind: 'edge', id: edge.id }); }}/>
                <path d={curvePath(pa, pb)} className="mindmap-edge-line" markerEnd="url(#mindmap-arrow)"/>
                {edge.label ? <text x={mid.x} y={mid.y - 6} className="mindmap-edge-label" textAnchor="middle">{edge.label}</text> : null}
              </g>;
            })}
            {connection&&(()=>{const source=nodeById.get(connection.from);if(!source)return null;const target=connection.target?nodeById.get(connection.target):undefined;const a=sidePoint(source,connection.side,4),b=target?(connection.targetSide?sidePoint(target,connection.targetSide,4):borderPoint(target,a)):connection.point;return <path d={curvePath(a,b)} className="mindmap-connection-ghost" markerEnd="url(#mindmap-arrow)"/>})()}
          </svg>
          {nodes.map((node) => {
            const viewNode = mapNode(node.id);
            const size = nodeSize(node);
            const sel = selected?.kind === 'node' && selected.id === node.id;
            return <div key={node.id} role="button" tabIndex={0} data-node-id={node.id}
              className={'mindmap-node' + (sel ? ' selected' : '') + (size.h<96?' card-tiny':'') + (size.h>=140?' card-expanded':'') + (connection?.target===node.id?' connection-target':'') + (resizing===node.id?' resizing':'') + (dragNode === node.id ? ' dragging' : '') + (dropNode === node.id ? ' drop' : '') + (busyIds.has(node.id) ? ' busy' : '') + (viewNode?.missing ? ' missing' : '') + (viewNode && !viewNode.owned ? ' reference' : '')}
              style={{ left: node.x, top: node.y, width: size.w, height: size.h, ['--node-color' as string]: node.color || 'var(--accent)' } as React.CSSProperties}
              aria-label={`${viewNode?.title || node.id}${viewNode?.owned ? '' : ' (reference)'}${viewNode?.missing ? ' (missing document)' : ''}`}
              title={viewNode?.path || 'Document unavailable'}
              onPointerDown={(e) => onNodePointerDown(e, node)}
              onClick={(e) => { if (!(e.target as HTMLElement).closest('button,a,[data-resize],.mindmap-card-scroll')) void selectNode(node.id); }}
              onDoubleClick={(e) => { if (!(e.target as HTMLElement).closest('button,a,[data-resize],.mindmap-card-scroll') && viewNode?.path && !viewNode.missing && Date.now() >= draggedUntil.current) onOpenDoc(node.id); }}
              onContextMenu={(e) => { e.preventDefault(); e.stopPropagation(); if (viewNode) setNodeMenu({ node: viewNode, x: e.clientX, y: e.clientY }); }}
              onKeyDown={(e) => nodeKeys(e, node)}
              onDragOver={(e) => { if (!acceptsDrop(e)) return; e.preventDefault(); e.stopPropagation(); e.dataTransfer.dropEffect = 'copy'; setDropNode(node.id); }}
              onDragLeave={(e) => { if (e.currentTarget.contains(e.relatedTarget as Node | null)) return; setDropNode((d) => d === node.id ? '' : d); }}>
              <button className="mindmap-node-actions" aria-label={`Actions for ${viewNode?.title||'node'}`} onClick={e=>{e.stopPropagation();const r=e.currentTarget.getBoundingClientRect();if(viewNode)setNodeMenu({node:viewNode,x:r.left,y:r.bottom})}}><Icon name="more" size={16}/></button>
              <span className="mindmap-node-title">{viewNode?.title || node.id}</span>
              <span className="mindmap-node-path">{viewNode?.missing ? 'Document missing' : viewNode?.path || ''}</span>
              <MindMapCardContent space={space} id={node.id} revision={viewNode?.revision} epoch={epoch} docs={docs} attachments={attachments} expanded={size.h>=140} text={previewId===node.id?previewDraft?.text:undefined} onOpen={onOpenDoc}/>
              {writable&&<>{NODE_SIDES.map(side=><button key={side} className={'mindmap-port port-'+side} data-connection-side={side} aria-label={`Connect ${viewNode?.title||'node'} from ${side}`} title="Drag to connect · release on canvas to add an idea" onPointerDown={e=>startConnection(e,node.id,side)} onClick={e=>{e.stopPropagation();if(e.detail===0){if(connectionRef.current){const c=connectionRef.current;if(c.from!==node.id&&!edges.some(edge=>edge.from===c.from&&edge.to===node.id))queueSave({nodes,edges:[...edges,{id:newEdgeId(edges.map(edge=>edge.id)),from:c.from,to:node.id,fromSide:c.side,toSide:side}]});updateConnection(null)}else {setConnectFrom('');updateConnection({from:node.id,side,point:sidePoint(node,side)})}}}}/>)}</>}
              {writable&&<button className="mindmap-node-resize" data-resize aria-label={`Resize ${viewNode?.title||'node'}`} title="Drag to resize; arrow keys adjust size" onPointerDown={e=>resizeNode(e,node)} onClick={e=>e.stopPropagation()} onKeyDown={e=>{if(!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(e.key))return;e.preventDefault();e.stopPropagation();queueSave({nodes:nodes.map(n=>n.id===node.id?{...n,w:clamp(size.w+(e.key==='ArrowLeft'?-16:e.key==='ArrowRight'?16:0),160,1200),h:clamp(size.h+(e.key==='ArrowUp'?-16:e.key==='ArrowDown'?16:0),96,1200)}:n),edges})}}><Icon name="expand" size={12}/></button>}
              <span className="mindmap-node-badges">
                {viewNode && !viewNode.owned ? <Icon name="link" size={12}/> : null}
                {viewNode?.warnings?.length ? <Icon name="info" size={12}/> : null}
              </span>
            </div>;
          })}
          {!nodes.length && <div className="mindmap-empty"><Icon name="mindmap" size={38}/><h2>A little space for a big idea</h2><p>Add your first idea, then branch out.<br/>Your notes stay connected to the rest of your vault.</p>{writable&&<button className="btn primary" onClick={()=>{const c=centerWorld();setNewTitle('');setNewNode(availablePosition({x:c.x-95,y:c.y-32},nodes))}}><Icon name="plus" size={16}/>Add first idea</button>}<small>Double-click to add · Drag to arrange · Scroll to zoom</small></div>}
        </div>
      </div>
      {selected?.kind === 'edge' && <div className="mindmap-edge-inspector" role="group" aria-label="Connector actions">
        <input aria-label="Connector label" readOnly={!writable} maxLength={200} placeholder="Label…" value={edges.find((ed) => ed.id === selected.id)?.label || ''} onChange={(e) => { if (!writable) return; queueSave({ nodes, edges: edges.map((ed) => ed.id === selected.id ? { ...ed, label: e.target.value } : ed) }); }}/>
        <label><input type="checkbox" disabled={!writable} checked={!!edges.find((ed) => ed.id === selected.id)?.dashed} onChange={(e) => { if (!writable) return; queueSave({ nodes, edges: edges.map((ed) => ed.id === selected.id ? { ...ed, dashed: e.target.checked } : ed) }); }}/>Dashed</label>
        {writable && <button className="btn small danger" onClick={() => { queueSave({ nodes, edges: edges.filter((ed) => ed.id !== selected.id) }); setSelected(null); }}><Icon name="trash" size={13}/>Delete</button>}
        <button className="btn small" onClick={() => setSelected(null)}>Done</button>
      </div>}
      {previewId && <aside className="mindmap-preview" aria-label="Node preview">
        <div className="mindmap-preview-resizer" role="separator" tabIndex={0} aria-label="Resize node preview" aria-orientation={narrow?"horizontal":"vertical"} aria-valuemin={narrow?30:Math.round(minWidth)} aria-valuemax={narrow?80:60} aria-valuenow={Math.round(narrow?height:effectiveWidth)}
          onKeyDown={(e) => { if (!(narrow?['ArrowUp','ArrowDown','Home','End']:['ArrowLeft','ArrowRight','Home','End']).includes(e.key)) return; e.preventDefault(); changeSize(e.key==='Home'?(narrow?30:minWidth):e.key==='End'?(narrow?80:60):(narrow?height:effectiveWidth)+(['ArrowLeft','ArrowUp'].includes(e.key)?2:-2)); }}
          onPointerDown={(e) => { e.preventDefault(); e.currentTarget.focus(); e.currentTarget.setPointerCapture(e.pointerId); const rect = content.current!.getBoundingClientRect(); (e.currentTarget as HTMLElement).dataset.right = String(narrow?rect.bottom:rect.right); (e.currentTarget as HTMLElement).dataset.width = String(narrow?rect.height:rect.width); }}
          onPointerMove={(e) => { const el = e.currentTarget; if (!el.dataset.right) return; changeSize((Number(el.dataset.right) - (narrow?e.clientY:e.clientX)) / Number(el.dataset.width) * 100); }}
          onPointerUp={(e) => { delete (e.currentTarget as HTMLElement).dataset.right; }} onPointerCancel={(e) => { delete (e.currentTarget as HTMLElement).dataset.right; }} onLostPointerCapture={(e) => { delete (e.currentTarget as HTMLElement).dataset.right; }}/>
        <header className="mindmap-preview-header">
          <div className="mindmap-preview-heading"><strong>{mapNode(previewId)?.title || preview?.title || 'Node'}</strong><small className="mindmap-path" title={preview?.path || mapNode(previewId)?.path}>{preview?.path || mapNode(previewId)?.path}</small><div className="mindmap-preview-meta">{mapNode(previewId)?.owned ? <span>Owned by this map</span> : <span>Referenced document</span>}{previewDraft && <small>{!previewDraft.canEdit ? 'Read only' : ({ saved: 'Saved', saving: 'Saving…', unsaved: 'Unsaved changes', error: 'Save failed', conflict: 'Save conflict' }[previewDraft.status])}</small>}</div></div>
          <Tool icon="close" label="Close node preview" onClick={() => void closePreview()}/>
        </header>
        {previewError && <div className="mindmap-banner" role="alert"><span>{previewError}</span><button className="btn small" onClick={() => setPreviewRetry((n) => n + 1)}>Retry preview</button>{previewDraft && <button className="btn small" onClick={() => exportDraft(preview?.path || 'node.md', previewDraft.text)}>Export draft</button>}</div>}
        {previewDraft && (previewDraft.status === 'error' || previewDraft.status === 'conflict') && <div className="save-error" role="alert"><span>{previewDraft.error}</span><button className="btn small" onClick={() => previewDraft.status === 'conflict' ? void onResolveConflict(previewId) : void previewDraft.flush()}>{previewDraft.status === 'conflict' ? 'Resolve conflict' : 'Retry save'}</button><button className="btn small" onClick={() => exportDraft(preview?.path || 'node.md', previewDraft.text)}>Export draft</button></div>}
        {!previewDraft && !previewError && <p className="muted" role="status">Loading node…</p>}
        {documentsInNodeFolder(docs,map.path,previewId).length>0&&<div className="mindmap-collected"><strong>Files in this idea</strong>{documentsInNodeFolder(docs,map.path,previewId).map(d=><button className="mindmap-collected-file" key={d.id} onClick={()=>onOpenDoc(d.id)}><Icon name="file" size={14}/>{d.title||d.path.split('/').pop()}</button>)}</div>}
        {previewDraft && <div className="mindmap-preview-body" inert={busyIds.has(previewId)} aria-busy={busyIds.has(previewId)}>
          {previewMode==='source'?<><div className="board-preview-mode"><span>Markdown source</span><button className="btn small" onClick={()=>setPreviewMode('visual')}>Visual editor</button></div><NoteEditor draft={previewDraft} docs={docs} notePath={preview?.path||''} onUploadImages={onUploadImages?files=>onUploadImages(files,preview?.path||''):undefined} onImageError={setPreviewError}/></>:<Suspense fallback={<p className="muted" role="status">Loading Visual editor…</p>}><VisualEditor compact onSourceMode={()=>setPreviewMode('source')} draft={previewDraft} onUploadImages={onUploadImages?(files)=>onUploadImages(files,preview?.path||mapNode(previewId)?.path||''):undefined} docs={docs} notePath={preview?.path || mapNode(previewId)?.path || ''} attachments={attachments.map((a) => ({ path: a.path, url: api.attachmentURL(space, a.path) }))}/></Suspense>}
        </div>}
      </aside>}
    </div>
    {nodeMenu && <div className="mindmap-menu-backdrop" onClick={() => setNodeMenu(null)} onContextMenu={(e) => { e.preventDefault(); e.stopPropagation(); setNodeMenu(null); }}>
      <div className="mindmap-menu" role="menu" style={overlayPoint(nodeMenu.x,nodeMenu.y,260,320)} onClick={(e) => e.stopPropagation()} onKeyDown={e=>{if(!['ArrowDown','ArrowUp','Home','End'].includes(e.key))return;e.preventDefault();const items=[...e.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')],index=items.indexOf(document.activeElement as HTMLButtonElement);items[e.key==='Home'?0:e.key==='End'?items.length-1:(index+(e.key==='ArrowDown'?1:items.length-1))%items.length]?.focus()}}>
        <button role="menuitem" disabled={!nodeMenu.node.path || nodeMenu.node.missing} onClick={() => { const id = nodeMenu.node.id; setNodeMenu(null); onOpenDoc(id); }}>Open document</button>
        {writable && <button role="menuitem" onClick={()=>{setConnectFrom(nodeMenu.node.id);setNodeMenu(null)}}>Connect to another node…</button>}
        {writable && <div className="mindmap-menu-colors" role="group" aria-label="Node color">
          {PALETTE.map((c) => <button key={c || 'none'} role="menuitemradio" aria-checked={(nodeMenu.node.color || '') === c} aria-label={c ? 'Color ' + c : 'No color'} className={'mindmap-swatch' + ((nodeMenu.node.color || '') === c ? ' active' : '')} style={c ? { background: c } : undefined} onClick={() => { if (!canvas) return; queueSave({ nodes: canvas.nodes.map((n) => n.id === nodeMenu.node.id ? { ...n, color: c } : n), edges: canvas.edges }); setNodeMenu(null); }}/>)}
        </div>}
        {writable && nodeMenu.node.path && !nodeMenu.node.owned && !nodeMenu.node.path.startsWith(map.path + '/') && <button role="menuitem" onClick={() => { const node = nodeMenu.node; setNodeMenu(null); setConfirm({ kind: 'moveToMap', node, target: map.path + '/' + (node.path?.split('/').pop() || '') }); }}>Move file into this map folder…</button>}
        {writable && !nodeMenu.node.missing && <button role="menuitem" onClick={() => { const id = nodeMenu.node.id; setNodeMenu(null); setPicker({ mode: 'collect', nodeId: id }); setPickQuery(''); }}>Move another file into this node…</button>}
        <button role="menuitem" onClick={() => { void navigator.clipboard?.writeText(nodeMenu.node.path || '').catch(() => {}); setNodeMenu(null); }}>Copy path</button>
        {writable && <button role="menuitem" onClick={() => { const id = nodeMenu.node.id; setNodeMenu(null); void removeNode(id, false); }}>Remove from canvas</button>}
        {writable && nodeMenu.node.owned && !nodeMenu.node.missing && <button role="menuitem" className="danger" onClick={() => { const node = nodeMenu.node; setNodeMenu(null); setConfirm({ kind: 'trash', node }); }}>Move document to trash…</button>}
      </div>
    </div>}
    {renaming&&<Dialog title="Rename mind map" close={()=>setRenaming(false)}><label className="field"><span>Name</span><input data-autofocus value={mapTitle} onChange={e=>setMapTitle(e.target.value)}/></label><footer className="dialog-actions"><button className="btn" onClick={()=>setRenaming(false)}>Cancel</button><button className="btn primary" disabled={!mapTitle.trim()} onClick={()=>{void (async()=>{try{await flushSave();applyServer(await api.updateMindMapConfig(space,mapPath,{title:mapTitle.trim()},revisionRef.current));setRenaming(false);onChanged()}catch(e){setError(errMsg(e))}})()}}>Save name</button></footer></Dialog>}
    {newNode && <Dialog title={newNode.parent ? 'New child node' : 'New node'} close={() => {if(!creatingNode)setNewNode(null)}}>
      <p className="muted">A node is an ordinary Markdown note inside <code>{map.path}</code>. {newNode.parent||newNode.siblings?.length?'It will be connected to the selected branch.':'Arrange it freely or connect it to another node.'}</p>
      {error&&<p className="mindmap-dialog-error" role="alert">{error}</p>}
      <label className="field"><span>Node title</span><input autoFocus value={newTitle} onChange={(e) => setNewTitle(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void createNode(); } }} placeholder="New node…"/></label>
      <footer className="dialog-actions"><button className="btn" onClick={() => setNewNode(null)}>Cancel</button><button className="btn primary" disabled={!newTitle.trim()||creatingNode} onClick={() => void createNode()}>{creatingNode?'Creating…':'Create node'}</button></footer>
    </Dialog>}
    {picker && <Dialog title={picker.mode === 'link' ? 'Link an existing document' : 'Move a file into this node'} close={() => setPicker(null)}>
      {error&&<p className="mindmap-dialog-error" role="alert">{error}</p>}
    <input autoFocus aria-label="Search documents" placeholder="Search documents…" value={pickQuery} onChange={(e) => setPickQuery(e.target.value)}/>
      <ul className="mindmap-picker-list" role="listbox" aria-label="Documents">
        {pickerCandidates.map((d) => <li key={d.id}><button role="option" aria-selected="false" onClick={() => { if (picker.mode === 'link') void linkDocument(d.id); else { setPicker(null); setConfirm({ kind: 'collect', nodeId: picker.nodeId!, docs: [{ id: d.id, path: d.path, target: mindMapFileTarget(map.path, picker.nodeId!, d.path) }] }); } }}>
          <strong>{d.title}</strong><small>{d.path}</small></button></li>)}
        {!pickerCandidates.length && <li className="muted">No matching documents.</li>}
      </ul>
      <footer className="dialog-actions"><button className="btn" onClick={() => setPicker(null)}>Cancel</button></footer>
    </Dialog>}
    {confirm?.kind === 'trash' && <Dialog title="Move node document to trash" close={() => setConfirm(null)}>
      <p>Trash <strong>{confirm.node.title}</strong> (<code>{confirm.node.path}</code>)? The document goes to the vault trash with normal retention, and the node leaves the canvas.</p>
      <footer className="dialog-actions"><button className="btn" onClick={() => setConfirm(null)}>Cancel</button><button className="btn danger" onClick={() => { const id = confirm.node.id; setConfirm(null); void removeNode(id, true); }}>Move to trash</button></footer>
    </Dialog>}
    {confirm?.kind === 'moveToMap' && <Dialog title="Move file into the map folder" close={() => setConfirm(null)}>
      <p>Move <code>{confirm.node.path}</code> into <code>{confirm.target}</code>? The document keeps its identity, links and history; only its folder changes.</p>
      <footer className="dialog-actions"><button className="btn" onClick={() => setConfirm(null)}>Cancel</button><button className="btn primary" onClick={() => { const node = confirm.node; setConfirm(null); void moveDocToMap(node); }}>Move file</button></footer>
    </Dialog>}
    {confirm?.kind === 'collect' && <Dialog title="Move files into node folder" close={() => setConfirm(null)}>
      <p>Files are filed under <code>{mindMapNodeFolder(map.path, confirm.nodeId)}</code> so the node document collects them. Canvas membership does not change.</p>
      <ul className="mindmap-move-list">{confirm.docs.map((d) => <li key={d.id}><code>{d.path}</code> → <code>{d.target}</code></li>)}</ul>
      {documentsInNodeFolder(docs, map.path, confirm.nodeId).length > 0 && <p className="muted">Already in this node folder: {documentsInNodeFolder(docs, map.path, confirm.nodeId).map((d) => d.path.split('/').pop()).join(', ')}</p>}
      <footer className="dialog-actions"><button className="btn" onClick={() => setConfirm(null)}>Cancel</button><button className="btn primary" onClick={() => void collectFiles(confirm.docs.map((d) => ({ id: d.id, path: d.path })), confirm.nodeId)}>Move {confirm.docs.length === 1 ? 'file' : `${confirm.docs.length} files`}</button></footer>
    </Dialog>}
  </div>;
}
