// Pure helpers for the folder-backed mind map UI. No React, no fetch —
// everything here is unit-testable and mirrors the backend's validation and
// layout rules so the canvas can refuse an invalid edit before the server does.

import type { MindMapEdgeConfig, MindMapNodeConfig } from '../api/client';

export const MINDMAP_NODE_LIMIT = 200;
export const MINDMAP_EDGE_LIMIT = 400;
export const DEFAULT_NODE_W = 190;
export const DEFAULT_NODE_H = 112;
export const MIN_ZOOM = 0.2;
export const MAX_ZOOM = 2.5;

// Mirrors the backend's mind map folder rules (validFolderRel + no root).
export function mindMapPathError(path: string, existingPaths: string[] = []): string {
  const clean = path.trim().replace(/\/+$/, '');
  if (!clean) return 'Mind map folder must not be the vault root.';
  if (clean.length > 1000 || /[\x00-\x1f\x7f%\\:*?"<>|]/.test(clean)) return 'Mind map folder contains invalid characters or is too long.';
  if (clean.split('/').some((s) => !s || s === '.' || s === '..')) return 'Mind map folder path must not contain "." or "..".';
  if (clean.split('/').some((s) => s.startsWith('.'))) return 'Mind maps cannot live inside hidden folders.';
  for (const m of existingPaths) {
    if (m !== clean && (clean.startsWith(m + '/') || m.startsWith(clean + '/'))) return 'Owning mind maps cannot be nested; use a sibling folder.';
  }
  return '';
}

// The backend collects files under a node into "<map>/Nodes/<nodeId>/".
export function mindMapNodeFolder(mapPath: string, nodeId: string): string {
  return `${mapPath}/Nodes/${nodeId}`;
}

// Node ids double as folder segments for collected files; mirror the backend guard.
export function nodeIdFolderSafe(nodeId: string): boolean {
  return !!nodeId && !nodeId.includes('/') && !nodeId.includes('\\') && !nodeId.startsWith('.');
}

export function baseName(path: string): string {
  const i = path.lastIndexOf('/');
  return i < 0 ? path : path.slice(i + 1);
}

// Preview of where a document will physically live after "move file into node".
export function mindMapFileTarget(mapPath: string, nodeId: string, docPath: string): string {
  return `${mindMapNodeFolder(mapPath, nodeId)}/${baseName(docPath)}`;
}

// Documents that already live inside a node's collection folder.
export function documentsInNodeFolder<T extends { path: string }>(docs: T[], mapPath: string, nodeId: string): T[] {
  const prefix = mindMapNodeFolder(mapPath, nodeId) + '/';
  return docs.filter((d) => d.path.startsWith(prefix));
}

export function mindMapColorOk(color: string): boolean {
  return color === '' || /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(color);
}

export function edgeIdOk(id: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$/.test(id);
}

// Deterministic unique connector id (backend regex-safe).
export function newEdgeId(existing: Iterable<string>): string {
  const used = new Set(existing);
  let n = 1;
  while (used.has(`e${n}`)) n++;
  return `e${n}`;
}

export interface CanvasIssue {
  message: string;
}

// Mirrors validateMindMapConfig so the autosave queue can refuse a broken
// canvas locally instead of round-tripping a guaranteed 400.
export function validateCanvas(nodes: MindMapNodeConfig[], edges: MindMapEdgeConfig[]): string[] {
  const errors: string[] = [];
  if (nodes.length > MINDMAP_NODE_LIMIT) errors.push(`A mind map supports at most ${MINDMAP_NODE_LIMIT} nodes.`);
  if (edges.length > MINDMAP_EDGE_LIMIT) errors.push(`A mind map supports at most ${MINDMAP_EDGE_LIMIT} connectors.`);
  const seen = new Set<string>();
  for (const n of nodes) {
    if (!n.id) errors.push('Node id must not be empty.');
    else if (seen.has(n.id)) errors.push(`Duplicate node for the same document: ${n.id}`);
    seen.add(n.id);
    if (![n.x, n.y].every((v) => Number.isFinite(v) && Math.abs(v) <= 1e6)) errors.push('Node position must be a finite coordinate within bounds.');
    if (!((n.w ?? 0) >= 0 && (n.w ?? 0) <= 2000 && (n.h ?? 0) >= 0 && (n.h ?? 0) <= 2000 && Number.isFinite(n.w ?? 0) && Number.isFinite(n.h ?? 0))) errors.push('Node size must be between 0 and 2000.');
    if (!mindMapColorOk(n.color ?? '')) errors.push('Node color must be empty or a #rgb/#rrggbb hex value.');
  }
  const edgeIDs = new Set<string>();
  for (const e of edges) {
    if (!edgeIdOk(e.id)) errors.push('Connector id must use letters, numbers, dots, underscores, colons or hyphens.');
    else if (edgeIDs.has(e.id)) errors.push(`Duplicate connector id: ${e.id}`);
    edgeIDs.add(e.id);
    if ((e.fromSide && !NODE_SIDES.includes(e.fromSide)) || (e.toSide && !NODE_SIDES.includes(e.toSide))) errors.push('Invalid connector attachment side.');
    if (e.from === e.to) errors.push('A connector cannot link a node to itself.');
    if (!seen.has(e.from) || !seen.has(e.to)) errors.push('Connector refers to an unknown node.');
    if ((e.label ?? '').length > 200) errors.push('Connector label must not exceed 200 characters.');
  }
  return errors;
}

export function nodeSize(node: { w?: number; h?: number }): { w: number; h: number } {
  return { w: node.w && node.w > 0 ? node.w : DEFAULT_NODE_W, h: node.h && node.h > 0 ? node.h : DEFAULT_NODE_H };
}

export function clampZoom(z: number): number {
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, z));
}

export interface Viewport {
  x: number;
  y: number;
  scale: number;
}

// Convert a client-space point into world coordinates for the given viewport.
export function worldPoint(clientX: number, clientY: number, rect: { left: number; top: number }, view: Viewport, pixelScale=1): { x: number; y: number } {
  return { x: ((clientX - rect.left)/pixelScale - view.x) / view.scale, y: ((clientY - rect.top)/pixelScale - view.y) / view.scale };
}

// Fit all nodes into the viewport with padding; empty canvas keeps the view.
export function fitView(nodes: MindMapNodeConfig[], width: number, height: number, padding = 48): Viewport | null {
  if (!nodes.length || width <= 0 || height <= 0) return null;
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const n of nodes) {
    const s = nodeSize(n);
    minX = Math.min(minX, n.x);
    minY = Math.min(minY, n.y);
    maxX = Math.max(maxX, n.x + s.w);
    maxY = Math.max(maxY, n.y + s.h);
  }
  const bw = Math.max(1, maxX - minX), bh = Math.max(1, maxY - minY);
  const scale = clampZoom(Math.min((width - padding * 2) / bw, (height - padding * 2) / bh, 1));
  return { scale, x: (width - bw * scale) / 2 - minX * scale, y: (height - bh * scale) / 2 - minY * scale };
}

export type NodeSide = 'top' | 'right' | 'bottom' | 'left';
export const NODE_SIDES: NodeSide[] = ['top','right','bottom','left'];
export function autoSide(node: {x:number;y:number;w?:number;h?:number}, toward: {x:number;y:number}): NodeSide {
  const {w,h}=nodeSize(node),dx=toward.x-node.x-w/2,dy=toward.y-node.y-h/2;
  return Math.abs(dx)/(w/2)>=Math.abs(dy)/(h/2) ? (dx>=0?'right':'left') : (dy>=0?'bottom':'top');
}
export function sidePoint(node:{x:number;y:number;w?:number;h?:number}, side:NodeSide, gap=0){
  const {w,h}=nodeSize(node);
  return {x:node.x+(side==='left'?-gap:side==='right'?w+gap:w/2),y:node.y+(side==='top'?-gap:side==='bottom'?h+gap:h/2),side};
}
export function borderPoint(node:{x:number;y:number;w?:number;h?:number},toward:{x:number;y:number},gap=4){
  return sidePoint(node,autoSide(node,toward),gap);
}
// Control points follow each border's outward normal; the final tangent points
// into the destination, so SVG arrowheads rotate correctly in every direction.
export function curvePath(a:{x:number;y:number;side?:NodeSide},b:{x:number;y:number;side?:NodeSide}):string{
  const source=a.side||(Math.abs(b.x-a.x)>=Math.abs(b.y-a.y)?(b.x>=a.x?'right':'left'):(b.y>=a.y?'bottom':'top'));
  const opposite:Record<NodeSide,NodeSide>={top:'bottom',bottom:'top',left:'right',right:'left'};
  const target=b.side||opposite[source];
  const normals:Record<NodeSide,{x:number;y:number}>={top:{x:0,y:-1},right:{x:1,y:0},bottom:{x:0,y:1},left:{x:-1,y:0}};
  const distance=Math.max(32,Math.min(200,Math.hypot(b.x-a.x,b.y-a.y)*.45)),n=normals[source],m=normals[target];
  return `M ${a.x} ${a.y} C ${a.x+n.x*distance} ${a.y+n.y*distance}, ${b.x+m.x*distance} ${b.y+m.y*distance}, ${b.x} ${b.y}`;
}

// Keyboard layout: Tab creates a child to the right, Shift+Tab a sibling below.
export function childPosition(node: Pick<MindMapNodeConfig,'x'|'y'|'w'|'h'>): { x: number; y: number } {
  const s = nodeSize(node);
  return { x: node.x + s.w + 80, y: node.y };
}

export function siblingPosition(node: Pick<MindMapNodeConfig,'x'|'y'|'w'|'h'>): { x: number; y: number } {
  const s = nodeSize(node);
  return { x: node.x, y: node.y + s.h + 48 };
}

// Snap a world coordinate to the canvas grid used for drops and new nodes.
export function snapCoord(v: number, grid = 8): number {
  return Math.round(v / grid) * grid;
}

// Keep repeated branch creation readable without placing cards on one another.
export function availablePosition(seed:{x:number;y:number},nodes:MindMapNodeConfig[]):{x:number;y:number}{
 let point={...seed};
 for(let attempt=0;attempt<=nodes.length;attempt++){
  if(!nodes.some(n=>{const size=nodeSize(n);return point.x<n.x+size.w+24&&point.x+DEFAULT_NODE_W+24>n.x&&point.y<n.y+size.h+24&&point.y+DEFAULT_NODE_H+24>n.y}))return point;
  point={...point,y:point.y+DEFAULT_NODE_H+48};
 }
 return point;
}

// Branch layout tolerates cycles and disconnected ideas. Nodes keep their IDs,
// styles and documents; only coordinates change, so the operation is undoable.
export function arrangeMindMap(nodes:MindMapNodeConfig[],edges:MindMapEdgeConfig[]):MindMapNodeConfig[]{
 const byId=new Map(nodes.map(n=>[n.id,n])),incoming=new Set(edges.map(e=>e.to));
 const roots=nodes.filter(n=>!incoming.has(n.id)).map(n=>n.id),level=new Map<string,number>();
 const visit=(root:string)=>{const queue:[string,number][]=[[root,0]];while(queue.length){const [id,depth]=queue.shift()!;if(level.has(id)||!byId.has(id))continue;level.set(id,depth);for(const edge of edges.filter(e=>e.from===id))queue.push([edge.to,depth+1])}};
 roots.forEach(visit);nodes.forEach(n=>{if(!level.has(n.id))visit(n.id)});
 const buckets=new Map<number,string[]>();nodes.forEach(n=>{const depth=level.get(n.id)||0;buckets.set(depth,[...(buckets.get(depth)||[]),n.id])});
 const xByDepth=new Map<number,number>();let x=0;
 for(const depth of [...buckets.keys()].sort((a,b)=>a-b)){xByDepth.set(depth,x);x+=Math.max(...buckets.get(depth)!.map(id=>nodeSize(byId.get(id)!).w))+100}
 const positions=new Map<string,{x:number;y:number}>();
 for(const [depth,ids]of buckets){const height=ids.reduce((sum,id)=>sum+nodeSize(byId.get(id)!).h,0)+Math.max(0,ids.length-1)*48;let y=-height/2;for(const id of ids){positions.set(id,{x:xByDepth.get(depth)!,y});y+=nodeSize(byId.get(id)!).h+48}}
 return nodes.map(n=>({...n,...positions.get(n.id)!}));
}
