// End-to-end setup uses only the isolated review operator. Never return a PAT.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'},assert=(ok,message)=>{if(!ok)throw new Error(message)};
 await page.context().clearCookies();await page.goto(origin+'/settings/mcp');
 await page.getByRole('heading',{name:'Connect your agent',exact:true}).waitFor();
 assert((await page.locator('.mcp-setup').innerText()).includes('Paste to agent: add '+origin+'/mcp'),'Login installation message missing');
 const rpc=async(method,params={},token)=>{
  const response=await page.request.post(origin+'/mcp',{headers:{'Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25',...(token?{'Authorization':'Bearer '+token}:{})},data:{jsonrpc:'2.0',id:1,method,params}});
  if(!response.ok())return {httpStatus:response.status()};const envelope=await response.json();assert(!envelope.error,'MCP protocol request failed');return envelope.result;
 };
 await rpc('initialize',{protocolVersion:'2025-11-25',capabilities:{},clientInfo:{name:'Playwright-onboarding',version:'1'}});
 const anonymous=await rpc('tools/list');assert(anonymous.tools.length===1&&anonymous.tools[0].name==='netsidian_connect','Anonymous setup exposes data tools');
 const setup=await rpc('tools/call',{name:'netsidian_connect',arguments:{}}),instructions=JSON.parse(setup.content[0].text);assert(instructions.settingsUrl===origin+'/settings/mcp'&&instructions.memory.folder==='agent-memory'&&instructions.memory.tag==='agent-memory','Anonymous setup links or memory defaults are incorrect');
 await page.getByRole('textbox',{name:'Email',exact:true}).fill('playwright@example.test');await page.getByRole('textbox',{name:'Password',exact:true}).fill('PlaywrightTest-2026-Strong!');await page.getByRole('button',{name:/sign in/i}).click();
 const panel=page.locator('#settings-panel-agents');await panel.waitFor();assert(page.url().endsWith('/settings/mcp'),'Login did not return to MCP settings');
 assert(await page.getByRole('tab',{name:'MCP & agents',exact:true}).getAttribute('aria-selected')==='true','MCP settings tab not selected');
 await page.context().grantPermissions(['clipboard-read','clipboard-write']);await panel.getByRole('button',{name:'Copy agent instructions',exact:true}).click();
 const clipboard=await page.evaluate(()=>navigator.clipboard.readText());assert(clipboard.includes('Paste to agent: add '+origin+'/mcp')&&clipboard.includes(origin+'/settings/mcp')&&clipboard.includes('agent-memory'),'Copied onboarding instructions incomplete');
 await panel.getByRole('button',{name:'Copy MCP link',exact:true}).click();assert(await page.evaluate(()=>navigator.clipboard.readText())===origin+'/mcp','Copied endpoint incorrect');
 const cookieOnly=await rpc('tools/list');assert(cookieOnly.tools.length===1,'Browser session authorized agent data tools');
 const spaces=(await (await page.request.get(origin+'/api/v1/spaces')).json()).spaces,review=spaces.find(item=>item.space.name==='MCP integration review');assert(review,'Dedicated review vault missing');
 for(const selected of await panel.locator('.token-selected button').all())await selected.click();
 await panel.getByRole('textbox',{name:'Find token vaults',exact:true}).fill(review.space.name);await panel.getByLabel(review.space.name,{exact:true}).check();
 await panel.getByRole('combobox',{name:/^Permissions/}).selectOption('read');await panel.getByRole('combobox',{name:/^Expires after/}).selectOption('7');
 const label='Playwright MCP onboarding '+Date.now();await panel.getByRole('textbox',{name:'New token label',exact:true}).fill(label);
 const issuance=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/v1/me/keys'&&response.request().method()==='POST');
 await panel.getByRole('button',{name:'Create token',exact:true}).click();const created=await (await issuance).json();await panel.getByRole('button',{name:'Hide',exact:true}).click();
 const key=created.key||created.record||created;const token=created.secret;assert(key&&typeof token==='string','Token issuance failed');
 try{
  const privateTools=await rpc('tools/list',{},token);assert(privateTools.tools.some(tool=>tool.name==='netsidian_read')&&privateTools.tools.some(tool=>tool.name==='netsidian_write'),'Authenticated tools missing');
  assert(await panel.getByRole('button',{name:'Copy token',exact:true}).count()===0,'Token remains visible after hiding');
  const memory=panel.getByRole('region',{name:'Agent memory folder',exact:true});await memory.getByRole('combobox',{name:/^Skill vault/}).selectOption(review.space.id);assert(await memory.getByRole('textbox',{name:'Agent memory folder path',exact:true}).inputValue()==='agent-memory','Default memory folder incorrect');
  await memory.getByRole('button',{name:'Prepare memory folder',exact:true}).click();await memory.getByRole('status').or(memory.getByRole('alert')).waitFor();if(await memory.getByRole('alert').count())throw new Error('Memory preparation: '+await memory.getByRole('alert').innerText());
  const lookup=await page.request.get(origin+'/api/v1/spaces/'+review.space.id+'/documents/lookup?path=agent-memory%2FREADME.md');assert(lookup.ok(),'Memory folder was not prepared');const metadata=await lookup.json();assert(metadata.path==='agent-memory/README.md'&&!('markdown' in metadata),'Lookup fetches document body');
  const read=await (await page.request.get(origin+'/api/v1/spaces/'+review.space.id+'/documents/'+metadata.id)).json();assert(read.markdown.includes('tags: [agent-memory]'),'Memory README missing tag');
  await page.screenshot({path:'/tmp/netsidian-mcp/mcp-onboarding.png'});
 }finally{await page.request.delete(origin+'/api/v1/me/keys/'+key.id,{headers})}
 assert((await rpc('ping',{},token)).httpStatus===401,'Revoked token still accepted');
 return {login:'copyable endpoint and public setup; return link preserved',settings:'MCP & agents selected; instructions copied',authentication:'scoped seven-day PAT; hidden and revoked; cookies do not confer access',memory:'agent-memory default/tag; indexed lookup; no body fetched'};
}
