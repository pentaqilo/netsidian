import {queuePreferenceWrite} from '../lib/preferences';
// User-scoped interaction preferences: how documents open and how the map
// canvas reacts to clicks. Persisted in /api/v1/me/preferences under the
// "interaction" key, mirroring the appearance persistence pattern (local
// cache + pending write so an offline change survives until the server
// accepts it). Every control here drives real behavior; none are inert.

import {useEffect,useRef,useState} from 'react';

export type DocMode='source'|'read'|'split'|'visual';
export interface Interaction{
  defaultDocumentMode:DocMode;
  readingDoubleClick:'visual'|'source'|'none';
  mapPreviewClick:'single'|'double';
  mapInlineEditing:boolean;
  memoComposerMode:'visual'|'markdown';
  canvasMinimap:boolean;
}
export const interactionDefaults:Interaction={defaultDocumentMode:'read',readingDoubleClick:'visual',mapPreviewClick:'double',mapInlineEditing:true,memoComposerMode:'visual',canvasMinimap:false};

const DOC_MODES=['source','read','split','visual'] as const;
function oneOf<T extends string>(value:unknown,allowed:readonly T[],fallback:T):T{
  return allowed.includes(value as T)?value as T:fallback;
}
// Stored values merge defensively: unknown or missing fields fall back to the
// defaults so a partial or hand-edited payload can never break the workspace.
export function mergeInteraction(saved:unknown):Interaction{
  const p=(saved&&typeof saved==='object'?saved:{}) as Record<string,unknown>;
  return{
    defaultDocumentMode:oneOf(p.defaultDocumentMode,DOC_MODES,interactionDefaults.defaultDocumentMode),
    readingDoubleClick:oneOf(p.readingDoubleClick,['visual','source','none'] as const,interactionDefaults.readingDoubleClick),
    mapPreviewClick:oneOf(p.mapPreviewClick,['single','double'] as const,interactionDefaults.mapPreviewClick),
    mapInlineEditing:p.mapInlineEditing!==false,
    memoComposerMode:oneOf(p.memoComposerMode,['visual','markdown'] as const,interactionDefaults.memoComposerMode),
    canvasMinimap:p.canvasMinimap===true,
  };
}

export function useInteraction(user:string){
  const [interaction,setInteraction]=useState<Interaction>(()=>{try{return mergeInteraction(JSON.parse(localStorage.getItem('netsidian:interaction:'+user)||'{}'))}catch{return interactionDefaults}});
  const [loaded,setLoaded]=useState(false);
  const edits=useRef(0),writes=useRef<Promise<unknown>>(Promise.resolve()),alive=useRef(false),currentUser=useRef(user);currentUser.current=user;
  const cacheKey='netsidian:interaction:'+user,pendingKey=cacheKey+':pending';
  const queue=(next:Interaction)=>{const owner=user;writes.current=queuePreferenceWrite(async()=>{
    if(!alive.current||currentUser.current!==owner)return;const r=await fetch('/api/v1/me/preferences');if(!r.ok)return;const p=await r.json();if(!alive.current||currentUser.current!==owner)return;
    const saved=await fetch('/api/v1/me/preferences',{method:'PUT',keepalive:true,headers:{'Content-Type':'application/json','X-Netsidian-Requested':'1'},body:JSON.stringify({...p,interaction:next})});
    if(saved.ok)try{const pending=localStorage.getItem(pendingKey);if(pending&&JSON.stringify(mergeInteraction(JSON.parse(pending)))===JSON.stringify(mergeInteraction(next)))localStorage.removeItem(pendingKey)}catch{}
  }).catch(()=>{})};
  useEffect(()=>{let active=true;alive.current=true;edits.current=0;setLoaded(false);let pending:Interaction|null=null;
    try{const raw=localStorage.getItem(pendingKey);pending=raw?mergeInteraction(JSON.parse(raw)):null;setInteraction(pending||mergeInteraction(JSON.parse(localStorage.getItem(cacheKey)||'{}')))}catch{setInteraction(interactionDefaults)}
    if(pending){queue(pending);setLoaded(true);}
    else fetch('/api/v1/me/preferences').then(r=>r.ok?r.json():{}).then((p:any)=>{if(active&&edits.current===0&&p.interaction){const next=mergeInteraction(p.interaction);setInteraction(next);try{localStorage.setItem(cacheKey,JSON.stringify(next))}catch{}}}).catch(()=>{}).finally(()=>{if(active)setLoaded(true)});
    return()=>{active=false;alive.current=false};
  },[user]);
  const update=(patch:Partial<Interaction>)=>{edits.current++;const next={...interaction,...patch};setInteraction(next);try{localStorage.setItem(cacheKey,JSON.stringify(next));localStorage.setItem(pendingKey,JSON.stringify(next))}catch{}queue(next)};
  return {interaction,update,loaded};
}

export function InteractionPanel({value,onChange}:{value:Interaction;onChange:(v:Partial<Interaction>)=>void}){
  const reset=()=>onChange({...interactionDefaults});
  return <div className="interaction-panel"><h3>Interaction</h3>
    <label>Default document mode<select aria-label="Default document mode" value={value.defaultDocumentMode} onChange={e=>onChange({defaultDocumentMode:e.target.value as DocMode})}><option value="read">Reading (default)</option><option value="source">Markdown source</option><option value="visual">Visual editor</option><option value="split">Split editor and preview</option></select></label>
    <p className="muted">How ordinary documents open. A mode you pick for a document stays with that document while it is open.</p>
    <label>Double-click in reading mode<select aria-label="Double-click in reading mode" value={value.readingDoubleClick} onChange={e=>onChange({readingDoubleClick:e.target.value as Interaction['readingDoubleClick']})}><option value="visual">Edit visually (default)</option><option value="source">Edit as Markdown source</option><option value="none">Do nothing</option></select></label>
    <p className="muted">Double-clicking the text of a document in reading mode switches to the chosen editor. Links, checkboxes and other controls keep their own double-click behavior.</p>
    <label>Open a map idea's preview on<select aria-label="Map preview activation" value={value.mapPreviewClick} onChange={e=>onChange({mapPreviewClick:e.target.value as Interaction['mapPreviewClick']})}><option value="double">Double click (default)</option><option value="single">Single click</option></select></label>
    <p className="muted">On a mind map, clicking an idea card opens its editable side preview with this gesture. Selecting, dragging and connecting ideas are unaffected.</p>
    <label>Edit idea text on the card<input aria-label="Map inline editing" type="checkbox" checked={value.mapInlineEditing} onChange={e=>onChange({mapInlineEditing:e.target.checked})}/></label>
    <p className="muted">Turn off to keep idea cards read-only on the canvas; double-click then always opens the side preview instead of editing on the card.</p>
    <label>New memos open in<select aria-label="Memo composer mode" value={value.memoComposerMode} onChange={e=>onChange({memoComposerMode:e.target.value as Interaction['memoComposerMode']})}><option value="visual">Visual editor (default)</option><option value="markdown">Markdown source</option></select></label>
    <p className="muted">The writing surface for a new memo and for opening a freshly created memo document.</p>
    <label>Show the canvas minimap by default<input aria-label="Canvas minimap by default" type="checkbox" checked={value.canvasMinimap} onChange={e=>onChange({canvasMinimap:e.target.checked})}/></label>
    <p className="muted">Opens the map overview when a canvas first loads. You can still hide it per canvas.</p>
    <button className="btn" onClick={reset}>Reset to defaults</button></div>;
}
