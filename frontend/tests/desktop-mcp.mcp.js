// Uses the opt-in Go desktop fixture, not a user's running desktop session.
async(page)=>{
 await page.goto('http://127.0.0.1:3002/settings/mcp');
 const panel=page.getByRole('region',{name:'Desktop MCP server',exact:true}),assert=(ok,message)=>{if(!ok)throw new Error(message)};await panel.getByRole('heading',{name:'Local MCP server',exact:true}).waitFor();
 assert(!await panel.getByRole('checkbox',{name:'Enable local MCP server',exact:true}).isChecked(),'MCP enabled by default');assert(await panel.getByRole('checkbox',{name:'Read-only access',exact:true}).isChecked(),'Access not read-only by default');
 const spaces=(await(await page.request.get('http://127.0.0.1:3002/api/v1/spaces')).json()).spaces,selected=spaces[0].space;
 await panel.getByRole('checkbox',{name:selected.name,exact:true}).check();await panel.getByRole('spinbutton',{name:'Local MCP port',exact:true}).fill('0');await panel.getByRole('checkbox',{name:'Allow local agents without a token',exact:true}).check();await panel.getByRole('checkbox',{name:'Enable local MCP server',exact:true}).check();await panel.getByRole('button',{name:'Apply MCP settings',exact:true}).click();
 await panel.getByRole('status').filter({hasText:'Running'}).waitFor();const endpoint=await panel.locator('code').first().innerText();assert(endpoint.startsWith('http://127.0.0.1:'),'Not bound to IPv4 loopback');
 const rpc=async(method,params={})=>{const response=await page.request.post(endpoint,{headers:{Accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25'},data:{jsonrpc:'2.0',id:1,method,params}});assert(response.ok(),'Local MCP request failed');const envelope=await response.json();assert(!envelope.error,'MCP protocol failed');return envelope.result};
 await rpc('initialize',{protocolVersion:'2025-11-25',capabilities:{},clientInfo:{name:'Playwright-desktop',version:'1'}});
 const call=async(name,args)=>{const result=await rpc('tools/call',{name,arguments:args});return{result,data:JSON.parse(result.content[0].text)}};
 const setup=await call('netsidian_connect',{});assert(setup.data.connected===true&&setup.data.authentication.includes('no token'),'Token-free setup not reported');
 const listed=await call('netsidian_read',{operation:'GET /api/v1/spaces'});assert(listed.data.body.spaces.length===1&&listed.data.body.spaces[0].space.id===selected.id,'Unselected vaults exposed');
 const create={operation:'POST /api/v1/spaces/{space}/documents',pathParameters:{space:selected.id},body:{path:'agent-memory/Desktop-test.md',content:'---\ntags: [agent-memory]\n---\n# Local agent test'},idempotencyKey:'desktop-browser-test'};
 assert((await call('netsidian_write',create)).result.isError,'Read-only local listener permitted creation');
 await panel.getByRole('checkbox',{name:'Read-only access',exact:true}).uncheck();await panel.getByRole('button',{name:'Apply MCP settings',exact:true}).click();
 await page.waitForFunction(async()=>!(await(await fetch('/api/v1/native/mcp')).json()).config.readOnly);
 const created=await call('netsidian_write',create);assert(!created.result.isError&&created.data.status===201,'Write-enabled token-free access failed');
 const administration=await page.request.get(endpoint.replace('/mcp','/api/v1/native/mcp'));assert(administration.status()===404,'Listener exposed management routes');
 await page.context().grantPermissions(['clipboard-read','clipboard-write']);await panel.getByRole('button',{name:'Copy agent instructions',exact:true}).click();const clipboard=await page.evaluate(()=>navigator.clipboard.readText());assert(clipboard.includes(endpoint)&&clipboard.includes('No token is required'),'Copied local setup instructions incorrect');
 await page.screenshot({path:'/tmp/netsidian-mcp/desktop-mcp.png'});
 await panel.getByRole('checkbox',{name:'Enable local MCP server',exact:true}).uncheck();await panel.getByRole('button',{name:'Apply MCP settings',exact:true}).click();await panel.getByRole('status').filter({hasText:'Stopped'}).waitFor();
 return {defaults:'disabled/read-only',scope:'selected vault only',authentication:'no token on localhost',writes:'blocked until enabled',management:'not exposed',copy:'correct instructions',lifecycle:'started/stopped'};
}
