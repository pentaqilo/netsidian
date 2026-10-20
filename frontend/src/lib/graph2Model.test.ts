import {describe,it,expect} from 'vitest';
import {defaultGraph2Settings,prepareGraph2,graphMatches,zoomCamera,type Graph2Data} from './graph2Model';
const data:Graph2Data={total:3,nodes:[{id:'a',title:'Launch plan',path:'Work/Launch.md',tags:['project/api'],links:1,kind:'document'},{id:'b',title:'Budget',path:'Home/Budget.md',tags:['money'],links:1,kind:'document'},{id:'c',title:'Orphan',path:'Other.md',tags:[],links:0,kind:'document'}],edges:[{source:'a',target:'b'}]};
describe('Graph 2 continuous navigation',()=>{
 it('colors nested folder groups distinctly in link layout and supports monochrome',()=>{
  const nodes=['Vault/Work/A.md','Vault/Work/B.md','Vault/Home/C.md','Vault/Archive/D.md'].map((path,i)=>({...data.nodes[0],id:String(i),path}));
  const graph=prepareGraph2({nodes,edges:[],total:4},{...defaultGraph2Settings,layout:'links'});
  expect(graph.color(nodes[0])).toBe(graph.color(nodes[1]));
  expect(new Set(graph.nodes.map(graph.color)).size).toBe(3);
  const plain=prepareGraph2({nodes,edges:[],total:4},{...defaultGraph2Settings,layout:'links',colorBy:'none'});
  expect(new Set(plain.nodes.map(plain.color)).size).toBe(1);
 });
 it('extends group colors beyond the base palette for large vaults',()=>{
  const nodes=Array.from({length:1000},(_,i)=>({...data.nodes[0],id:String(i),path:`Vault/Folder-${i}/Note.md`}));
  const graph=prepareGraph2({nodes,edges:[],total:nodes.length},{...defaultGraph2Settings,layout:'links'});
  expect(new Set(graph.nodes.map(graph.color)).size).toBe(nodes.length);
 });
 it('keeps all documents and cross-folder links by default',()=>{const g=prepareGraph2(data,defaultGraph2Settings);expect(g.nodes.map(n=>n.id)).toEqual(['a','b','c']);expect(g.edges).toEqual(data.edges)});
 it('finds tags, paths, phrases and excluded terms without changing the graph',()=>{expect(graphMatches(data.nodes[0],'path:Work tag:project "launch plan" -budget')).toBe(true);expect(graphMatches(data.nodes[0],'#project')).toBe(true);expect(graphMatches(data.nodes[1],'#project')).toBe(false);expect(graphMatches(data.nodes[1],'tag:project')).toBe(false);expect(prepareGraph2(data,defaultGraph2Settings).edges).toHaveLength(1)});
 it('filters explicitly and can add tag nodes',()=>{const g=prepareGraph2(data,{...defaultGraph2Settings,tags:true,folder:'Work'});expect(g.nodes.map(n=>n.id)).toEqual(['a','tag:project/api']);expect(g.edges).toEqual([{source:'a',target:'tag:project/api'}])});
 it('handles the optional tag field in backend graph nodes',()=>{const untagged={...data.nodes[2],tags:undefined} as unknown as typeof data.nodes[2];expect(graphMatches(untagged,'Orphan')).toBe(true);expect(graphMatches(untagged,'tag:project')).toBe(false);expect(prepareGraph2({...data,nodes:[untagged]},{...defaultGraph2Settings,tag:'project'}).nodes).toEqual([])});
 it('keeps the world point under the cursor stationary while zooming',()=>{const camera={k:.5,x:100,y:80},anchor={x:320,y:240};const next=zoomCamera(camera,2,anchor);expect((anchor.x-camera.x)/camera.k).toBe((anchor.x-next.x)/next.k);expect((anchor.y-camera.y)/camera.k).toBe((anchor.y-next.y)/next.k)});
});
it('smart grouping retains every document and real reference and gives untagged neighbors a topic',()=>{const g=prepareGraph2(data,{...defaultGraph2Settings,groupBy:'smart'});expect(g.nodes.map(n=>n.id)).toEqual(data.nodes.map(n=>n.id));expect(g.edges).toEqual(data.edges);expect(g.nodes[0].atlasGroup).toContain('project/api');expect(g.nodes[2].atlasGroup).toBeUndefined();expect(prepareGraph2(data,defaultGraph2Settings).nodes[0].atlasGroup).toBeUndefined()});

it('uses the real document link layout by default',()=>{expect(defaultGraph2Settings.layout).toBe('links')});
