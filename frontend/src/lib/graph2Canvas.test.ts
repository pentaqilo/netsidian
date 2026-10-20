// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {Graph2Canvas} from './graph2Canvas';
afterEach(()=>vi.useRealTimers());
function fixture(){
 const engine=Object.create(Graph2Canvas.prototype) as Graph2Canvas;
 const state={settings:{nodeSize:1},receivedPositions:true,nodes:[{id:'a',links:4},{id:'b',links:1}],positions:new Float32Array([-100,20,300,220]),regions:[],colors:['#123456','#abcdef'],camera:{x:50,y:-30,k:2},width:800,height:400,actions:{changed:vi.fn()},invalidate:vi.fn(),remember:vi.fn(),options:{},fittedZoom:.5};
 Object.assign(engine,state);return {engine,state};
}
it('reports the actual world viewport and finite node bounds without mutating positions',()=>{
 const {engine,state}=fixture(),before=Array.from(state.positions),overview=engine.getOverview()!;
 expect(overview.bounds).toEqual({x:-100,y:20,w:400,h:200});expect(overview.viewport).toEqual({x:-25,y:15,w:400,h:200});expect(overview.items.map(n=>n.id)).toEqual(['a','b']);expect(Array.from(state.positions)).toEqual(before);expect(engine.maxScale).toBe(60);
});
it('centers a minimap destination without changing scale or node positions and ignores invalid destinations',()=>{
 vi.useFakeTimers();const {engine,state}=fixture(),before=Array.from(state.positions);
 engine.centerOn(120,-20);const overview=engine.getOverview()!;
 expect(overview.viewport.x+overview.viewport.w/2).toBe(120);expect(overview.viewport.y+overview.viewport.h/2).toBe(-20);expect(Array.from(state.positions)).toEqual(before);expect(state.actions.changed).toHaveBeenCalledTimes(1);
 engine.centerOn(NaN,0);expect(state.actions.changed).toHaveBeenCalledTimes(1);vi.runAllTimers();expect(state.remember).toHaveBeenCalledTimes(1);
});
it('caches a quiet world-anchored dot texture, retaining small dots and bounded density at extreme zoom',()=>{
 const {engine,state}=fixture(),tile={beginPath:vi.fn(),arc:vi.fn(),fill:vi.fn(),fillStyle:''};
 const getContext=vi.spyOn(HTMLCanvasElement.prototype,'getContext').mockReturnValue(tile as never);
 const context={createPattern:vi.fn((_tile:HTMLCanvasElement,_repeat:string)=>({})),save:vi.fn(),restore:vi.fn(),translate:vi.fn(),fillRect:vi.fn(),fillStyle:''};
 Object.assign(engine,{context,dotColor:'rgba(120,120,120,.12)'});
 const draw=()=>((engine as unknown as {drawDots:()=>void}).drawDots());
 try{
  draw();draw();expect(context.createPattern).toHaveBeenCalledTimes(1);expect(context.translate).toHaveBeenLastCalledWith(2,18);expect(context.fillRect).toHaveBeenLastCalledWith(-2,-18,800,400);expect(tile.arc.mock.calls[0][2]).toBe(1);
  state.camera.k=.0001;draw();const denseTile=context.createPattern.mock.calls[1][0] as HTMLCanvasElement;expect(denseTile.width).toBeGreaterThanOrEqual(12);expect(denseTile.width).toBeLessThanOrEqual(24);
  state.camera.k=30;draw();expect(tile.arc.mock.calls.at(-1)?.[2]).toBe(1);expect(context.save).toHaveBeenCalledTimes(4);expect(context.restore).toHaveBeenCalledTimes(4);
 }finally{getContext.mockRestore()}
});
