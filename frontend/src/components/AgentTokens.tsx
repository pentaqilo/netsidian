import {useEffect,useRef,useState} from 'react';
import * as api from '../api/client';
import {useApp} from '../AppState';
import './AgentTokens.css';

const message=(e:unknown)=>e instanceof Error?e.message:String(e);
const date=(value:string)=>new Date(value).toLocaleDateString();
export function AgentTokens({initialVault,request=0}:{initialVault?:string;request?:number}){
 const {spaces}=useApp();
 const [keys,setKeys]=useState<api.KeyRecord[]|null>(null),[listError,setListError]=useState('');
 const [label,setLabel]=useState(''),[vaults,setVaults]=useState<string[]>(initialVault?[initialVault]:[]),[query,setQuery]=useState('');
 const [access,setAccess]=useState('read'),[trash,setTrash]=useState(false),[days,setDays]=useState(90);
 const [busy,setBusy]=useState(false),[revoking,setRevoking]=useState(''),[error,setError]=useState(''),[copied,setCopied]=useState(false);
 const [created,setCreated]=useState<(api.KeyRecord&{secret:string})|null>(null);
 const nameRef=useRef<HTMLInputElement>(null),secretRef=useRef<HTMLInputElement>(null);
 const vaultName=(id:string)=>spaces.find(v=>v.space.id===id)?.space.name||id;
 const load=async()=>{setListError('');try{setKeys((await api.listKeys()).keys)}catch(e){setListError(message(e))}};
 useEffect(()=>{void load()},[]);
 useEffect(()=>{if(initialVault&&!created)setVaults([initialVault])},[initialVault,request]);
 const create=async()=>{
  if(busy||!label.trim()||!vaults.length)return;
  setBusy(true);setError('');setCopied(false);
  try{
   const key=await api.createKey(label.trim(),{spaceIds:vaults,expiresInDays:days,scopes:['documents:read','search:read','graph:read','history:read','attachments:read',...(access==='write'?['documents:write','attachments:write',...(trash?['documents:delete']:[])]:[])]});
   setCreated(key);setLabel('');void load();
  }catch(e){setError(message(e))}finally{setBusy(false)}
 };
 const reset=()=>{setCreated(null);setCopied(false);setError('');requestAnimationFrame(()=>nameRef.current?.focus())};
 const copy=async()=>{try{await navigator.clipboard.writeText(created!.secret);setCopied(true);setError('')}catch{secretRef.current?.focus();secretRef.current?.select();setError('Select the token and copy it manually. Clipboard access is unavailable.')}};
 const replace=(key:api.KeyRecord)=>{if(created)return;setLabel(key.label+' replacement');setVaults(key.spaceIds.filter(id=>spaces.some(v=>v.space.id===id)));setAccess(key.scopes?.includes('documents:write')?'write':'read');setTrash(!!key.scopes?.includes('documents:delete'));setError('');nameRef.current?.focus()};
 return <div className="agent-token-workspace">
  <section className="agent-card agent-token-create" aria-labelledby="create-token-heading">
   <header><span className="agent-step">ACCESS</span><h3 id="create-token-heading">{created?'Token created':'Create access token'}</h3><p>{created?'Copy this token now. It will not be shown again after you dismiss it.':'Give each agent its own token so access is easy to review and revoke.'}</p></header>
   {created?<div className="agent-token-result">
    <div className="agent-token-success" role="status">Ready to connect · {created.label}</div>
    <label htmlFor="created-agent-token">Your token · shown once</label>
    <div className="agent-secret-row"><input id="created-agent-token" ref={secretRef} value={created.secret} readOnly spellCheck={false} autoComplete="off" onFocus={e=>e.target.select()}/><button className="btn primary" onClick={()=>void copy()}>{copied?'Copied':'Copy token'}</button></div>
    <p className="muted">{created.spaceIds.map(vaultName).join(', ')} · {created.scopes?.includes('documents:write')?'Read and write':'Read only'} · {created.expiresAt?'Expires '+date(created.expiresAt):'No expiry'}</p>
    <p>Paste it into your MCP client when prompted for authentication. Treat it like a password.</p>
    <button className="btn" onClick={reset}>Done — hide token</button>
   </div>:<form onSubmit={e=>{e.preventDefault();void create()}}>
    <label className="agent-field" htmlFor="new-token-label">Token name<input id="new-token-label" aria-label="Token name" aria-describedby="token-name-help" ref={nameRef} className="input" value={label} onChange={e=>setLabel(e.target.value)} placeholder="e.g. OpenCode · project research" required maxLength={200}/><small id="token-name-help">Use a name that helps you recognise the agent later.</small></label>
    <fieldset className="agent-vault-picker"><legend>Vault access <span>{vaults.length} selected</span></legend>
     {spaces.length>4&&<input className="input" aria-label="Find token vaults" placeholder="Find a vault…" value={query} onChange={e=>setQuery(e.target.value)}/>}
     <div className="token-vault-list">{spaces.filter(v=>v.space.name.toLowerCase().includes(query.toLowerCase())).map(({space})=><label key={space.id}><input type="checkbox" checked={vaults.includes(space.id)} onChange={e=>setVaults(old=>e.target.checked?[...old,space.id]:old.filter(id=>id!==space.id))}/><span>{space.name}</span></label>)}</div>
     {!spaces.length&&<p className="muted">Create or attach a vault before creating a token.</p>}
     {!!spaces.length&&!spaces.some(v=>v.space.name.toLowerCase().includes(query.toLowerCase()))&&<p className="muted">No matching vaults.</p>}
     <small>The agent can only access selected vaults.</small>
    </fieldset>
    <div className="agent-token-fields"><label className="agent-field">Permissions<select className="input" value={access} onChange={e=>setAccess(e.target.value)}><option value="read">Read only</option><option value="write">Read and write documents</option></select></label><label className="agent-field">Expires after<select className="input" value={days} onChange={e=>setDays(Number(e.target.value))}>{[7,30,90,365].map(n=><option key={n} value={n}>{n} days</option>)}</select></label></div>
    {access==='write'&&<label className="agent-trash-option"><input type="checkbox" checked={trash} onChange={e=>setTrash(e.target.checked)}/> Allow moving documents to trash</label>}
    <footer><span>{vaults.length} {vaults.length===1?'vault':'vaults'} · {access==='read'?'Read only':trash?'Read, write and trash':'Read and write'} · {days} days</span><button className="btn primary" disabled={busy||!label.trim()||!vaults.length}>{busy?'Creating…':'Create token'}</button></footer>
   </form>}
   {error&&<p className="agent-inline-error" role="alert">{error}</p>}
  </section>
  <section className="agent-card" aria-labelledby="existing-tokens-heading"><header className="agent-token-list-heading"><div><h3 id="existing-tokens-heading">Your tokens</h3><p>Review access or revoke a token when an agent no longer needs it.</p></div><button className="btn small" disabled={!!revoking} onClick={()=>void load()}>Refresh</button></header>
   {listError&&<p role="alert" className="agent-inline-error">Could not load tokens: {listError} <button className="btn small" onClick={()=>void load()}>Retry</button></p>}
   {!keys&&!listError&&<p role="status">Loading tokens…</p>}
   {keys?.length===0&&<div className="agent-token-empty"><strong>No tokens yet</strong><p>Create your first token above to connect an agent.</p></div>}
   <div className="agent-token-list">{keys?.map(key=>{
    const status=key.revokedAt?'Revoked':key.expiresAt&&Date.parse(key.expiresAt)<=Date.now()?'Expired':'Active';
    return <article className="agent-token-item" key={key.id}><div className="agent-token-item-main"><div className="agent-token-name"><strong>{key.label}</strong><span className={'agent-token-status '+status.toLowerCase()}>{status}</span></div><p>{key.spaceIds.length?key.spaceIds.map(vaultName).join(', '):'All accessible vaults'} · {!key.scopes?.length?'Full access':key.scopes.includes('documents:write')?'Read and write':'Read only'}</p><small>Created {date(key.createdAt)} · {key.expiresAt?'Expires '+date(key.expiresAt):'No expiry'}</small><details><summary>Permission details</summary><p>{key.scopes?.join(', ')||'Full access'}{key.scopes?.includes('documents:delete')?' · Can move documents to trash':''}</p></details></div><div className="agent-token-item-actions">{status==='Active'?<button className="btn small danger" disabled={!!revoking} onClick={async()=>{setRevoking(key.id);setListError('');try{await api.revokeKey(key.id);await load()}catch(e){setListError(message(e))}finally{setRevoking('')}}}>{revoking===key.id?'Revoking…':'Revoke'}</button>:<button className="btn small" disabled={!!created} onClick={()=>replace(key)}>Replace</button>}</div></article>
   })}</div>
   <details className="agent-api-details"><summary>Using the HTTP API instead?</summary><p>Send your token as <code>Authorization: Bearer</code>. Start with <code>GET /api/v1/agent/whoami</code> to discover accessible vaults and capabilities.</p><a href="/api/v1/openapi.json" target="_blank" rel="noreferrer">View API contract</a></details>
  </section>
 </div>
}
