import {useEffect,useRef,useState} from 'react';
import {getTextFile,putTextFile} from '../api/client';
import {renderDiagrams} from '../lib/diagrams';

const kindOf=(path:string)=>/\.svg$/i.test(path)?'svg':/\.mmd$/i.test(path)?'mermaid':'puml';

// TextFileEditor edits diagram/vector sources with optimistic concurrency:
// saves carry the last read etag, a server conflict is surfaced verbatim.
// Right pane previews SVG via a blob <img> (scripts inert), puml/mmd via the
// same lazy diagram engines as the read view.
export function TextFileEditor({space,path,onClose,onMutated}:{space:string;path:string;onClose:()=>void;onMutated:()=>void}){
 const [text,setText]=useState(''),[etag,setEtag]=useState(''),[status,setStatus]=useState('Loading…'),[error,setError]=useState(''),[dirty,setDirty]=useState(false);
 const preview=useRef<HTMLDivElement>(null);
 const kind=kindOf(path);
 useEffect(()=>{let live=true;setText('');setEtag('');setDirty(false);setError('');setStatus('Loading…');
  void getTextFile(space,path).then(r=>{if(!live)return;setText(r.content);setEtag(r.etag);setStatus('')}).catch(e=>{if(live)setStatus(e instanceof Error?e.message:String(e))});
  return()=>{live=false}},[space,path]);
 useEffect(()=>{const el=preview.current;if(!el)return;
  if(kind==='svg'){const url=URL.createObjectURL(new Blob([text],{type:'image/svg+xml'}));el.innerHTML='';const img=document.createElement('img');img.src=url;img.alt=path;el.append(img);return()=>URL.revokeObjectURL(url)}
  const timer=setTimeout(()=>{el.innerHTML='';const pre=document.createElement('pre');const code=document.createElement('code');code.className='language-'+kind;code.textContent=text;pre.append(code);el.append(pre);void renderDiagrams(el).catch(()=>{})},500);
  return()=>clearTimeout(timer)},[text,kind,path]);
 const save=async()=>{setError('');try{const r=await putTextFile(space,path,text,etag);setEtag(r.etag);setDirty(false);setStatus('Saved.');onMutated()}catch(e){const m=e instanceof Error?e.message:String(e);setError(m);setStatus(/server|conflict|precondition/i.test(m)?'Conflict — reload first':'Save failed')}};
 const title=path.split('/').pop()||path;
 return <div className="textfile">
  <div className="textfile-bar"><b>{title}</b><small>{path}</small><span className="spacer"/><small>{status}</small><button className="btn small" disabled={!dirty||!etag} onClick={()=>void save()}>Save</button><button className="btn small" onClick={onClose}>Close</button></div>
  {error&&<p className="textfile-error">{error}</p>}
  <div className="textfile-panes"><textarea className="textfile-src" value={text} aria-label={'Source of '+title} spellCheck={false} onChange={e=>{setText(e.target.value);setDirty(true)}}/><div className="textfile-preview" ref={preview}/></div>
 </div>;
}
