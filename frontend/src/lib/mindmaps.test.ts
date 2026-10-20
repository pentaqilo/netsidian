import { expect, it } from 'vitest';
import {
  sidePoint, autoSide, borderPoint, childPosition, clampZoom, curvePath, documentsInNodeFolder, edgeIdOk,
  fitView, mindMapColorOk, mindMapFileTarget, mindMapNodeFolder, mindMapPathError,
  newEdgeId, nodeIdFolderSafe, siblingPosition, snapCoord, validateCanvas, worldPoint,
  edgeAutoPanVelocity, effectivePanBounds, AUTO_PAN_MARGIN, AUTO_PAN_MAX_SPEED,
  uncoveredCanvasBounds, minimalRevealPan,
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

it('ramps edge auto-pan speed from zero at the zone boundary to the cap at the edge',()=>{
 const bounds={left:100,top:50,right:900,bottom:650};
 // Neutral middle: no pan in any direction.
 expect(edgeAutoPanVelocity({x:500,y:350},bounds)).toEqual({x:0,y:0});
 // Exactly at the inner boundary of the activation band there is no jump: speed starts at 0.
 expect(edgeAutoPanVelocity({x:bounds.right-AUTO_PAN_MARGIN,y:350},bounds)).toEqual({x:0,y:0});
 expect(edgeAutoPanVelocity({x:500,y:bounds.top+AUTO_PAN_MARGIN},bounds)).toEqual({x:0,y:0});
 // Halfway into the band: half speed, pointing outward (right edge pans the camera right, i.e. positive x).
 expect(edgeAutoPanVelocity({x:bounds.right-AUTO_PAN_MARGIN/2,y:350},bounds).x).toBeCloseTo(AUTO_PAN_MAX_SPEED/2);
 // At the edge and beyond: capped, never faster.
 expect(edgeAutoPanVelocity({x:bounds.right,y:350},bounds).x).toBeCloseTo(AUTO_PAN_MAX_SPEED);
 expect(edgeAutoPanVelocity({x:bounds.right+500,y:350},bounds).x).toBeCloseTo(AUTO_PAN_MAX_SPEED);
 // Left/top edges pan in the negative direction.
 expect(edgeAutoPanVelocity({x:bounds.left,y:350},bounds).x).toBeCloseTo(-AUTO_PAN_MAX_SPEED);
 expect(edgeAutoPanVelocity({x:500,y:bounds.top-10},bounds).y).toBeCloseTo(-AUTO_PAN_MAX_SPEED);
 // Corners move diagonally with both components active.
 const corner=edgeAutoPanVelocity({x:bounds.right,y:bounds.bottom},bounds);
 expect(corner.x).toBeGreaterThan(0);expect(corner.y).toBeGreaterThan(0);
 // Degenerate bounds never pan.
 expect(edgeAutoPanVelocity({x:0,y:0},{left:0,top:0,right:0,bottom:0})).toEqual({x:0,y:0});
});

it('excludes obstructing overlays from the auto-pan activation bounds per pointer position',()=>{
 const surface={left:0,top:0,right:800,bottom:600};
 const nav={left:640,top:540,right:790,bottom:590}; // floating navigation, bottom-right
 // Pointer over the overlay itself: the board is not under it, so nothing pans.
 expect(effectivePanBounds(surface,[nav],{x:700,y:560})).toBeNull();
 // Pointer along the bottom edge next to the overlay: the bottom band stops at the overlay's side.
 const beside=effectivePanBounds(surface,[nav],{x:500,y:580})!;
 expect(beside.bottom).toBe(600);
 // Pointer along the right edge within the overlay's vertical span: the right band stops at its left.
 const above=effectivePanBounds(surface,[nav],{x:795,y:560})!;
 expect(above.right).toBe(nav.left);
 // A full-width preview covering the bottom on narrow layouts removes the bottom band there
 // but leaves the left edge usable above it.
 const preview={left:0,top:300,right:800,bottom:600};
 expect(effectivePanBounds(surface,[preview],{x:400,y:590})).toBeNull();
  const upper=effectivePanBounds(surface,[preview],{x:5,y:100})!;
  expect(upper.left).toBe(0);
});

it('subtracts an overlapping preview from the visible canvas but leaves an adjacent one alone',()=>{
  const surface={left:0,top:0,right:800,bottom:600};
  // Wide layout: the preview is a flex sibling touching the right edge, so the
  // whole surface stays usable (the preview never overlaps the canvas).
  expect(uncoveredCanvasBounds(surface,{left:800,top:0,right:1120,bottom:600})).toEqual(surface);
  // Phone layout: the preview overlays the bottom of the surface, so the usable
  // canvas shrinks to the strip above it.
  expect(uncoveredCanvasBounds(surface,{left:0,top:360,right:800,bottom:600})).toEqual({left:0,top:0,right:800,bottom:360});
  // A preview docked on the right that overlaps the surface trims the right edge.
  expect(uncoveredCanvasBounds(surface,{left:600,top:0,right:800,bottom:600})).toEqual({left:0,top:0,right:600,bottom:600});
  // No preview, or a degenerate one, leaves the canvas untouched.
  expect(uncoveredCanvasBounds(surface,null)).toEqual(surface);
  expect(uncoveredCanvasBounds(surface,{left:0,top:0,right:0,bottom:0})).toEqual(surface);
  // A preview that would erase the canvas entirely is ignored (canvas preserved).
  expect(uncoveredCanvasBounds(surface,{left:0,top:0,right:800,bottom:600})).toEqual(surface);
});

it('pans the minimum needed to reveal a previewed node, never changing zoom',()=>{
  const uncovered={left:0,top:0,right:600,bottom:600};
  // Fully visible: no movement at all.
  expect(minimalRevealPan({left:100,top:100,right:290,bottom:212},uncovered)).toEqual({x:0,y:0});
  // Clipped on the right (a side preview just opened): slide left by the exact overflow.
  expect(minimalRevealPan({left:450,top:100,right:640,bottom:212},uncovered)).toEqual({x:-40,y:0});
  // Clipped on the left: slide right by the exact overflow.
  expect(minimalRevealPan({left:-30,top:100,right:160,bottom:212},uncovered)).toEqual({x:30,y:0});
  // Clipped at the bottom (phone preview overlay): slide up.
  expect(minimalRevealPan({left:100,top:520,right:290,bottom:660},uncovered)).toEqual({x:0,y:-60});
  // Clipped on two axes at once pans both, each by its own minimum.
  const both=minimalRevealPan({left:-20,top:540,right:650,bottom:660},uncovered);
  expect(both.x).toBe(20);expect(both.y).toBe(-60);
  // A node larger than the uncovered canvas is nudged by the smaller overflow so
  // its nearer edge stays visible rather than overshooting to the far edge.
  expect(minimalRevealPan({left:-40,top:0,right:900,bottom:100},uncovered)).toEqual({x:40,y:0});
  // Degenerate uncovered area (fully covered) never pans.
  expect(minimalRevealPan({left:0,top:0,right:10,bottom:10},{left:0,top:0,right:0,bottom:0})).toEqual({x:0,y:0});
});


it('reads canvas bounds whose coordinates are inherited, as on browser DOMRect objects',()=>{
 const surface=Object.create({left:100,top:50,right:900,bottom:650});
 expect(uncoveredCanvasBounds(surface,{left:900,top:50,right:1200,bottom:650})).toEqual({left:100,top:50,right:900,bottom:650});
});
it('keeps an oversized card header visible without oscillation on repeated preview resizes',()=>{
 let node={left:-40,top:-20,right:1100,bottom:900};
 const bounds={left:0,top:0,right:600,bottom:500};
 const pan=minimalRevealPan(node,bounds);expect(pan).toEqual({x:40,y:20});
 node={left:node.left+pan.x,right:node.right+pan.x,top:node.top+pan.y,bottom:node.bottom+pan.y};
 expect(minimalRevealPan(node,bounds)).toEqual({x:0,y:0});
 expect(minimalRevealPan(node,{...bounds,right:550,bottom:450})).toEqual({x:0,y:0});
 expect(minimalRevealPan({left:100,top:100,right:1400,bottom:1000},bounds)).toEqual({x:0,y:0});
});
