// Playwright MCP scenario: new UI baseline, unchanged Markdown scale.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'};
 const spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const response=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'UI-scale-'+Date.now()+'.md',content:'# Scale check\n\nPlaintext\n\n```javascript\nconst name = "test";\n```'}});
 if(!response.ok())throw new Error(await response.text());const note=await response.json();
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 const physicalScale=async locator=>locator.evaluate(node=>{let scale=1;for(let el=node;el;el=el.parentElement)scale*=Number(getComputedStyle(el).zoom)||1;return scale});
 try {
  await page.goto(origin+'/s/'+space+'/settings');await page.getByRole('tab',{name:'Appearance',exact:true}).click();
  const select=page.getByRole('combobox',{name:'UI scale',exact:true});await select.selectOption('100');
  await page.waitForFunction(()=>Math.abs(Number(getComputedStyle(document.documentElement).zoom)-1.2)<.001);
  assert(await select.inputValue()==='100','New baseline is not labelled 100%');
  await page.goto(origin+'/s/'+space+'/d/'+note.id);await page.getByRole('button',{name:'Reading mode',exact:true}).click();
  const paragraph=page.locator('.document-views .markdown-preview p').filter({hasText:'Plaintext'});await paragraph.waitFor();
  assert(Math.abs(await physicalScale(paragraph)-1)<.001,'Reading text grew with the new UI baseline');
  const textHeight=await paragraph.evaluate(el=>el.getBoundingClientRect().height),lineHeight=await paragraph.evaluate(el=>parseFloat(getComputedStyle(el).lineHeight));
  assert(Math.abs(textHeight-lineHeight)<1,'Paragraph physical line height changed');
  await page.getByRole('button',{name:'Visual Markdown editor',exact:true}).click();const visual=page.locator('.ProseMirror');await visual.waitFor();
  assert(Math.abs(await physicalScale(visual)-1)<.001,'Visual document grew with the new UI baseline');
  await page.getByRole('button',{name:'Source mode',exact:true}).click();const source=page.locator('.cm-content');await source.waitFor();
  assert(Math.abs(await physicalScale(source)-1)<.001,'Source document grew with the new UI baseline');
  await page.locator('.cm-content .hljs-keyword').first().waitFor();
  await page.getByRole('button',{name:'Settings and appearance',exact:true}).click();await page.getByRole('tab',{name:'Appearance',exact:true}).click();await select.selectOption('120');
  await page.waitForFunction(()=>Math.abs(Number(getComputedStyle(document.documentElement).zoom)-1.44)<.001);
  await select.selectOption('100');await page.waitForFunction(()=>Math.abs(Number(getComputedStyle(document.documentElement).zoom)-1.2)<.001);
  await page.goto(origin+'/s/'+space+'/d/'+note.id);await page.getByRole('button',{name:'Reading mode',exact:true}).click();
  await page.screenshot({path:'/tmp/netsidian-mcp/ui-baseline.png'});
  return {ui:'100% is former 120%; 120% gives 144%',markdown:'Reading, Visual and Source retain physical scale',code:'highlighting retained'};
 } finally {
  const current=await (await page.request.get(origin+'/api/v1/spaces/'+space+'/documents/'+note.id)).json();
  await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...headers,'If-Match':current.revision}});
 }
}
