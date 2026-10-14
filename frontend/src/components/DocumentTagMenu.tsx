import {useEffect,useId,useLayoutEffect,useRef,useState} from 'react';
import {createPortal} from 'react-dom';
import {readDocumentTags} from '../lib/documentTags';
import {overlayPoint,uiZoom} from '../lib/overlayPosition';
import {DocumentTags} from './DocumentTags';
import {Icon,Tool} from './Icon';

type Props={text:string;onChange:(text:string)=>void;editable:boolean;known:string[];onSearch:(tag:string)=>void};
function tagsOf(text:string){try{return readDocumentTags(text)}catch{return []}}
export function DocumentTagMenu(props:Props){
 const [open,setOpen]=useState(false),[position,setPosition]=useState({left:0,top:0});
 const trigger=useRef<HTMLButtonElement>(null),popup=useRef<HTMLDivElement>(null),id=useId(),tags=tagsOf(props.text);
 const close=(restore=false)=>{setOpen(false);if(restore)trigger.current?.focus()};
 useLayoutEffect(()=>{
  if(!open)return;
  const place=()=>{if(!trigger.current||!popup.current)return;const r=trigger.current.getBoundingClientRect();setPosition(overlayPoint(r.right-popup.current.offsetWidth*uiZoom(),r.bottom+6,popup.current.offsetWidth,popup.current.offsetHeight))};
  place();window.addEventListener('resize',place);window.addEventListener('scroll',place,true);
  return()=>{window.removeEventListener('resize',place);window.removeEventListener('scroll',place,true)};
 },[open,props.text]);
 useEffect(()=>{
  if(!open)return;
  popup.current?.querySelector<HTMLButtonElement>('button')?.focus();
  const outside=(e:PointerEvent)=>{if(!popup.current?.contains(e.target as Node)&&!trigger.current?.contains(e.target as Node))close()};
  const focusOutside=(e:FocusEvent)=>{if(!popup.current?.contains(e.target as Node)&&!trigger.current?.contains(e.target as Node))close()};
  const escape=(e:KeyboardEvent)=>{if(e.key==='Escape'){e.preventDefault();close(true)}};
  document.addEventListener('pointerdown',outside);document.addEventListener('focusin',focusOutside);document.addEventListener('keydown',escape);
  return()=>{document.removeEventListener('pointerdown',outside);document.removeEventListener('focusin',focusOutside);document.removeEventListener('keydown',escape)};
 },[open]);
 return <><button type="button" ref={trigger} className={'tool document-tag-trigger'+(open?' active':'')} aria-label="Document tags" title={tags.length?`Document tags (${tags.length})`:'Document tags'} aria-expanded={open} aria-haspopup="dialog" aria-controls={open?id:undefined} onClick={()=>setOpen(!open)}><Icon name="tag"/>{tags.length>0&&<small aria-hidden="true">{tags.length>99?'99+':tags.length}</small>}</button>{open&&createPortal(<div id={id} ref={popup} className="document-tag-popover" role="dialog" aria-label="Document tags" style={position}><header><strong>Document tags</strong><Tool icon="close" label="Close document tags" onClick={()=>close(true)}/></header><DocumentTags {...props} onSearch={tag=>{close();props.onSearch(tag)}}/></div>,document.body)}</>;
}
export function DocumentTagSummary({text,onSearch}:{text:string;onSearch:(tag:string)=>void}){
 const tags=tagsOf(text);
 if(!tags.length)return null;
 return <div className="document-tag-summary" aria-label="Document tag list">{tags.map(tag=><button type="button" key={tag} aria-label={'Find documents tagged '+tag} title={'Find documents tagged '+tag} onClick={()=>onSearch(tag)}>#{tag}</button>)}</div>;
}
