import {Navigate,Route,Routes,useNavigate,useParams} from 'react-router-dom';
import {useEffect,useState} from 'react';
import {useApp} from './AppState';
import AuthPage from './pages/AuthPage';
import Workspace from './pages/Workspace';
import SettingsPage from './pages/SettingsPage';
import {Dialog} from './components/Dialog';
import {Icon} from './components/Icon';
import {StoragePanel} from './components/StoragePanel';
function LoadError(){const {spacesError,refresh}=useApp();return <main className="workspace-empty onboarding"><h1>Could not load your workspace</h1><div className="workspace-error" role="alert"><span>{spacesError}</span></div><button className="btn" onClick={()=>void refresh()}>Retry</button></main>}
function Home(){const {spaces,me,loading,spacesError}=useApp(),navigate=useNavigate();const [newVault,setNewVault]=useState(false);useEffect(()=>{
 if(loading||spacesError||!spaces.length)return;let vault=spaces[0].space.id;
 try{const recent=JSON.parse(localStorage.getItem('netsidian:vault-recents:'+(me?.userId||'local'))||'[]');if(Array.isArray(recent)){const last=recent.find(id=>typeof id==='string'&&spaces.some(s=>s.space.id===id));if(last)vault=last}}catch{}
 navigate('/s/'+vault,{replace:true});
},[spaces,me?.userId,navigate,loading,spacesError]);
 // Transient fetch: never imply the account has no vaults while it is loading.
 if(loading)return <main className="workspace-empty onboarding"><p role="status">Opening your vaults…</p></main>;
 // A real vault-list failure stays an error, not a "no vaults" empty state.
 if(spacesError)return <LoadError/>;
 // Genuine absence: a neutral, informational empty state with the existing
 // vault-creation flow wired to the Create button.
 if(!spaces.length)return <main className="workspace-empty onboarding"><Icon name="book" size={44}/><h1>No vault yet</h1><p>Create a vault to start writing. Your vaults hold Markdown documents and attachments.</p><div><button className="btn primary" onClick={()=>setNewVault(true)}><Icon name="plus"/> Create new vault</button></div>{newVault&&<Dialog title="New vault" close={()=>setNewVault(false)}><StoragePanel createOnly onCreated={id=>navigate('/s/'+id)}/></Dialog>}</main>;
 return <p className="muted">Opening your space…</p>}
function MCPSettings(){const {spaces}=useApp();return spaces.length?<Navigate to={'/s/'+spaces[0].space.id+'/settings/mcp'} replace/>:<main className="settings-scroll"><p>Create or join a vault before issuing a vault-scoped token.</p><SettingsPage/></main>}
function VaultRoute(){const {spaceId}=useParams(),{spaces,loading,spacesError,me}=useApp();
 // Do not unmount a known workspace (and its drafts) during a background refresh.
 if(spaces.some(s=>s.space.id===spaceId))return <Workspace key={me?.userId}/>;
 if(loading)return <main className="workspace-empty onboarding"><p role="status">Opening your vaults…</p></main>;
 if(spacesError)return <LoadError/>;
 if(!spaces.length)return <Home/>;
 return <main className="workspace-empty onboarding"><Icon name="book" size={44}/><h1>Vault unavailable</h1><p>This vault may have been removed, or you may no longer have access.</p><button className="btn primary" onClick={()=>location.assign('/')}>Open my vaults</button></main>;
}
export default function App(){const {ready,me,spacesError}=useApp();if(!ready)return <div className="workspace-empty onboarding">Opening Netsidian…</div>;if(!me&&spacesError)return <LoadError/>;return <Routes><Route path="/auth" element={<AuthPage/>}/>{!me?<Route path="*" element={<Navigate to={'/auth?next='+encodeURIComponent(location.pathname+location.hash)} replace/>}/>:<><Route path="/settings/mcp" element={<MCPSettings/>}/><Route path="/s/:spaceId/*" element={<VaultRoute/>}/><Route path="*" element={<Home/>}/></>}</Routes>}
