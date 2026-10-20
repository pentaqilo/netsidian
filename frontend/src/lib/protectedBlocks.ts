import MarkdownIt from 'markdown-it';

// Only application-owned membership blocks are presentation metadata. Matching
// uses Markdown block tokens so code examples, indented code, frontmatter and
// unrelated comments remain ordinary source. Incomplete blocks stay visible.
const parser=new MarkdownIt({html:false});
const BLOCK=/^ {0,3}<!-- netsidian:mindmap-links -->[^\S\r\n]*\r?\n[\s\S]*?^ {0,3}<!-- \/netsidian:mindmap-links -->[^\S\r\n]*(?:\r?\n|$)/gm;
export interface ProtectedBlock{text:string;before:string;atStart?:boolean}
export function splitProtectedBlocks(src:string):{rest:string;blocks:ProtectedBlock[]}{
 const lines=[0];for(let i=0;i<src.length;i++)if(src[i]==='\n')lines.push(i+1);
 const excluded=parser.parse(src,{}).filter(t=>(t.type==='fence'||t.type==='code_block')&&t.map).map(t=>({start:lines[t.map![0]],end:lines[t.map![1]]??src.length}));
 const fm=src.match(/^\uFEFF?---\r?\n[\s\S]*?^(?:---|\.\.\.)(?:\r?\n|$)/m);if(fm&&fm.index===0)excluded.push({start:0,end:fm[0].length});
 const blocks:ProtectedBlock[]=[];let rest='',cursor=0;
 for(const match of src.matchAll(BLOCK)){
  const offset=match.index!;if(excluded.some(r=>offset<r.end&&offset+match[0].length>r.start))continue;
  rest+=src.slice(cursor,offset);blocks.push({text:match[0],before:rest.slice(-40),atStart:rest.length===0});cursor=offset+match[0].length;
 }
 return{rest:rest+src.slice(cursor),blocks};
}
export const stripProtectedBlocks=(src:string)=>splitProtectedBlocks(src).rest;
export const isProtectedBlock=(src:string)=>splitProtectedBlocks(src).blocks.length>0;
export function reinsertProtectedBlocks(edited:string,blocks:ProtectedBlock[]):string{
 let next=edited;let minimum=0;
 for(const block of blocks){
  const anchor=block.before?next.lastIndexOf(block.before):-1;
  const at=block.atStart?minimum:anchor>=0?Math.max(minimum,anchor+block.before.length):-1;
  if(at>=0){next=next.slice(0,at)+block.text+next.slice(at);minimum=at+block.text.length;}
  else{next+=(next.endsWith('\n')?'':'\n')+block.text;minimum=next.length;}
 }
 return next;
}
