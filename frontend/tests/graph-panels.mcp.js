// Run with Playwright MCP on an authenticated disposable test vault.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'};
 const me=await (await page.request.get(origin+'/api/v1/me')).json(),spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const key=`netsidian:graph2:${me.userId}:${space}:vault`;
 await page.evaluate(key=>{for(const suffix of ['settings','navigation','topics-visible','area-visible','controls'])localStorage.removeItem(key+':'+suffix)},key);
 const stamp=Date.now().toString(36),response=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'Panels-'+stamp+'/A.md',content:'# Panel test'}});
 if(!response.ok())throw new Error(await response.text());const note=await response.json();
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 try {
  await page.goto(origin+'/s/'+space);await page.getByRole('button',{name:'Open graph view',exact:true}).click();
  const topics=page.getByRole('button',{name:'Toggle knowledge base sidebar',exact:true});await topics.waitFor();
  assert(await topics.evaluate(node=>!!node.closest('.g2-body')&&!node.closest('.g2-header')),'Knowledge toggle is still in toolbar');
  const canvas=page.locator('.g2-canvas:visible');await page.waitForFunction(()=>document.querySelector('.g2-canvas')?.dataset.layoutState==='settled');
  const left=await topics.boundingBox(),map=await canvas.boundingBox();assert(Math.abs(left.x-map.x)<35&&Math.abs(left.y-map.y)<35,'Knowledge toggle is not at graph top left');
  await page.getByRole('button',{name:'Close knowledge base',exact:true}).click();
  assert(await page.getByRole('complementary',{name:'Knowledge map topics',exact:true}).count()===0,'Knowledge close failed');
  assert(await topics.getAttribute('aria-expanded')==='false','Knowledge toggle state did not update');
  assert(await topics.evaluate(node=>node===document.activeElement),'Close did not restore toggle focus');
  await topics.click();await page.getByRole('button',{name:'Close knowledge base',exact:true}).waitFor();
  await page.locator('.g2-atlas-sidebar .g2-topic').filter({hasText:stamp}).first().click();
  const area=page.getByRole('button',{name:'Toggle topic area details',exact:true});await area.waitFor();
  assert(await area.evaluate(node=>!!node.closest('.g2-body')&&!node.closest('.g2-header')),'Topic toggle is still in toolbar');
  const right=await area.boundingBox(),body=await page.locator('.g2-body').boundingBox();assert(Math.abs(body.x+body.width-right.x-right.width)<25&&Math.abs(right.y-body.y)<25,'Topic toggle is not at graph top right');
  await page.getByRole('button',{name:'Close topic area details',exact:true}).click();
  assert(await page.getByRole('complementary',{name:'Topic area details',exact:true}).count()===0,'Topic close failed');
  assert(await area.getAttribute('aria-expanded')==='false','Topic toggle state did not update');
  await area.click();await page.getByRole('button',{name:'Close topic area details',exact:true}).waitFor();
  await page.screenshot({path:'/tmp/netsidian-mcp/graph-panels.png'});
  await page.setViewportSize({width:600,height:800});
  await page.getByRole('button',{name:'Close knowledge base',exact:true}).click();await topics.click();
  await page.getByRole('button',{name:'Close topic area details',exact:true}).click();await area.click();
  return {knowledgeBase:'top left; close/reopen works',topicArea:'top right; close/reopen works',mobile:'both close controls work'};
 } finally {
  await page.setViewportSize({width:1280,height:800});
  await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...headers,'If-Match':note.currentRevision}});
 }
}
