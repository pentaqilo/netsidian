import {uiZoom} from './overlayPosition';

// Navigation only: never participates in HTML task/file drag-and-drop or writes
// board data. Native touch scrolling remains the default outside the Hand tool.
export function bindBoardPan(el:HTMLElement, hand:boolean, onEscape:()=>void){
 const owner=el.closest('.board-view')||el;
 const controls='button,input,textarea,select,a,[contenteditable="true"],[role="menu"],[role="listbox"]';
 let space=false,drag:{id:number;x:number;y:number;left:number;top:number;scale:number;moved:boolean}|null=null,blockClickUntil=0;
 const paint=()=>{el.classList.toggle('board-hand',hand||space);el.classList.toggle('board-panning',!!drag)};
 const finish=()=>{const id=drag?.id;if(drag?.moved)blockClickUntil=Date.now()+300;drag=null;paint();if(id!==undefined&&el.hasPointerCapture?.(id))el.releasePointerCapture(id)};
 const down=(e:PointerEvent)=>{
  const target=e.target instanceof Element?e.target:null;
  if(drag||!target||target.closest(controls)||![0,1].includes(e.button))return;
  if(e.pointerType==='touch'&&!hand)return;
  if(!hand&&!space&&e.button!==1&&target.closest('.board-card'))return;
  e.preventDefault();e.stopPropagation();el.focus({preventScroll:true});
  drag={id:e.pointerId,x:e.clientX,y:e.clientY,left:el.scrollLeft,top:el.scrollTop,scale:uiZoom(),moved:false};
  el.setPointerCapture?.(e.pointerId);paint();
 };
 const move=(e:PointerEvent)=>{
  if(!drag||drag.id!==e.pointerId)return;
  e.preventDefault();e.stopPropagation();const dx=(e.clientX-drag.x)/drag.scale,dy=(e.clientY-drag.y)/drag.scale;
  if(Math.hypot(dx,dy)>3){drag.moved=true;blockClickUntil=Date.now()+300;}
  el.scrollLeft=Math.max(0,Math.min(el.scrollWidth-el.clientWidth,drag.left-dx));
  el.scrollTop=Math.max(0,Math.min(el.scrollHeight-el.clientHeight,drag.top-dy));
 };
 const up=(e:PointerEvent)=>{if(drag?.id===e.pointerId){e.stopPropagation();finish()}};
 const click=(e:Event)=>{if(Date.now()<blockClickUntil||((hand||space)&&(e.target as Element).closest('.board-card')&&!(e.target as Element).closest(controls))){e.preventDefault();e.stopPropagation()}};
 const preventTaskDrag=(e:DragEvent)=>{if(hand||space||drag){e.preventDefault();e.stopPropagation()}};
 const keydown=(e:KeyboardEvent)=>{
  const target=e.target instanceof Element?e.target:null;
  if(!target||!owner.contains(target))return;
  if(e.key==='Escape'&&(hand||space||drag)){e.preventDefault();e.stopPropagation();space=false;finish();onEscape();return}
  if(target.closest(controls))return;
  // Space on a focused task retains its existing Preview shortcut.
  if(e.code==='Space'&&!e.repeat&&!target.closest('.board-card')){e.preventDefault();space=true;paint()}
 };
 const keyup=(e:KeyboardEvent)=>{if(e.code==='Space'&&space){e.preventDefault();space=false;paint()}};
 const blur=()=>{space=false;finish()};
 el.addEventListener('pointerdown',down,true);el.addEventListener('pointermove',move,true);
 el.addEventListener('pointerup',up,true);el.addEventListener('pointercancel',up,true);el.addEventListener('lostpointercapture',up,true);
 el.addEventListener('click',click,true);el.addEventListener('dblclick',click,true);el.addEventListener('auxclick',click,true);el.addEventListener('dragstart',preventTaskDrag,true);
 window.addEventListener('keydown',keydown,true);window.addEventListener('keyup',keyup,true);window.addEventListener('blur',blur);paint();
 return()=>{
  finish();el.classList.remove('board-hand','board-panning');
  el.removeEventListener('pointerdown',down,true);el.removeEventListener('pointermove',move,true);
  el.removeEventListener('pointerup',up,true);el.removeEventListener('pointercancel',up,true);el.removeEventListener('lostpointercapture',up,true);
  el.removeEventListener('click',click,true);el.removeEventListener('dblclick',click,true);el.removeEventListener('auxclick',click,true);el.removeEventListener('dragstart',preventTaskDrag,true);
  window.removeEventListener('keydown',keydown,true);window.removeEventListener('keyup',keyup,true);window.removeEventListener('blur',blur);
 };
}
