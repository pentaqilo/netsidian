import type {Graph2Visualization} from './graph2Model';

const TAU=Math.PI*2;
type Point={x:number;y:number};

// The drawing geometry is shared with signal placement: particles follow real links.
export function linkPoint(style:Graph2Visualization,x:number,y:number,u:number,v:number,t:number):Point{
 if(style==='circuit'){
  const mid=(x+u)/2,length=Math.abs(mid-x)+Math.abs(v-y)+Math.abs(u-mid);
  let distance=t*length;
  if(distance<=Math.abs(mid-x))return{x:x+Math.sign(mid-x)*distance,y};
  distance-=Math.abs(mid-x);
  if(distance<=Math.abs(v-y))return{x:mid,y:y+Math.sign(v-y)*distance};
  return{x:mid+Math.sign(u-mid)*(distance-Math.abs(v-y)),y:v};
 }
 if(style==='aurora'||style==='contour'){
  const dx=u-x,dy=v-y,d=Math.hypot(dx,dy)||1,bend=Math.min(28,d*.12)*(style==='contour'?-1:1);
  const cx=(x+u)/2-dy/d*bend,cy=(y+v)/2+dx/d*bend,s=1-t;
  return{x:s*s*x+2*s*t*cx+t*t*u,y:s*s*y+2*s*t*cy+t*t*v};
 }
 return{x:x+(u-x)*t,y:y+(v-y)*t};
}
export function traceLink(ctx:CanvasRenderingContext2D,style:Graph2Visualization,x:number,y:number,u:number,v:number){
 ctx.moveTo(x,y);
 if(style==='circuit'){ctx.lineTo((x+u)/2,y);ctx.lineTo((x+u)/2,v);ctx.lineTo(u,v)}
 else if(style==='aurora'||style==='contour'){
  const dx=u-x,dy=v-y,d=Math.hypot(dx,dy)||1,bend=Math.min(28,d*.12)*(style==='contour'?-1:1);
  ctx.quadraticCurveTo((x+u)/2-dy/d*bend,(y+v)/2+dx/d*bend,u,v);
 }else ctx.lineTo(u,v);
}
function polygon(ctx:CanvasRenderingContext2D,x:number,y:number,r:number,sides:number,rotation=-Math.PI/2,inner=1){
 ctx.beginPath();for(let i=0;i<sides;i++){const angle=rotation+i*TAU/sides,radius=r*(i%2?inner:1);const px=x+Math.cos(angle)*radius,py=y+Math.sin(angle)*radius;i?ctx.lineTo(px,py):ctx.moveTo(px,py)}ctx.closePath();
}
function cloud(ctx:CanvasRenderingContext2D,x:number,y:number,r:number,phase:number,shape:number){
 const points=Array.from({length:12},(_,i)=>{const angle=i*TAU/12,radius=r*(.92+.06*Math.sin(i*2.3+shape+phase));return{x:x+Math.cos(angle)*radius,y:y+Math.sin(angle)*radius}});
 ctx.beginPath();ctx.moveTo((points[11].x+points[0].x)/2,(points[11].y+points[0].y)/2);
 for(let i=0;i<12;i++){const p=points[i],next=points[(i+1)%12];ctx.quadraticCurveTo(p.x,p.y,(p.x+next.x)/2,(p.y+next.y)/2)}ctx.closePath();
}
export function drawRegion(ctx:CanvasRenderingContext2D,style:Graph2Visualization,x:number,y:number,r:number,color:string,time:number,nested:boolean,seed:number){
 // Borderless topic fields. Animation time comes from the shared Anime.js timer.
 // Low opacity, stable geometry and bounded passes keep labels and real links readable.
 if(style==='bloom'||style==='silk'){
  ctx.save();
  const phase=time*.14,opacity=nested?.65:1;
  if(style==='bloom'){
   for(let lobe=0;lobe<4;lobe++){
    const angle=seed*1.9+lobe*2.39996,spread=r*(lobe===0?.08:.35);
    const cx=x+Math.cos(angle+phase*.12)*spread,cy=y+Math.sin(angle+phase*.12)*spread;
    const radius=r*(lobe===0?1.12:.76),g=ctx.createRadialGradient(cx,cy,0,cx,cy,radius);
    g.addColorStop(0,color+(nested?'18':'25'));g.addColorStop(.38,color+'15');g.addColorStop(.72,color+'08');g.addColorStop(1,color+'00');
    ctx.globalAlpha=opacity;ctx.fillStyle=g;ctx.fillRect(cx-radius,cy-radius,radius*2,radius*2);
   }
  }else{
   // Feathered ribbon strokes follow an organic topic field; these are scenery, not links.
   for(let band=0;band<3;band++){
    const offset=(band-1)*r*.32,bend=Math.sin(seed+phase)*r*.09;
    ctx.beginPath();ctx.moveTo(x-r*.95,y+offset+r*.2);
    ctx.bezierCurveTo(x-r*.45,y+offset-r*.62+bend,x+r*.2,y+offset+r*.48+bend,x+r*.95,y+offset-r*.22);
    const g=ctx.createLinearGradient(x-r,y-r,x+r,y+r);
    g.addColorStop(0,color+'00');g.addColorStop(.25,color+'19');g.addColorStop(.52,color+'2a');g.addColorStop(.8,color+'15');g.addColorStop(1,color+'00');
    ctx.strokeStyle=g;ctx.lineCap='round';
    for(const [width,alpha] of [[r*.65,.2],[r*.4,.35],[r*.2,.55]]){
     ctx.globalAlpha=opacity*alpha;ctx.lineWidth=width;ctx.stroke();
    }
   }
  }
  ctx.restore();return;
 }
 const path=(scale=1)=>{
  if(style==='circuit'){ctx.beginPath();ctx.roundRect(x-r*.86*scale,y-r*.86*scale,r*1.72*scale,r*1.72*scale,Math.min(28,r*.15))}
  else if(style==='constellation')polygon(ctx,x,y,r*scale,8,-Math.PI/8);
  else cloud(ctx,x,y,r*scale,style==='aurora'?time*.1:0,seed);
 };
 const gradient=ctx.createRadialGradient(x-r*.25,y-r*.3,0,x,y,r);
 gradient.addColorStop(0,color+(nested?'12':style==='aurora'?'26':'1c'));gradient.addColorStop(.65,color+'0d');gradient.addColorStop(1,color+'02');
 path();ctx.fillStyle=gradient;ctx.fill();ctx.strokeStyle=color+(nested?'35':'65');ctx.lineWidth=1;ctx.stroke();
 if(style==='contour')for(let layer=1;layer<=4;layer++){path(1-layer*.11);ctx.strokeStyle=color+(layer===1?'36':'1c');ctx.stroke()}
 if(style==='aurora'){path(.95);ctx.strokeStyle=color+'50';ctx.lineWidth=2;ctx.stroke()}
 if(style==='circuit'){
  ctx.save();path();ctx.clip();ctx.strokeStyle=color+'13';ctx.lineWidth=.5;ctx.beginPath();
  for(let offset=-r;offset<=r;offset+=Math.max(22,r/10)){ctx.moveTo(x+offset,y-r);ctx.lineTo(x+offset,y+r);ctx.moveTo(x-r,y+offset);ctx.lineTo(x+r,y+offset)}ctx.stroke();ctx.restore();
 }else if(style==='constellation'){
  for(let i=0;i<8;i++){const angle=-Math.PI/8+i*TAU/8;ctx.fillStyle=color+'90';ctx.fillRect(x+Math.cos(angle)*r-1.5,y+Math.sin(angle)*r-1.5,3,3)}
 }else if(style==='nebula'&&!nested&&r>50){
  ctx.save();path();ctx.clip();for(let i=0;i<9;i++){const angle=i*2.39996+seed,d=r*Math.sqrt((i+1)/11);ctx.fillStyle=color+'45';ctx.fillRect(x+Math.cos(angle)*d,y+Math.sin(angle)*d,1,1)}ctx.restore();
 }
}
export function drawNode(ctx:CanvasRenderingContext2D,style:Graph2Visualization,x:number,y:number,r:number,color:string,time:number,emphasis:boolean,detail:boolean,unresolved:boolean){
 const pulse=1+(detail?.07:0)*Math.sin(time*1.7+x*.01),size=r*pulse;
 if(detail&&(style==='nebula'||style==='aurora'||style==='bloom'||style==='silk'||emphasis)){
  const glow=ctx.createRadialGradient(x,y,0,x,y,size*3);glow.addColorStop(0,color+'80');glow.addColorStop(.4,color+'25');glow.addColorStop(1,color+'00');ctx.fillStyle=glow;ctx.fillRect(x-size*3,y-size*3,size*6,size*6);
 }
 ctx.fillStyle=color;ctx.strokeStyle=color;ctx.lineWidth=1.3;
 if(style==='circuit')polygon(ctx,x,y,size*1.1,4);
 else if(style==='contour')polygon(ctx,x,y,size*1.15,6);
 else if(style==='constellation'&&detail)polygon(ctx,x,y,size*1.4,8,-Math.PI/2,.4);
 else{ctx.beginPath();ctx.arc(x,y,size,0,TAU)}
 unresolved?ctx.stroke():ctx.fill();
 if(detail&&!unresolved){ctx.fillStyle='#ffffffbb';ctx.beginPath();ctx.arc(x-size*.22,y-size*.25,Math.max(.6,size*.28),0,TAU);ctx.fill()}
 if(detail&&(style==='constellation'||style==='aurora')&&time>0){
  const angle=time*.75+x*.01;ctx.fillStyle=color+'aa';ctx.beginPath();ctx.arc(x+Math.cos(angle)*size*1.9,y+Math.sin(angle)*size*1.9,1.3,0,TAU);ctx.fill();
 }
}
