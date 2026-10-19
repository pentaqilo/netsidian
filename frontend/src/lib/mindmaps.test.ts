import { expect, it } from 'vitest';
import {
  sidePoint, autoSide, borderPoint, childPosition, clampZoom, curvePath, documentsInNodeFolder, edgeIdOk,
  fitView, mindMapColorOk, mindMapFileTarget, mindMapNodeFolder, mindMapPathError,
  newEdgeId, nodeIdFolderSafe, siblingPosition, snapCoord, validateCanvas, worldPoint,
} from './mindmaps';

it('refuses root, hidden, traversing and nested mind map folders', () => {
  expect(mindMapPathError('')).toBe('Mind map folder must not be the vault root.');
  expect(mindMapPathError('a/../b')).toBe('Mind map folder path must not contain "." or "..".');
  expect(mindMapPathError('.secrets/maps')).toBe('Mind maps cannot live inside hidden folders.');
  expect(mindMapPathError('Projects/ideas')).toBe('');
  expect(mindMapPathError('Projects/ideas/sub', ['Projects/ideas'])).toBe('Owning mind maps cannot be nested; use a sibling folder.');
  expect(mindMapPathError('Projects', ['Projects/ideas'])).toBe('Owning mind maps cannot be nested; use a sibling folder.');
  expect(mindMapPathError('Projects/other', ['Projects/ideas'])).toBe('');
});

it('previews the node collection folder exactly like the backend', () => {
  expect(mindMapNodeFolder('Maps/Research', 'doc_1')).toBe('Maps/Research/Nodes/doc_1');
  expect(mindMapFileTarget('Maps/Research', 'doc_1', 'Inbox/Notes/Alpha.md')).toBe('Maps/Research/Nodes/doc_1/Alpha.md');
  const docs = [{ path: 'Maps/Research/Nodes/doc_1/Beta.md' }, { path: 'Maps/Research/Nodes/doc_2/Gamma.md' }, { path: 'Maps/Research/Other.md' }];
  expect(documentsInNodeFolder(docs, 'Maps/Research', 'doc_1').map((d) => d.path)).toEqual(['Maps/Research/Nodes/doc_1/Beta.md']);
  expect(nodeIdFolderSafe('doc_1')).toBe(true);
  expect(nodeIdFolderSafe('a/b')).toBe(false);
  expect(nodeIdFolderSafe('.hidden')).toBe(false);
});

it('mirrors backend config validation before saving', () => {
  expect(validateCanvas([{ id: 'a', x: 0, y: 0 }], [{ id: 'e1', from: 'a', to: 'a' }])).toEqual(['A connector cannot link a node to itself.']);
  expect(validateCanvas([{ id: 'a', x: 0, y: 0 }], [{ id: 'bad id', from: 'a', to: 'a' }])).toContain('Connector id must use letters, numbers, dots, underscores, colons or hyphens.');
  expect(validateCanvas([{ id: 'a', x: 0, y: 0 }, { id: 'a', x: 1, y: 1 }], [])).toEqual(['Duplicate node for the same document: a']);
  expect(validateCanvas([{ id: 'a', x: Number.NaN, y: 0 }], [])).toEqual(['Node position must be a finite coordinate within bounds.']);
  expect(validateCanvas([{ id: 'a', x: 0, y: 0, color: 'red' }], [])).toEqual(['Node color must be empty or a #rgb/#rrggbb hex value.']);
  expect(validateCanvas([{ id: 'a', x: 0, y: 0, color: '#abc' }, { id: 'b', x: 10, y: 10 }], [{ id: 'e1', from: 'a', to: 'b', label: 'ok' }])).toEqual([]);
  expect(mindMapColorOk('#AABBCC')).toBe(true);
  expect(mindMapColorOk('#xyz')).toBe(false);
  expect(edgeIdOk('e12')).toBe(true);
  expect(edgeIdOk('1e')).toBe(true);
  expect(edgeIdOk('-x')).toBe(false);
  expect(newEdgeId(['e1', 'e3'])).toBe('e2');
});

it('fits, zooms and maps coordinates for the canvas', () => {
  expect(clampZoom(0.01)).toBe(0.2);
  expect(clampZoom(9)).toBe(2.5);
  const nodes = [{ id: 'a', x: 0, y: 0 }, { id: 'b', x: 1000, y: 0 }];
  const view = fitView(nodes, 800, 600)!;
  expect(view.scale).toBeCloseTo((800 - 96) / (1000 + 190));
  // The whole content band is centered: left and right margins match.
  expect(view.x).toBeCloseTo((800 - (1190) * view.scale) / 2);
  expect(fitView([], 800, 600)).toBeNull();
  const p = worldPoint(100, 100, { left: 20, top: 30 }, { x: 40, y: 50, scale: 2 });
  expect(p).toEqual({ x: (100 - 20 - 40) / 2, y: (100 - 30 - 50) / 2 });
  expect(snapCoord(13)).toBe(16);
});

it('anchors connectors at card borders and curves them horizontally', () => {
  const a = { x: 0, y: 0 }; // default 190x64 -> center 95,56
  const b = { x: 400, y: 0 }; // center 495,56
  const from = borderPoint(a, { x: 495, y: 56 });
  expect(from.x).toBeCloseTo(95 + 95 + 4); // right border + gap
  expect(from.y).toBeCloseTo(56);
  const to = borderPoint(b, { x: 95, y: 56 });
  expect(to.x).toBeCloseTo(400 + 95 - 95 - 4);
  const d = curvePath(from, to);
  expect(d).toMatch(/^M /);
  expect(d).toContain('C ');
  expect(childPosition(a)).toEqual({ x: 190 + 80, y: 0 });
  expect(siblingPosition(a)).toEqual({ x: 0, y: 112 + 48 });
});

it('places repeated branches without overlap and arranges cyclic or disconnected maps safely',async()=>{
 const {availablePosition,arrangeMindMap}=await import('./mindmaps');
 const nodes=[{id:'a',x:0,y:0},{id:'b',x:270,y:0},{id:'c',x:900,y:500}];
 expect(availablePosition({x:270,y:0},nodes)).toEqual({x:270,y:160});
 const arranged=arrangeMindMap(nodes,[{id:'ab',from:'a',to:'b'},{id:'ba',from:'b',to:'a'}]);
 expect(arranged.map(n=>n.id)).toEqual(['a','b','c']);expect(new Set(arranged.map(n=>`${n.x}/${n.y}`)).size).toBe(3);expect(arranged.every(n=>Number.isFinite(n.x)&&Number.isFinite(n.y))).toBe(true);
});
it('converts physical pointer coordinates through app UI scaling',()=>{
 expect(worldPoint(400,300,{left:100,top:100},{x:50,y:20,scale:2},1.5)).toEqual({x:75,y:(200/1.5-20)/2});
});

it('keeps arrow tangents perpendicular to all four attachment borders',()=>{
 const node={x:0,y:0,w:200,h:100};
 expect(autoSide(node,{x:100,y:300})).toBe('bottom');
 expect(autoSide(node,{x:-300,y:50})).toBe('left');
 expect(sidePoint(node,'top')).toEqual({x:100,y:0,side:'top'});
 expect(curvePath({x:0,y:0,side:'bottom'},{x:0,y:200,side:'top'})).toBe('M 0 0 C 0 90, 0 110, 0 200');
 expect(curvePath({x:200,y:0,side:'left'},{x:0,y:0,side:'right'})).toBe('M 200 0 C 110 0, 90 0, 0 0');
 expect(curvePath({x:0,y:200,side:'top'},{x:0,y:0,side:'bottom'})).toBe('M 0 200 C 0 110, 0 90, 0 0');
});

it('arranges expanded cards without overlapping neighboring rows or levels',async()=>{
 const {arrangeMindMap,nodeSize}=await import('./mindmaps');
 const nodes=[{id:'a',x:0,y:0,w:500,h:250},{id:'b',x:0,y:0,w:380,h:400},{id:'c',x:0,y:0,w:200,h:150},{id:'d',x:0,y:0}];
 const result=arrangeMindMap(nodes,[{id:'ab',from:'a',to:'b'},{id:'ac',from:'a',to:'c'},{id:'bd',from:'b',to:'d'}]);
 const [a,b,c,d]=result;
 expect(b.x).toBeGreaterThan(a.x+nodeSize(a).w);
 expect(c.y).toBeGreaterThan(b.y+nodeSize(b).h);
 expect(d.x).toBeGreaterThan(b.x+nodeSize(b).w);
 expect(result.map(n=>n.w)).toEqual(nodes.map(n=>n.w));
});
