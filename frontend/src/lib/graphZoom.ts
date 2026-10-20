type Anchor = {clientX:number;clientY:number};
type Gesture = Event & Anchor & {scale:number};

// Chromium sends trackpad pinch as Ctrl+wheel; WebKit uses gesture events.
// Cancel these on the graph surface, leaving browser accessibility zoom elsewhere.
export function bindGraphZoom(surface:HTMLElement, zoom:(factor:number,anchor:Anchor)=>void,ignore:(target:EventTarget|null)=>boolean=()=>false){
 let gestureScale:number|null=null;
 let nativeAnchor:Anchor|null=null;
 const nativePinch=(event:Event)=>{
  const detail=(event as CustomEvent<{phase:string;factor:number}&Anchor>).detail;
  if(!detail)return;
  if(detail.phase==='start'){
   const target=document.elementFromPoint(detail.clientX,detail.clientY);
   nativeAnchor=target&&!ignore(target)&&(target===surface||surface.contains(target))?{clientX:detail.clientX,clientY:detail.clientY}:null;
  }else if(detail.phase==='end')nativeAnchor=null;
  else if(nativeAnchor&&Number.isFinite(detail.factor)&&detail.factor>0)zoom(detail.factor,nativeAnchor);
 };
 document.addEventListener('netsidian-native-pinch',nativePinch);
 const wheel=(event:WheelEvent)=>{
  if(ignore(event.target))return;
  event.preventDefault();event.stopPropagation();
  if(gestureScale!==null)return;
  const unit=event.deltaMode===1?16:event.deltaMode===2?surface.clientHeight:1;
  zoom(Math.exp(-Math.max(-500,Math.min(500,event.deltaY*unit))*.0018),event);
 };
 const start=(event:Event)=>{if(ignore(event.target))return;event.preventDefault();event.stopPropagation();gestureScale=1};
 const change=(event:Event)=>{
  if(ignore(event.target))return;
  event.preventDefault();event.stopPropagation();
  const e=event as Gesture;
  if(gestureScale===null||!Number.isFinite(e.scale)||e.scale<=0)return;
  const rect=surface.getBoundingClientRect();
  zoom(e.scale/gestureScale,{clientX:Number.isFinite(e.clientX)?e.clientX:rect.left+rect.width/2,clientY:Number.isFinite(e.clientY)?e.clientY:rect.top+rect.height/2});
  gestureScale=e.scale;
 };
 const end=(event:Event)=>{if(ignore(event.target))return;event.preventDefault();event.stopPropagation();gestureScale=null};
 const options={passive:false,capture:true};
 surface.addEventListener('wheel',wheel,options);
 surface.addEventListener('gesturestart',start,options);
 surface.addEventListener('gesturechange',change,options);
 surface.addEventListener('gestureend',end,options);
 return ()=>{document.removeEventListener('netsidian-native-pinch',nativePinch);surface.removeEventListener('wheel',wheel,options);surface.removeEventListener('gesturestart',start,options);surface.removeEventListener('gesturechange',change,options);surface.removeEventListener('gestureend',end,options)};
}
