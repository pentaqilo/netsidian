import { useEffect, useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import * as api from "../api/client";
import {AgentTokens} from "../components/AgentTokens";
import { useApp } from "../AppState";
import { FolderPicker } from "../components/FolderPicker";
import { StoragePanel } from "../components/StoragePanel";
import {AgentMemoryFolder} from "../components/AgentMemoryFolder";
import { AdminPanel } from "../components/AdminPanel";
import {MCPSetup} from '../components/MCPSetup';

const errText = (e: unknown) =>
  e instanceof api.ApiError ? `${e.code}: ${e.message}` : String(e);

export default function SettingsPage({appearance,interaction,initialVault,agentRequest=0,onAgentOpened,accountRequest=0,onAccountOpened}:{appearance?:ReactNode;interaction?:ReactNode;initialVault?:string;agentRequest?:number;onAgentOpened?:()=>void;accountRequest?:number;onAccountOpened?:()=>void}) {
 const [section,setSection]=useState(()=>location.pathname.endsWith('/settings/mcp')?'agents':localStorage.getItem("netsidian.settings.section")||"account");
 useEffect(()=>{if(location.pathname.endsWith('/settings/mcp'))setSection('agents')},[location.pathname]);
  const { me, settings, refresh } = useApp();
  useEffect(()=>{if(accountRequest){setSection("account");localStorage.setItem("netsidian.settings.section","account");onAccountOpened?.()}},[accountRequest]);

  useEffect(()=>{const allowed=['account','ui',...(interaction?['interaction']:[]),'storage','agents',...(me?.isOperator?['system',...(settings?.authMode==='none'?[]:['users'])]:[])];if(!allowed.includes(section))setSection('account')},[section,me?.isOperator,settings?.authMode]);
  const [error, setError] = useState<string | null>(null);
 useEffect(()=>{if(agentRequest){setSection('agents');localStorage.setItem('netsidian.settings.section','agents');onAgentOpened?.()}},[agentRequest]);
  const [pwCurrent,setPwCurrent]=useState(''),[pwNew,setPwNew]=useState(''),[pwConfirm,setPwConfirm]=useState(''),[pwBusy,setPwBusy]=useState(false),[pwCode,setPwCode]=useState('');
  const [connection,setConnection]=useState(''),[checking,setChecking]=useState(false),[integration,setIntegration]=useState<{storage:string;oauthConfigured:boolean;oauthLabel?:string;backendReachable:boolean}|null>(null);
  // Second factor (TOTP) state for the account security panel.
  const [totp,setTotp]=useState<api.TOTPStatus|null>(null);
  const [enroll,setEnroll]=useState<api.TOTPEnrollmentView|null>(null);
  const [codes,setCodes]=useState<string[]|null>(null);
  const [totpPassword,setTotpPassword]=useState(''),[totpCode,setTotpCode]=useState(''),[totpBusy,setTotpBusy]=useState(false),[totpMsg,setTotpMsg]=useState('');

  // Local deployments with NETSIDIAN_LOCAL_FOLDER_PICK: offer attaching a server-side folder as a vault.
  const navigate = useNavigate();
  const localPick=settings?.runtime.storage.folderPicker==='server';
  const [folderOpen, setFolderOpen] = useState(false);
  const attachFolder = async (path: string) => {
    setFolderOpen(false);
    setError(null);
    try {
      const v=await api.attachLocalVault(path);
      if(v.cancelled)return;
      await refresh();
      navigate(`/s/${v.space.id}`);
    } catch (e) {
      setError(errText(e));
    }
  };

  // Load second-factor status whenever the account panel is shown.
  useEffect(() => {
    if (me?.canChangePassword) { api.getTOTPStatus().then(setTotp).catch(() => setTotp(null)); }
    else { setTotp(null); setEnroll(null); }
  }, [me?.userId, me?.canChangePassword, section]);

  // Local-account password change. The server revokes every session and
  // personal token atomically, so on success the app must re-authenticate.
  const changePassword = async () => {
    setError(null);
    if (pwNew.length < 10) { setError('New password must be at least 10 characters.'); return; }
    if (pwNew !== pwConfirm) { setError('New passwords do not match.'); return; }
    setPwBusy(true);
    try {
      await api.changePassword(pwCurrent, pwNew, totp?.enabled ? pwCode : undefined);
      setPwCurrent(''); setPwNew(''); setPwConfirm(''); setPwCode('');
      await refresh();
    } catch (e) {
      setError(errText(e));
    } finally {
      setPwBusy(false);
    }
  };

  // Second-factor enrollment, confirmation, recovery-code regeneration and
  // disable. Enrollment is only enforced after a valid code is confirmed.
  const [codesSaved, setCodesSaved] = useState(false);
  const startEnroll = async () => {
    setTotpBusy(true); setTotpMsg(''); setCodes(null); setCodesSaved(false);
    try {
      const v = await api.enrollTOTP(totpPassword);
      setEnroll(v); setTotpPassword(''); setTotpCode('');
      setTotp(await api.getTOTPStatus());
    } catch (e) { setTotpMsg(errText(e)); } finally { setTotpBusy(false); }
  };
  const confirmEnroll = async () => {
    setTotpBusy(true); setTotpMsg('');
    try {
      setTotp(await api.confirmTOTP(totpCode));
      setEnroll(null); setTotpCode('');
      await refresh();
    } catch (e) { setTotpMsg(errText(e)); } finally { setTotpBusy(false); }
  };
  const regenCodes = async () => {
    setTotpBusy(true); setTotpMsg('');
    try {
      const r = await api.regenerateRecoveryCodes(totpPassword, totpCode);
      setCodes(r.recoveryCodes); setEnroll(null); setTotpPassword(''); setTotpCode('');
      setTotp(await api.getTOTPStatus());
    } catch (e) { setTotpMsg(errText(e)); } finally { setTotpBusy(false); }
  };
  const disableFactor = async () => {
    setTotpBusy(true); setTotpMsg('');
    try {
      await api.disableTOTP(totpPassword, totpCode);
      setEnroll(null); setCodes(null); setTotpPassword(''); setTotpCode('');
      await refresh();
    } catch (e) { setTotpMsg(errText(e)); } finally { setTotpBusy(false); }
  };

  return (
    <div className="pane">
      <div className="topbar" style={{ border: "none", padding: "0 0 12px" }}>
        <h2 style={{ margin: 0 }}>Settings</h2>
        <div className="spacer" />
        <span className="muted">Workspace preferences and services</span>
      </div>
      {error && <div className="banner error">{error}</div>}

      <div className="settings-tabs" role="tablist" aria-label="Settings sections" onKeyDown={e=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;e.preventDefault();const tabs=Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('[role=tab]'));const i=tabs.indexOf(document.activeElement as HTMLButtonElement);const n=e.key==='Home'?0:e.key==='End'?tabs.length-1:(i+(e.key==='ArrowRight'?1:-1)+tabs.length)%tabs.length;tabs[n].click();tabs[n].focus()}}>
 {(['account','ui',...(interaction?['interaction']:[]),'storage','agents',...(me?.isOperator?['system',...(settings?.authMode==='none'?[]:['users'])]:[])]).map(id=><button className="btn" key={id} id={'settings-tab-'+id} role="tab" aria-selected={section===id} aria-controls={'settings-panel-'+id} tabIndex={section===id?0:-1} onClick={()=>{setSection(id);localStorage.setItem('netsidian.settings.section',id)}}>{id==='ui'?'Appearance':id==='agents'?'MCP & agents':id[0].toUpperCase()+id.slice(1)}</button>)}
 </div>
 <section id="settings-panel-ui" role="tabpanel" aria-labelledby="settings-tab-ui" hidden={section!=='ui'}>{appearance}</section>
 <section id="settings-panel-interaction" role="tabpanel" aria-labelledby="settings-tab-interaction" hidden={section!=='interaction'}>{interaction}</section>
 <section id="settings-panel-account" role="tabpanel" aria-labelledby="settings-tab-account" hidden={section!=='account'}>
 <h3>Account</h3>
       <table className="data">
         <tbody>
           <tr><td>Display name</td><td>{me?.displayName}</td></tr>
           <tr><td>User id</td><td className="muted">{me?.userId}</td></tr>
           <tr><td>Auth mode</td><td>{settings?.authMode}</td></tr>
           <tr><td>Version</td><td>{settings?.version}</td></tr>
         </tbody>
       </table>
       {me?.canChangePassword && (
         <div style={{ marginTop: 12, maxWidth: 420 }}>
           <h4 style={{ margin: '0 0 4px' }}>Change password</h4>
           <p className="muted" style={{ margin: '0 0 8px' }}>Changing your password signs out every device and revokes all personal access tokens. You will need to sign in again.</p>
           <label>Current password <input className="input" type="password" autoComplete="current-password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} /></label>
           <label>New password <input className="input" type="password" autoComplete="new-password" minLength={10} maxLength={1024} value={pwNew} onChange={(e) => setPwNew(e.target.value)} /></label>
            <label>Confirm new password <input className="input" type="password" autoComplete="new-password" minLength={10} maxLength={1024} value={pwConfirm} onChange={(e) => setPwConfirm(e.target.value)} /></label>
            {totp?.enabled && <label>Authenticator or recovery code <input className="input" maxLength={128} autoComplete="one-time-code" value={pwCode} onChange={(e) => setPwCode(e.target.value)} /></label>}
             <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
               <button className="btn primary" disabled={pwBusy || !pwCurrent || pwNew.length < 10 || pwNew !== pwConfirm || (totp?.enabled && !pwCode)} onClick={() => void changePassword()}>{pwBusy ? 'Changing…' : 'Change password'}</button>
             </div>
          </div>
        )}
        {me?.canChangePassword && (
          <div style={{ marginTop: 16, maxWidth: 460 }}>
            <h4 style={{ margin: '0 0 4px' }}>Second factor</h4>
            <p className="muted" style={{ margin: '0 0 8px' }}>
              {totp?.enabled
                ? `Two-step verification is on. ${totp.recoveryCodesRemaining} recovery code(s) remaining.`
                : totp?.pending
                  ? "Setup has started but is not confirmed — two-step verification is not yet enforced."
                  : "Protect your account with a time-based code from an authenticator app."}
            </p>
            {totpMsg && <div className="banner error" role="alert">{totpMsg}</div>}
            {(enroll || codes) && (
              <div className="banner">
                {enroll?.secret && (
                  <>
                    <p style={{ margin: '0 0 6px' }}>Add this secret manually in your authenticator app, or open the setup link:</p>
                    <code style={{ wordBreak: 'break-all' }}>{enroll.secret}</code>
                    <p style={{ margin: '6px 0' }}><a href={enroll.otpauthUrl}>{enroll.otpauthUrl}</a></p>
                  </>
                )}
                <p style={{ margin: '0 0 4px' }}><strong>Recovery codes — shown once, store them safely:</strong></p>
                <pre className="agent-example"><code>{(enroll?.recoveryCodes ?? codes ?? []).join("\n")}</code></pre>
              </div>
            )}
            {!totp?.enabled && (
              <div>
                <label>Your password <input className="input" type="password" autoComplete="current-password" value={totpPassword} onChange={(e) => setTotpPassword(e.target.value)} /></label>
                <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                  <button className="btn primary" disabled={totpBusy || !totpPassword} onClick={() => void startEnroll()}>{totpBusy ? 'Working…' : (totp?.pending ? "Restart setup" : "Start setup")}</button>
                </div>
              </div>
            )}
            {enroll && (
              <div style={{ marginTop: 8 }}>
                <label><input type="checkbox" checked={codesSaved} onChange={e=>setCodesSaved(e.target.checked)}/> I saved my recovery codes</label>
                <p>Save your recovery codes before confirming. Enabling verification signs out all devices and revokes personal tokens.</p>
                <label>Enter the 6-digit code to confirm <input className="input" inputMode="numeric" autoComplete="one-time-code" value={totpCode} onChange={(e) => setTotpCode(e.target.value)} /></label>
                <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                  <button className="btn primary" disabled={totpBusy || !codesSaved || totpCode.length !== 6} onClick={() => void confirmEnroll()}>{totpBusy ? 'Confirming…' : 'Confirm and enable'}</button>
                </div>
              </div>
            )}
            {totp?.enabled && (
              <div style={{ marginTop: 8 }}>
                <label>Your password <input className="input" type="password" autoComplete="current-password" value={totpPassword} onChange={(e) => setTotpPassword(e.target.value)} /></label>
                <label>Authenticator or recovery code <input className="input" maxLength={128} autoComplete="one-time-code" value={totpCode} onChange={(e) => setTotpCode(e.target.value)} /></label>
                <div style={{ display: 'flex', gap: 8, marginTop: 8, flexWrap: 'wrap' }}>
                  <button className="btn" disabled={totpBusy || !totpPassword || totpCode.length < 6} onClick={() => void regenCodes()}>Regenerate recovery codes</button>
                  <button className="btn danger" disabled={totpBusy || !totpPassword || totpCode.length < 6} onClick={() => void disableFactor()}>{totpBusy ? '…' : 'Disable two-step'}</button>
                </div>
              </div>
            )}
          </div>
        )}
        <p style={{ marginTop: 12 }}>
          <span className="muted">Use the account menu in the upper-right corner to sign out safely.</span>
        </p>

</section>
      <section id="settings-panel-agents" role="tabpanel" aria-labelledby="settings-tab-agents" hidden={section!=="agents"} className="agent-settings" aria-label="Agent access">
        <header className="agent-settings-heading"><h2>MCP & agents</h2><p className="muted">Connect an agent and choose exactly what it can access.</p></header>
        <div className="agent-connect-card"><MCPSetup compact inSettings/></div>
        <AgentTokens initialVault={initialVault} request={agentRequest}/>
        <details className="agent-connect-card"><summary>Agent memory & project knowledge</summary><AgentMemoryFolder initialVault={initialVault}/></details>
      </section>
      {me?.isOperator&&<section id="settings-panel-system" role="tabpanel" aria-labelledby="settings-tab-system" hidden={section!=="system"} aria-label="Integration status"><h3>Integration status</h3><p>Authentication: <strong>{settings?.authMode}</strong> · Local folder access: <strong>{settings?.runtime.storage.folderPicker!=='none'?'Available':'Disabled'}</strong></p><p className="muted">A successful instance check verifies your session and API access. S3 and OAuth credentials are managed in the server configuration.</p><button className="btn" disabled={checking} onClick={async()=>{setChecking(true);setConnection('');try{await api.getMe();if(me?.isOperator){const response=await fetch('/api/v1/me/integrations');if(!response.ok)throw new Error('Integration check unavailable');const status=await response.json();setIntegration(status);setConnection(status.backendReachable?'Session, database and document storage are reachable.':'Session verified; database or document storage is unavailable.')}else setConnection('Session and API connection verified.')}catch(e){setConnection(errText(e))}finally{setChecking(false)}}}>{checking?'Checking…':'Check instance connection'}</button><p role="status">{connection}</p>{integration&&<p>Document storage: <strong>{integration.storage==='s3'?'S3 compatible':'Local'}</strong> · OAuth: <strong>{integration.oauthConfigured?(integration.oauthLabel||'Configured'):'Not configured'}</strong></p>}<details><summary>Server setup</summary><p>Storage: set NETSIDIAN_STORAGE_DRIVER=s3 and configure the S3 bucket, region and credentials. OAuth: configure NETSIDIAN_AUTH_MODE, NETSIDIAN_OIDC_ISSUER, client ID, client secret and public URL. Restart the server after changing configuration.</p><p>The backend check is read only. OAuth configuration status does not verify a complete sign-in flow.</p><a href="/api/v1/openapi.json" target="_blank" rel="noreferrer">Instance API and capabilities</a></details></section>}
      {me?.isOperator&&settings?.authMode!=='none'&&<section id="settings-panel-users" role="tabpanel" aria-labelledby="settings-tab-users" hidden={section!=="users"}><AdminPanel userId={me.userId} defaultOpen/></section>}
<section id="settings-panel-storage" role="tabpanel" aria-labelledby="settings-tab-storage" hidden={section!=="storage"}><StoragePanel onFolderBrowse={localPick?()=>setFolderOpen(true):undefined}/>
 </section>
      {folderOpen && <FolderPicker onPick={(p) => void attachFolder(p)} onClose={() => setFolderOpen(false)} />}
    </div>
  );
}
