import { useEffect, useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import * as api from "../api/client";
import { useApp } from "../AppState";
import {AppLogo} from "../components/AppLogo";
import {MCPInstructionsDisclosure} from '../components/MCPInstructionsDisclosure';

export default function AuthPage() {
  const { settings, refresh, me } = useApp();
  const navigate = useNavigate();
  const [providers,setProviders]=useState<{name:string;url:string}[]>([]);
  useEffect(()=>{let active=true;fetch('/api/v1/auth/oauth/providers').then(r=>r.json()).then(v=>{if(active)setProviders(v.providers??[])}).catch(()=>{});return()=>{active=false}},[]);
  const externalOnly=!settings?.runtime.auth.passwordLogin;
  const nextPath=()=>{const next=new URLSearchParams(location.search).get('next');return next?.startsWith('/s/')||next==='/settings/mcp'?next:'/'};
  useEffect(()=>{if(me)navigate(nextPath(),{replace:true})},[me,settings?.authMode,navigate]);
  const needsSetup = !!settings?.needsSetup;
  const signupOpen = !externalOnly && !needsSetup && !!settings?.signupEnabled;

  const [mode, setMode] = useState<"login" | "signup">("login");
  const registering = needsSetup || mode === "signup";

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState<string | null>(()=>{
 const code=new URLSearchParams(location.search).get('oauth_error');
 return code==='access_denied'?'Your identity does not meet this instance’s access policy. Contact the administrator.':code==='account_conflict'?'An account conflict needs administrator attention. In mixed mode, complete local setup first.':code==='signin_failed'?'Sign-in could not finish. Start again; your previous login may have expired.':null;
 });
  const [busy, setBusy] = useState(false);
  // A confirmed second factor turns login into a two-step flow: the password
  // yields a short-lived challenge token, and only the factor completes it.
  const [challenge, setChallenge] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setBusy(true);
    const destination=nextPath();
    try {
      if (needsSetup) await api.setup(email, password, displayName);
      else if (mode === "signup") await api.signup(email, password, displayName);
      else {
        const res = await api.login(email, password);
        if (res.challenge && res.challengeToken) { setChallenge(res.challengeToken); setPassword(""); setCode(""); return; }
      }
      await refresh();
      navigate(destination, { replace: true });
    } catch (err) {
      if (err instanceof api.ApiError) setError(`${err.code}: ${err.message}`);
      else setError(String(err));
    } finally {
      setBusy(false);
    }
  };

  const submitChallenge = async (e: FormEvent) => {
    e.preventDefault();
    if (!challenge) return;
    setError(null);
    setBusy(true);
    const destination=nextPath();
    try {
      if (recovery) await api.completeChallengeRecovery(challenge, code);
      else await api.completeChallengeTOTP(challenge, code);
      await refresh();
      navigate(destination, { replace: true });
    } catch (err) {
      if (err instanceof api.ApiError) setError(`${err.code}: ${err.message}`);
      else setError(String(err));
    } finally {
      setBusy(false);
    }
  };

  if(settings?.authMode==='none')return <div className="auth-center"><div className="auth-card"><h1>Open local workspace</h1><p>The local workspace could not be loaded. Retry when the service is available.</p>{error&&<p role="alert">{error}</p>}<button className="btn primary" disabled={busy} onClick={async()=>{setBusy(true);setError(null);try{await api.getMe();await refresh()}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}}}>{busy?'Opening…':'Retry opening workspace'}</button></div></div>;

  if (challenge) return (
    <div className="auth-center">
      <form className="auth-card" onSubmit={(e) => void submitChallenge(e)}>
        <AppLogo size={56} className="auth-app-icon"/>
        <h1>Second factor</h1>
        <p className="sub">{recovery ? "Enter one of your recovery codes." : "Enter the code from your authenticator app."}</p>
        {error && <div className="banner error" role="alert">{error}</div>}
        <label htmlFor="auth-code">{recovery ? "Recovery code" : "Authentication code"}</label>
        <input
          id="auth-code"
          className="input"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          required
          autoComplete="one-time-code"
          inputMode={recovery ? "text" : "numeric"}
        />
        <button className="btn primary" type="submit" disabled={busy}>{busy ? "Verifying…" : "Verify"}</button>
        <button className="btn" type="button" disabled={busy} onClick={() => { setRecovery(!recovery); setError(null); }}>
          {recovery ? "Use an authenticator code" : "Lost your device? Use a recovery code"}
        </button>
        <button className="btn" type="button" disabled={busy} onClick={() => { setChallenge(null); setCode(""); setError(null); }}>
          Back to sign in
        </button>
      </form>
      <MCPInstructionsDisclosure/>
    </div>
  );

  return (
    <div className="auth-center">
      <form className="auth-card" onSubmit={(e) => void submit(e)}>
        <AppLogo size={56} className="auth-app-icon"/>
        <h1>{needsSetup ? "Set up Netsidian" : "Netsidian"}</h1>
        <p className="sub">
          {needsSetup
            ? "Create the first account (operator)."
            : mode === "signup"
              ? "Create your account."
              : "Sign in to your workspace."}
        </p>
        {error && <div className="banner error" role="alert">{error}</div>}
        {!externalOnly&&<>
        <label htmlFor="auth-email">Email</label>
        <input
          id="auth-email"
          className="input"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          autoComplete="email"
        />
        {registering && (
          <>
            <label htmlFor="auth-name">Display name</label>
            <input
              id="auth-name"
              className="input"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              required
              autoComplete="name"
            />
          </>
        )}
        <label htmlFor="auth-password">Password</label>
        <input
          id="auth-password"
          className="input"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          minLength={registering ? 10 : 1}
          autoComplete={registering ? "new-password" : "current-password"}
        />
        <button className="btn primary" type="submit" disabled={busy}>
          {busy ? "Working…" : needsSetup ? "Create account" : mode === "signup" ? "Sign up" : "Sign in"}
        </button>
        </>}
        {!needsSetup&&providers.map(p=><a className="btn primary" key={p.url} href={p.url+'?next='+encodeURIComponent(new URLSearchParams(location.search).get('next')??'/')}>Continue with {p.name}</a>)}
        {needsSetup&&providers.length>0&&<p className="muted">Complete administrator setup to enable single sign-on.</p>}
        {signupOpen && (
          <button
            className="btn"
            type="button"
            disabled={busy}
            onClick={() => setMode(mode === "login" ? "signup" : "login")}
          >
            {mode === "login" ? "Create an account" : "Sign in instead"}
          </button>
        )}
      </form>
      <MCPInstructionsDisclosure/>
    </div>
  );
}
