// Playwright MCP scenario for the graph preview and zoom-control layout.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'};
 const me=await (await page.request.get(origin+'/api/v1/me')).json(),spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const stamp=Date.now().toString(36),created=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'Zoom-preview-'+stamp+'/Work/Preview.md',content:'# Zoom preview '+stamp+'\n\n[[../Home/Related]]\n\n'+Array.from({length:100},(_,i)=>'Paragraph '+i+'. This is preview content.').join('\n\n')}});
 if(!created.ok())throw new Error(await created.text());const note=await created.json();
 const otherResponse=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'Zoom-preview-'+stamp+'/Home/Related.md',content:'# Related '+stamp+'\n\n[[../Work/Preview]]'}});if(!otherResponse.ok())throw new Error(await otherResponse.text());const other=await otherResponse.json();
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 try {
  await page.setViewportSize({width:1900,height:1000});
  await page.evaluate(({user,space})=>{const key=`netsidian:graph2:${user}:${space}:vault`;for(const suffix of ['settings','topics-visible','controls','navigation'])localStorage.removeItem(key+':'+suffix);localStorage.setItem(key+':settings',JSON.stringify({colorBy:'none'}))}, {user:me.userId,space});
  await page.goto(origin+'/s/'+space);await page.getByRole('button',{name:'Open graph view',exact:true}).click();
  await page.getByRole('textbox',{name:'Find a note in Graph 2',exact:true}).fill(stamp);
  await page.locator('.g2-results [data-graph-result]').filter({hasText:'Zoom preview '+stamp}).first().click();
  const preview=page.getByRole('complementary',{name:'Graph 2 note preview',exact:true});await preview.waitFor();
  const geometry=async()=>{
   const panel=await preview.boundingBox(),body=await page.locator('.g2-body').boundingBox(),map=await page.locator('.g2-canvas').boundingBox(),zoom=await page.locator('.g2-navigation').boundingBox();
   assert(Math.abs(panel.y-body.y)<2&&Math.abs(panel.y+panel.height-body.y-body.height)<2,'Preview does not fill graph height');
   assert(Math.abs(panel.x+panel.width-body.x-body.width)<2,'Preview does not reach the right edge');
   assert(Math.abs(map.x+map.width-panel.x)<2,'Gap between graph and preview');
   assert(zoom.x>=map.x&&zoom.x+zoom.width<=map.x+map.width&&zoom.y>=map.y&&zoom.y+zoom.height<=map.y+map.height,'Zoom controls are outside graph canvas');
  };
  await geometry();
  assert(await preview.locator(':scope > header,:scope > footer').count()===0,'Preview still has title or action bars');
  const close=preview.getByRole('button',{name:'Close Graph 2 preview',exact:true}),tabs=preview.locator('.g2-inspector-tabs');
  const cb=await close.boundingBox(),tb=await tabs.boundingBox();assert(cb.y+cb.height<=tb.y+1,'Close is not above tabs');
  assert(await preview.locator('.g2-selection-context').count()===0,'Instruction frame remains');
  const filename=await preview.locator('.g2-preview-heading > .g2-path').boundingBox(),firstTab=await tabs.locator('[role=tab]').first().boundingBox(),frame=await preview.boundingBox();
  assert(filename.y+filename.height<=tb.y+1,'Filename is not above tabs');assert(Math.abs(filename.y+filename.height/2-cb.y-cb.height/2)<3,'Filename and close are not aligned');assert(firstTab.x-frame.x>=10,'First tab touches preview frame');assert(firstTab.height<=36,'Preview tabs are too tall');
  await page.getByRole('button',{name:'Link layout',exact:true}).click();
  assert(await page.getByRole('button',{name:'Group colors',exact:true}).count()===0,'Group color toggle remains');
  await page.waitForFunction(()=>document.querySelector('.g2-canvas').dataset.groupColors==='true');
  assert(JSON.parse(await page.locator('.g2-canvas').getAttribute('data-node-colors')).length>1,'Folder nodes have identical colors');
  assert(JSON.parse(await page.locator('.g2-canvas').getAttribute('data-link-colors')).length>1,'Folder links have identical colors');
  await page.getByRole('button',{name:'Topic atlas',exact:true}).click();
  await page.getByRole('textbox',{name:'Find a note in Graph 2',exact:true}).fill(stamp);await page.locator('.g2-results [data-graph-result]').filter({hasText:'Zoom preview '+stamp}).first().click();await preview.waitFor();
  const resizer=page.getByRole('separator',{name:'Resize graph preview width',exact:true});await resizer.focus();await page.keyboard.press('ArrowLeft');await geometry();
  const before=await page.locator('.g2-canvas').getAttribute('data-zoom');await page.getByRole('button',{name:'Zoom in Graph 2',exact:true}).click();
  await page.waitForFunction(previous=>Number(document.querySelector('.g2-canvas').dataset.zoom)>Number(previous),before);
  await page.screenshot({path:'/tmp/netsidian-mcp/graph-preview-layout.png'});
  await page.setViewportSize({width:600,height:800});await page.waitForTimeout(100);
  const zoom=await page.locator('.g2-navigation').boundingBox(),panel=await preview.boundingBox();assert(zoom.y+zoom.height<=panel.y,'Mobile zoom controls overlap the preview');
  return {preview:'filename and close above compact tabs; no footer',colors:'folder nodes and links distinct by default; no toggle',desktop:'full-height right panel; zoom on canvas',resizing:'zoom stays on canvas',zoom:'works',mobile:'controls above document drawer'};
 } finally {
  await page.setViewportSize({width:1280,height:800});
  await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...headers,'If-Match':note.currentRevision}});
 }
}
