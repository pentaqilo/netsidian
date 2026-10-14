import {useEffect,useState} from 'react';
import {Dialog} from './Dialog';

// Server-side folder browser for local deployments with
// NETSIDIAN_LOCAL_FOLDER_PICK=1: the SPA cannot read absolute paths from a
// browser picker, so it walks the server's own directory tree instead and
// hands the chosen absolute path to POST /local/vaults.
interface Dir{name:string;path:string}
interface Listing{path:string;parent:string;folders:Dir[];truncated:boolean}

async function call<T>(path:string,init?:RequestInit):Promise<T>{
 const r=await fetch('/api/v1'+path,{...init,headers:{'Content-Type':'application/json','X-Netsidian-Requested':'1',...(init?.headers||{})}});
 const v=await r.json().catch(()=>({}));
 if(!r.ok)throw new Error((v as any)?.error?.message||'Request failed');
 return v as T;
}

export function FolderPicker({onPick,onClose}:{onPick:(path:string)=>void;onClose:()=>void}){
 const [listing,setListing]=useState<Listing|null>(null);
 const [manual,setManual]=useState('');
 const [name,setName]=useState('');
 const [error,setError]=useState('');
 const load=(p:string)=>{call<Listing>('/local/folders'+(p?'?path='+encodeURIComponent(p):'')).then(l=>{setListing(l);setManual(l.path);setError('')}).catch(e=>setError(e instanceof Error?e.message:String(e)));};
 useEffect(()=>{load('')},[]);
 const create=async()=>{if(!name.trim()||!listing)return;try{const r=await call<{path:string}>('/local/folders',{method:'POST',body:JSON.stringify({path:listing.path,name:name.trim()})});setName('');load(r.path)}catch(e){setError(e instanceof Error?e.message:String(e))}};
 return <Dialog title="Choose a folder for the vault" close={onClose}>
  {error&&<p className="error">{error}</p>}
  <div className="folder-path"><button className="btn" onClick={()=>listing?.parent&&load(listing.parent)} disabled={!listing?.parent} title="Go to parent folder">↑</button>
   <input aria-label="Folder path" value={manual} onChange={e=>setManual(e.target.value)} onKeyDown={e=>e.key==='Enter'&&load(manual)}/>
   <button className="btn" onClick={()=>void load(manual)}>Go</button></div>
  <div className="folder-list" role="listbox" aria-label="Folders">{listing?.folders.map(d=><button key={d.path} role="option" aria-selected={false} className="fp-row" onClick={()=>void load(d.path)} onDoubleClick={()=>onPick(d.path)}>🗀 {d.name}</button>)}
   {listing&&!listing.folders.length&&<p className="muted">No folders here — create one below or pick this folder.</p>}
   {listing?.truncated&&<p className="muted">Too many folders; only the first ones are listed. Type a path above.</p>}</div>
  <form className="folder-new" onSubmit={e=>{e.preventDefault();void create()}}><label>New folder<input value={name} onChange={e=>setName(e.target.value)} placeholder="my-vault"/></label><button className="btn" type="submit" disabled={!name.trim()||!listing}>Create</button></form>
  <div className="vault-actions"><button className="btn primary" disabled={!listing} onClick={()=>listing&&onPick(listing.path)}>Use this folder</button><button className="btn" onClick={onClose}>Cancel</button></div>
  <p className="muted">Markdown files inside are indexed in place — nothing is copied, Netsidian edits the folder as your vault root.</p>
 </Dialog>;
}
