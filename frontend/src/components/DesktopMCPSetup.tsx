import {useEffect,useState} from 'react';
import * as api from '../api/client';
import {useApp} from '../AppState';
export function DesktopMCPSetup(){
 const {spaces}=useApp();const[state,setState]=useState<api.DesktopMCPStatus|null>(null),[config,setConfig]=useState<api.DesktopMCPConfig|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(''),[copied,setCopied]=useState(false);
 const load=()=>api.getDesktopMCP().then(status=>{setState(status);setConfig(status.config);setError('')}).catch(e=>setError(e instanceof Error?e.message:String(e)));
 useEffect(()=>{void load()},[]);
 const save=async()=>{if(!config)return;setBusy(true);setError('');setCopied(false);try{const status=await api.setDesktopMCP(config);setState(status);setConfig(status.config)}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}};
 const copy=async()=>{if(!state?.running)return;const message=`Paste to agent: add ${state.url}\nNetsidian desktop MCP accesses the selected local vaults. ${state.config.allowUnauthenticated?'No token is required for this localhost listener.':'Get a scoped PAT from Netsidian desktop Settings → MCP & agents and keep it in your client secret store or NETSIDIAN_TOKEN.'}\nDefault memory folder: agent-memory; tag: agent-memory. ${state.config.readOnly?'Access is read-only.':'Use If-Match and idempotency keys when editing.'}`;try{await navigator.clipboard.writeText(message);setCopied(true)}catch{setError('Copy failed. Select and copy the endpoint below.')}};
 return <section className="mcp-setup" aria-label="Desktop MCP server"><h3>Local MCP server</h3><p>Connect agents to selected desktop vaults through localhost.</p>{error&&<p role="alert">{error}</p>}{!config?<><p>Loading MCP settings…</p>{error&&<button className="btn small" onClick={()=>void load()}>Retry MCP settings</button>}</>:<>
 <label><input type="checkbox" checked={config.enabled} onChange={e=>setConfig({...config,enabled:e.target.checked})}/> Enable local MCP server</label>
 <label>Local MCP port <input className="input" type="number" min={0} max={65535} value={config.port} onChange={e=>setConfig({...config,port:Number(e.target.value)})}/></label><p className="muted">Bound to 127.0.0.1 only. Port 0 chooses an available port.</p>
 <label><input type="checkbox" checked={config.allowUnauthenticated} onChange={e=>setConfig({...config,allowUnauthenticated:e.target.checked})}/> Allow local agents without a token</label><p className="muted">When enabled, local processes can access the selected vaults with these permissions.</p>
 <label><input type="checkbox" checked={config.readOnly} onChange={e=>setConfig({...config,readOnly:e.target.checked,allowTrash:e.target.checked?false:config.allowTrash})}/> Read-only access</label>
 {!config.readOnly&&<label><input type="checkbox" checked={config.allowTrash} onChange={e=>setConfig({...config,allowTrash:e.target.checked})}/> Allow moving notes to trash</label>}
 <fieldset><legend>Vaults available to local agents</legend>{spaces.map(({space})=><label key={space.id}><input type="checkbox" checked={config.vaultIds.includes(space.id)} onChange={e=>setConfig({...config,vaultIds:e.target.checked?[...config.vaultIds,space.id]:config.vaultIds.filter(id=>id!==space.id)})}/> {space.name}</label>)}{config.vaultIds.filter(id=>!spaces.some(item=>item.space.id===id)).map(id=><label key={id}><input type="checkbox" checked onChange={()=>setConfig({...config,vaultIds:config.vaultIds.filter(value=>value!==id)})}/> Unavailable vault ({id})</label>)}</fieldset>
 <button className="btn primary" disabled={busy||(config.enabled&&!config.vaultIds.length)} onClick={()=>void save()}>{busy?'Applying…':'Apply MCP settings'}</button>
 <p role="status">{state?.running?'Running':'Stopped'}{state?.error?' · '+state.error:''}</p>
 {state?.running&&<><p>Paste to agent: add <code>{state.url}</code></p><button className="btn small" onClick={()=>void copy()}>{copied?'Copied':'Copy agent instructions'}</button>{!state.config.allowUnauthenticated&&<p>Create a personal access token below and supply it to your agent separately.</p>}</>}
 <p>Default memory folder: <code>agent-memory/</code>, tagged <code>agent-memory</code>.</p>
 </>}</section>
}
