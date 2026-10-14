import {describe,it,expect} from 'vitest';
import {linkPoint} from './graph2Effects';
import {graph2Visualizations,defaultGraph2Settings,prepareGraph2,type Graph2Data} from './graph2Model';

describe('Graph effects preserve document navigation',()=>{
 it('keeps link endpoints exact for every visual style, including reversed and zero-length links',()=>{
  for(const {id} of graph2Visualizations)for(const [x,y,u,v] of [[12,30,200,180],[200,180,12,30],[30,30,30,30]]){
   expect(linkPoint(id,x,y,u,v,0)).toEqual({x,y});expect(linkPoint(id,x,y,u,v,1)).toEqual({x:u,y:v});
   for(let i=0;i<=20;i++){const p=linkPoint(id,x,y,u,v,i/20);expect(Number.isFinite(p.x)&&Number.isFinite(p.y)).toBe(true)}
  }
 });
 it('moves circuit signals continuously along the horizontal/vertical route',()=>{
  expect(linkPoint('circuit',0,0,100,100,.25)).toEqual({x:50,y:0});
  expect(linkPoint('circuit',0,0,100,100,.5)).toEqual({x:50,y:50});
  expect(linkPoint('circuit',0,0,100,100,.75)).toEqual({x:50,y:100});
 });
 it('retains all nodes and real references when switching visual styles or motion',()=>{
  const data:Graph2Data={total:2,nodes:[{id:'a',path:'A.md',title:'A',kind:'document',links:1,tags:[]},{id:'b',path:'B.md',title:'B',kind:'document',links:1,tags:[]}],edges:[{source:'a',target:'b'}]};
  for(const {id} of graph2Visualizations)for(const motion of [true,false]){const scene=prepareGraph2(data,{...defaultGraph2Settings,visualization:id,motion});expect(scene.nodes).toEqual(data.nodes);expect(scene.edges).toEqual(data.edges)}
 });
});
