import {forceSimulation,forceLink,forceManyBody,forceCollide,forceX,forceY,type Simulation,type SimulationNodeDatum} from 'd3-force';
import {buildAtlas,type AtlasRegion} from '../lib/graph2Atlas';
interface Node extends SimulationNodeDatum {id:string;degree:number}
let simulation:Simulation<Node,undefined>|undefined,timer:ReturnType<typeof setTimeout>|undefined,nodes:Node[]=[],paused=false,tick=0,generation=0,regions:AtlasRegion[]=[];
const send=(settled=false)=>{const positions=new Float32Array(nodes.length*2);nodes.forEach((n,i)=>{positions[i*2]=n.x||0;positions[i*2+1]=n.y||0});self.postMessage({positions,settled,generation,regions}, {transfer:[positions.buffer]})};
function step(){clearTimeout(timer);if(!simulation||paused)return;const start=performance.now();do{simulation.tick();tick++}while(performance.now()-start<12&&tick%6!==0);const settled=simulation.alpha()<.018;send(settled);if(!settled)timer=setTimeout(step,30)}
self.onmessage=e=>{const m=e.data;if(m.type==='init'){clearTimeout(timer);simulation?.stop();simulation=undefined;paused=false;tick=0;generation=m.generation;nodes=m.nodes;regions=[];
 if(m.layout==='atlas'){const atlas=buildAtlas(m.documents,m.edges);regions=atlas.regions;nodes.forEach((n,i)=>{n.x=n.fx??atlas.positions[i*2];n.y=n.fy??atlas.positions[i*2+1]});send(true);return;}
 simulation=forceSimulation(nodes).stop().alpha(.7).alphaDecay(.035).velocityDecay(.4)
 .force('links',forceLink<Node, {source:string;target:string}>(m.edges).id(n=>n.id).distance(m.distance).strength(.3))
 .force('repel',forceManyBody<Node>().strength(-m.repel).theta(.9))
 .force('collision',forceCollide<Node>(n=>10+Math.min(12,Math.sqrt(n.degree||0)*2)).strength(.7))
 .force('x',forceX<Node>(0).strength(n=>n.degree>0?.025:.01)).force('y',forceY<Node>(0).strength(n=>n.degree>0?.025:.01));send();step();
 }else if(m.generation!==generation)return;
 else if(m.type==='pause'){paused=m.value;if(!paused&&simulation){simulation.alpha(Math.max(.15,simulation.alpha()));step()}}
 else if(m.type==='pin'){const n=nodes[m.index];if(n){n.fx=m.x;n.fy=m.y;if(m.x!=null){n.x=m.x;n.y=m.y}if(simulation){simulation.alpha(Math.max(.15,simulation.alpha()));if(!paused)step();else send()}else send(true)}}
 else if(m.type==='forces'&&simulation){simulation.force('repel',forceManyBody<Node>().strength(-m.repel).theta(.9));simulation.force('links',forceLink<Node,{source:string;target:string}>(m.edges).id(n=>n.id).distance(m.distance).strength(.3));simulation.alpha(.5);if(!paused)step()}
};
