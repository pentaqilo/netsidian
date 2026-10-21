import {highlightCode} from './codeHighlight';
 import MarkdownIt from 'markdown-it';
import {stripProtectedBlocks} from './protectedBlocks';
import {proseMask} from './documentTags';
import DOMPurify from 'dompurify';
import katex from 'katex';
import footnote from 'markdown-it-footnote';
import 'katex/dist/katex.min.css';
export interface Resolved {href:string;missing:boolean;attach?:string;embed?:'image'|'file';title?:string}
export const attachmentExt=/\.(png|jpe?g|gif|webp|svg|pdf|mp4)$/i;
const imageExt=/\.(png|jpe?g|gif|webp|svg)$/i;
// Stable document IDs (domain.NewID("doc")): 26 characters after the prefix.
export const docIDRe=/^doc_[a-z0-9]{26}$/;
export function makeResolver(paths:string[],currentPath='',attachments:{path:string;url:string}[]=[],docs:{id:string;path:string;title?:string}[]=[]){
 return(target:string):Resolved=>{
  const hash=target.indexOf('#'), name=(hash<0?target:target.slice(0,hash)).trim(),fragment=hash<0?'':target.slice(hash+1);
  // Stable-ID links (memory refs and detail links) resolve by document ID
  // and survive renames and moves without any rewriting.
  if(docIDRe.test(name)){const hit=docs.find(d=>d.id===name);return hit?{href:hit.path+(fragment?'#'+fragment:''),missing:false,...(hit.title?{title:hit.title}:{})}:{href:'',missing:true}}
  // Known attachment extensions resolve as before; any other dotted target also gets an
  // attachment try when a real attachment shares its basename (.puml, .mmd, .docx, …).
  const attCandidate=attachmentExt.test(name)||attachments.some(a=>a.path.split('/').pop()===name.split('/').pop());
  if(attCandidate){
   const norm=(p:string)=>{const a:string[]=[];for(const s of p.split('/')){if(s==='..')a.pop();else if(s&&s!=='.')a.push(s)}return a.join('/')};
   const local=norm((name.startsWith('/')?'':currentPath.slice(0,currentPath.lastIndexOf('/')+1))+name);
   let hit=attachments.find(a=>a.path===local)||attachments.find(a=>a.path===norm(name));
   if(!hit){const matches=attachments.filter(a=>a.path.split('/').pop()===name.split('/').pop());if(matches.length===1)hit=matches[0]}
   return hit?{href:hit.url,missing:false,attach:hit.url,embed:imageExt.test(hit.path)?'image':'file'}:{href:'',missing:true,embed:imageExt.test(name)?'image':'file'};
  }
  if(!name&&currentPath)return{href:currentPath+'#'+fragment,missing:false};
  const norm=(p:string)=>{const a:string[]=[];for(const s of p.split('/')){if(s==='..')a.pop();else if(s&&s!=='.')a.push(s)}return a.join('/')};
  const extension=/\.md$/i.test(name)?name:name+'.md';
  const local=norm((name.startsWith('/')?'':currentPath.slice(0,currentPath.lastIndexOf('/')+1))+extension);
  let hit=paths.find(p=>p===local)||paths.find(p=>p===norm(extension));
  if(!hit){const matches=paths.filter(p=>p.split('/').pop()?.replace(/\.md$/i,'')===name.replace(/\.md$/i,''));if(matches.length===1)hit=matches[0]}
  return{href:hit?hit+(fragment?'#'+fragment:''):'',missing:!hit};
 }
}
// Interpret only structured memory reference fields in reading mode. The source
// document stays byte-identical; examples, comments and unrelated IDs stay literal.
export function memoryReadingLinks(source:string){
 const mask=proseMask(source).split('\n');
 return source.split('\n').map((line,i)=>{
  if(!/^ {0,3}- memory: /.test(mask[i]||''))return line;
  const prefix=line.match(/^ {0,3}- memory: /)![0];
  try{
   const entry=JSON.parse(line.slice(prefix.length));
   if(!entry||typeof entry!=='object'||typeof entry.key!=='string'||typeof entry.text!=='string')return line;
   let changed=false;
   const wrap=(value:unknown)=>{if(typeof value==='string'&&docIDRe.test(value)){changed=true;return '[['+value+']]'}return value};
   if('detail' in entry)entry.detail=wrap(entry.detail);
   if(Array.isArray(entry.refs))entry.refs=entry.refs.map(wrap);
   return changed?prefix+JSON.stringify(entry):line;
  }catch{return line}
 }).join('\n');
}
export const headingId=(s:string)=>s.toLowerCase().replace(/[^\p{L}\p{N}\s-]/gu,'').trim().replace(/\s+/g,'-');
export function renderInto(container:HTMLElement,src:string,resolve:(target:string)=>Resolved,open:(path:string,frag?:string)=>void,onTask?:(index:number,checked:boolean)=>void){
 const md=new MarkdownIt({html:false,linkify:true,breaks:false,highlight:highlightCode});
 md.use(footnote);
 // Obsidian-style $...$ and $$...$$ render through KaTeX; code spans and
 // fences are immune because inline rules never run inside code tokens.
 const mathHtml=(content:string,display:boolean)=>{try{return (display?'<div class="math math-block">':'<span class="math">')+katex.renderToString(content,{displayMode:display,throwOnError:false})+(display?'</div>':'</span>')}catch{return '<code>'+md.utils.escapeHtml(content)+'</code>'}};
 md.inline.ruler.before('escape','math_inline',(state,silent)=>{
  if(state.src[state.pos]!=='$'||state.src[state.pos+1]==='$')return false;
  const end=state.src.indexOf('$',state.pos+1);if(end<0)return false;
  const content=state.src.slice(state.pos+1,end);
  if(!content||/^\s|\s$/.test(content)||content.includes('\n'))return false;
  if(!silent){const t=state.push('math_inline','',0);t.content=content}
  state.pos=end+1;return true;
 });
 md.renderer.rules.math_inline=(tokens,i)=>mathHtml(tokens[i].content,false);
 md.block.ruler.before('fence','math_block',(state,start,end,silent)=>{
  const opening=state.src.slice(state.bMarks[start]+state.sCount[start],state.eMarks[start]);
  if(!opening.startsWith('$$'))return false;
  const single=opening.match(/^\$\$([\s\S]+)\$\$$/);
  if(single){if(!silent){const t=state.push('math_block','',0);t.content=single[1].trim();state.line=start+1}return true}
  let close=-1;const content:string[]=[];
  for(let l=start+1;l<end;l++){
   const line=state.src.slice(state.bMarks[l]+state.sCount[l],state.eMarks[l]);
   if(/^\$\$\s*$/.test(line)){close=l;break}
   content.push(state.src.slice(state.bMarks[l],state.eMarks[l]));
  }
  if(close<0)return false;
  if(!silent){const t=state.push('math_block','',0);t.content=content.join('\n');state.line=close+1}
  return true;
 });
 md.renderer.rules.math_block=(tokens,i)=>mathHtml(tokens[i].content,true);
 md.inline.ruler.before('link','wikilink',(state,silent)=>{
  if(state.src.slice(state.pos,state.pos+2)!=='[[')return false;
  const end=state.src.indexOf(']]',state.pos+2);if(end<0)return false;
  if(!silent){
   const embed=state.pos>0&&state.src[state.pos-1]==='!';
   if(embed){if(state.pending.endsWith('!'))state.pending=state.pending.slice(0,-1);else{const prev=state.tokens[state.tokens.length-1];if(prev&&prev.type==='text'&&prev.content==='!')state.tokens.pop()}}
   const body=state.src.slice(state.pos+2,end);const [target,...alias]=body.split('|');const token=state.push(embed?'wikiembed':'wikilink','',0);const resolved=resolve(target);token.content=alias.join('|')||resolved.title||target.split('/').pop()||target;token.meta={...resolved,target}}
  state.pos=end+2;return true;
 });
 md.renderer.rules.wikilink=(tokens,i)=>{const t=tokens[i],r=t.meta as Resolved;return '<a class="wikilink'+(r.missing?' missing':'')+'" href="#"'+(r.missing?' title="Note does not exist yet" aria-label="'+md.utils.escapeHtml(t.content)+' — note does not exist yet"':'')+' data-target="'+md.utils.escapeHtml(t.meta?.target||t.content)+'" data-note="'+md.utils.escapeHtml(r.href)+'">'+md.utils.escapeHtml(t.content)+'</a>'};
 md.renderer.rules.wikiembed=(tokens,i)=>{const t=tokens[i],r=t.meta as Resolved;
  if(r.attach&&r.embed==='image')return '<img class="embed-media" src="'+md.utils.escapeHtml(r.attach)+'" alt="'+md.utils.escapeHtml(t.content)+'" loading="lazy">';
  if(r.attach)return '<a class="wikilink attach-embed" href="'+md.utils.escapeHtml(r.attach)+'">'+md.utils.escapeHtml(t.content)+'</a>';
  return '<a class="wikilink missing" href="#" title="Attachment does not exist yet">'+md.utils.escapeHtml(t.content)+'</a>'};
 md.renderer.rules.heading_open=(tokens,i)=>{tokens[i].attrSet('id',headingId(tokens[i+1]?.content||''));return md.renderer.renderToken(tokens,i,{})};
 // Obsidian comments are prose-only; inline/code tokens are left untouched.
 md.inline.ruler.before('emphasis','highlight',(state,silent)=>{
  const start=state.src.slice(state.pos,state.pos+2);if(start!=='=='&&start!=='%%')return false;
  const end=state.src.indexOf(start,state.pos+2);if(end<0)return false;
  if(!silent&&start==='=='){state.push('mark_open','mark',1);const t=state.push('text','',0);t.content=state.src.slice(state.pos+2,end);state.push('mark_close','mark',-1)}state.pos=end+2;return true;
 });
 md.inline.ruler.before('emphasis','hashtag',(state,silent)=>{
  if(state.src[state.pos]!=='#'||(state.pos>0&&!/\s/.test(state.src[state.pos-1])))return false;
  const m=state.src.slice(state.pos).match(/^#([\p{L}\p{N}_/-]+)/u);if(!m)return false;
  if(!silent){const t=state.push('tagchip','',0);t.content=m[0]}state.pos+=m[0].length;return true;
 });
 md.renderer.rules.tagchip=(tokens,i)=>'<span class="tag-chip">'+md.utils.escapeHtml(tokens[i].content)+'</span>';
 let body=src;const fm=body.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);if(fm)body=body.slice(fm[0].length);
 // Netsidian's own metadata blocks (mind-map membership links) are bookkeeping,
 // not prose: hide them from the rendered view while the raw source keeps them.
 body=memoryReadingLinks(stripProtectedBlocks(body));
 container.innerHTML=DOMPurify.sanitize(md.render(body));
 // markdown images pointing at vault attachments resolve to served URLs
 for(const img of container.querySelectorAll<HTMLImageElement>('img')){const src=img.getAttribute('src')||'';if(/^(https?:|data:|\/api\/)/.test(src))continue;const r=resolve(decodeURI(src.split('#')[0]));if(r.attach)img.src=r.attach;else img.classList.add('missing-embed')}
 // DOMPurify strips target, so attachment embeds reopen their target after sanitizing
 for(const a of container.querySelectorAll<HTMLAnchorElement>('a.attach-embed')){a.target='_blank';a.rel='noopener noreferrer'}
 for(const a of container.querySelectorAll<HTMLAnchorElement>('a[data-note]'))a.onclick=e=>{e.preventDefault();const target=a.dataset.note;if(target){const [p,...frag]=target.split('#');open(p,frag.join('#'))}else window.dispatchEvent(new CustomEvent('netsidian:missing-link',{detail:{target:a.dataset.target||a.textContent||'',documentId:container.dataset.documentId||''}}))};
 for(const a of container.querySelectorAll<HTMLAnchorElement>('a[href]')){
  const href=a.getAttribute('href')||'';
  if(/^https?:/.test(href)){a.target='_blank';a.rel='noopener noreferrer';continue}
  if(href.startsWith('#')||/^(mailto:|tel:)/.test(href))continue;
  if(href.startsWith('/api/')){a.target='_blank';a.rel='noopener noreferrer';continue}
  // In-vault targets go through the same resolver as wikilinks. Anything unknown stays
  // inert: navigating it would leave the SPA and land on a server 404 with no way back.
  const r=resolve(decodeURI(href));
  if(r.attach){a.href=r.attach;a.target='_blank';a.rel='noopener noreferrer'}
  else if(!r.missing)a.onclick=e=>{e.preventDefault();const[p,f]=r.href.split('#');open(p,f)};
  else{a.onclick=e=>e.preventDefault();a.classList.add('dead-link');a.title='This link does not point to anything in the vault'}
 }
 let taskIndex=0;for(const li of container.querySelectorAll('li')){const first=li.firstChild;if(first?.nodeType===Node.TEXT_NODE&&/^\[[ xX]\] /.test(first.textContent||'')){const done=/^\[[xX]\]/.test(first.textContent!);first.textContent=first.textContent!.slice(4);const box=document.createElement('input');box.type='checkbox';box.checked=done;box.disabled=!onTask;const index=taskIndex++;box.onchange=()=>onTask?.(index,box.checked);box.setAttribute('aria-label',done?'Completed task':'Incomplete task');li.prepend(box);li.classList.add('task-item')}}
 for(const q of container.querySelectorAll('blockquote')){const p=q.querySelector('p');if(p&&/^\[!\w+\]/.test(p.textContent||'')){q.classList.add('callout');p.innerHTML=p.innerHTML.replace(/^\[!(\w+)\]/,'<strong>$1</strong>')}}
 // mermaid/plantuml fences render their engines lazily; module loads on demand
 if(container.querySelector('pre>code[class*="language-mermaid"],pre>code[class*="language-puml"],pre>code[class*="language-plantuml"]'))void import('./diagrams').then(m=>m.renderDiagrams(container)).catch(()=>{});
}
