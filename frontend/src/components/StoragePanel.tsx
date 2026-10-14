import {useEffect,useState} from 'react';
import {useApp} from '../AppState';
import {useNavigate} from 'react-router-dom';
import {Icon} from './Icon';
import * as api from '../api/client';
import type {VaultStorageConfig as Profile} from '../api/types';

const initial:Profile={provider:'base',bucket:'',region:'us-east-1',endpoint:'',prefix:'netsidian',pathStyle:false,auth:'keys',accessKey:'',secretKey:'',sessionToken:'',stsEndpoint:'',roleArn:''};
function storageError(error:unknown){const message=error instanceof Error?error.message:String(error);if(message.includes('StatusCode: 403'))return 'Access to this bucket was denied. Check the credentials and bucket permissions.';if(message.includes('StatusCode: 404'))return 'The bucket could not be found. Check its name, region, and endpoint.';return message}

type StoragePanelProps={createOnly?:boolean;onCreated?:(id:string)=>void;onFolderBrowse?:()=>void};

export function StoragePanel(props:StoragePanelProps){
 const {settings}=useApp();
 if(!settings)return <p role="status">Loading storage options…</p>;
 const native=settings.runtime.kind==='desktop';
 return native?<LocalStoragePanel {...props}/>:<><ManagedStoragePanel {...props}/>{props.onFolderBrowse&&<p className="vault-folder-alternative"><button type="button" className="btn" onClick={props.onFolderBrowse}><Icon name="folder"/> Open a folder on the server…</button></p>}</>;
}

function LocalStoragePanel({createOnly=false,onCreated}:StoragePanelProps){
 const {spaces,refresh}=useApp(),navigate=useNavigate();
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const pick=async()=>{setBusy(true);setError('');try{const result=await api.attachNativeVault();if(result.cancelled)return;await refresh();if(onCreated)onCreated(result.space.id);else navigate('/s/'+result.space.id)}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}};
 return <section className="storage-panel" aria-label="Local vault storage">
 {!createOnly&&<h3>Local vault storage</h3>}
 <label>Storage provider<select className="input" disabled value="local"><option value="local">Local folder on this computer</option></select></label>
 <p>Choose a folder to create or open a vault. Markdown files and attachments stay in that folder. Existing documents are indexed in place.</p>
 <p className="muted">The folder name becomes the vault name. You can rename the vault later without renaming its folder.</p>
 {error&&<p role="alert" className="banner error">{error}</p>}
 <button className="btn primary" disabled={busy} onClick={()=>void pick()}><Icon name="folder"/>{busy?'Opening folder picker…':'Choose folder…'}</button>
 {!createOnly&&<><h4>Configured vault folders</h4><ul className="vault-storage-locations">{spaces.map(v=><li key={v.space.id}><strong>{v.space.name}</strong><span>{v.space.storageBinding?.startsWith('local-folder:')?v.space.storageBinding.slice('local-folder:'.length):'Managed local app storage'}</span></li>)}</ul><p className="muted">To work in a different folder, open it as another vault. Your current vault files stay in their configured location.</p></>}
 </section>;
}

function ManagedStoragePanel({createOnly=false,onCreated}:StoragePanelProps){
 const{spaces,refresh,settings}=useApp();
 const options=settings?{providers:settings.runtime.storage.providers,base:settings.runtime.storage.defaultProvider}:null;
 const[error,setError]=useState(''),[success,setSuccess]=useState(''),[busy,setBusy]=useState(false);
 const[name,setName]=useState(''),[selected,setSelected]=useState(''),[profile,setProfile]=useState<Profile>(initial),[loading,setLoading]=useState(false);
 useEffect(()=>{if(!selected){setProfile({...initial});setLoading(false);setError('');return}let alive=true;setLoading(true);setError('');api.getVaultStorage(selected).then(v=>{if(alive)setProfile({...initial,...v})}).catch(e=>{if(alive)setError(e.message)}).finally(()=>{if(alive)setLoading(false)});return()=>{alive=false}},[selected]);
 const set=(key:keyof Profile,value:string|boolean)=>setProfile(p=>({...p,[key]:value}));
 const existing=selected!=='';
 const binding=spaces.find(v=>v.space.id===selected)?.space.storageBinding;
 const attached=binding?.startsWith('local-folder:');
 const save=async()=>{setBusy(true);setError('');setSuccess('');try{if(existing){await api.configureVaultStorage(selected,profile);setSuccess('Credentials updated. Bucket connection verified.');await refresh()}else{const result=await api.createSpace(name.trim(),'shared',profile.provider==='base'?undefined:profile);setName('');setSuccess('Vault created.');await refresh();onCreated?.(result.space.id)}setProfile(p=>({...p,accessKey:'',secretKey:'',sessionToken:''}))}catch(e){setError(storageError(e))}finally{setBusy(false)}};
 return <form className="storage-panel" aria-label="Vault storage" onSubmit={e=>{e.preventDefault();if(!busy&&options&&!loading&&(!existing?name.trim():profile.provider!=='base'))void save()}}>{!createOnly&&<h3>Vault storage</h3>}<p>Choose where this vault’s documents and attachments are stored.</p><p className="muted">Service default: <strong>{options?.base==='s3-main'?'S3 compatible':options?'Local managed storage':'Loading…'}</strong>. Service default storage is configured by your administrator. No personal credentials are needed.</p>
 {error&&<p role="alert" className="banner error">{error}</p>}{success&&<p role="status" className="banner">{success}</p>}
 {!createOnly&&<label>Manage storage for<select className="input" aria-label="Manage vault storage" disabled={busy} value={selected} onChange={e=>{setSelected(e.target.value);setSuccess('')}}><option value="">Create a new vault</option>{spaces.filter(v=>v.role==='owner').map(v=><option key={v.space.id} value={v.space.id}>{v.space.name}</option>)}</select></label>}
 {!existing&&<label>Vault name<input className="input" disabled={busy} required maxLength={200} autoFocus={createOnly} value={name} onChange={e=>setName(e.target.value)} placeholder="New vault name"/></label>}
 {loading?<p>Loading storage configuration…</p>:<>
 <label>Storage provider<select className="input" aria-label="Storage provider" value={profile.provider} onChange={e=>set('provider',e.target.value)} disabled={busy||attached||(existing&&profile.provider!=='base')}><option value="base">{attached?'Attached local folder':existing&&binding==='local-main'?'Local managed storage':existing&&binding==='s3-main'?'Service S3 storage':options?.base==='s3-main'?'Service default — S3 compatible':'Service default — Local managed storage'}</option>{options?.providers.includes('s3')&&<option value="s3">Your S3 bucket</option>}</select></label>
 {profile.provider==='s3'&&<fieldset disabled={busy}><legend>S3 compatible storage</legend><p className="muted">Current documents and attachments are stored in your bucket; revision history and application metadata remain on this instance. Each vault uses an isolated prefix. {existing?'Switching an existing vault requires it to be empty. Configured bucket locations cannot be changed; credentials can be rotated.':''}</p>
 <div className="storage-fields">{([['bucket','Bucket'],['region','Region'],['endpoint','Endpoint (blank for AWS S3)'],['prefix','Key prefix']] as const).map(([key,label])=><label key={key}>{label}<input className="input" required={key==='bucket'} value={String(profile[key]||'')} disabled={existing&&spaces.find(v=>v.space.id===selected)?.space.storageBinding==='s3-vault'} onChange={e=>set(key,e.target.value)}/></label>)}</div>
 <label><input type="checkbox" checked={profile.pathStyle} onChange={e=>set('pathStyle',e.target.checked)} disabled={existing&&spaces.find(v=>v.space.id===selected)?.space.storageBinding==='s3-vault'}/> Path style addressing (MinIO and other compatible services)</label>
 <label>Authentication<select className="input" aria-label="Authentication" value={profile.auth} onChange={e=>setProfile(p=>({...p,auth:e.target.value,accessKey:'',secretKey:'',sessionToken:''}))}><option value="keys">Access key and secret key</option><option value="session">Temporary credentials with session token</option>{(settings?.authMode==='external'||settings?.authMode==='mixed')&&<option value="oidc">Use my OIDC identity through STS</option>}</select></label>
 {profile.auth==='oidc'?<><p>Requires a trusted STS server and a role trusting this instance’s OIDC issuer and client audience. The server exchanges your identity token for temporary S3 credentials.</p><label>STS endpoint<input className="input" value={profile.stsEndpoint} onChange={e=>set('stsEndpoint',e.target.value)}/></label><label>Role ARN<input className="input" value={profile.roleArn} onChange={e=>set('roleArn',e.target.value)}/></label></>:<><div className="storage-fields"><label>Access key<input className="input" autoComplete="off" value={profile.accessKey} onChange={e=>set('accessKey',e.target.value)} placeholder={existing?'Leave all credential fields empty to keep saved credentials':''}/></label><label>Secret key<input className="input" type="password" autoComplete="new-password" value={profile.secretKey} onChange={e=>set('secretKey',e.target.value)}/></label></div>{profile.auth==='session'&&<label>Session token<input className="input" type="password" autoComplete="new-password" value={profile.sessionToken} onChange={e=>set('sessionToken',e.target.value)}/></label>}</>}
 <p className="muted">Credentials are encrypted on the server and never returned by the API. Bucket access is checked before saving. Custom endpoints must be in the operator’s allowlist.</p></fieldset>}
 <button type="submit" className="btn primary" disabled={busy||loading||!options||(!existing&&!name.trim())||(existing&&profile.provider==='base')}>{busy?'Checking storage…':existing?'Verify and save credentials':'Create vault'}</button>
 {existing&&profile.provider==='base'&&<p className="muted">{attached?'This vault uses its attached local folder. To use S3, create a new vault and import its documents.':'This vault uses managed service storage. Select your own S3 bucket to configure it if the vault is empty.'}</p>}</>}
 </form>
}
