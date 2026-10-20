import {useEffect,useRef,useState} from 'react';
import type {DocMeta} from '../api/types';
import {closingBrackets,documentWikilink,linkTrigger,type LinkRequest} from '../lib/documentLinks';
import {proseMask} from '../lib/documentTags';
import {DocumentLinkPicker} from './DocumentLinkPicker';
import {LinkWritingHint} from './LinkWritingHint';
import {textareaCaretRect} from '../lib/textareaCaret';

export function LinkTextarea({value,onChange,docs,disabled,onSave,currentPath='',hintDisclosure=false,showLinkHint=true}:{value:string;onChange:(text:string)=>void;docs:DocMeta[];disabled:boolean;onSave:()=>void;currentPath?:string;hintDisclosure?:boolean;showLinkHint?:boolean}){
  const element=useRef<HTMLTextAreaElement>(null),dismissed=useRef('');const [request,setRequest]=useState<LinkRequest|null>(null);
  // The whole composer panel grows with the draft instead of trapping text in
  // a fixed-height inner scroll: measure the content and match the height.
  useEffect(()=>{const el=element.current;if(!el)return;const resize=()=>{el.style.height='auto';el.style.height=`${el.scrollHeight}px`};resize();let width=el.getBoundingClientRect().width;const observer=typeof ResizeObserver==='undefined'?null:new ResizeObserver(()=>{const next=el.getBoundingClientRect().width;if(Math.abs(next-width)<.5)return;width=next;resize()});observer?.observe(el);return()=>observer?.disconnect()},[value]);
 const suggest=(text:string,explicit=false)=>{const el=element.current;if(!el||disabled)return;const cursor=el.selectionStart,trigger=linkTrigger(proseMask(text.slice(0,cursor)),explicit),key=text+'\0'+cursor;if(!trigger||(!explicit&&key===dismissed.current)){setRequest(null);return}const from=trigger.from,to=el.selectionEnd+closingBrackets(text,el.selectionEnd),rect=textareaCaretRect(el,cursor);setRequest({query:trigger.query,manual:explicit,rect,owner:el,close:()=>{dismissed.current=key;setRequest(null)},choose:doc=>{const insert=documentWikilink(doc);onChange(text.slice(0,from)+insert+text.slice(to));setRequest(null);requestAnimationFrame(()=>{el.focus();el.setSelectionRange(from+insert.length,from+insert.length)})}})};
 return <div className="link-textarea">{showLinkHint&&<LinkWritingHint enabled={!disabled} disclosure={hintDisclosure}/>}<textarea ref={element} aria-label="New memo" placeholder="What's on your mind?" value={value} disabled={disabled} onChange={e=>{onChange(e.target.value);suggest(e.target.value)}} onClick={()=>suggest(value)} onScroll={()=>{if(request)suggest(value,request.manual)}} onKeyDown={e=>{if((e.ctrlKey||e.metaKey)&&e.code==='Space'){e.preventDefault();suggest(value,true)}else if((e.ctrlKey||e.metaKey)&&e.key==='Enter'){e.preventDefault();onSave()}}}/><DocumentLinkPicker request={request} docs={docs} currentPath={currentPath}/></div>;
}
