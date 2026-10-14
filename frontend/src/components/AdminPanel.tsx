import {useEffect,useState} from 'react';
import * as api from '../api/client';

export function AdminPanel({userId,defaultOpen=false}:{userId:string;defaultOpen?:boolean}){
 const [open,setOpen]=useState(defaultOpen),[query,setQuery]=useState(''),[offset,setOffset]=useState(0),[auditOffset,setAuditOffset]=useState(0),[revision,setRevision]=useState(0);
 const [users,setUsers]=useState<api.AdminUser[]>([]),[total,setTotal]=useState(0),[policy,setPolicy]=useState<api.AdminAuthSettings|null>(null),[events,setEvents]=useState<api.AdminAuditEvent[]>([]);
 const [error,setError]=useState(''),[busy,setBusy]=useState(''),[loading,setLoading]=useState(false);
 const [resetTarget,setResetTarget]=useState<api.AdminUser|null>(null),[resetPassword,setResetPassword]=useState(''),[resetCode,setResetCode]=useState('');
 const [pending,setPending]=useState<{user:api.AdminUser;change:{disabled?:boolean;isOperator?:boolean};label:string}|null>(null);
 useEffect(()=>{
  if(!open)return;let alive=true;setLoading(true);setError('');
  const timer=setTimeout(()=>{Promise.all([api.adminUsers(query,offset),api.adminSettings(),api.adminAudit(auditOffset)]).then(([accounts,settings,audit])=>{if(alive){setUsers(accounts.users);setTotal(accounts.total);setPolicy(settings);setEvents(audit.events)}}).catch(e=>{if(alive)setError(e.message||String(e))}).finally(()=>{if(alive)setLoading(false)})},150);
  return()=>{alive=false;clearTimeout(timer)};
 },[open,query,offset,auditOffset,revision]);
  const update=async(user:api.AdminUser,change:{disabled?:boolean;isOperator?:boolean;revokeSessions?:boolean})=>{
   setBusy(user.id);setError('');
   try{await api.adminUpdateUser(user.id,change);setPending(null);setRevision(v=>v+1)}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy('')}
  };
  // Operator recovery: remove a member's second factor so they can sign in
  // with their password and re-enroll. The action is audited server-side.
  const resetFactor=async(user:api.AdminUser)=>{
   setBusy(user.id);setError('');
   try{await api.adminResetTOTP(user.id,resetPassword,resetCode);setResetTarget(null);setResetPassword('');setResetCode('');setRevision(v=>v+1)}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy('')}
  };
 return <section className="admin-panel" aria-label="Administration">
  <h3><button type="button" className="btn" aria-expanded={open} onClick={()=>setOpen(v=>!v)}>Administration {open?'▾':'▸'}</button></h3>
  {open&&<>
   <p>Manage instance accounts. Administrator access does not grant access to other users’ vault contents.</p>
   {error&&<p className="banner error" role="alert">{error}</p>}
   {policy&&<div className="admin-policy"><h4>Access policy</h4><p>Local registration: <strong>{policy.signupEnabled?'Open':'Closed'}</strong>. Set <code>NETSIDIAN_SIGNUP_ENABLED</code> in the deployment configuration and restart to change it.</p>{policy.oidcIssuer&&<><p>Identity provider: <code>{policy.oidcIssuer}</code>. Roles are refreshed at sign-in; external sessions last at most {policy.externalSessionMinutes} minutes.</p><p>Admission: {policy.oidcAllowAllUsers?'All identities from this provider':JSON.stringify(policy.oidcAllowedClaims)}.</p><p>Administrator claims: <code>{JSON.stringify(policy.oidcAdminClaims||{})}</code>.</p></>}</div>}
   <h4>Accounts</h4>
   <input className="input" aria-label="Search accounts" placeholder="Search name or email…" value={query} onChange={e=>{setQuery(e.target.value);setOffset(0)}}/>
   {pending&&<div className="admin-confirm" role="alert"><p>{pending.label} for <strong>{pending.user.email||pending.user.displayName}</strong>?</p>{pending.change.disabled&&<p>This signs the account out and revokes its API keys. Vault data is retained.</p>}<button className="btn" disabled={!!busy} onClick={()=>void update(pending.user,pending.change)}>Confirm {pending.label.toLowerCase()}</button><button className="btn" disabled={!!busy} onClick={()=>setPending(null)}>Cancel</button></div>}
   {resetTarget&&<form className="admin-confirm" onSubmit={e=>{e.preventDefault();void resetFactor(resetTarget)}}><p>Reset two-step verification for <strong>{resetTarget.email||resetTarget.displayName}</strong>? This signs out that account and revokes its personal tokens.</p><label>Your current password <input className="input" type="password" autoComplete="current-password" required value={resetPassword} onChange={e=>setResetPassword(e.target.value)}/></label><label>Your authenticator or recovery code (if enabled) <input className="input" autoComplete="one-time-code" maxLength={128} value={resetCode} onChange={e=>setResetCode(e.target.value)}/></label><button className="btn danger" disabled={!!busy||!resetPassword}>Reset verification</button><button type="button" className="btn" disabled={!!busy} onClick={()=>{setResetTarget(null);setResetPassword('');setResetCode('')}}>Cancel</button></form>}
   <div className="admin-table-scroll"><table className="data"><thead><tr><th>Account</th><th>Role</th><th>Access</th><th>Actions</th></tr></thead><tbody>{users.map(user=><tr key={user.id}><td><strong>{user.displayName}</strong><small>{user.email||user.id}</small>{user.id===userId&&<small>You</small>}</td><td>{user.isOperator?'Administrator':'Member'}{user.roleManaged&&<small>Managed by SSO</small>}</td><td>{user.disabled?'Disabled':'Enabled'}</td><td><div className="admin-actions"><button className="btn small" disabled={!!busy||user.id===userId} onClick={()=>setPending({user,change:{disabled:!user.disabled},label:user.disabled?'Enable account':'Disable account'})}>{user.disabled?'Enable':'Disable'}</button>{!user.roleManaged&&<button className="btn small" disabled={!!busy||user.id===userId} onClick={()=>setPending({user,change:{isOperator:!user.isOperator},label:user.isOperator?'Remove administrator role':'Grant administrator role'})}>{user.isOperator?'Make member':'Make administrator'}</button>}<button className="btn small" disabled={!!busy||user.disabled||user.id===userId} onClick={()=>void update(user,{revokeSessions:true})}>Sign out</button><button className="btn small" disabled={!!busy||user.id===userId} onClick={()=>{setResetTarget(user);setResetPassword('');setResetCode('')}}>Reset 2FA</button></div></td></tr>)}</tbody></table></div>
   <div className="admin-pagination"><button className="btn small" disabled={offset===0||loading} onClick={()=>setOffset(v=>Math.max(0,v-50))}>Previous accounts</button><span>{loading?'Loading…':`${total?offset+1:0}–${Math.min(offset+50,total)} of ${total}`}</span><button className="btn small" disabled={offset+50>=total||loading} onClick={()=>setOffset(v=>v+50)}>Next accounts</button></div>
   <h4>Account audit</h4><div className="admin-table-scroll"><table className="data"><thead><tr><th>When</th><th>Action</th><th>Actor / account</th></tr></thead><tbody>{events.map(event=><tr key={event.id}><td>{new Date(event.createdAt).toLocaleString()}</td><td>{event.action}<small>{event.details}</small></td><td><code>{event.actor}</code><small>{event.resource}</small></td></tr>)}</tbody></table></div><div className="admin-pagination"><button className="btn small" disabled={auditOffset===0||loading} onClick={()=>setAuditOffset(v=>Math.max(0,v-50))}>Newer events</button><button className="btn small" disabled={events.length<50||loading} onClick={()=>setAuditOffset(v=>v+50)}>Older events</button></div>
  </>}
 </section>;
}
