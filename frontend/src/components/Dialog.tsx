import {useEffect,useRef,type ReactNode} from 'react';
import {Tool} from './Icon';
export function Dialog({title,close,children}:{title:string;close:()=>void;children:ReactNode}){
 const ref=useRef<HTMLDialogElement>(null);
 useEffect(()=>{const el=ref.current;const last=document.activeElement as HTMLElement;el?.showModal();// showModal() re-anchors focus after React's autoFocus commit, so honour an explicit // [data-autofocus] target and preselect any text: renaming/creating dialogs must let the user type immediately.
const t=el?.querySelector<HTMLElement>('[data-autofocus]');if(t){t.focus();try{(t as HTMLInputElement).select?.()}catch{}};return()=>{last?.focus()}},[]);
 return <dialog ref={ref} aria-label={title} className="workspace-dialog" onCancel={e=>{e.preventDefault();close()}} onClick={e=>{if(e.target===ref.current)close()}}><header><h2>{title}</h2><Tool icon="close" label="Close dialog" onClick={close}/></header>{children}</dialog>;
}
