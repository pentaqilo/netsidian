// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {CanvasNavigation,overviewFrame} from './CanvasNavigation';
let root:Root,host:HTMLDivElement;
const navigate=vi.fn(),fit=vi.fn(),zoom=vi.fn();
beforeEach(()=>{vi.clearAllMocks();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const mount=()=>act(async()=>root.render(<CanvasNavigation zoom={1} label="Mind map navigation" onZoomIn={zoom} onZoomOut={zoom} onReset={zoom} onFit={fit} overview={{bounds:{x:-100,y:-50,w:600,h:200},viewport:{x:0,y:0,w:200,h:100},items:[{id:'a',x:0,y:0,w:50,h:20}],onNavigate:navigate}}/>));
const click=(label:string)=>act(async()=>host.querySelector<HTMLButtonElement>(`[aria-label="${label}"]`)!.click());
it('fits a stable overview frame around negative coordinates and preserves aspect ratio',()=>{
 const frame=overviewFrame({x:-100,y:-50,w:600,h:200});expect(frame.w/frame.h).toBeCloseTo(220/136);expect(frame.x).toBeLessThan(-100);expect(frame.y).toBeLessThan(-50);expect(frame.x+frame.w).toBeGreaterThan(500);
});
it('toggles the minimap, navigates with keys and closes with Escape without invoking fit',async()=>{
 await mount();expect(host.querySelector('.canvas-minimap')).toBeNull();await click('Show minimap');const svg=host.querySelector('svg[tabindex]')!;
 await act(async()=>svg.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true,cancelable:true})));expect(navigate).toHaveBeenLastCalledWith({x:130,y:50});
 await act(async()=>svg.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true})));expect(host.querySelector('.canvas-minimap')).toBeNull();expect(document.activeElement).toBe(host.querySelector('[aria-label="Show minimap"]'));expect(fit).not.toHaveBeenCalled();
});
it('maps physical pointer positions through scaled SVG bounds without application zoom drift',async()=>{
 await mount();await click('Show minimap');const svg=host.querySelector<SVGSVGElement>('svg[tabindex]')!;
 svg.getBoundingClientRect=()=>({left:100,top:50,width:440,height:272,right:540,bottom:322,x:100,y:50,toJSON:()=>({})});svg.setPointerCapture=vi.fn();
 await act(async()=>svg.dispatchEvent(new MouseEvent('pointerdown',{bubbles:true,button:0,clientX:320,clientY:186})));
 expect(navigate).toHaveBeenLastCalledWith({x:200,y:50});expect(zoom).not.toHaveBeenCalled();
});
it('drags the viewport rectangle without jumping its center to the grabbed edge',async()=>{
 await mount();await click('Show minimap');const svg=host.querySelector<SVGSVGElement>('svg[tabindex]')!;
 svg.getBoundingClientRect=()=>({left:100,top:50,width:440,height:272,right:540,bottom:322,x:100,y:50,toJSON:()=>({})});svg.setPointerCapture=vi.fn();
 await act(async()=>host.querySelector('.canvas-minimap-viewport')!.dispatchEvent(new MouseEvent('pointerdown',{bubbles:true,button:0,clientX:320,clientY:186})));
 expect(navigate).not.toHaveBeenCalled();
 await act(async()=>svg.dispatchEvent(new MouseEvent('pointermove',{bubbles:true,clientX:364,clientY:186})));
 expect(navigate.mock.lastCall?.[0].x).toBeCloseTo(167.2);expect(navigate.mock.lastCall?.[0].y).toBeCloseTo(50);
});
it('uses an anchored popover when the canvas is too short for the minimap',async()=>{
 const rect=vi.spyOn(Element.prototype,'getBoundingClientRect').mockImplementation(function(this:Element){const box=this.classList.contains('canvas-navigation-bar')?{x:10,y:70,width:220,height:32}:this.classList.contains('canvas-minimap')?{x:10,y:-98,width:220,height:160}:{x:0,y:0,width:300,height:110};return {...box,left:box.x,top:box.y,right:box.x+box.width,bottom:box.y+box.height,toJSON:()=>({})}});
 try{await mount();await click('Show minimap');const popover=document.querySelector('.canvas-minimap-popover');expect(popover).not.toBeNull();expect(host.contains(popover)).toBe(false);await act(async()=>popover!.querySelector('svg[tabindex]')!.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true})));expect(document.querySelector('.canvas-minimap-popover')).toBeNull()}finally{rect.mockRestore()}
});

it('provides an independent accessible Hand tool when the canvas supports it',async()=>{
 const pan=vi.fn();await act(async()=>root.render(<CanvasNavigation zoom={1} onZoomIn={zoom} onZoomOut={zoom} onReset={zoom} onFit={fit} panMode onTogglePan={pan}/>));
 expect(host.querySelector('[aria-label="Hand tool"]')?.getAttribute('aria-pressed')).toBe('true');await click('Hand tool');expect(pan).toHaveBeenCalledOnce();expect(zoom).not.toHaveBeenCalled();expect(fit).not.toHaveBeenCalled();
});
it('opens the configured minimap when content arrives and respects manual closing',async()=>{
 const overview={bounds:{x:0,y:0,w:100,h:100},viewport:{x:0,y:0,w:50,h:50},items:[],onNavigate:navigate};
 await act(async()=>root.render(<CanvasNavigation defaultOpen zoom={1} onZoomIn={zoom} onZoomOut={zoom} onReset={zoom} onFit={fit}/>));expect(host.querySelector('.canvas-minimap')).toBeNull();
 await act(async()=>root.render(<CanvasNavigation defaultOpen overview={overview} zoom={1} onZoomIn={zoom} onZoomOut={zoom} onReset={zoom} onFit={fit}/>));expect(host.querySelector('.canvas-minimap')).not.toBeNull();await click('Hide minimap');
 await act(async()=>root.render(<CanvasNavigation defaultOpen overview={{...overview,items:[{id:'new',x:0,y:0,w:1,h:1}]}} zoom={1} onZoomIn={zoom} onZoomOut={zoom} onReset={zoom} onFit={fit}/>));expect(host.querySelector('.canvas-minimap')).toBeNull();
});
it('defers the default minimap until a retained hidden tab becomes visible',async()=>{
 host.hidden=true;const overview={bounds:{x:0,y:0,w:100,h:100},viewport:{x:0,y:0,w:50,h:50},items:[],onNavigate:navigate};
 await act(async()=>root.render(<CanvasNavigation defaultOpen overview={overview} zoom={1} onZoomIn={zoom} onZoomOut={zoom} onReset={zoom} onFit={fit}/>));expect(host.querySelector('.canvas-minimap')).toBeNull();
 await act(async()=>{host.hidden=false;await Promise.resolve()});expect(host.querySelector('.canvas-minimap')).not.toBeNull();
});
