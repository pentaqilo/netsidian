// Run through Playwright MCP against an authenticated disposable test vault.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'};
 const spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const created=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'Compact-tags-'+Date.now()+'.md',content:'# Compact tags\n\nBody text.'}});
 if(!created.ok())throw new Error(await created.text());const note=await created.json();
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 const saved=()=>page.waitForFunction(()=>document.querySelector('.document-status')?.textContent.includes('All changes saved'));
 try {
  await page.goto(origin+'/s/'+space+'/d/'+note.id);await page.getByRole('button',{name:'Reading mode',exact:true}).click();
  const trigger=page.getByRole('button',{name:'Document tags',exact:true});await trigger.waitFor();
  assert(await trigger.evaluate(el=>!!el.closest('.document-toolbar')&&el.nextElementSibling?.getAttribute('aria-label')==='Bookmark this document'),'Tag action is not next to Bookmark');
  assert(await page.locator('.document-pane>.document-tags').count()===0,'Dedicated tag bar remains');
  assert(await page.locator('.document-tag-summary').count()===0,'Empty document reserves space for tags');
  await trigger.click();const menu=page.getByRole('dialog',{name:'Document tags',exact:true});await menu.waitFor();
  await menu.getByRole('button',{name:'Add tags',exact:true}).click();await menu.getByRole('combobox',{name:'Add document tags',exact:true}).fill('Alpha, project/test');await page.keyboard.press('Enter');
  await page.getByRole('button',{name:'Find documents tagged alpha',exact:true}).waitFor();await saved();
  assert(await page.locator('.document-tag-summary button').count()===2,'Document tag summary is missing');
  await menu.getByRole('button',{name:'Remove tag alpha',exact:true}).click();
  await page.waitForFunction(()=>document.querySelectorAll('.document-tag-summary button').length===1);await saved();
  await page.keyboard.press('Escape');assert(await trigger.getAttribute('aria-expanded')==='false','Escape did not close the popover');
  assert(await trigger.evaluate(el=>el===document.activeElement),'Popover did not restore focus');
  await page.getByRole('button',{name:'Find documents tagged project/test',exact:true}).click();
  const search=page.getByRole('textbox',{name:'Search documents',exact:true});await search.waitFor();assert(await search.inputValue()==='tag:project/test','Tag click did not search');
  await page.goto(origin+'/s/'+space+'/d/'+note.id);await page.getByRole('button',{name:'Reading mode',exact:true}).click();
  await page.getByRole('button',{name:'Find documents tagged project/test',exact:true}).waitFor();
  assert(await page.getByRole('button',{name:'Find documents tagged alpha',exact:true}).count()===0,'Removed tag returned after reload');
  await trigger.click();await menu.waitFor();const anchor=await trigger.boundingBox(),popup=await menu.boundingBox();assert(Math.abs(anchor.x+anchor.width-popup.x-popup.width)<2,'Popover is not aligned with tag button');await page.screenshot({path:'/tmp/netsidian-mcp/document-tags-menu.png'});
  await page.keyboard.press('Escape');await page.setViewportSize({width:600,height:800});
  const scrim=page.getByRole('button',{name:'Close file explorer',exact:true});if(await scrim.isVisible())await scrim.click();
  await trigger.click();await menu.waitFor();const bounds=await menu.boundingBox();assert(bounds.x>=0&&bounds.y>=0&&bounds.x+bounds.width<=600&&bounds.y+bounds.height<=800,'Tag popover exceeds mobile viewport');
  await menu.getByRole('button',{name:'Close document tags',exact:true}).click();
  return {toolbar:'tag button beside Bookmark',empty:'no tag bar or blank summary',editing:'add/remove persist',summary:'click searches',keyboard:'Escape restores focus',mobile:'popover fits'};
 } finally {
  await page.setViewportSize({width:1280,height:800});const current=await (await page.request.get(origin+'/api/v1/spaces/'+space+'/documents/'+note.id)).json();
  await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...headers,'If-Match':current.revision}});
 }
}
