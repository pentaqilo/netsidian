import type {DocMeta} from '../api/types';
import {proseMask} from './documentTags';
import {makeResolver} from './markdown';

export interface LinkRequest {query:string;rect:{left:number;top:number;bottom:number};manual?:boolean;owner:HTMLElement;choose:(doc:DocMeta)=>void;close:()=>void}
export function linkTrigger(before:string,explicit=false){
 const line=before.slice(before.lastIndexOf('\n')+1),match=line.match(/(?:^|[^\\])\[\[?([^\[\]\n]*)$/);
 if(match){const brackets=match[0].indexOf('['),from=before.length-match[0].length+brackets;return{from,query:match[1]}}
 return explicit?{from:before.length,query:''}:null;
}
const fold=(s:string)=>s.normalize('NFKD').replace(/\p{M}/gu,'').toLocaleLowerCase();
export function rankLinkDocuments(docs:DocMeta[],query:string,currentPath=''){
 const terms=fold(query).trim().split(/\s+/).filter(Boolean),folder=currentPath.slice(0,currentPath.lastIndexOf('/')+1);
 return docs.filter(d=>!d.tombstoneAt&&(d.kind==='note'||d.kind==='memory')).map(doc=>{
  const title=fold(doc.title||doc.path.split('/').pop()!.replace(/\.md$/i,'')),path=fold(doc.path);let score=0;
  for(const term of terms){if(title===term)score+=120;else if(title.startsWith(term))score+=70;else if(title.split(/[^\p{L}\p{N}]+/u).some(w=>w.startsWith(term)))score+=50;else if(title.includes(term))score+=35;else if(path.includes(term))score+=20;else{let cursor=0;for(const char of title)if(char===term[cursor])cursor++;if(cursor!==term.length)return{doc,score:-1};score+=5}}
  if(folder&&doc.path.startsWith(folder))score+=2;return{doc,score};
 }).filter(d=>d.score>=0).sort((a,b)=>b.score-a.score||a.doc.path.localeCompare(b.doc.path)).map(d=>d.doc);
}
export function documentLinkTarget(doc:Pick<DocMeta,'path'>){return '/'+doc.path.replace(/\.md$/i,'')}
export function documentWikilink(doc:Pick<DocMeta,'path'|'title'>,alias=''){const target=documentLinkTarget(doc);return '[['+target+(alias?'|'+alias.replace(/[\]\n|]/g,''):'')+']]'}
export function closingBrackets(text:string,at:number){return text.slice(at).match(/^\]{1,2}/)?.[0].length||0}

export function rewriteMovedLinks(text:string,sourcePath:string,oldPath:string,newPath:string,paths:string[],relocateSource=false){
 let masked=proseMask(text);const frontmatter=/^\uFEFF?---\r?\n/.test(text)?text.match(/^\uFEFF?---\r?\n[\s\S]*?^(?:---|\.\.\.)(?:\r?\n|$)/m)?.[0]:undefined;if(frontmatter)masked=masked.slice(0,frontmatter.length).replace(/[^\n]/g,' ')+masked.slice(frontmatter.length);const resolve=makeResolver(paths,sourcePath),changes:{from:number;to:number;insert:string}[]=[];
 for(const m of masked.matchAll(/(!?)\[\[([^\]\n]+)\]\]/g)){
  const [target,...alias]=m[2].split('|'),resolved=resolve(target);const previous=resolved.href.split('#')[0];if(resolved.missing||(!relocateSource&&previous!==oldPath))continue;const destination=previous===oldPath?newPath:previous;const fragment=target.includes('#')?target.slice(target.indexOf('#')):'';
  changes.push({from:m.index!,to:m.index!+m[0].length,insert:m[1]+'[['+'/'+destination.replace(/\.md$/i,'')+fragment+(alias.length?'|'+alias.join('|'):'')+']]'});
 }
 for(const m of masked.matchAll(/\[[^\]\n]*\]\(([^)\n]+)\)/g)){
  const raw=m[1];let target:string;try{target=decodeURIComponent(raw)}catch{continue}const resolved=resolve(target),previous=resolved.href.split('#')[0];if(resolved.missing||(!relocateSource&&previous!==oldPath))continue;const destination=previous===oldPath?newPath:previous;const fragment=target.includes('#')?target.slice(target.indexOf('#')):'';
  const from=m.index!+m[0].indexOf('](')+2;changes.push({from,to:from+raw.length,insert:encodeURI('/'+destination).replace(/\(/g,'%28').replace(/\)/g,'%29')+fragment});
 }
 for(const change of changes.sort((a,b)=>b.from-a.from))text=text.slice(0,change.from)+change.insert+text.slice(change.to);return text;
}

// Repair only prose links; fenced/inline code and frontmatter are left intact.
export function repairMissingWikilink(text:string,target:string,path:string){
 let masked=proseMask(text);const fm=text.match(/^\uFEFF?---\r?\n[\s\S]*?^(?:---|\.\.\.)(?:\r?\n|$)/m)?.[0];if(fm)masked=' '.repeat(fm.length)+masked.slice(fm.length);
 const changes:{from:number;to:number;insert:string}[]=[];
 for(const m of masked.matchAll(/\[\[([^\]\n]+)\]\]/g)){const [name,...alias]=m[1].split('|');if(name!==target)continue;const fragment=name.includes('#')?name.slice(name.indexOf('#')):'';changes.push({from:m.index!,to:m.index!+m[0].length,insert:'[['+documentLinkTarget({path})+fragment+(alias.length?'|'+alias.join('|'):'')+']]'})}
 for(const c of changes.reverse())text=text.slice(0,c.from)+c.insert+text.slice(c.to);return text;
}
