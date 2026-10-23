import {restoredMemoryConfiguration} from '../lib/memorySettings';
import {randomId} from '../lib/randomId';
import {useEffect,useState} from 'react';
import * as api from '../api/client';
import {useApp} from '../AppState';
import {agentSkill,memoryConfiguration,memorySkillPackage,memoryPolicyVersion,newMemoryConfiguration,type MemoryConfiguration} from '../lib/agentSkill';

export function AgentMemoryFolder({initialVault}:{initialVault?:string}){
 const {spaces,me}=useApp();
 const [vault,setVault]=useState(initialVault||spaces[0]?.space.id||''),[folder,setFolder]=useState('agent-memory');
 const [configuration,setConfiguration]=useState<MemoryConfiguration>({...newMemoryConfiguration,scopeId:me?.userId||''});
 const [adopted,setAdopted]=useState(false),[legacy,setLegacy]=useState(false),[copied,setCopied]=useState(false),[busy,setBusy]=useState(false),[status,setStatus]=useState(''),[error,setError]=useState('');
 useEffect(()=>{if(initialVault)setVault(initialVault)},[initialVault]);
 const key='netsidian:agent-folder:'+me?.userId+':'+vault;
 useEffect(()=>{
  let existing:string|null=null;try{existing=localStorage.getItem(key)}catch{};setFolder(existing||'agent-memory');setCopied(false);setStatus('');setError('');
  let saved:unknown=null;try{saved=JSON.parse(localStorage.getItem(key+':policy')||'null')}catch{}
  const restored=restoredMemoryConfiguration(saved,me?.userId||'',!!existing);
  setAdopted(restored.adopted);setLegacy(!!existing&&!restored.adopted);setConfiguration(restored.configuration);
 },[key]);
 const update=(patch:Partial<MemoryConfiguration>)=>{setConfiguration(old=>({...old,...patch}));setAdopted(false);setCopied(false);setStatus('')};
 const clean=folder.trim().replace(/\/+$/,'');
 const valid=!!clean&&!clean.startsWith('/')&&!clean.split('/').some(x=>!x||x==='.'||x==='..'||x.toLowerCase()==='.netsidian')&&!/[\\%:*?"<>|\u0000-\u001f\u007f]/.test(clean)&&new TextEncoder().encode(clean+'/index.md').length<=1024;
 const chosen=spaces.find(v=>v.space.id===vault),writable=chosen?.role==='owner'||chosen?.role==='editor';
 const netsidian=configuration.provider==='netsidian',destinationValid=netsidian?!!vault&&valid:!!configuration.destination?.trim();
 const ready=destinationValid&&!!configuration.scopeId.trim()&&Array.from(configuration.scopeId).length<=64;
 const skill=agentSkill(location.origin,netsidian?vault:'',netsidian?clean:'',configuration);
 const copyOversized=new TextEncoder().encode(skill).length>7000||new TextEncoder().encode(JSON.stringify(JSON.stringify(memoryConfiguration(location.origin,netsidian?vault:'',netsidian?clean:'',configuration)))).length>600;
 const save=()=>{if(!ready)return;try{localStorage.setItem(key+':policy',JSON.stringify({...configuration,policyVersion:memoryPolicyVersion}));if(netsidian)localStorage.setItem(key,clean);setAdopted(true);setLegacy(false);setStatus('Configuration saved in this browser. Your agent must load the updated skill; copying does not enable automatic memory.');setError('')}catch{setError('Could not save configuration in this browser. Enable browser storage and retry.')}};
 const prepare=async()=>{
  if(!netsidian||!writable||!ready)return;
  setBusy(true);setError('');setStatus('');
  try{const path=clean+'/README.md';let exists=true;try{await api.lookupDocument(vault,path)}catch(e){if(e instanceof api.ApiError&&e.status===404)exists=false;else throw e}
   if(!exists)await api.createDocument(vault,path,'---\ntags: [agent-memory]\n---\n\n# Agent memory\n\nOrdinary Markdown memory: compact index, evidence, topical knowledge and derived views. Follow the configured retention policy; preserve exact wording, revisions and unrelated human content.\n','agent-folder-'+randomId());
   localStorage.setItem(key,clean);setStatus('Memory folder is ready. Existing documents were preserved. No index entries were migrated or created.');
  }catch(e){setError('Folder preparation did not complete: '+(e instanceof Error?e.message:String(e))+'. Existing files were not migrated.')}finally{setBusy(false)}
 };
 const download=()=>{
  const bytes=memorySkillPackage(location.origin,netsidian?vault:'',netsidian?clean:'',configuration);const url=URL.createObjectURL(new Blob([new Uint8Array(bytes)],{type:'application/zip'}));const anchor=document.createElement('a');anchor.href=url;anchor.download='agent-memory-'+memoryPolicyVersion+'.zip';anchor.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
 };
 return <section className="agent-memory-folder" aria-label="Agent memory folder">
  <h3>Evidence-backed memory</h3><p>Agents retain useful preferences, decisions and outcomes as editable Markdown. Recall first, preserve evidence, and detect stale summaries.</p>
  <p className="muted">Policy {memoryPolicyVersion} · Reasoning runs in your connected agent. Copying a skill does not enable automatic or background memory.</p>
  <label>Storage provider<select className="input" aria-label="Memory storage provider" value={configuration.provider} onChange={e=>update({provider:e.target.value as MemoryConfiguration['provider']})}><option value="netsidian">Netsidian vault</option><option value="local">Local Markdown files</option><option value="remote">Other remote document provider</option></select></label>
  <label>Deployment location<select className="input" value={configuration.location} onChange={e=>update({location:e.target.value as MemoryConfiguration['location']})}><option value="remote">Remote deployment</option><option value="local">Local deployment</option></select></label>
  {netsidian?<><label>Destination vault<select className="input" aria-label="Skill vault" value={vault} onChange={e=>setVault(e.target.value)}>{spaces.map(v=><option key={v.space.id} value={v.space.id}>{v.space.name}</option>)}</select></label><label>Memory folder<input className="input" aria-label="Agent memory folder path" value={folder} onChange={e=>{setFolder(e.target.value);setAdopted(false);setCopied(false);setStatus('')}} placeholder="agent-memory"/></label>{!valid&&<p role="status">Use a relative folder path without reserved names, traversal or invalid filename characters. Folder and index.md must fit 1024 bytes.</p>}<p className="muted">{writable?'Your account can prepare this folder. The connected token may have fewer permissions.':'Read-only vault: recall and skill export are available; folder preparation and memory writes are unavailable.'}</p></>:<><label>{configuration.provider==='local'?'Configured local root':'Configured provider destination'}<input className="input" value={configuration.destination||''} onChange={e=>update({destination:e.target.value})} placeholder={configuration.provider==='local'?'/configured/memory':'Provider name and configured resource ID'}/></label><p className="muted">This exports adapter instructions only. Netsidian does not access this destination or advertise its capabilities. {configuration.provider==='local'?'The included helper supports POSIX/Linux single-file writes.':'The agent must discover conditional-write and recovery capabilities before merging.'}</p></>}
  <div className="agent-token-fields"><label>Audience<select className="input" value={configuration.audience} onChange={e=>update({audience:e.target.value as MemoryConfiguration['audience']})}><option value="personal">Personal</option><option value="shared">Shared</option></select></label><label>Scope<select className="input" value={configuration.scope} onChange={e=>update({scope:e.target.value as MemoryConfiguration['scope'],scopeId:e.target.value==='user'?me?.userId||'':''})}>{['user','project','environment','session'].map(scope=><option key={scope} value={scope}>{scope}</option>)}</select></label></div>
  <label>Scope identity<input className="input" maxLength={64} value={configuration.scopeId} onChange={e=>update({scopeId:e.target.value})} placeholder="Stable user, project or environment identifier"/></label>
  <label>Retention mode<select className="input" aria-label="Memory retention mode" value={configuration.mode} onChange={e=>update({mode:e.target.value as MemoryConfiguration['mode']})}><option value="selective">Selective · important requests and durable outcomes</option><option value="explicit-only">Explicit only · when asked to remember</option></select></label>
  {legacy&&<p className="token-summary">Your existing workflow is unchanged. Choose a mode and adopt this policy before exporting updated instructions.</p>}
  <p className="muted">Vault access and permission to retain information are separate. Folders and scope identities do not restrict token access. Use a private vault for personal preferences and an authoritative project vault for shared decisions.</p>
  {error&&<p role="alert">{error}</p>}{status&&<p role="status">{status}</p>}
  <div className="agent-skill-actions"><button className="btn primary" disabled={!ready||busy} onClick={save}>{adopted?'Configuration saved':'Adopt memory policy'}</button>{netsidian&&<button className="btn" disabled={busy||!ready||!writable||!adopted} onClick={()=>void prepare()}>{busy?'Preparing…':'Prepare memory folder'}</button>}<button className="btn" disabled={!ready||!adopted||copyOversized} onClick={()=>void navigator.clipboard.writeText(skill).then(()=>{setCopied(true);setError('')}).catch(()=>setError('Copy failed. Open the skill below and copy its text.'))}>{copied?'Skill copied':'Copy self-contained skill'}</button><button className="btn" disabled={!ready||!adopted} onClick={download}>Download skill package</button></div>
  {copyOversized&&<p role="status">This configuration exceeds the reviewed single-file size budget. Download the package to keep configuration separate; nothing was truncated.</p>}
  {configuration.provider==='local'&&<p className="muted">Local writes require the tested helper from the downloaded package. Copying instructions alone does not install it.</p>}
  <p className="muted">Configuration is saved in this browser and included in exports, not applied as a server-wide retention setting. Supply credentials separately; exports contain no token. Existing notes remain editable and are never automatically migrated.</p>
  <details><summary>Review self-contained skill</summary><pre className="agent-example"><code>{skill}</code></pre></details>
 </section>
}
