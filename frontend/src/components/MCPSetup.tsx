import {useState} from 'react';
import {DesktopMCPSetup} from './DesktopMCPSetup';
import {useApp} from '../AppState';
export function MCPSetup({compact=false}:{compact?:boolean}){
 const {settings:instance}=useApp();
 const [copied,setCopied]=useState(''),[error,setError]=useState('');
 const url=location.origin+'/mcp',settings=location.origin+'/settings/mcp';
 const prompt=`Paste to agent: add ${url}\nNetsidian is an MCP server for Markdown documents and durable memory. If authentication is needed, show me ${settings} to create a scoped personal access token. Store the token in NETSIDIAN_TOKEN or your secret store and send Authorization: Bearer. Never put it in a URL or document. Default memory folder: agent-memory; tag memory notes agent-memory.`;
 const copy=async(value:string,label:string)=>{try{await navigator.clipboard.writeText(value);setCopied(label);setError('')}catch{setError('Copy failed. Select and copy the text below.')}};
 if(instance?.runtime.kind==='desktop')return <DesktopMCPSetup/>;
 return <section className={'mcp-setup'+(compact?' compact':'')} aria-label="Connect an agent with MCP">
  <h3>Connect your agent</h3><p>Paste to agent: add <code>{url}</code></p>
  <div className="mcp-setup-actions"><button type="button" className="btn small" onClick={()=>void copy(prompt,'instructions')}>{copied==='instructions'?'Copied':'Copy agent instructions'}</button><button type="button" className="btn small" onClick={()=>void copy(url,'link')}>{copied==='link'?'Copied':'Copy MCP link'}</button><a className="btn small" href="/settings/mcp">MCP and token settings</a></div>
  {!compact&&<><p>Sign in here, choose the vaults and permissions your agent needs, and create a personal access token below. Supply it to the agent separately; keep it in an environment variable or secret store.</p><p>Netsidian can hold durable memory in <code>agent-memory/</code>, tagged <code>agent-memory</code>.</p><details><summary>Client configuration</summary><pre className="agent-example"><code>{`[mcp_servers.netsidian]\nurl = "${url}"\nbearer_token_env_var = "NETSIDIAN_TOKEN"\n\n# Alternative command:\n# codex mcp add netsidian --url ${url} --bearer-token-env-var NETSIDIAN_TOKEN`}</code></pre></details></>}
  {error&&<p role="alert">{error}</p>}
 </section>
}
