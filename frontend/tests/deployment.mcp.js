// Run with Playwright MCP browser_run_code_unsafe after signing into a
// disposable local-auth deployment as its administrator. Registration must be open.
// The scenario creates only uniquely named test accounts and notes.
async (page) => {
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const origin = new URL(page.url()).origin;
  const mutationHeaders = {'X-Netsidian-Requested': '1'};
  const stamp = Date.now().toString(36);
  const me = await (await page.request.get(origin+'/api/v1/me')).json();
  assert(me.isOperator, 'Run this scenario as a test administrator');
  const descriptor = await (await page.request.get(origin+'/api/v1/settings')).json();
  assert(descriptor.signupEnabled, 'Test deployment registration is closed');
  const spaces = await (await page.request.get(origin+'/api/v1/spaces')).json();
  const space = spaces.spaces[0].space.id;
  await page.goto(origin+'/s/'+space+'/settings');
  const scale = page.getByRole('combobox', {name:'UI scale',exact:true});
  await Promise.all([
    page.waitForResponse(r=>r.url().endsWith('/me/preferences') && r.request().method()==='PUT' && r.status()===200),
    scale.selectOption('150'),
  ]);
  await page.waitForFunction(()=>getComputedStyle(document.documentElement).zoom==='1.5');
  // Fixed overlays must stay adjacent to their trigger when UI scaling changes.
  const color = page.getByRole('button',{name:'Headings color',exact:true});
  await color.scrollIntoViewIfNeeded();await color.click();
  const palette = page.getByRole('dialog',{name:'Headings color picker'});
  const triggerBox=await color.boundingBox(),paletteBox=await palette.boundingBox();
  assert(triggerBox && paletteBox && Math.abs(triggerBox.y-(paletteBox.y+paletteBox.height))<20, 'Color palette moved away at 150% UI scale');
  await page.keyboard.press('Escape');
  const notes=[];
  const memberContext=await page.context().browser().newContext();
  let memberID='';
  try {
    for(const name of ['A','B']) {
      const response=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents', {headers:mutationHeaders,data:{path:'Playwright-'+stamp+'/'+name+'.md',content:'# '+name+'\n\n[[Playwright-'+stamp+'/'+(name==='A'?'B':'A')+']]'}});
      assert(response.status()===201,'Graph fixture creation failed: '+await response.text());notes.push(await response.json());
    }
    await page.getByRole('button',{name:'Open graph view',exact:true}).click();
    const canvas=page.locator('.g2-canvas');await canvas.waitFor({state:'visible'});
    await page.waitForFunction(()=>!!document.querySelector('.g2-canvas')?.dataset.camera);
    const before=await page.evaluate(()=>({zoom:document.querySelector('.g2-canvas').dataset.zoom,ui:getComputedStyle(document.documentElement).zoom,viewport:visualViewport.scale,ribbon:document.querySelector('.ribbon').getBoundingClientRect().width}));
    const box=await canvas.boundingBox();
    await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.keyboard.down('Control');await page.mouse.wheel(0,-200);await page.keyboard.up('Control');
    await page.waitForFunction(previous=>Number(document.querySelector('.g2-canvas').dataset.zoom)>Number(previous),before.zoom);
    const afterWheel=await page.evaluate(()=>({zoom:document.querySelector('.g2-canvas').dataset.zoom,ui:getComputedStyle(document.documentElement).zoom,viewport:visualViewport.scale,ribbon:document.querySelector('.ribbon').getBoundingClientRect().width}));
    assert(before.ui===afterWheel.ui && before.viewport===afterWheel.viewport && Math.abs(before.ribbon-afterWheel.ribbon)<1,'Ctrl+wheel changed the app scale');
    const cancelled=await canvas.evaluate((node,point)=>{
      const result=[];
      for(const [type,scale] of [['gesturestart',1],['gesturechange',1.4],['gestureend',1.4]]) {
        const event=new Event(type,{bubbles:true,cancelable:true});Object.assign(event,{scale,clientX:point.x,clientY:point.y});node.dispatchEvent(event);result.push(event.defaultPrevented);
      }
      return result;
    },{x:box.x+box.width/2,y:box.y+box.height/2});
    assert(cancelled.every(Boolean),'WebKit gesture default was not cancelled');
    await page.waitForFunction(previous=>Number(document.querySelector('.g2-canvas').dataset.zoom)>Number(previous),afterWheel.zoom);
    assert(await page.evaluate(()=>getComputedStyle(document.documentElement).zoom)==='1.5','Gesture changed UI scale');
    await page.goto(origin+'/s/'+space+'/settings');
    await page.getByRole('button',{name:/^Administration/}).click();
    await page.getByRole('heading',{name:'Accounts',exact:true}).waitFor();
    const memberPage=await memberContext.newPage();await memberPage.goto(origin+'/auth');
    await memberPage.getByRole('button',{name:'Create an account',exact:true}).click();
    const email='playwright-'+stamp+'@example.test';
    await memberPage.getByRole('textbox',{name:'Email',exact:true}).fill(email);
    await memberPage.getByRole('textbox',{name:'Display name',exact:true}).fill('Playwright '+stamp);
    await memberPage.getByRole('textbox',{name:'Password',exact:true}).fill('Playwright-Registration-'+stamp+'!');
    await memberPage.getByRole('button',{name:'Sign up',exact:true}).click();
    await memberPage.getByRole('button',{name:'Settings and appearance',exact:true}).waitFor();
    const member=await (await memberContext.request.get(origin+'/api/v1/me')).json();memberID=member.userId;
    assert(!member.isOperator,'Registration granted administrator rights');
    const memberSpaces=await (await memberContext.request.get(origin+'/api/v1/spaces')).json();assert(memberSpaces.spaces.length===1,'New user did not receive a personal vault');
    assert((await memberContext.request.get(origin+'/api/v1/admin/users')).status()===403,'Member accessed administrator API');
    await page.getByRole('textbox',{name:'Search accounts',exact:true}).fill(email);
    const row=page.locator('.admin-panel tr').filter({hasText:email});await row.waitFor();
    await row.getByRole('button',{name:'Disable',exact:true}).click();
    await page.getByRole('button',{name:'Confirm disable account',exact:true}).click();
    await row.getByRole('button',{name:'Enable',exact:true}).waitFor();
    assert((await memberContext.request.get(origin+'/api/v1/me')).status()===401,'Disabled member session survived');
    await row.getByRole('button',{name:'Enable',exact:true}).click();await page.getByRole('button',{name:'Confirm enable account',exact:true}).click();
    await row.getByRole('button',{name:'Disable',exact:true}).waitFor();
    await page.getByRole('textbox',{name:'Search accounts',exact:true}).fill('');
    // A small scale must still be usable, and restoring it must persist.
    await page.getByRole('combobox',{name:'UI scale',exact:true}).selectOption('80');
    await page.waitForFunction(()=>getComputedStyle(document.documentElement).zoom==='0.8');
    await Promise.all([page.waitForResponse(r=>r.url().endsWith('/me/preferences')&&r.request().method()==='PUT'&&r.status()===200),page.getByRole('combobox',{name:'UI scale',exact:true}).selectOption('100')]);
    await page.waitForFunction(()=>getComputedStyle(document.documentElement).zoom==='1');
    await page.reload();await page.getByRole('combobox',{name:'UI scale',exact:true}).waitFor();
    assert(await page.getByRole('combobox',{name:'UI scale',exact:true}).inputValue()==='100','UI scale was not persisted');
    return {passed:true,graphZoom:{before:before.zoom,afterWheel:afterWheel.zoom},uiScale:'150 → 80 → 100%, unchanged by graph gestures',registration:'regular account with private vault',admin:'member denied; disable revoked session; re-enable succeeded'};
  } finally {
    if(memberID) await page.request.patch(origin+'/api/v1/admin/users/'+memberID,{headers:mutationHeaders,data:{disabled:true}});
    for(const note of notes) await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...mutationHeaders,'If-Match':note.currentRevision}});
    await memberContext.close();
  }
}
