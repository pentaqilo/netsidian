import {expect,it} from 'vitest';
import {parseDragPayload} from './FileTree';
it('accepts legacy moves and attachment moves while deduplicating entries',()=>{
 expect(parseDragPayload('{"docs":["d","d"],"folders":["Folder"]}')).toEqual({docs:['d'],folders:['Folder'],atts:[]});
 expect(parseDragPayload('{"atts":["Folder/a.pdf"]}')).toEqual({docs:[],folders:[],atts:['Folder/a.pdf']});
});
it('rejects malformed move payloads as a whole rather than mutating a partial selection',()=>{
 for(const raw of ['', 'bad', '[]', '{}','{"docs":"d"}','{"docs":["d",null]}','{"atts":[""]}'])expect(parseDragPayload(raw)).toBeNull();
});
it('bounds move lists and payload bytes',()=>{
 expect(parseDragPayload(JSON.stringify({docs:Array.from({length:257},(_,i)=>String(i))}))).toBeNull();
 expect(parseDragPayload(' '.repeat(65537))).toBeNull();
});
