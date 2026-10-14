// Run with Playwright MCP on an authenticated disposable test vault.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'};
 const spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const samples={javascript:'const name = "test";',bash:'echo "$HOME" # comment',yaml:'enabled: true',jaml:'enabled: true',c:'int main(void) { return 0; }',cpp:'class Example { public: int value = 1; };',java:'public class Example {}',csharp:'public class Example { string Name = "test"; }',toml:'[server]\nport = 3000',config:'[server]\nport=3000'};
 const content=Object.entries(samples).map(([language,code])=>'```'+language+'\n'+code+'\n```').join('\n\n');
 const created=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'Code-test-'+Date.now()+'.md',content}});
 if(!created.ok())throw new Error(await created.text());const note=await created.json();
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 try {
  await page.goto(origin+'/s/'+space+'/d/'+note.id);
  await page.getByRole('button',{name:'Reading mode',exact:true}).click();
  for(const [language,code] of Object.entries(samples)){
   const block=page.locator('.document-views .markdown-preview pre>code.language-'+language);
   await block.waitFor();assert(await block.textContent()===code+'\n','Code text changed for '+language);assert(await block.locator('[class*="hljs-"]').count()>0,'No colors for '+language);
  }
  const token=page.locator('.document-views .markdown-preview .language-javascript .hljs-keyword').first();
  const color=await token.evaluate(el=>getComputedStyle(el).color),stringColor=await page.locator('.document-views .markdown-preview .language-javascript .hljs-string').first().evaluate(el=>getComputedStyle(el).color);
  assert(color!==stringColor,'Token classes have the same color');
  const original=await page.evaluate(()=>document.documentElement.dataset.theme);
  await page.evaluate(()=>document.documentElement.dataset.theme='light');
  assert(await token.evaluate(el=>getComputedStyle(el).color)!==color,'Syntax colors do not follow theme');
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,original);
  await page.getByRole('button',{name:'Visual Markdown editor',exact:true}).click();
  await page.locator('.ProseMirror .hljs-keyword').first().waitFor();
  assert(await page.locator('.ProseMirror .hljs-string').count()>0,'Visual editor strings are not colored');
  await page.getByRole('button',{name:'Source mode',exact:true}).click();
  await page.locator('.cm-content .hljs-keyword').first().waitFor();
  assert(await page.locator('.cm-content .hljs-string').count()>0,'Source editor strings are not colored');
  await page.getByRole('button',{name:'Reading mode',exact:true}).click();
  await page.screenshot({path:'/tmp/netsidian-mcp/code-highlighting.png'});
  return {languages:Object.keys(samples),reading:'colored; exact code preserved',visual:'colored',source:'colored',theme:'dark/light token colors differ'};
 } finally {
  const current=await (await page.request.get(origin+'/api/v1/spaces/'+space+'/documents/'+note.id)).json();
  await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...headers,'If-Match':current.revision}});
 }
}
