import {load,dump} from 'js-yaml';

export function normalizeTag(value:string){
 const tag=value.trim().replace(/^#+|\/+$/g,'').replace(/^\/+/,'').toLocaleLowerCase();
 if(!tag||!/^[\p{L}\p{N}_-]+(?:\/[\p{L}\p{N}_-]+)*$/u.test(tag)||/^\p{N}+$/u.test(tag))throw new Error('Use letters, numbers, hyphens or underscores. Separate nested tags with /. Tags cannot be only numbers.');
 return tag;
}
export function splitTagInput(value:string){return [...new Set(value.split(/[\s,]+/).filter(Boolean).map(normalizeTag))]}
function tagValues(value:unknown):string[]{return Array.isArray(value)?value.flatMap(tagValues):typeof value==='string'?value.split(/[\s,]+/).filter(Boolean).flatMap(t=>{try{return[normalizeTag(t)]}catch{return[]}}):[]}
export function tagSource(text:string){
 const bom=text.startsWith('\uFEFF')?'\uFEFF':'',newline=text.includes('\r\n')?'\r\n':'\n',normalized=text.slice(bom.length).replace(/\r\n/g,'\n');
 let yaml='',body=normalized,metadata:Record<string,unknown>={};
 if(normalized.startsWith('---\n')){
  const match=normalized.match(/^---\n([\s\S]*?)^(?:---|\.\.\.)(?:\n|$)/m);if(!match)throw new Error('Frontmatter is not closed. Fix it in Source mode before changing tags.');
  const parsed=load(match[1]);if(parsed!=null&&(typeof parsed!=='object'||Array.isArray(parsed)))throw new Error('Frontmatter must be a YAML mapping.');
  yaml=match[1].replace(/\n$/,'');metadata=(parsed||{}) as Record<string,unknown>;body=normalized.slice(match[0].length);
 }
 return{bom,newline,yaml,body,metadata,frontmatter:normalized.startsWith('---\n')};
}
// Preserve offsets while hiding fences, indented code, inline code and comments.
export function proseMask(text:string){
 let fence='',comment=false;
 return text.split('\n').map(line=>{
  const match=line.match(/^\s*(`{3,}|~{3,})/);
  if(match){if(!fence)fence=match[1];else if(match[1][0]===fence[0]&&match[1].length>=fence.length&&!line.slice(match[0].length).trim())fence='';return' '.repeat(line.length)}
  if(fence||/^(?: {4}|\t)/.test(line))return' '.repeat(line.length);
  let out='',code='';for(let i=0;i<line.length;){if(comment){const end=line.indexOf('-->',i);if(end<0){out+=' '.repeat(line.length-i);break}out+=' '.repeat(end+3-i);i=end+3;comment=false;continue}if(line.startsWith('<!--',i)){comment=true;continue}if(line[i]==='`'){const run=line.slice(i).match(/^`+/)![0];if(!code)code=run;else if(code===run)code='';out+=' '.repeat(run.length);i+=run.length;continue}out+=code?' ':line[i];i++}return out;
 }).join('\n');
}
function inlineTags(body:string){return [...proseMask(body).matchAll(/(?:^|[\s(])#([\p{L}\p{N}_/-]+)/gu)].flatMap(match=>{try{return[{tag:normalizeTag(match[1]),from:match.index!+match[0].indexOf('#'),to:match.index!+match[0].length}]}catch{return[]}})}
export function readDocumentTags(text:string){const source=tagSource(text);return [...new Set([...tagValues(source.metadata.tags),...tagValues(source.metadata.tag),...inlineTags(source.body).map(t=>t.tag)])].sort()}
export function writeDocumentTags(text:string,values:string[]){
 const tags=[...new Set(values.map(normalizeTag))].sort(),source=tagSource(text),wanted=new Set(tags);
 let body=source.body;for(const token of inlineTags(body).reverse())if(!wanted.has(token.tag))body=body.slice(0,token.from)+body.slice(token.to);
 let yaml=source.yaml;
 // Patch only top-level tag properties; leave other YAML and its comments intact.
 if(yaml&&(/^\s*\{/.test(yaml)||/^\s+\S/.test(yaml.split('\n').find(line=>line.trim()&&!line.trimStart().startsWith('#'))||'')||/^(?:tags|tag|"tags"|"tag"|'tags'|'tag')\s*:.*&/m.test(yaml))){const metadata={...source.metadata};delete metadata.tag;if(tags.length)metadata.tags=tags;else delete metadata.tags;yaml=dump(metadata,{lineWidth:120,noRefs:true}).trimEnd()}
 else{const lines=yaml?yaml.split('\n'):[],kept:string[]=[];let skipping=false;for(const line of lines){if(/^(?:tags|tag|"tags"|"tag"|'tags'|'tag')\s*:/.test(line)){skipping=true;continue}if(skipping&&line&&!/^\s|^-\s/.test(line))skipping=false;if(!skipping)kept.push(line)}yaml=kept.join('\n').trimEnd();if(tags.length)yaml+=(yaml?'\n':'')+dump({tags},{lineWidth:120,noRefs:true}).trimEnd()}
 const result=(source.frontmatter||tags.length?'---\n'+yaml+'\n---\n': '')+body;
 return source.bom+result.replace(/\n/g,source.newline);
}

export function documentBody(text:string){try{return tagSource(text).body}catch{return text}}
export function replaceDocumentBody(text:string,body:string){let source:ReturnType<typeof tagSource>;try{source=tagSource(text)}catch{return body};return source.bom+(source.frontmatter?'---'+source.newline+source.yaml.replace(/\n/g,source.newline)+source.newline+'---'+source.newline:'')+body}
