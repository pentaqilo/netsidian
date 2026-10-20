// @vitest-environment jsdom
import {describe,it,expect,vi} from 'vitest';
import {bindGraphZoom} from './graphZoom';

describe('graph gesture ownership',()=>{
 it('routes native Linux pinch only to the graph where it began, and cleans up',()=>{
  const graph=document.createElement('canvas'),outside=document.createElement('div');document.body.append(graph,outside);
  let target:Element=graph;
  const originalHit=Object.getOwnPropertyDescriptor(document,'elementFromPoint');
  Object.defineProperty(document,'elementFromPoint',{configurable:true,value:()=>target});
  const zoom=vi.fn(),unbind=bindGraphZoom(graph,zoom);
  const pinch=(phase:string,factor=1)=>document.dispatchEvent(new CustomEvent('netsidian-native-pinch',{detail:{phase,factor,clientX:100,clientY:80}}));
  pinch('start');target=outside;pinch('change',1.2);pinch('change',1.25);pinch('end');pinch('change',2);
  expect(zoom.mock.calls).toEqual([[1.2,{clientX:100,clientY:80}],[1.25,{clientX:100,clientY:80}]]);
  pinch('start');target=graph;pinch('change',2);pinch('end');expect(zoom).toHaveBeenCalledTimes(2);
  pinch('start');pinch('change',NaN);pinch('change',0);expect(zoom).toHaveBeenCalledTimes(2);
  unbind();pinch('change',2);expect(zoom).toHaveBeenCalledTimes(2);
  if(originalHit)Object.defineProperty(document,'elementFromPoint',originalHit);else Reflect.deleteProperty(document,'elementFromPoint');
  graph.remove();outside.remove();
 });
 it('cancels Ctrl+wheel only over the graph and removes its listeners',()=>{
  const graph=document.createElement('canvas'),outside=document.createElement('div');document.body.append(graph,outside);
  const zoom=vi.fn(),unbind=bindGraphZoom(graph,zoom);
  const event=new WheelEvent('wheel',{deltaY:-100,ctrlKey:true,bubbles:true,cancelable:true,clientX:100,clientY:80});
  graph.dispatchEvent(event);expect(event.defaultPrevented).toBe(true);expect(zoom.mock.calls[0][0]).toBeGreaterThan(1);
  const other=new WheelEvent('wheel',{deltaY:-100,ctrlKey:true,cancelable:true});outside.dispatchEvent(other);expect(other.defaultPrevented).toBe(false);
  unbind();const after=new WheelEvent('wheel',{deltaY:-100,cancelable:true});graph.dispatchEvent(after);expect(after.defaultPrevented).toBe(false);
  graph.remove();outside.remove();
 });
 it('handles WebKit pinch scale incrementally and suppresses duplicate wheel events',()=>{
  const graph=document.createElement('canvas'),zoom=vi.fn(),unbind=bindGraphZoom(graph,zoom);
  const gesture=(type:string,scale:number)=>{const event=new Event(type,{cancelable:true});Object.assign(event,{scale,clientX:100,clientY:80});graph.dispatchEvent(event);expect(event.defaultPrevented).toBe(true)};
  gesture('gesturestart',1);gesture('gesturechange',1.2);gesture('gesturechange',1.5);
  expect(zoom.mock.calls.map(call=>call[0])).toEqual([1.2,1.25]);
  graph.dispatchEvent(new WheelEvent('wheel',{deltaY:-100,cancelable:true}));expect(zoom).toHaveBeenCalledTimes(2);
  gesture('gestureend',1.5);unbind();
 });
});

it('leaves native scrolling and pinch in editable children to the editor',()=>{
 const surface=document.createElement('div'),editor=document.createElement('div');editor.className='inline-editor';surface.append(editor);document.body.append(surface);
 const zoom=vi.fn(),unbind=bindGraphZoom(surface,zoom,target=>target instanceof Element&&!!target.closest('.inline-editor'));
 const wheel=new WheelEvent('wheel',{bubbles:true,cancelable:true,deltaY:100});editor.dispatchEvent(wheel);
 expect(wheel.defaultPrevented).toBe(false);expect(zoom).not.toHaveBeenCalled();
 const pinch=new Event('gesturestart',{bubbles:true,cancelable:true});editor.dispatchEvent(pinch);
 expect(pinch.defaultPrevented).toBe(false);
 unbind();surface.remove();
});
