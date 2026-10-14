import {useEffect,useId,useRef,useState} from 'react';
import {useApp} from '../AppState';
import {Icon} from './Icon';

/** WAI-ARIA menu-button pattern: arrows, Home/End, Escape and normal Tab order. */
export function AccountMenu({onSettings,onSignOut}:{onSettings:()=>void;onSignOut:()=>Promise<void>}) {
 const {me,settings}=useApp();
 const [open,setOpen]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const root=useRef<HTMLDivElement>(null),trigger=useRef<HTMLButtonElement>(null),menu=useRef<HTMLDivElement>(null);
 const first=useRef(0),id=useId(),local=!settings?.runtime.auth.logout;
 const items=()=>Array.from(menu.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')||[]);
 const close=(restore=false)=>{setOpen(false);if(restore)trigger.current?.focus()};
 useEffect(()=>{if(!open)return;const actions=items();actions[first.current===-1?actions.length-1:0]?.focus();const outside=(e:PointerEvent)=>{if(!root.current?.contains(e.target as Node))setOpen(false)};document.addEventListener('pointerdown',outside);return()=>document.removeEventListener('pointerdown',outside)},[open]);
 if(!me)return null;
 return <div className="account-control" ref={root} onBlur={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node))setOpen(false)}}>
  <button ref={trigger} className="account-trigger" aria-label={'Account: '+me.displayName} title={'Account: '+me.displayName} aria-haspopup="menu" aria-expanded={open} aria-controls={open?id:undefined} disabled={busy} onClick={()=>{first.current=0;setOpen(!open)}} onKeyDown={e=>{if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();first.current=e.key==='ArrowUp'?-1:0;setOpen(true)}}}><span className="account-avatar"><Icon name="user" size={18}/></span></button>
  {open&&<div ref={menu} id={id} className="account-menu" role="menu" aria-label="Your account" onKeyDown={e=>{const actions=items(),index=actions.indexOf(document.activeElement as HTMLButtonElement);if(e.key==='Escape'){e.preventDefault();e.stopPropagation();close(true)}if(['ArrowDown','ArrowUp','Home','End'].includes(e.key)){e.preventDefault();actions[e.key==='Home'?0:e.key==='End'?actions.length-1:(index+(e.key==='ArrowDown'?1:-1)+actions.length)%actions.length]?.focus()}}}>
   <div className="account-identity" role="presentation"><strong>{me.displayName}</strong><span>{local?'Local workspace · No sign-in required':'Signed in'}{me.isOperator&&!local?' · Administrator':''}</span></div>
   <button role="menuitem" tabIndex={-1} onClick={()=>{close(true);onSettings()}}><Icon name="settings"/>Account settings</button>
   {!local&&<button role="menuitem" tabIndex={-1} onClick={async()=>{close(true);setBusy(true);setError('');try{await onSignOut()}catch(e){setError('Could not sign out. '+(e instanceof Error?e.message:'Please try again.'))}finally{setBusy(false)}}}><Icon name="logout"/>Sign out</button>}
  </div>}
  {error&&<div className="account-error" role="alert">{error}<button aria-label="Dismiss sign-out error" onClick={()=>setError('')}><Icon name="close"/></button></div>}
 </div>;
}
