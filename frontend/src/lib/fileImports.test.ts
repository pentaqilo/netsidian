import {expect,it} from 'vitest';
import {importRelativePath,unusedImportPath} from './fileImports';
it('preserves nested browser paths and normalizes Unicode names',()=>{
 expect(importRelativePath({name:'note.md',webkitRelativePath:'Folder/note.md'})).toBe('Folder/note.md');
 expect(importRelativePath({name:'Cafe\u0301.pdf'})).toBe('Café.pdf');
});
it('rejects traversal, hidden and ambiguous names before importing into a destination',()=>{
 for(const name of ['../x.pdf','/x.pdf','A/../x.pdf','A/./x.pdf','.hidden/x.pdf','A\\x.pdf','x%.pdf','bad\u0000.pdf','A//x.pdf','space /.pdf'])
  expect(()=>importRelativePath({name})).toThrow();
});
it('allocates distinct keep-both names for existing and same-batch collisions',()=>{
 const taken=new Set(['folder/file.pdf','folder/file copy.pdf']);
 expect(unusedImportPath('Folder/file.pdf',taken)).toBe('Folder/file copy 2.pdf');
 expect(unusedImportPath('new.pdf',taken)).toBe('new.pdf');
});
