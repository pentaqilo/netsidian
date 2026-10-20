// MindMapView: the mind map canvas tab. Nodes are ordinary Markdown documents
// (owned inside the map folder or referenced from elsewhere), connectors and
// placement live in mindmap.md. Canvas edits autosave with If-Match. File
// drops add ordinary Markdown links; moving files is a separate explicit action.

import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import * as api from '../api/client';
import type { MindMapEdgeConfig, MindMapNodeConfig, MindMapNodeView } from '../api/client';
import type { DocMeta, NoteContent } from '../api/types';
import type { Draft, DraftDocument } from '../lib/drafts';
import { DRAG_KEY, parseDragPayload } from './FileTree';
import {CanvasNavigation} from './CanvasNavigation';
import './mindMapTypography.css';
import {MindMapCardContent} from './MindMapCardContent';
import {NoteEditor} from './NoteEditor';
import { Dialog } from './Dialog';
import { Icon, Tool } from './Icon';
import {uiZoom,overlayPoint} from '../lib/overlayPosition';
import {MindMapDraft,sharedMindMapDraft} from '../lib/mindmapDraft';
import { bindGraphZoom } from '../lib/graphZoom';
import { randomIdemKey } from '../lib/boards';
import {
  availablePosition, arrangeMindMap, borderPoint, sidePoint, NODE_SIDES, MIN_ZOOM, MAX_ZOOM, type NodeSide, childPosition, clampZoom, curvePath, documentsInNodeFolder,
  edgeAutoPanVelocity, effectivePanBounds, uncoveredCanvasBounds, minimalRevealPan, type CanvasBounds,
  fitView,
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

export function MindMapView({ space, mapPath, writable, docs, attachments = [], epoch, principal, onPreviewDraft, onResolveConflict, onOpenDoc, onFlushDraft, onInvalidateDoc, onChanged, onRevealFolder, onUploadImages, previewActivation = 'double', inlineEditing = true, minimapDefault = false }: {
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
  // Interaction preferences (Settings → Interaction): which gesture opens a
  // node's side preview, whether cards can be edited in place, and whether
  // the minimap starts open.
  previewActivation?: 'single' | 'double';
  inlineEditing?: boolean;
  minimapDefault?: boolean;
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
  // `reconnect` marks an in-progress endpoint move for an existing edge: the
  // fixed endpoint lives in from/side and the move keeps the edge id and label.
  const [connection,setConnection]=useState<{from:string;side:NodeSide;point:{x:number;y:number};target?:string;targetSide?:NodeSide;reconnect?:{edgeId:string;end:'from'|'to'}}|null>(null);
  const connectionRef=useRef<typeof connection>(null);
  const connectionStart=useRef<{x:number;y:number;moved:boolean}|null>(null);
  const nodeResize=useRef<{id:string;px:number;py:number;w:number;h:number}|null>(null);
  const [resizing,setResizing]=useState('');
  const pendingDrops=useRef(new Map<string,{key:string;nodeId?:string}>());
  const updateConnection=(next:typeof connection)=>{connectionRef.current=next;setConnection(next)};
  useEffect(()=>{const cancel=(e:KeyboardEvent)=>{if(e.key!=='Escape')return;if(nodeDrag.current){e.preventDefault();e.stopImmediatePropagation();cancelCanvasGesture();return;}if(connectionRef.current||connectFrom){e.preventDefault();e.stopImmediatePropagation();stopAutoPan();gesturePointer.current=null;updateConnection(null);connectionStart.current=null;setConnectFrom('')}};document.addEventListener('keydown',cancel,true);return()=>document.removeEventListener('keydown',cancel,true)},[connectFrom]);

  const [view, setView] = useState<Viewport>({ x: 0, y: 0, scale: 1 });
  const [surfaceSize,setSurfaceSize]=useState({w:0,h:0});
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

  // A click selects; a header double-click (or Space) explicitly opens the preview.
  const [previewId, setPreviewId] = useState('');
  // Bumped when a previewed node document finishes saving so its mini-card
  // refetches the fresh Markdown even though the canvas revision never changed.
  const [docEpoch, setDocEpoch] = useState<Record<string, number>>({});
  const [preview, setPreview] = useState<NoteContent | null>(null);
  const [previewError, setPreviewError] = useState('');
  const [previewDraft, setPreviewDraft] = useState<Draft | null>(null);
  const draftRef = useRef<Draft | null>(null), selection = useRef(0);
  const [, renderDraft] = useState(0);
  const [previewMode,setPreviewMode]=useState<'visual'|'source'>('visual');
  const [previewRetry, setPreviewRetry] = useState(0);
  // Inline text editing: the node card hosts the very same shared Draft the
  // preview uses, so both views edit one document with one persistence owner.
  const [editingId, setEditingId] = useState('');
  const [editDraft, setEditDraft] = useState<Draft | null>(null);
  const editingRef = useRef(''), editDraftRef = useRef<Draft | null>(null);
  const inlineFlush = useRef<(() => string | null) | null>(null), previewFlush = useRef<(() => string | null) | null>(null);
  const editRequest=useRef(0),[editError,setEditError]=useState('');
  const inlineHost=useRef<HTMLDivElement>(null),inlineBlurTimer=useRef<ReturnType<typeof setTimeout>|undefined>(undefined);
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
    const guard=(e:BeforeUnloadEvent)=>{const protectedEdit=inlineFlush.current?.()===null||previewFlush.current?.()===null;if(protectedEdit||layoutDraft.current?.dirty||draftRef.current?.dirty||editDraftRef.current?.dirty){e.preventDefault();e.returnValue=''}};
    window.addEventListener('beforeunload',guard);
    return()=>{window.removeEventListener('beforeunload',guard);unsubscribeLayout.current?.();layoutDraft.current?.dispose();layoutDraft.current=null};
  },[]);

  // ---- viewport: pan, wheel/pinch zoom, fit ----
  const surfaceRef = useRef<HTMLDivElement>(null),previewPane=useRef<HTMLElement>(null);
  const revealPreviewRef=useRef<()=>void>(()=>{});
  // The board is fit to the container exactly once, when the map first lays out.
  // Every later container change (preview open/close/resize, window resize) is a
  // "later ResizeObserver change" and must leave the camera and zoom untouched.
  const didFit = useRef(false);
  useEffect(() => {
    const el = surfaceRef.current; if (!el) return;
    return bindGraphZoom(el, (factor, anchor) => {
      if(nodeDrag.current||connectionRef.current)return;
      const rect = el.getBoundingClientRect();
      setView((v) => {
        const scale = clampZoom(v.scale * factor);
        const ax = (anchor.clientX - rect.left)/uiZoom(), ay = (anchor.clientY - rect.top)/uiZoom();
        return { scale, x: ax - (ax - v.x) * (scale / v.scale), y: ay - (ay - v.y) * (scale / v.scale) };
      });
    },target=>target instanceof Element&&!!target.closest('.canvas-navigation,.mindmap-node-edit,.mindmap-card-scroll'));
  }, [!!map]);
  const zoomAtCenter = (factor: number) => {
    const el = surfaceRef.current; if (!el) return;
    const rect = el.getBoundingClientRect();
    setView((v) => { const scale = clampZoom(v.scale * factor), ax = rect.width / (2*uiZoom()), ay = rect.height / (2*uiZoom()); return { scale, x: ax - (ax - v.x) * (scale / v.scale), y: ay - (ay - v.y) * (scale / v.scale) }; });
  };
  const fit = () => {
    const el = surfaceRef.current; if (!el || !canvas) return;
    const rect = el.getBoundingClientRect();
    const next = fitView(canvas.nodes, rect.width/uiZoom(), rect.height/uiZoom());
    if (next) setView(next);
  };
  useEffect(()=>{
    const el=surfaceRef.current;if(!el)return;let last={w:-1,h:-1};
    const resize=()=>{
      const rect=el.getBoundingClientRect(),w=rect.width/uiZoom(),h=rect.height/uiZoom();setSurfaceSize({w,h});
      if(w<=0||h<=0)return;
      if(Math.abs(w-last.w)<1&&Math.abs(h-last.h)<1)return;
      last={w,h};
      if(!didFit.current){
        const nodes=layoutDraft.current?.canvas.nodes||[];
        if(nodes.length){const fitted=fitView(nodes,w,h);if(fitted)setView(fitted)}
        didFit.current=true;
      }else revealPreviewRef.current();
    };
    resize();if(typeof ResizeObserver==='undefined')return;
    const observer=new ResizeObserver(resize);observer.observe(el);return()=>observer.disconnect();
  },[!!map]);
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
    if (connectionRef.current) { e.preventDefault(); finishConnection(e.clientX,e.clientY,!connectionRef.current.reconnect); return; }
    // Edge hit paths and node controls own their pointer; the background must
    // not swallow an edge click or clear the selection that click is about to make.
    if (!canvas?.nodes.length || e.button !== 0 || (e.target as Element).closest('[data-node-id],button,[role="menu"],[role="separator"],.mindmap-edge-hit,.mindmap-edge-handle')) return;
    panRef.current = { px: e.clientX, py: e.clientY, vx: view.x, vy: view.y };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setSelected(null); setNodeMenu(null);
  };
  const onSurfacePointerMove = (e: React.PointerEvent) => {
    if (nodeDrag.current || connectionRef.current) gesturePointer.current = { x: e.clientX, y: e.clientY };
    const c=connectionRef.current;
    if(c&&surfaceRef.current){
      const point=worldPoint(e.clientX,e.clientY,surfaceRef.current.getBoundingClientRect(),viewRef.current,uiZoom());
      const target=document.elementFromPoint(e.clientX,e.clientY)?.closest<HTMLElement>('[data-node-id]');
      const id=target?.dataset.nodeId,side=document.elementFromPoint(e.clientX,e.clientY)?.closest<HTMLElement>('[data-connection-side]')?.dataset.connectionSide as NodeSide|undefined;
      const start=connectionStart.current;if(start&&Math.hypot(e.clientX-start.x,e.clientY-start.y)>4)start.moved=true;
      updateConnection({...c,point,target:id&&id!==c.from?id:undefined,targetSide:side});ensureAutoPan();return;
    }
    const resize=nodeResize.current;
    if(resize&&canvas){const w=clamp(snapCoord(resize.w+(e.clientX-resize.px)/(view.scale*uiZoom())),160,1200),h=clamp(snapCoord(resize.h+(e.clientY-resize.py)/(view.scale*uiZoom())),96,1200);setCanvas({...canvas,nodes:canvas.nodes.map(n=>n.id===resize.id?{...n,w,h}:n)});return}
    const p = panRef.current;
    if (p) setView((v) => ({ ...v, x: p.vx + (e.clientX - p.px)/uiZoom(), y: p.vy + (e.clientY - p.py)/uiZoom() }));
    const d = nodeDrag.current;
    if (d && canvas && (d.moved || Math.abs(e.clientX - d.px) + Math.abs(e.clientY - d.py) > 3)) {
      if(!d.moved){window.getSelection()?.removeAllRanges();surfaceRef.current?.focus({preventScroll:true})}
      d.moved = true; setDragNode(d.id);
      const nodes = canvas.nodes.map((n) => n.id === d.id ? { ...n, x: snapCoord(d.ox + (e.clientX - d.px) / (view.scale*uiZoom())), y: snapCoord(d.oy + (e.clientY - d.py) / (view.scale*uiZoom())) } : n);
      const next={...canvas,nodes};canvasRef.current=next;setCanvas(next);
      ensureAutoPan();
    }
  };
  const onSurfacePointerUp = (e:React.PointerEvent) => {
    stopAutoPan(); gesturePointer.current = null;
    if(e.type==='pointercancel'){cancelCanvasGesture();return}
    const latest=canvasRef.current;
    if(connectionRef.current){if(connectionStart.current?.moved)finishConnection(e.clientX,e.clientY,!connectionRef.current.reconnect);connectionStart.current=null;return}
    if(nodeResize.current){nodeResize.current=null;setResizing('');draggedUntil.current=Date.now()+250;if(canvas&&writable)queueSave(canvas);return}
    panRef.current = null;
    const d = nodeDrag.current;
    nodeDrag.current = null; setDragNode('');
    if (d?.moved && latest) { draggedUntil.current = Date.now() + 250; if (writable) queueSave({ ...latest }); }
  };
  const onNodePointerDown = (e: React.PointerEvent, node: MindMapNodeConfig) => {
    // The header/frame (title, path, padding) is the defined drag surface;
    // buttons, links, the resize grip, the inline editor and the selectable card body own their
    // own pointer gestures and are excluded here.
    if (editingRef.current===node.id || connectionRef.current || !writable || e.button !== 0 || (e.target as HTMLElement).closest('button,a,.mindmap-card-scroll,.mindmap-node-edit,[data-resize]')) return;
    // Suppress native text dragging on the frame, but leave an existing
    // selection intact until the pointer crosses the move threshold.
    e.preventDefault();
    nodeDrag.current = { id: node.id, px: e.clientX, py: e.clientY, ox: node.x, oy: node.y, moved: false };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  };

  // ---- edge auto-pan while dragging a node or a connector ----
  // Same pattern as React Flow's calcAutoPan (inner edge band, proximity ramp,
  // rAF loop for the gesture's duration) with two refinements: speed is
  // px/second scaled by elapsed frame time so it is refresh-rate independent,
  // and it starts at exactly 0 on the band boundary so activation never jumps.
  // The dragged node / connection ghost stays glued to the pointer because
  // each pan shifts the drag reference by the same client-space delta. Camera
  // movement alone never touches the layout draft, so it cannot dirty or save
  // the canvas.
  const viewRef = useRef(view); viewRef.current = view;
  const canvasRef = useRef(canvas); canvasRef.current = canvas;
  const gesturePointer = useRef<{ x: number; y: number } | null>(null);
  const autoPanFrame = useRef<number | null>(null);
  const autoPanTime = useRef(0);
  const autoPanTick = useRef<(now: number) => void>(() => {});
  const stopAutoPan = () => { if (autoPanFrame.current !== null) { cancelAnimationFrame(autoPanFrame.current); autoPanFrame.current = null; } autoPanTime.current = 0; };
  const panGestureActive = () => (!!nodeDrag.current?.moved || !!connectionRef.current) && !nodeResize.current && !panRef.current;
  const panObstacles = (): CanvasBounds[] => {
    const out: CanvasBounds[] = [];
    const push = (el: Element | null) => { const b = el?.getBoundingClientRect(); if (b && b.width > 1 && b.height > 1) out.push({ left: b.left, top: b.top, right: b.right, bottom: b.bottom }); };
    surfaceRef.current?.querySelectorAll('.canvas-navigation,.mindmap-edge-inspector').forEach(push);
    document.querySelectorAll('.mindmap-preview,.canvas-minimap-popover').forEach(push);
    return out;
  };
  autoPanTick.current = (now: number) => {
    autoPanFrame.current = null;
    const surface = surfaceRef.current, p = gesturePointer.current;
    if (!surface?.isConnected || document.hidden || !panGestureActive() || !p) return;
    const rect = surface.getBoundingClientRect();
    if (rect.width < 1 || rect.height < 1) return;
    const bounds = effectivePanBounds({ left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom }, panObstacles(), p);
    if (!bounds) return;
    const v = edgeAutoPanVelocity(p, bounds);
    if (!v.x && !v.y) return; // pointer left the band: stop; the next pointermove restarts the loop
    const dt = autoPanTime.current ? Math.min(50, now - autoPanTime.current) : 16;
    autoPanTime.current = now;
    const z = uiZoom(), mx = -v.x * dt / 1000, my = -v.y * dt / 1000; // client px travelled this frame
    const current = viewRef.current;
    const next = { ...current, x: current.x + mx / z, y: current.y + my / z };
    viewRef.current = next; setView(next);
    const d = nodeDrag.current;
    if (d?.moved) {
      // Shift the drag reference with the camera so the node stays under the pointer.
      d.px += mx; d.py += my;
      const c = canvasRef.current;
      if (c) { const moved = { ...c, nodes: c.nodes.map(n => n.id === d.id ? { ...n, x: snapCoord(d.ox + (p.x - d.px) / (next.scale * z)), y: snapCoord(d.oy + (p.y - d.py) / (next.scale * z)) } : n) }; canvasRef.current = moved; setCanvas(moved); }
    }
    const conn = connectionRef.current;
    if (conn) {
      const point = worldPoint(p.x, p.y, rect, next, z);
      const el = document.elementFromPoint(p.x, p.y);
      const target = el?.closest<HTMLElement>('[data-node-id]')?.dataset.nodeId;
      const side = el?.closest<HTMLElement>('[data-connection-side]')?.dataset.connectionSide as NodeSide | undefined;
      updateConnection({ ...conn, point, target: target && target !== conn.from ? target : undefined, targetSide: side });
    }
    autoPanFrame.current = requestAnimationFrame((t) => autoPanTick.current(t));
  };
  const ensureAutoPan = () => { if (autoPanFrame.current === null && panGestureActive()) { autoPanTime.current = 0; autoPanFrame.current = requestAnimationFrame((t) => autoPanTick.current(t)); } };
  const cancelCanvasGesture=()=>{
    stopAutoPan();gesturePointer.current=null;panRef.current=null;
    const d=nodeDrag.current,r=nodeResize.current,c=canvasRef.current;
    nodeDrag.current=null;nodeResize.current=null;setDragNode('');setResizing('');
    if(c&&(d||r)){const restored={...c,nodes:c.nodes.map(n=>n.id===d?.id?{...n,x:d.ox,y:d.oy}:n.id===r?.id?{...n,w:r.w,h:r.h}:n)};canvasRef.current=restored;setCanvas(restored);draggedUntil.current=Date.now()+250}
    updateConnection(null);connectionStart.current=null;setConnectFrom('');
  };
  const cancelGestureRef=useRef(cancelCanvasGesture);cancelGestureRef.current=cancelCanvasGesture;
  useEffect(()=>{const hide=()=>{if(document.hidden)cancelGestureRef.current()};document.addEventListener('visibilitychange',hide);return()=>{document.removeEventListener('visibilitychange',hide);stopAutoPan();editRequest.current++}},[]);

  // Completes an in-progress connection. New connections are deduplicated and
  // never self-connected; reconnects keep the edge id/label and re-point only
  // the moving endpoint, rejecting duplicates against the other edges.
  const completeConnection=(targetId:string,targetSide?:NodeSide):boolean=>{
    const c=connectionRef.current,current=layoutDraft.current?.canvas;
    if(!c||!current||!writable)return false;
    if(c.reconnect){
      const edge=current.edges.find(ed=>ed.id===c.reconnect!.edgeId);if(!edge)return false;
      const other=c.reconnect.end==='from'?edge.to:edge.from;
      if(targetId===other||targetId===c.from)return false;
      const nextEdge={...edge};
      if(c.reconnect.end==='from'){nextEdge.from=targetId;nextEdge.fromSide=targetSide}else{nextEdge.to=targetId;nextEdge.toSide=targetSide}
      if(current.edges.some(ed=>ed.id!==edge.id&&ed.from===nextEdge.from&&ed.to===nextEdge.to))return false;
      queueSave({...current,edges:current.edges.map(ed=>ed.id===edge.id?nextEdge:ed)});
      return true;
    }
    if(targetId===c.from)return false;
    if(!current.edges.some(ed=>ed.from===c.from&&ed.to===targetId))queueSave({...current,edges:[...current.edges,{id:newEdgeId(current.edges.map(ed=>ed.id)),from:c.from,to:targetId,fromSide:c.side,toSide:targetSide}]});
    return true;
  };
  const beginReconnect=(edge:MindMapEdgeConfig,end:'from'|'to'):boolean=>{
    if(!writable)return false;
    const current=layoutDraft.current?.canvas;if(!current)return false;
    const fixedId=end==='from'?edge.to:edge.from;
    const fixed=current.nodes.find(n=>n.id===fixedId);if(!fixed)return false;
    const fixedSide=(end==='from'?edge.toSide:edge.fromSide)||'right';
    updateConnection({from:fixedId,side:fixedSide,point:sidePoint(fixed,fixedSide),reconnect:{edgeId:edge.id,end}});
    connectionStart.current=null;
    return true;
  };
  const startReconnect=(e:React.PointerEvent,edge:MindMapEdgeConfig,end:'from'|'to')=>{
    e.stopPropagation();e.preventDefault();if(!writable||e.button!==0)return;
    if(beginReconnect(edge,end)){connectionStart.current={x:e.clientX,y:e.clientY,moved:false};gesturePointer.current={x:e.clientX,y:e.clientY}}
    surfaceRef.current?.setPointerCapture(e.pointerId);
  };
  const finishConnection=(clientX:number,clientY:number,createOnEmpty:boolean)=>{
    const c=connectionRef.current,current=layoutDraft.current?.canvas;
    if(!c||!current||!writable){updateConnection(null);setConnectFrom('');return}
    const element=document.elementFromPoint(clientX,clientY),target=element?.closest<HTMLElement>('[data-node-id]')?.dataset.nodeId;
    const targetSide=element?.closest<HTMLElement>('[data-connection-side]')?.dataset.connectionSide as NodeSide|undefined;
    if(target)completeConnection(target,targetSide);
    else if(!c.reconnect&&createOnEmpty&&element&&!element.closest('.canvas-navigation,.mindmap-edge-inspector,.mindmap-preview')&&surfaceRef.current?.contains(element)){
      const pos=worldPoint(clientX,clientY,surfaceRef.current.getBoundingClientRect(),view,uiZoom());setNewTitle('');setNewNode({x:pos.x,y:pos.y,parent:c.from,fromSide:c.side});
    }
    updateConnection(null);setConnectFrom('');draggedUntil.current=Date.now()+250;
  };
  const startConnection=(e:React.PointerEvent,id:string,side:NodeSide)=>{
    e.stopPropagation();if(!writable||e.button!==0)return;
    if(connectionRef.current){finishConnection(e.clientX,e.clientY,false);return}
    const node=layoutDraft.current?.canvas.nodes.find(n=>n.id===id);if(!node)return;
    setConnectFrom('');updateConnection({from:id,side,point:sidePoint(node,side)});connectionStart.current={x:e.clientX,y:e.clientY,moved:false};gesturePointer.current={x:e.clientX,y:e.clientY};
    surfaceRef.current?.setPointerCapture(e.pointerId);
  };
  const resizeNode=(e:React.PointerEvent,node:MindMapNodeConfig)=>{e.stopPropagation();e.preventDefault();if(!writable||e.button!==0)return;stopAutoPan();gesturePointer.current=null;const size=nodeSize(node);nodeResize.current={id:node.id,px:e.clientX,py:e.clientY,...size};setResizing(node.id);surfaceRef.current?.setPointerCapture(e.pointerId)};

  // ---- selection, preview, keyboard ----
  const mapNode = (id: string) => map?.nodes.find((n) => n.id === id);
  const flushPreview = useCallback(async () => { if(previewFlush.current?.()===null)return false;const d = draftRef.current; if (!d) return true; try { await d.flush(); if (d.dirty) throw new Error('Save or export this draft before closing it or selecting another node.'); return true; } catch (e) { setPreviewError(errMsg(e)); return false; } }, []);
  // Single click on a node's header/frame selects it and nothing else: it never
  // opens or switches the document preview. Connection and click-to-connect
  // gestures still complete on the same click.
  const selectNode = async (id: string) => {
    if (busyRef.current.has(id) || Date.now() < draggedUntil.current) return;
    if(connectionRef.current&&writable){completeConnection(id);updateConnection(null);connectionStart.current=null;return}
    if(connectFrom && writable){
      if(connectFrom!==id && canvas){
        if(!canvas.edges.some(e=>e.from===connectFrom&&e.to===id))queueSave({nodes:canvas.nodes,edges:[...canvas.edges,{id:newEdgeId(canvas.edges.map(e=>e.id)),from:connectFrom,to:id}]});
      }
      setConnectFrom('');return;
    }
    if(editingRef.current&&!await finishInlineEdit(false))return;
    setSelected({ kind: 'node', id });
  };
  // Double-click on the header/frame (or Space on a focused node) opens or
  // switches the editable side preview. It never opens the full document tab —
  // that stays an explicit Enter / context-menu action.
  const openPreview = async (id: string) => {
    if(connectionRef.current||connectFrom){await selectNode(id);return}
    if (busyRef.current.has(id) || Date.now() < draggedUntil.current) return;
    setSelected({ kind: 'node', id });
    const vn = mapNode(id);
    if (!vn?.path || vn.missing) return;
    if (editingRef.current && !await finishInlineEdit(false)) return;
    if (id === previewId) {revealPreviewRef.current();return;}
    const seq = ++selection.current;
    if (!await flushPreview() || seq !== selection.current) return;
    draftRef.current = null; setPreviewDraft(null); setPreview(null); setPreviewError(''); setPreviewMode('visual'); setPreviewId(id);
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
  useEffect(() => {
    if (!previewDraft) return;
    return previewDraft.subscribe(() => {
      renderDraft((n) => n + 1);
      // A completed document save must refresh that node's card body without
      // touching the canvas draft (node Markdown is not board metadata).
      if (previewId && previewDraft.status === 'saved' && !previewDraft.dirty) setDocEpoch((p) => ({ ...p, [previewId]: (p[previewId] || 0) + 1 }));
    });
  }, [previewDraft, previewId]);
  // One active inline editor and the same autosaving document draft used by tabs.
  // restoreFocus returns the caret's home to the card only for deliberate
  // keyboard finishes (Escape/Ctrl+Enter); losing focus to the outside simply
  // flushes and closes without stealing focus back to the node.
  const finishInlineEdit = async (restoreFocus=true,fromBlur=false):Promise<boolean> => {
    if(inlineFlush.current?.()===null)return false;
    const draft=editDraftRef.current,id=editingRef.current,request=editRequest.current;
    if(!id)return true;
    if(draft){await draft.flush();if(draft.dirty){setEditError(draft.error||'Save or export this draft before leaving the editor.');return false}}
    // The save was async: if a newer edit started meanwhile, leave it alone.
    if(editingRef.current!==id||editRequest.current!==request)return true;
    if(fromBlur&&inlineHost.current&&document.activeElement instanceof HTMLElement&&inlineBlurOwns(inlineHost.current,document.activeElement))return true;
    editRequest.current++;inlineFlush.current=null;editingRef.current='';editDraftRef.current=null;
    setEditDraft(null);setEditingId('');setEditError('');
    setDocEpoch(p=>({...p,[id]:(p[id]||0)+1}));if(restoreFocus)setFocusNode(id);
    return true;
  };
  // Losing focus outside the inline editor finishes the edit and saves the
  // latest text. Focus moving into the editor's own error/retry controls, the
  // document-link picker portal it owns, or the card's resize grip (resizing
  // stays possible while editing) keeps the editor open.
  const inlineBlurOwns = (editEl:HTMLElement,next:HTMLElement)=>{
    if(editEl.contains(next))return true;
    if(next.closest('.document-link-picker'))return true;
    const card=editEl.closest('[data-node-id]');
    return !!card&&card.contains(next)&&!!next.closest('[data-resize]');
  };
  const scheduleInlineFinish=()=>{
    clearTimeout(inlineBlurTimer.current);
    const id=editingRef.current;
    inlineBlurTimer.current=setTimeout(()=>{
      const host=inlineHost.current;if(!host||editingRef.current!==id||nodeResize.current?.id===id)return;
      if(document.activeElement instanceof HTMLElement&&inlineBlurOwns(host,document.activeElement))return;
      void finishInlineEdit(false,true);
    },0);
  };
  // Listen beyond the editor as well: keyboard focus can leave through its
  // resize grip or an owned portal. Defer until the outside click has run.
  useEffect(()=>{
    if(!editingId)return;
    const change=(e:Event)=>{
      const host=inlineHost.current,target=e.target;
      if(host&&target instanceof HTMLElement&&inlineBlurOwns(host,target)){clearTimeout(inlineBlurTimer.current);return}
      scheduleInlineFinish();
    };
    document.addEventListener('focusin',change);document.addEventListener('pointerdown',change);
    return()=>{clearTimeout(inlineBlurTimer.current);document.removeEventListener('focusin',change);document.removeEventListener('pointerdown',change)};
  },[editingId]);
  const startInlineEdit = async (id: string) => {
    if(!inlineEditing||!writable||connectionRef.current||connectFrom||nodeDrag.current||nodeResize.current||busyRef.current.has(id))return;
    const vn=mapNode(id);if(!vn?.path||vn.missing)return;
    if(editingRef.current===id)return;
    if(editingRef.current&&!await finishInlineEdit(false))return;
    if(!await flushPreview())return;
    const request=++editRequest.current;setEditError('');
    try{
      const {draft}=await onPreviewDraft(id);
      if(request!==editRequest.current)return;
      if(!draft.canEdit){setError('This document is read-only.');return}
      setSelected({kind:'node',id});editingRef.current=id;editDraftRef.current=draft;setEditDraft(draft);setEditingId(id);
    }catch(e){if(request===editRequest.current)setError(errMsg(e))}
  };
  const inlinePopupOpen=()=>[...document.querySelectorAll<HTMLElement>('dialog[open],[role="listbox"],[role="menu"]')].some(el=>typeof el.checkVisibility==='function'?el.checkVisibility():!el.closest('details:not([open])'));
  const switchPreviewToSource=async(id:string)=>{
    if(!await finishInlineEdit(false))return;
    await openPreview(id);setPreviewMode('source');
  };
  useEffect(() => {
    if (!editDraft) return;
    return editDraft.subscribe(() => {
      renderDraft((n) => n + 1);
      if (editingId && editDraft.status === 'saved' && !editDraft.dirty) setDocEpoch((p) => ({ ...p, [editingId]: (p[editingId] || 0) + 1 }));
    });
  }, [editDraft, editingId]);
  useEffect(() => {
    if (!map || !previewId) return;
    if (!map.nodes.some((n) => n.id === previewId)) {
      if (draftRef.current?.dirty) setPreviewError('This node is no longer on the canvas. Your unsaved draft is retained; resolve or export it before closing.');
      else { selection.current++; draftRef.current = null; setPreviewDraft(null); setPreviewId(''); setPreview(null); }
    }
  }, [map, previewId]);
  // A node removed by a remote edit closes its inline editor; the editor's
  // unmount flush keeps any pending text in the shared draft (and its
  // localStorage copy), matching the preview's retained-draft behaviour.
  useEffect(() => {
    if (!map || !editingId) return;
    if (!map.nodes.some((n) => n.id === editingId)) { editingRef.current = ''; editDraftRef.current = null; setEditDraft(null); setEditingId(''); }
  }, [map, editingId]);
  useEffect(() => {
    if (!previewId) return;
    const escape = (e: KeyboardEvent) => {
      if (editingRef.current || e.key !== 'Escape' || e.defaultPrevented || nodeMenu || newNode || picker || confirm || document.querySelector('dialog[open]') || (e.target instanceof Element && e.target.closest('[role="dialog"],[role="menu"],[role="listbox"]'))) return;
      e.preventDefault(); void closePreview();
    };
    document.addEventListener('keydown', escape);
    return () => document.removeEventListener('keydown', escape);
  }, [previewId, nodeMenu, newNode, picker, confirm, closePreview]);
  useEffect(() => {
    // A selected connection is removable from the keyboard while focus is on
    // the canvas/edge, but never while typing in the label input or an editor.
    if (selected?.kind !== 'edge') return;
    const remove = (e: KeyboardEvent) => {
      if (e.key !== 'Delete' && e.key !== 'Backspace') return;
      const t = e.target as HTMLElement | null;
      if(!t?.closest('.mindmap-surface,.mindmap-edge-inspector')||t.closest('.canvas-navigation'))return;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return;
      if (!writable || e.defaultPrevented) return;
      e.preventDefault();
      const current = layoutDraft.current?.canvas; if (!current) return;
      queueSave({ nodes: current.nodes, edges: current.edges.filter((ed) => ed.id !== selected.id) });
      setSelected(null);
    };
    document.addEventListener('keydown', remove);
    return () => document.removeEventListener('keydown', remove);
  }, [selected, writable, queueSave]);
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
    if(e.key==='F2'){if(inlineEditing){e.preventDefault();void startInlineEdit(node.id)}return}
    if(['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(e.key)){
      e.preventDefault();const dx=e.key==='ArrowLeft'?-1:e.key==='ArrowRight'?1:0,dy=e.key==='ArrowUp'?-1:e.key==='ArrowDown'?1:0;
      if(e.altKey&&writable){queueSave({nodes:canvas.nodes.map(n=>n.id===node.id?{...n,x:n.x+dx*16,y:n.y+dy*16}:n),edges:canvas.edges})}
      else{const candidates=canvas.nodes.filter(n=>n.id!==node.id&&((n.x-node.x)*dx+(n.y-node.y)*dy)>0).sort((a,b)=>Math.hypot(a.x-node.x,a.y-node.y)-Math.hypot(b.x-node.x,b.y-node.y));if(candidates[0])setFocusNode(candidates[0].id)}return;
    }
    if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='z'&&writable){e.preventDefault();e.shiftKey?layoutDraft.current?.redo():layoutDraft.current?.undo();return}

    if (e.key === ' ') { e.preventDefault(); void openPreview(node.id); return; }
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
      if(editingRef.current===nodeId&&!await finishInlineEdit(false))throw new Error('Save or export the inline draft before removing its node.');
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
 // Editing renders inside the card at its saved dimensions: no temporary
  // expansion, no camera move. The body (editor or preview) takes whatever
  // space the card has, so entering/leaving edit never reflows the canvas.
  const nodeById = useMemo(() => new Map(nodes.map(n=>[n.id,n])), [nodes]);
  // Reveal only the requested node in the actual uncovered canvas. Work from
  // the current camera in the updater so observer/layout callbacks cannot add
  // the same correction twice. Camera-only state never touches the map draft.
  revealPreviewRef.current=()=>{
    const el=surfaceRef.current,pane=previewPane.current;
    if(!previewId||editingId||!el||!pane||nodeDrag.current||nodeResize.current||connectionRef.current)return;
    const r=el.getBoundingClientRect();if(r.width<=0||r.height<=0)return;
    const bounds=uncoveredCanvasBounds(r,pane.getBoundingClientRect()),z=uiZoom();
    const mx=Math.min(16*z,(bounds.right-bounds.left)/4),my=Math.min(16*z,(bounds.bottom-bounds.top)/4);
    const visible={left:bounds.left+mx,right:bounds.right-mx,top:bounds.top+my,bottom:bounds.bottom-my};
    const node=canvasRef.current?.nodes.find(n=>n.id===previewId);if(!node)return;
    const size=nodeSize(node);
    setView(v=>{
      const left=r.left+(v.x+node.x*v.scale)*z,top=r.top+(v.y+node.y*v.scale)*z;
      const delta=minimalRevealPan({left,top,right:left+size.w*v.scale*z,bottom:top+size.h*v.scale*z},visible);
      if(Math.abs(delta.x)<.5&&Math.abs(delta.y)<.5)return v;
      const next={...v,x:v.x+delta.x/z,y:v.y+delta.y/z};viewRef.current=next;return next;
    });
  };
  useLayoutEffect(()=>{revealPreviewRef.current()},[previewId,effectiveWidth,height,narrow,narrow&&!!editingId]);
  const overviewBounds=useMemo(()=>{if(!nodes.length)return{x:0,y:0,w:1,h:1};const x=Math.min(...nodes.map(n=>n.x)),y=Math.min(...nodes.map(n=>n.y));return{x,y,w:Math.max(...nodes.map(n=>n.x+nodeSize(n).w))-x,h:Math.max(...nodes.map(n=>n.y+nodeSize(n).h))-y}},[nodes]);
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

  // No ordinary Saved/Saving chip: quiet autosave needs no status row. Real
  // conflict/error states below stay visible with retry and recovery actions.
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
      <div className="mindmap-tools" role="toolbar" aria-label="Mind map tools">
      {writable && <Tool caption="Add idea" icon="plus" label="Add node" onClick={() => { const c = centerWorld(); setNewTitle(''); setNewNode(availablePosition({x:c.x-95,y:c.y-32},nodes)); }}/>}
      {writable && <Tool caption="Link note" icon="link" label="Link an existing document" onClick={() => { setPicker({ mode: 'link' }); setPickQuery(''); }}/>}
      {writable&&<><Tool icon="undoArrow" label="Undo canvas edit" disabled={!layoutDraft.current?.canUndo} onClick={()=>layoutDraft.current?.undo()}/><Tool icon="redoArrow" label="Redo canvas edit" disabled={!layoutDraft.current?.canRedo} onClick={()=>layoutDraft.current?.redo()}/></>}
      {writable&&<Tool icon="arrange" label="Arrange branches" disabled={!nodes.length} onClick={()=>{queueSave({nodes:arrangeMindMap(nodes,edges),edges});requestAnimationFrame(()=>{const el=surfaceRef.current;if(el){const r=el.getBoundingClientRect();const fitted=fitView(arrangeMindMap(nodes,edges),r.width/uiZoom(),r.height/uiZoom());if(fitted)setView(fitted)}})}}/>}
      {writable&&<Tool icon="link" label="Connect selected idea" disabled={selected?.kind!=='node'} active={!!connectFrom} onClick={()=>{if(selected?.kind==='node'){updateConnection(null);setConnectFrom(selected.id)}}}/>}
      <Tool icon="history" label="Reload mind map" onClick={() => { setLoading(true); void reload(); }}/></div>
    </header>
    {/* No connection banner: it used to change the canvas height and refit the
        whole board mid-connection. Escape, the port toggle and releasing on the
        canvas or a node are the cancel/commit controls. */}
    {error && <div className="mindmap-banner" role="alert">{error}</div>}
    {operationWarnings.length > 0 && <div className="mindmap-banner warning" role="alert">{operationWarnings.join(' · ')}</div>}
    {map.warnings?.length ? <div className="mindmap-banner warning">{map.warnings.join(' · ')}</div> : null}
    {saveState === 'conflict' && <div className="mindmap-banner" role="alert"><span>{saveError || 'The canvas changed elsewhere. Your layout is retained locally.'}</span><div><button className="btn small" onClick={retrySave}>Save my version</button><button className="btn small" onClick={adoptRemote}>Load theirs</button></div></div>}
    {saveState === 'error' && <div className="mindmap-banner" role="alert"><span>{saveError}</span><button className="btn small" onClick={retrySave}>Retry save</button></div>}
    <div className="mindmap-content" ref={content} style={{ '--map-preview-width': effectiveWidth + '%', '--map-preview-height':height+'%' } as React.CSSProperties}>
      <div className={'mindmap-surface'+(connection?' connecting':'')} ref={surfaceRef} role="application" aria-label="Mind map canvas" tabIndex={0} style={{backgroundPosition:`${view.x}px ${view.y}px`,backgroundSize:`${24*view.scale}px ${24*view.scale}px`}}
        onPointerDown={onSurfacePointerDown} onPointerMove={onSurfacePointerMove} onPointerUp={onSurfacePointerUp} onPointerCancel={onSurfacePointerUp}
        onLostPointerCapture={()=>{if(nodeDrag.current||nodeResize.current||connectionStart.current)cancelCanvasGesture();else stopAutoPan()}}
        onDoubleClick={(e) => { if (!writable || connectionRef.current || (e.target as HTMLElement).closest('[data-node-id],.mindmap-edge-hit,.mindmap-edge-handle')) return; const el = surfaceRef.current!; const rect = el.getBoundingClientRect(); const p = worldPoint(e.clientX, e.clientY, rect, view,uiZoom()); setNewTitle(''); setNewNode({ x: p.x - 95, y: p.y - 32 }); }}
        onDragOver={(e) => { if (!acceptsDrop(e)) return; e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; }}
        onDrop={(e) => void handleSurfaceDrop(e)}>
        <div className="mindmap-world" style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}>
          <svg className="mindmap-edges"><defs><marker id="mindmap-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="var(--accent)"/></marker></defs>
            {edges.map((edge) => {
              const a = nodeById.get(edge.from), b = nodeById.get(edge.to);
              if (!a || !b) return null;
              const as = nodeSize(a), bs = nodeSize(b);
              const pa = edge.fromSide?sidePoint(a,edge.fromSide,4):borderPoint(a, { x: b.x + bs.w / 2, y: b.y + bs.h / 2 });
              const pb = edge.toSide?sidePoint(b,edge.toSide,4):borderPoint(b, { x: a.x + as.w / 2, y: a.y + as.h / 2 });
              const sel = selected?.kind === 'edge' && selected.id === edge.id;
              const mid = { x: (pa.x + pb.x) / 2, y: (pa.y + pb.y) / 2 };
              // The hit path doubles as the keyboard handle for the connector:
              // Tab reaches it, Enter/Space opens the same inspector a click
              // does, and Delete keeps working while it holds focus.
              const fromTitle = mapNode(edge.from)?.title || 'Missing idea', toTitle = mapNode(edge.to)?.title || 'Missing idea';
              return <g key={edge.id} className={'mindmap-edge' + (sel ? ' selected' : '') + (edge.dashed ? ' dashed' : '')}>
                {/* Generous invisible hit path aligned to the visible curve; the
                    stroke width is divided by the zoom so the target stays the
                    same size on screen at any scale. */}
                <path d={curvePath(pa, pb)} className="mindmap-edge-hit" style={{ strokeWidth: 18 / view.scale }} tabIndex={0} role="button"
                  aria-label={`Connection from ${fromTitle} to ${toTitle}${edge.label ? `, labelled ${edge.label}` : ''}. Press Enter to inspect.`}
                  onPointerDown={(e) => { e.stopPropagation(); }} onClick={(e) => { e.stopPropagation(); surfaceRef.current?.focus({preventScroll:true});setSelected({ kind: 'edge', id: edge.id }); }}
                  onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); setSelected({ kind: 'edge', id: edge.id }); } }}/>
                <path d={curvePath(pa, pb)} className="mindmap-edge-line" markerEnd="url(#mindmap-arrow)" aria-hidden="true"/>
                {edge.label ? <text x={mid.x} y={mid.y - 6} className="mindmap-edge-label" textAnchor="middle" aria-hidden="true">{edge.label}</text> : null}

              </g>;
            })}
            {connection&&(()=>{const source=nodeById.get(connection.from);if(!source)return null;const target=connection.target?nodeById.get(connection.target):undefined;const a=sidePoint(source,connection.side,4),b=target?(connection.targetSide?sidePoint(target,connection.targetSide,4):borderPoint(target,a)):connection.point;return <path d={connection.reconnect?.end==='from'?curvePath(b,a):curvePath(a,b)} className="mindmap-connection-ghost" markerEnd="url(#mindmap-arrow)"/>})()}
          </svg>
          {nodes.map((node) => {
            const viewNode = mapNode(node.id);
            const size = nodeSize(nodeById.get(node.id)||node);
            const sel = selected?.kind === 'node' && selected.id === node.id;
            return <div key={node.id} role="button" tabIndex={0} data-node-id={node.id}
              className={'mindmap-node' + (editingId===node.id?' editing':'') + (sel ? ' selected' : '') + (size.h<96?' card-tiny':'') + (size.h>=140?' card-expanded':'') + (connection?.target===node.id?' connection-target':'') + (resizing===node.id?' resizing':'') + (dragNode === node.id ? ' dragging' : '') + (dropNode === node.id ? ' drop' : '') + (busyIds.has(node.id) ? ' busy' : '') + (viewNode?.missing ? ' missing' : '') + (viewNode && !viewNode.owned ? ' reference' : '')}
              style={{ left: node.x, top: node.y, width: size.w, height: size.h, ['--node-color' as string]: node.color || 'var(--accent)' } as React.CSSProperties}
              aria-label={`${viewNode?.title || node.id}${viewNode?.owned ? '' : ' (reference)'}${viewNode?.missing ? ' (missing document)' : ''}`}
              title={viewNode?.path || 'Document unavailable'}
              onPointerDown={(e) => onNodePointerDown(e, node)}
              onDragStart={(e) => { if ((e.target as HTMLElement).closest('.mindmap-card-scroll,.mindmap-file-chips,.mindmap-node-edit')) return; e.preventDefault(); }}
              onClick={(e) => { if (!(e.target as HTMLElement).closest('button,a,[data-resize],.mindmap-card-scroll,.mindmap-node-edit')) void (previewActivation==='single'?openPreview(node.id):selectNode(node.id)); }}
              onDoubleClick={(e) => {
                if (connectionRef.current || Date.now() < draggedUntil.current) return;
                const t = e.target as HTMLElement;
                if (t.closest('button,a,[data-resize]')) return;
                // Double-click on the rendered body edits the text where it is;
                // double-click on the header/frame opens or switches the editable
                // side preview instead of the full document (that stays on Enter/menu).
                // With inline editing off (Settings → Interaction) the body also
                // opens the side preview instead of editing on the card.
                if (t.closest('.mindmap-card-scroll')) { if (inlineEditing && writable && viewNode?.path && !viewNode.missing) void startInlineEdit(node.id); else void openPreview(node.id); return; }
                void openPreview(node.id);
              }}
              onContextMenu={(e) => { e.preventDefault(); e.stopPropagation(); if (viewNode) setNodeMenu({ node: viewNode, x: e.clientX, y: e.clientY }); }}
              onKeyDown={(e) => nodeKeys(e, node)}
              onDragOver={(e) => { if (!acceptsDrop(e)) return; e.preventDefault(); e.stopPropagation(); e.dataTransfer.dropEffect = 'copy'; setDropNode(node.id); }}
              onDragLeave={(e) => { if (e.currentTarget.contains(e.relatedTarget as Node | null)) return; setDropNode((d) => d === node.id ? '' : d); }}>
              <button className="mindmap-node-actions" aria-label={`Actions for ${viewNode?.title||'node'}`} onClick={e=>{e.stopPropagation();const r=e.currentTarget.getBoundingClientRect();if(viewNode)setNodeMenu({node:viewNode,x:r.left,y:r.bottom})}}><Icon name="more" size={16}/></button>
              {inlineEditing&&writable&&viewNode?.path&&!viewNode.missing&&editingId!==node.id&&<button className="mindmap-node-edit-open" aria-label={`Edit text of ${viewNode?.title||'node'}`} title="Edit this idea's text on the card (F2)" onClick={e=>{e.stopPropagation();void startInlineEdit(node.id)}}><Icon name="edit" size={14}/></button>}
              <span className="mindmap-node-title">{viewNode?.title || node.id}</span>
              <span className="mindmap-node-path">{viewNode?.missing ? 'Document missing' : viewNode?.path || ''}</span>
              <MindMapCardContent space={space} id={node.id} revision={viewNode?.revision} epoch={epoch + (docEpoch[node.id] || 0)} docs={docs} attachments={attachments} expanded={size.h>=96} text={editingId===node.id?editDraft?.text:previewId===node.id?previewDraft?.text:undefined} onOpen={onOpenDoc} editor={editingId === node.id ? <div key="inline-editor" ref={inlineHost} className="mindmap-node-edit" onPointerDown={e=>e.stopPropagation()} onClick={e=>e.stopPropagation()} onDoubleClick={e=>e.stopPropagation()} onDragStart={e=>e.stopPropagation()} onContextMenu={e=>e.stopPropagation()} onDrop={e=>e.stopPropagation()} onBlur={(e)=>{const next=e.relatedTarget as HTMLElement|null;if(next&&inlineBlurOwns(e.currentTarget,next))return;scheduleInlineFinish()}} onKeyDownCapture={e=>{if(!e.nativeEvent.isComposing&&(e.key==='Escape'||((e.ctrlKey||e.metaKey)&&e.key==='Enter'))&&!inlinePopupOpen()){e.preventDefault();e.stopPropagation();void finishInlineEdit()}}} onKeyDown={e=>{e.stopPropagation();if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='s'){e.preventDefault();if(inlineFlush.current?.()!==null)void editDraftRef.current?.flush();return;}if(!e.nativeEvent.isComposing&&!e.defaultPrevented&&(e.key==='Escape'||((e.ctrlKey||e.metaKey)&&e.key==='Enter'))&&!inlinePopupOpen()){e.preventDefault();void finishInlineEdit()}}}>
                {editError&&editDraft?.status!=='error'&&editDraft?.status!=='conflict'&&<p className="visual-error" role="alert">{editError}</p>}
                {editDraft&&(editDraft.status==='error'||editDraft.status==='conflict')&&<div className="save-error" role="alert"><span>{editDraft.error}</span><button className="btn small" onClick={e=>{e.stopPropagation();void (editDraft.status==='conflict'?onResolveConflict(node.id):editDraft.flush())}}>{editDraft.status==='conflict'?'Resolve conflict':'Retry save'}</button><button className="btn small" onClick={e=>{e.stopPropagation();exportDraft(viewNode?.path||'node.md',editDraft.text)}}>Export draft</button></div>}
                {editDraft?<Suspense fallback={<p className="muted" role="status">Loading editor…</p>}><VisualEditor bare autoFocus flushRef={inlineFlush} draft={editDraft} docs={docs} notePath={viewNode?.path||''} attachments={attachments.map(a=>({path:a.path,url:api.attachmentURL(space,a.path)}))} onUploadImages={onUploadImages?(files)=>onUploadImages(files,viewNode?.path||''):undefined} onSourceMode={()=>void switchPreviewToSource(node.id)}/></Suspense>:<p className="muted" role="status">Loading editor…</p>}
              </div>:undefined}/>
              {writable&&editingId!==node.id&&<>{NODE_SIDES.map(side=><button key={side} className={'mindmap-port port-'+side} data-connection-side={side} aria-label={`Connect ${viewNode?.title||'node'} from ${side}`} title="Drag to connect · release on canvas to add an idea" onPointerDown={e=>startConnection(e,node.id,side)} onClick={e=>{e.stopPropagation();if(e.detail===0){if(connectionRef.current){completeConnection(node.id,side);updateConnection(null);connectionStart.current=null}else {setConnectFrom('');updateConnection({from:node.id,side,point:sidePoint(node,side)});connectionStart.current=null}}}}/>)}</>}
              {/* The corner grip stays live while inline editing: pressing it
                  prevents default so the caret and selection survive, the drag
                  works from the current saved size (no snap-back), and focus
                  landing on it (keyboard) never closes the editor. */}
              {writable&&<button className="mindmap-node-resize" data-resize aria-label={`Resize ${viewNode?.title||'node'}`} title="Drag to resize; arrow keys adjust size" onPointerDown={e=>resizeNode(e,node)} onClick={e=>e.stopPropagation()} onKeyDown={e=>{if(e.key==='Escape'){if(editingRef.current===node.id){e.preventDefault();e.stopPropagation();void finishInlineEdit()}return}if(!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(e.key))return;e.preventDefault();e.stopPropagation();queueSave({nodes:nodes.map(n=>n.id===node.id?{...n,w:clamp(size.w+(e.key==='ArrowLeft'?-16:e.key==='ArrowRight'?16:0),160,1200),h:clamp(size.h+(e.key==='ArrowUp'?-16:e.key==='ArrowDown'?16:0),96,1200)}:n),edges})}}><Icon name="grip" size={14}/></button>}
              <span className="mindmap-node-badges">
                {viewNode && !viewNode.owned ? <Icon name="link" size={12}/> : null}
                {viewNode?.warnings?.length ? <Icon name="info" size={12}/> : null}
              </span>
            </div>;
          })}
          {selected?.kind==='edge'&&writable&&(()=>{const edge=edges.find(e=>e.id===selected.id);if(!edge)return null;const a=nodeById.get(edge.from),b=nodeById.get(edge.to);if(!a||!b)return null;
              const as = nodeSize(a), bs = nodeSize(b);
              const pa = edge.fromSide?sidePoint(a,edge.fromSide,4):borderPoint(a, { x: b.x + bs.w / 2, y: b.y + bs.h / 2 });
              const pb = edge.toSide?sidePoint(b,edge.toSide,4):borderPoint(b, { x: a.x + as.w / 2, y: a.y + as.h / 2 });
            return <svg className="mindmap-edges mindmap-endpoints" aria-hidden="true">                <><circle cx={pa.x} cy={pa.y} r={6 / view.scale} className="mindmap-edge-handle" data-edge-end="from" onPointerDown={(e) => startReconnect(e, edge, 'from')} onClick={(e) => e.stopPropagation()}/><circle cx={pb.x} cy={pb.y} r={6 / view.scale} className="mindmap-edge-handle" data-edge-end="to" onPointerDown={(e) => startReconnect(e, edge, 'to')} onClick={(e) => e.stopPropagation()}/></></svg>})()}
          {!nodes.length && <div className="mindmap-empty"><Icon name="mindmap" size={38}/><h2>A little space for a big idea</h2><p>Add your first idea, then branch out.<br/>Your notes stay connected to the rest of your vault.</p>{writable&&<button className="btn primary" onClick={()=>{const c=centerWorld();setNewTitle('');setNewNode(availablePosition({x:c.x-95,y:c.y-32},nodes))}}><Icon name="plus" size={16}/>Add first idea</button>}<small>Double-click to add · Drag to arrange · Scroll to zoom</small></div>}
        </div>
        <CanvasNavigation defaultOpen={minimapDefault} label="Mind map navigation" zoom={view.scale} minZoom={MIN_ZOOM} maxZoom={MAX_ZOOM}
          onZoomOut={()=>zoomAtCenter(1/1.2)} onZoomIn={()=>zoomAtCenter(1.2)} onReset={()=>zoomAtCenter(1/view.scale)} onFit={fit} disabledFit={!nodes.length}
          overview={nodes.length?{bounds:overviewBounds,items:nodes.map(n=>({...n,...nodeSize(n),color:n.color})),viewport:{x:-view.x/view.scale,y:-view.y/view.scale,w:surfaceSize.w/view.scale,h:surfaceSize.h/view.scale},onNavigate:point=>setView(v=>({...v,x:surfaceSize.w/2-point.x*v.scale,y:surfaceSize.h/2-point.y*v.scale}))}:undefined}/>

      </div>
      {selected?.kind === 'edge' && <div className="mindmap-edge-inspector" role="group" aria-label="Connector actions">
        <input aria-label="Connector label" readOnly={!writable} maxLength={200} placeholder="Label…" value={edges.find((ed) => ed.id === selected.id)?.label || ''} onChange={(e) => { if (!writable) return; queueSave({ nodes, edges: edges.map((ed) => ed.id === selected.id ? { ...ed, label: e.target.value } : ed) }); }}/>
        <label><input type="checkbox" disabled={!writable} checked={!!edges.find((ed) => ed.id === selected.id)?.dashed} onChange={(e) => { if (!writable) return; queueSave({ nodes, edges: edges.map((ed) => ed.id === selected.id ? { ...ed, dashed: e.target.checked } : ed) }); }}/>Dashed</label>
        {writable && <button className="btn small" onClick={() => { const ed = edges.find((x) => x.id === selected.id); if (ed) beginReconnect(ed, 'from'); }}>Move start</button>}
        {writable && <button className="btn small" onClick={() => { const ed = edges.find((x) => x.id === selected.id); if (ed) beginReconnect(ed, 'to'); }}>Move end</button>}
        {writable && <button className="btn small danger" onClick={() => { queueSave({ nodes, edges: edges.filter((ed) => ed.id !== selected.id) }); setSelected(null); }}><Icon name="trash" size={13}/>Delete</button>}
        <button className="btn small" onClick={() => setSelected(null)}>Done</button>
      </div>}
      {previewId && !(editingId&&narrow) && <aside ref={previewPane} className="mindmap-preview" aria-label="Node preview">
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
        {editingId&&<p className="mindmap-inline-notice muted">Finish editing the card to use this preview.</p>}
        {previewDraft && !editingId && <div className="mindmap-preview-body" inert={busyIds.has(previewId)} aria-busy={busyIds.has(previewId)}>
          {previewMode==='source'?<><div className="board-preview-mode"><span>Markdown source</span><button className="btn small" onClick={()=>setPreviewMode('visual')}>Visual editor</button></div><NoteEditor draft={previewDraft} docs={docs} notePath={preview?.path||''} onUploadImages={onUploadImages?files=>onUploadImages(files,preview?.path||''):undefined} onImageError={setPreviewError}/></>:<Suspense fallback={<p className="muted" role="status">Loading Visual editor…</p>}><VisualEditor compact flushRef={previewFlush} onSourceMode={()=>setPreviewMode('source')} draft={previewDraft} onUploadImages={onUploadImages?(files)=>onUploadImages(files,preview?.path||mapNode(previewId)?.path||''):undefined} docs={docs} notePath={preview?.path || mapNode(previewId)?.path || ''} attachments={attachments.map((a) => ({ path: a.path, url: api.attachmentURL(space, a.path) }))}/></Suspense>}
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
