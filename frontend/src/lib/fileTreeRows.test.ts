import {describe,it,expect} from 'vitest';
import type {DocMeta} from '../api/types';
import {indexFileTree,flattenFileTree,fileTreeGuides} from './fileTreeRows';
const doc=(path:string,id=path):DocMeta=>({id,path,kind:'note',title:path,currentRevision:'r',updatedAt:'2026-10-12T00:00:00Z'});
describe('large vault tree',()=>{
 it('indexes 10,000 documents and folders without rescanning paths on expansion',()=>{
  const documents=Array.from({length:10000},(_,i)=>doc(`Folder-${Math.floor(i/10)}/Note-${i}.md`));
  const index=indexFileTree(documents,['Empty/Nested'],[{path:'Folder-1/picture.png'}]);
  expect(index.folders).toHaveLength(1002);
  expect(index.branches.has('Empty/Nested')).toBe(false);
  expect(index.branches.has('Empty')).toBe(true);
  expect(flattenFileTree(index,new Set())).toHaveLength(11003);
  const collapsed=new Set(index.folders);expect(flattenFileTree(index,collapsed)).toHaveLength(1001);
  collapsed.delete('Folder-1');expect(flattenFileTree(index,collapsed)).toHaveLength(1012);
  expect(flattenFileTree(index,collapsed,true)).toHaveLength(11003);
 });
 it('keeps natural name order, mtime sort, memo labels, and empty folders',()=>{
  const older=doc('Folder/Note-10.md'),newer={...doc('Folder/Note-2.md'),updatedAt:'2026-10-13T00:00:00Z'};
  const memo={...doc('Folder/Memo.md'),title:'Useful memo',tags:['memo']};
  const rows=flattenFileTree(indexFileTree([older,newer,memo],['Empty'],[]),new Set());
  expect(rows.map(row=>row.name)).toEqual(['Empty','Folder','Useful memo','Note-2','Note-10']);
  const sorted=flattenFileTree(indexFileTree([older,newer],[],[],'mtime'),new Set());
  expect(sorted.filter(row=>row.kind==='doc').map(row=>row.name)).toEqual(['Note-2','Note-10']);
 });
});

describe('tree hierarchy guides',()=>{
 it('stops a parent guide at the last child, not at its last descendant',()=>{
  const rows=flattenFileTree(indexFileTree([doc('A/First/one.md'),doc('A/Last/Nested/two.md'),doc('Z.md')],[],[]),new Set());
  const layout=fileTreeGuides(rows);
  const at=(path:string)=>layout[rows.findIndex(r=>r.kind==='folder'?r.path===path:r.kind==='doc'&&r.doc.path===path)];
  expect(at('A').stem).toBe(true);
  expect(at('A/First').guides).toEqual([{level:0,end:false}]);
  expect(at('A/First/one.md').guides).toEqual([{level:0,end:false},{level:1,end:true}]);
  expect(at('A/Last').guides).toEqual([{level:0,end:true}]);
  expect(at('A/Last/Nested/two.md').guides).toEqual([{level:2,end:true}]);
  expect(at('Z.md').guides).toEqual([]);
 });
 it('has no hanging stems for collapsed or empty folders and keeps metadata for virtual slices',()=>{
  const rows=flattenFileTree(indexFileTree([doc('A/Child/one.md'),doc('A/two.md')],['Empty'],[]),new Set(['A']));
  expect(fileTreeGuides(rows).every(r=>!r.stem&&r.guides.length===0)).toBe(true);
  const expanded=flattenFileTree(indexFileTree([doc('A/Child/one.md'),doc('A/two.md')],[],[]),new Set());
  expect(fileTreeGuides(expanded).slice(2,3)[0].guides).toEqual([{level:0,end:false},{level:1,end:true}]);
 });
});
