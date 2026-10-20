import {useEffect,useId,useLayoutEffect,useRef,useState} from 'react';
import {createPortal} from 'react-dom';
import {overlayPoint,uiZoom} from '../lib/overlayPosition';
import {Icon,Tool} from './Icon';
import './CanvasNavigation.css';

export interface CanvasRect{x:number;y:number;w:number;h:number}
export interface CanvasOverview{
 bounds:CanvasRect;
 viewport:CanvasRect;
 items:(CanvasRect&{id:string;color?:string})[];
 onNavigate:(point:{x:number;y:number})=>void;
}
export interface CanvasNavigationProps{
 /** Scale factor: 1 means 100%. Navigation never changes document content. */
 zoom:number;
 onZoomIn:()=>void;onZoomOut:()=>void;onReset:()=>void;onFit:()=>void;
 minZoom?:number;maxZoom?:number;disabledFit?:boolean;
 overview?:CanvasOverview;defaultOpen?:boolean;label?:string;panMode?:boolean;onTogglePan?:()=>void;
}

// Keep world-to-minimap mapping stable while panning: bounds describe content,
// not the moving viewport. Pad and letterbox content into a fixed aspect ratio.
export function overviewFrame(bounds:CanvasRect):CanvasRect{
 const w=Math.max(1,Number.isFinite(bounds.w)?bounds.w:1),h=Math.max(1,Number.isFinite(bounds.h)?bounds.h:1);
 const padding=Math.max(24,Math.max(w,h)*.06),aspect=220/136;
 const width=Math.max(w+padding*2,(h+padding*2)*aspect),height=width/aspect;
 return {x:bounds.x+(w-width)/2,y:bounds.y+(h-height)/2,w:width,h:height};
}

export function CanvasNavigation({zoom,onZoomIn,onZoomOut,onReset,onFit,minZoom=.1,maxZoom=4,disabledFit=false,overview,defaultOpen=false,label='Canvas navigation',panMode=false,onTogglePan}:CanvasNavigationProps){
 const [detached,setDetached]=useState(false),[popupPosition,setPopupPosition]=useState({left:0,top:0});
 const panel=useRef<HTMLDivElement>(null),bar=useRef<HTMLDivElement>(null),mini=useRef<HTMLElement>(null);
 const [open,setOpen]=useState(false),[dragFrame,setDragFrame]=useState<CanvasRect|null>(null),toggle=useRef<HTMLButtonElement>(null),svg=useRef<SVGSVGElement>(null),pointer=useRef<number|null>(null),dragOffset=useRef({x:0,y:0}),id=useId();
 const initialOpenApplied=useRef(false);
 useEffect(()=>{
  if(!defaultOpen||!overview||initialOpenApplied.current)return;
  const observer=new MutationObserver(()=>apply());
  const apply=()=>{if(!initialOpenApplied.current&&panel.current&&!panel.current.closest('[hidden]')){initialOpenApplied.current=true;setOpen(true);observer.disconnect()}};
  observer.observe(document.body,{subtree:true,attributes:true,attributeFilter:['hidden']});apply();return()=>observer.disconnect();
 },[defaultOpen,!!overview]);
 const frame=dragFrame||overviewFrame(overview?.bounds||{x:0,y:0,w:1,h:1});
 // Short preview panes cannot contain a full overview. Use an anchored popover
 // in the viewport in that case, retaining the same undistorted map geometry.
 useLayoutEffect(()=>{
  if(!open){setDetached(false);return}
  const update=()=>{const host=panel.current?.parentElement,toolbar=bar.current,overview=mini.current;if(!host||!toolbar||!overview)return;if(host.closest('[hidden]')){setOpen(false);setDetached(false);return}const h=host.getBoundingClientRect(),b=toolbar.getBoundingClientRect(),m=overview.getBoundingClientRect(),z=uiZoom();if(!h.height||!b.height||!m.height)return;setDetached(b.top-h.top<m.height+8);setPopupPosition(overlayPoint(b.right-m.width,b.top-m.height-8,m.width/z,m.height/z))};
  update();window.addEventListener('resize',update);const observer=typeof ResizeObserver==='undefined'?null:new ResizeObserver(update);if(panel.current?.parentElement)observer?.observe(panel.current.parentElement);if(bar.current)observer?.observe(bar.current);
  const visibility=new MutationObserver(update);visibility.observe(document.body,{subtree:true,attributes:true,attributeFilter:['hidden']});return()=>{window.removeEventListener('resize',update);observer?.disconnect();visibility.disconnect()};
 },[open]);
 const close=()=>{initialOpenApplied.current=true;setOpen(false);setDragFrame(null);pointer.current=null;toggle.current?.focus()};
 const pointerPoint=(event:React.PointerEvent<SVGSVGElement>)=>{
  const rect=event.currentTarget.getBoundingClientRect();if(!rect.width||!rect.height)return null;
  const x=Math.max(0,Math.min(1,(event.clientX-rect.left)/rect.width)),y=Math.max(0,Math.min(1,(event.clientY-rect.top)/rect.height));
  return{x:frame.x+x*frame.w,y:frame.y+y*frame.h};
 };
 const navigate=(event:React.PointerEvent<SVGSVGElement>)=>{const point=pointerPoint(event);if(point&&overview)overview.onNavigate({x:point.x-dragOffset.current.x,y:point.y-dragOffset.current.y})};
 const minimap=open&&overview?<section ref={mini} className={'canvas-minimap'+(detached?' canvas-minimap-popover':'')} style={detached?{position:'fixed',zIndex:1000,...popupPosition}:undefined} id={id} aria-label={label.replace(/ navigation$/i,'')+' overview'}>
   <header><span>Overview</span><button type="button" aria-label="Close minimap" onClick={close}><Icon name="close" size={13}/></button></header>
   <svg ref={svg} width="220" height="136" viewBox={`${frame.x} ${frame.y} ${frame.w} ${frame.h}`} role="group" tabIndex={0} aria-label="Minimap. Click or drag to navigate; arrow keys move the view."
    onPointerDown={e=>{if(e.button!==0)return;e.preventDefault();e.stopPropagation();e.currentTarget.focus();pointer.current=e.pointerId;setDragFrame(frame);e.currentTarget.setPointerCapture(e.pointerId);const point=pointerPoint(e);if(point&&(e.target as Element).closest('.canvas-minimap-viewport')){const v=overview.viewport;dragOffset.current={x:point.x-v.x-v.w/2,y:point.y-v.y-v.h/2}}else{dragOffset.current={x:0,y:0};navigate(e)}}}
    onPointerMove={e=>{if(pointer.current===e.pointerId)navigate(e)}}
    onPointerUp={()=>{pointer.current=null;setDragFrame(null)}} onPointerCancel={()=>{pointer.current=null;setDragFrame(null)}} onLostPointerCapture={()=>{pointer.current=null;setDragFrame(null)}}
    onKeyDown={e=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();close();return}if(!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown','Home'].includes(e.key))return;e.preventDefault();e.stopPropagation();const v=overview.viewport,amount=e.shiftKey?.5:.15;overview.onNavigate(e.key==='Home'?{x:overview.bounds.x+overview.bounds.w/2,y:overview.bounds.y+overview.bounds.h/2}:{x:v.x+v.w/2+(e.key==='ArrowLeft'?-1:e.key==='ArrowRight'?1:0)*v.w*amount,y:v.y+v.h/2+(e.key==='ArrowUp'?-1:e.key==='ArrowDown'?1:0)*v.h*amount})}}>
    <g aria-hidden="true">{overview.items.filter(item=>[item.x,item.y,item.w,item.h].every(Number.isFinite)).map(item=><rect key={item.id} x={item.x} y={item.y} width={Math.max(item.w,frame.w/220*2)} height={Math.max(item.h,frame.h/136*2)} rx={frame.w/220*1.5} fill={item.color||'var(--accent)'} className="canvas-minimap-item"/>)}
    <rect className="canvas-minimap-viewport" x={overview.viewport.x} y={overview.viewport.y} width={Math.max(1,overview.viewport.w)} height={Math.max(1,overview.viewport.h)} vectorEffect="non-scaling-stroke"/></g>
   </svg>
   <small>Click or drag to navigate</small>
  </section>:null;
 return <div ref={panel} className="canvas-navigation" onPointerDown={e=>e.stopPropagation()} onPointerUp={e=>e.stopPropagation()} onPointerMove={e=>e.stopPropagation()} onDoubleClick={e=>e.stopPropagation()} onWheelCapture={e=>e.stopPropagation()}
  onDragOver={e=>{e.stopPropagation();e.dataTransfer.dropEffect='none'}} onDrop={e=>{e.preventDefault();e.stopPropagation()}}>
  {detached&&minimap?createPortal(minimap,document.body):minimap}
  <div ref={bar} className="canvas-navigation-bar" role="toolbar" aria-label={label} onKeyDown={e=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;e.preventDefault();e.stopPropagation();const buttons=[...e.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')],index=buttons.indexOf(document.activeElement as HTMLButtonElement);buttons[e.key==='Home'?0:e.key==='End'?buttons.length-1:(index+(e.key==='ArrowRight'?1:buttons.length-1))%buttons.length]?.focus()}}>
   {onTogglePan&&<Tool icon="hand" label="Hand tool" active={panMode} onClick={onTogglePan}/>}
   <Tool icon="minus" label="Zoom out" disabled={zoom<=minZoom} onClick={onZoomOut}/>
   <button className="canvas-navigation-zoom" type="button" aria-label="Reset zoom to 100%" aria-description={`Current zoom ${Math.round(zoom*100)}%`} title="Reset zoom to 100%" onClick={onReset}>{Math.round(zoom*100)}%</button>
   <Tool icon="plus" label="Zoom in" disabled={zoom>=maxZoom} onClick={onZoomIn}/>
   <span aria-hidden="true"/>
   <Tool icon="expand" label="Fit canvas to content" disabled={disabledFit} onClick={onFit}/>
   <button ref={toggle} className={'tool'+(open?' active':'')} type="button" disabled={!overview} aria-label={open?'Hide minimap':'Show minimap'} title={overview?(open?'Hide minimap':'Show minimap'):'Add content to use the minimap'} aria-expanded={open&&!!overview} aria-controls={open?id:undefined} onClick={()=>{initialOpenApplied.current=true;setOpen(!open);if(!open)requestAnimationFrame(()=>svg.current?.focus())}}><Icon name="map"/></button>
  </div>
 </div>;
}
