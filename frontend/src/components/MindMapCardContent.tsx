import {useEffect,useMemo,useRef,useState} from 'react';
import MarkdownIt from 'markdown-it';
import * as api from '../api/client';
import type {DocMeta,NoteContent} from '../api/types';
import {makeResolver,renderInto} from '../lib/markdown';
import {proseMask} from '../lib/documentTags';
import {Icon} from './Icon';

// Links live in the Markdown, so graph/backlinks and ordinary document editing
// remain authoritative. Cards resolve their titles against the current vault.
export function MindMapCardContent({space,id,revision,epoch,docs,attachments,expanded,text,onOpen}:{space:string;id:string;revision?:string;epoch:number;docs:DocMeta[];attachments:{path:string}[];expanded:boolean;text?:string;onOpen:(id:string)=>void}){
 const [note,setNote]=useState<NoteContent|null>(null),[error,setError]=useState(false),[retry,setRetry]=useState(0),host=useRef<HTMLDivElement>(null);
 useEffect(()=>{let active=true;setError(false);api.getDocument(space,id).then(n=>{if(active)setNote(n)}).catch(()=>{if(active)setError(true)});return()=>{active=false}},[space,id,revision,epoch,retry]);
 const source=text??note?.markdown??'';
 const resolver=useMemo(()=>makeResolver(docs.map(d=>d.path),note?.path,attachments.map(a=>({...a,url:api.attachmentURL(space,a.path)}))),[docs,attachments,note?.path,space]);
 const links=useMemo(()=>{
  const targets:string[]=[];const md=new MarkdownIt({html:false});
  for(const block of md.parse(source,{}))for(const token of block.children||[])if(token.type==='link_open'){const href=token.attrGet('href');if(href)try{targets.push(decodeURIComponent(href))}catch{/* malformed link remains in source */}}
  for(const match of proseMask(source).matchAll(/\[\[([^\]\n]+)\]\]/g))targets.push(match[1].split('|')[0]);
  const seen=new Set<string>();return targets.flatMap(target=>{const r=resolver(target);if(r.missing||seen.has(r.href))return[];seen.add(r.href);const doc=docs.find(d=>d.path===r.href.split('#')[0]);const attachment=attachments.find(a=>api.attachmentURL(space,a.path)===r.attach);return doc||attachment?[{key:r.href,title:doc?.title||attachment?.path.split('/').pop()||target,doc,attachment,url:r.attach}]:[]});
 },[source,resolver,docs,attachments,space]);
 useEffect(()=>{if(expanded&&host.current)renderInto(host.current,source.slice(0,12000),resolver,path=>{const doc=docs.find(d=>d.path===path);if(doc)onOpen(doc.id)})},[expanded,source,resolver,docs,onOpen]);
 return <>{links.length>0&&<div className="mindmap-file-chips" aria-label="Linked files" onPointerDown={e=>e.stopPropagation()} onClick={e=>e.stopPropagation()}>{links.map(link=>link.doc?<button key={link.key} type="button" title={link.doc.path} onClick={()=>onOpen(link.doc!.id)}><Icon name="file" size={13}/><span>{link.title}</span></button>:<a key={link.key} title={link.attachment?.path} href={link.url} target="_blank" rel="noopener noreferrer"><Icon name="link" size={13}/><span>{link.title}</span></a>)}</div>}{expanded&&<div className="mindmap-card-scroll" onPointerDown={e=>e.stopPropagation()} onClick={e=>e.stopPropagation()} onDoubleClick={e=>e.stopPropagation()}>{error?<button className="btn small" onClick={()=>setRetry(n=>n+1)}>Retry card preview</button>:!note&&text===undefined?<small className="muted">Loading content…</small>:<div className="markdown-preview mindmap-card-body" ref={host}/>}</div>}</>;
}
