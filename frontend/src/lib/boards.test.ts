import {describe,it,expect} from 'vitest';
import {boardPathError,columnFolderError,columnSlug,defaultBoardColumns,insertOrdered,parseBoardDrag,populatedColumnRemovalErrors,randomIdemKey,swapOrdered,validateBoardColumns} from './boards';

describe('parseBoardDrag',()=>{
 it('accepts a complete payload',()=>{expect(parseBoardDrag(JSON.stringify({cardId:'c1',docId:'d1',columnId:'todo',fromPath:'boards/project/todo/note.md',revision:'7'}))).toEqual({cardId:'c1',docId:'d1',columnId:'todo',fromPath:'boards/project/todo/note.md',revision:'7'})});
 it('rejects malformed, partial or oversized payloads',()=>{
  expect(parseBoardDrag(null)).toBeNull();expect(parseBoardDrag('not json')).toBeNull();expect(parseBoardDrag('[1,2]')).toBeNull();
  expect(parseBoardDrag(JSON.stringify({cardId:'c1'}))).toBeNull();
  expect(parseBoardDrag(JSON.stringify({cardId:'c1',docId:'d1',columnId:'todo',fromPath:'x'.repeat(2000),revision:'7'}))).toBeNull();
 });
});

describe('columnFolderError',()=>{
 it('mirrors the backend single-folder-name rule',()=>{
  expect(columnFolderError('todo')).toBe('');
  expect(columnFolderError('In Progress')).toBe('');
  expect(columnFolderError('')).not.toBe('');
  expect(columnFolderError('a/b')).not.toBe('');expect(columnFolderError('a\\b')).not.toBe('');
  expect(columnFolderError('.')).not.toBe('');expect(columnFolderError('..')).not.toBe('');
  expect(columnFolderError('.hidden')).not.toBe('');
  expect(columnFolderError('assets')).not.toBe('');expect(columnFolderError('Assets')).not.toBe('');
  expect(columnFolderError('board')).not.toBe('');
 });
});

describe('columnSlug',()=>{
 it('slugifies labels and deduplicates',()=>{const used=new Set<string>();expect(columnSlug('In Progress',used)).toBe('in-progress');used.add('in-progress');expect(columnSlug('In progress!',used)).toBe('in-progress-2');expect(columnSlug('???',used)).toBe('column')});
});

describe('validateBoardColumns',()=>{
 const ok={id:'todo',label:'Todo',folder:'todo',completion:false,wipLimit:0};
 it('requires at least one column',()=>{expect(validateBoardColumns([])).toEqual(['A board needs at least one column.'])});
 it('accepts a sane set',()=>{expect(validateBoardColumns([ok,{id:'done',label:'Done',folder:'done',completion:true,wipLimit:0}])).toEqual([])});
 it('flags duplicate ids, duplicate folders (case-insensitively), bad folders and bad limits',()=>{
  const errors=validateBoardColumns([ok,{...ok,id:'other',folder:'Todo'},ok]);
  expect(errors.some(e=>e.includes('Duplicate column id'))).toBe(true);
  expect(errors.some(e=>e.includes('Duplicate column folder'))).toBe(true);
  expect(validateBoardColumns([{...ok,folder:'a/b'}]).some(e=>e.includes('single folder name'))).toBe(true);
  expect(validateBoardColumns([{...ok,wipLimit:-1}]).some(e=>e.includes('WIP limit'))).toBe(true);
 });
 it('flags a default column that does not exist',()=>{expect(validateBoardColumns([ok],'nope')).toEqual(['Default column refers to a column that does not exist.'])});
});

describe('populatedColumnRemovalErrors',()=>{
 it('refuses to drop or rename a column folder that still holds cards',()=>{
  const existing=[{id:'todo',label:'Todo',folder:'todo',cards:3},{id:'done',label:'Done',folder:'done',cards:0}];
  const removal=populatedColumnRemovalErrors(existing,[{id:'done',label:'Done',folder:'done',completion:true,wipLimit:0}]);
  expect(removal).toHaveLength(1);expect(removal[0]).toContain('Todo');expect(removal[0]).toContain('3 cards');
  const rename=populatedColumnRemovalErrors(existing,[{id:'todo',label:'Todo',folder:'doing',completion:false,wipLimit:0},{id:'done',label:'Done',folder:'done',completion:true,wipLimit:0}]);
  expect(removal.length+rename.length).toBe(2);
 });
 it('allows dropping empty columns',()=>{expect(populatedColumnRemovalErrors([{id:'done',label:'Done',folder:'done',cards:0}],[])).toEqual([])});
});

describe('boardPathError',()=>{
 it('rejects root, traversal and hidden paths but trims trailing slashes',()=>{
  expect(boardPathError('boards/project')).toBe('');expect(boardPathError('boards/project/')).toBe('');
  expect(boardPathError('')).not.toBe('');expect(boardPathError('/')).not.toBe('');
  expect(boardPathError('a/../b')).not.toBe('');expect(boardPathError('.private/boards')).not.toBe('');
 });
});

describe('insertOrdered',()=>{
 it('inserts before or after the target card',()=>{
  expect(insertOrdered(['a','b','c'],'c','b')).toEqual(['a','c','b']);
  expect(insertOrdered(['a','b','c'],'a','b',false)).toEqual(['b','a','c']);
 });
 it('appends when there is no target and keeps a single copy',()=>{
  expect(insertOrdered(['a','b','c'],'a',null)).toEqual(['b','c','a']);
  expect(insertOrdered(['a','b','c'],'a','zz')).toEqual(['b','c','a']);
 });
});

describe('swapOrdered',()=>{
 it('moves one step up or down and no-ops at the edges',()=>{
  expect(swapOrdered(['a','b','c'],'b',-1)).toEqual(['b','a','c']);
  expect(swapOrdered(['a','b','c'],'b',1)).toEqual(['a','c','b']);
  expect(swapOrdered(['a','b','c'],'a',-1)).toEqual(['a','b','c']);
  expect(swapOrdered(['a','b','c'],'c',1)).toEqual(['a','b','c']);
  expect(swapOrdered(['a','b','c'],'z',1)).toEqual(['a','b','c']);
 });
});

describe('defaults and idempotency keys',()=>{
 it('ships a To do/In progress/Done template with Done as the completion column',()=>{
  const cols=defaultBoardColumns();
  expect(cols.map(c=>c.id)).toEqual(['todo','in-progress','done']);
  expect(cols[2].completion).toBe(true);
  expect(validateBoardColumns(cols)).toEqual([]);
 });
 it('produces prefixed unique keys',()=>{
  const a=randomIdemKey('board-task'),b=randomIdemKey('board-task');
  expect(a.startsWith('board-task:')).toBe(true);expect(a).not.toBe(b);
 });
});
