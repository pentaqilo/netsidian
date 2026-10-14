// Playwright MCP regression for menus opened under CSS UI scaling.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'},spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const stamp=Date.now().toString(36),created=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'Menus-'+stamp+'/Example-'+stamp+'.md',content:'# Context menu test'}});
 if(!created.ok())throw new Error(await created.text());const note=await created.json(),results=[];
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 const verify=async (target,name)=>{
  await target.scrollIntoViewIfNeeded();const r=await target.boundingBox(),x=r.x+Math.min(20,r.width/2),y=r.y+r.height/2;
  await page.mouse.click(x,y,{button:'right'});const menu=page.locator('.file-menu');await menu.waitFor();
  const box=await menu.boundingBox(),expected=Math.max(8,Math.min(x,1280-box.width-8));
  assert(Math.abs(box.x-expected)<2,name+' menu is displaced from its click: '+JSON.stringify({x,box}));
  assert(box.x>=0&&box.y>=0&&box.x+box.width<=1280&&box.y+box.height<=800,name+' menu exceeds viewport');
  await page.locator('.menu-dismiss').click({position:{x:1,y:1}});
 };
 let original='100';
 try {
  await page.goto(origin+'/s/'+space+'/settings');await page.getByRole('tab',{name:'Appearance',exact:true}).click();original=await page.getByRole('combobox',{name:'UI scale',exact:true}).inputValue();
  for(const value of ['80','100','150']) {
   await page.goto(origin+'/s/'+space+'/settings');await page.getByRole('tab',{name:'Appearance',exact:true}).click();await page.getByRole('combobox',{name:'UI scale',exact:true}).selectOption(value);
   await page.waitForFunction(expected=>Math.abs(Number(getComputedStyle(document.documentElement).zoom)-expected)<.001,Number(value)*.012);
   await page.goto(origin+'/s/'+space+'/d/'+note.id);const tab=page.locator('.document-tab').filter({hasText:stamp}).first();await tab.waitFor();
   await verify(tab,'Tab');
   await tab.locator('button').first().focus();const anchor=await tab.boundingBox();await page.keyboard.press('Shift+F10');const keyboardMenu=page.locator('.file-menu');await keyboardMenu.waitFor();const b=await keyboardMenu.boundingBox();assert(Math.abs(b.x-Math.max(8,Math.min(anchor.x,1280-b.width-8)))<2,'Keyboard tab menu is displaced');await page.keyboard.press('Escape');
   if(!await page.locator('.explorer').isVisible())await page.getByRole('button',{name:'Toggle file explorer',exact:true}).click();
   const folder=page.locator('.folder-row').filter({hasText:'Menus-'+stamp}).first();await folder.waitFor();await verify(folder,'Folder');
   if(await page.locator('.file-row:not(.folder-row)').filter({hasText:'Example-'+stamp}).count()===0)await folder.click();
   await verify(page.locator('.file-row:not(.folder-row)').filter({hasText:'Example-'+stamp}).first(),'Document');
   results.push({scale:value+'%',tab:'at click',keyboard:'at tab',folder:'at click',document:'at click'});
  }
  return results;
 } finally {
  await page.goto(origin+'/s/'+space+'/settings');await page.getByRole('tab',{name:'Appearance',exact:true}).click();
  await Promise.all([page.waitForResponse(r=>r.url().endsWith('/me/preferences')&&r.request().method()==='PUT'&&r.status()===200),page.getByRole('combobox',{name:'UI scale',exact:true}).selectOption(original)]);
  await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...headers,'If-Match':note.currentRevision}});
 }
}
