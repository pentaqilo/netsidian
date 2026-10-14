import {describe,it,expect} from 'vitest';
import type {DocMeta} from '../api/types';
import {indexFileTree,flattenFileTree} from './fileTreeRows';
const doc=(path:string,id=path):DocMeta=>({id,path,kind:'note',title:path,currentRevision:'r',updatedAt:'2026-10-12T00:00:00Z'});
describe('large vault tree',()=>{
 it('indexes 10,000 documents and folders without rescanning paths on expansion',()=>{
  const documents=Array.from({length:10000},(_,i)=>doc(`Folder-${Math.floor(i/10)}/Note-${i}.md`));
  const index=indexFileTree(documents,['Empty/Nested'],[{path:'Folder-1/picture.png'}]);
  expect(index.folders).toHaveLength(1002);
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
