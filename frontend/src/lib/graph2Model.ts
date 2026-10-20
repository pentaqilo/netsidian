export interface Graph2Node {id:string;path:string;title:string;tags:string[];links:number;kind:'document'|'unresolved'|'tag';memory?:boolean;atlasGroup?:string}
export interface Graph2Data {nodes:Graph2Node[];edges:{source:string;target:string}[];total:number}
export type Graph2Visualization='nebula'|'constellation'|'aurora'|'circuit'|'contour'|'bloom'|'silk';
export const graph2Visualizations:{id:Graph2Visualization;title:string;description:string}[]=[
 {id:'nebula',title:'Nebula',description:'Luminous stars, cloud-shaped topics and flowing particles'},
 {id:'constellation',title:'Constellation',description:'Faceted topic regions, star nodes and orbital accents'},
 {id:'aurora',title:'Aurora',description:'Iridescent ribbons, breathing nodes and curved light trails'},
 {id:'circuit',title:'Circuit',description:'Rounded circuit boards, diamond nodes and moving signals'},
 {id:'contour',title:'Contour',description:'Layered terrain contours, crystal nodes and traveling pulses'},
 {id:'bloom',title:'Bloom',description:'Soft overlapping color fields; crisp notes without region outlines'},
 {id:'silk',title:'Silk',description:'Flowing gradient ribbons and luminous topic landscapes'},
];
export interface Graph2Settings {layout:'atlas'|'links';groupBy:'folder'|'smart';bundle:boolean;visualization:Graph2Visualization;motion:boolean;folder:string;tag:string;filter:string;orphans:boolean;unresolved:boolean;tags:boolean;colorBy:'folder'|'tag'|'none';nodeSize:number;linkWidth:number;labels:number;repel:number;distance:number;arrows:boolean}
export const defaultGraph2Settings:Graph2Settings={layout:'links',groupBy:'folder',bundle:true,visualization:'nebula',motion:true,folder:'',tag:'',filter:'',orphans:true,unresolved:false,tags:false,colorBy:'folder',nodeSize:1,linkWidth:1,labels:1,repel:180,distance:90,arrows:false};
export const graphPalette=['#b7a4ed','#77bec9','#91bd9a','#e0b480','#ce95b0','#96aee0','#c8bf83','#9ebfc1'];
export function folderOf(path:string){const i=path.lastIndexOf('/');return i<0?'':path.slice(0,i)}
// Small, predictable subset of Obsidian-style search: quoted phrases,
// path:/folder and tag:#topic terms, with optional minus exclusions.
export function graphMatches(n:Graph2Node,query:string){const terms=query.match(/(?:[^\s"]*"[^"]*"|[^\s]+)/g)||[];return terms.every(raw=>{const exclude=raw.startsWith('-');const term=(exclude?raw.slice(1):raw).replace(/"/g,'').toLocaleLowerCase();let match:boolean;if(term.startsWith('tag:')||term.startsWith('#')){const t=(term.startsWith('tag:')?term.slice(4):term).replace(/^#/,'');match=(n.tags||[]).some(tag=>tag.toLocaleLowerCase()===t||tag.toLocaleLowerCase().startsWith(t+'/'))}else if(term.startsWith('path:'))match=n.path.toLocaleLowerCase().includes(term.slice(5));else match=(n.title+' '+n.path+' '+(n.tags||[]).join(' ')).toLocaleLowerCase().includes(term);return exclude?!match:match})}
export function prepareGraph2(data:Graph2Data,s:Graph2Settings){
 const documentIDs=new Set(data.nodes.filter(n=>n.kind==='document'&&(!s.folder||n.path.startsWith(s.folder+'/'))&&(!s.tag||(n.tags||[]).some(t=>t===s.tag||t.startsWith(s.tag+'/')))&&graphMatches(n,s.filter)&& (s.orphans||n.links>0)).map(n=>n.id));
 const unresolvedIDs=new Set<string>();if(s.unresolved)for(const e of data.edges)if(documentIDs.has(e.source))unresolvedIDs.add(e.target);
 const nodes=data.nodes.filter(n=>documentIDs.has(n.id)||(n.kind==='unresolved'&&unresolvedIDs.has(n.id))).map(n=>({...n,tags:n.tags||[]}));const ids=new Set(nodes.map(n=>n.id));const edges=data.edges.filter(e=>ids.has(e.source)&&ids.has(e.target));
 if(s.tags){const seen=new Set<string>();for(const n of [...nodes])if(n.kind==='document')for(const tag of n.tags){const id='tag:'+tag;if(!seen.has(id)){seen.add(id);nodes.push({id,path:'#'+tag,title:'#'+tag,tags:[tag],links:0,kind:'tag'})}edges.push({source:n.id,target:id})}}
 if(s.groupBy==='smart'){const frequency=new Map<string,number>();for(const n of nodes)for(const t of n.tags)if(!['memo','note','document'].includes(t))frequency.set(t,(frequency.get(t)||0)+1);const adjacent=new Map<string,Graph2Node[]>(),lookup=new Map(nodes.map(n=>[n.id,n]));for(const e of edges)for(const [a,b] of [[e.source,e.target],[e.target,e.source]]){if(!adjacent.has(a))adjacent.set(a,[]);const other=lookup.get(b);if(other)adjacent.get(a)!.push(other)}for(const n of nodes){const scores=new Map<string,number>();for(const t of n.tags)if(frequency.has(t))scores.set(t,3*Math.log((nodes.length+1)/(frequency.get(t)!+1)));for(const neighbor of adjacent.get(n.id)||[])for(const t of neighbor.tags)if(frequency.has(t))scores.set(t,(scores.get(t)||0)+Math.log((nodes.length+1)/(frequency.get(t)!+1)));const best=[...scores].sort((a,b)=>b[1]-a[1]||a[0].localeCompare(b[0]))[0];if(best&&best[1]>0)n.atlasGroup='Topic · '+best[0]}}
 const group=(n:Graph2Node)=>s.colorBy==='folder'?((s.layout==='links'?folderOf(n.path):folderOf(n.path).split('/')[0])||'Start'):s.colorBy==='tag'?(n.tags[0]||'Untagged'):'Documents';
 const groups=[...new Set(nodes.map(group))].sort(),used=new Set<string>();
 const colors=new Map(groups.map(g=>{
  let hash=0;for(const c of g)hash=(Math.imul(hash,31)+c.charCodeAt(0))|0;
  let color=graphPalette[(hash>>>0)%graphPalette.length];
  // Avoid palette collisions between visible groups; extend it for large vaults.
  for(let step=1;used.has(color)&&step<=graphPalette.length;step++)color=graphPalette[((hash>>>0)+step)%graphPalette.length];
  let seed=hash>>>0;
  while(used.has(color)){
   seed=(Math.imul(seed,1664525)+1013904223)>>>0;
   color='#'+[seed&255,(seed>>>8)&255,(seed>>>16)&255].map(value=>(96+Math.floor(value/2)).toString(16).padStart(2,'0')).join('');
  }
  used.add(color);return [g,color];
 }));
 const color=(n:Graph2Node)=>n.kind==='unresolved'?'#929aa5':n.kind==='tag'?'#d6bd77':colors.get(group(n))||graphPalette[0];
 return {nodes,edges,groups,colors,color};
}
export interface Camera {x:number;y:number;k:number}
export function zoomCamera(camera:Camera,factor:number,anchor:{x:number;y:number},minimum=.025,maximum=20):Camera{const k=Math.max(minimum,Math.min(maximum,camera.k*factor));const ratio=k/camera.k;return {k,x:anchor.x-(anchor.x-camera.x)*ratio,y:anchor.y-(anchor.y-camera.y)*ratio}}
