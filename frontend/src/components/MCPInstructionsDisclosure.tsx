import {useEffect,useId,useRef,useState} from 'react';
import {MCPSetup} from './MCPSetup';
import {Icon} from './Icon';

// Compact MCP instructions control for auth views. Anchored bottom-left and
// collapsed to a single button, so it never covers the login form; expanding
// opens a scrollable panel that reuses MCPSetup's content and copy actions.
// Disclosure semantics: a button with aria-expanded/aria-controls drives a
// labelled region. Enter/Space activate the button natively; Escape collapses
// the panel and returns focus to the button. Expanding moves focus into the
// panel so keyboard users can read it and dismiss it right away.
export function MCPInstructionsDisclosure(){
 const [open,setOpen]=useState(false);
 const id=useId(),panelId='mcp-panel-'+id,toggleId='mcp-toggle-'+id;
 const host=useRef<HTMLDivElement>(null);
 const toggle=useRef<HTMLButtonElement>(null),panel=useRef<HTMLDivElement>(null);
 const close=()=>{setOpen(false);toggle.current?.focus()};
 useEffect(()=>{
  if(!open)return;
  panel.current?.focus();
  const onKey=(e:KeyboardEvent)=>{if(e.key!=='Escape')return;e.preventDefault();close()};
  // Outside clicks keep focus on the user's destination, including login inputs.
  const outside=(e:PointerEvent)=>{if(!host.current?.contains(e.target as Node))setOpen(false)};
  document.addEventListener('keydown',onKey);
  document.addEventListener('pointerdown',outside);
  return()=>{document.removeEventListener('keydown',onKey);document.removeEventListener('pointerdown',outside)};
 },[open]);
 return <div className="mcp-disclosure" ref={host}>
  <button id={toggleId} ref={toggle} type="button" className="btn mcp-disclosure-toggle" aria-expanded={open} aria-controls={panelId} onClick={()=>{open?close():setOpen(true)}}><Icon name="braces" size={16}/>MCP instructions<Icon name={open?'down':'up'} size={13}/></button>
  {open&&<div id={panelId} ref={panel} className="mcp-disclosure-panel" role="region" aria-labelledby={toggleId} tabIndex={-1}><button type="button" className="mcp-disclosure-close" aria-label="Close MCP instructions" onClick={close}><Icon name="close" size={17}/></button><MCPSetup compact/></div>}
 </div>;
}
