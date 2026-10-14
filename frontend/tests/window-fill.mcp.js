// Playwright MCP regression: the workspace fills its window at every UI scale.
async (page) => {
 const origin=new URL(page.url()).origin,spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 await page.goto(origin+'/s/'+space+'/settings');await page.getByRole('tab',{name:'Appearance',exact:true}).click();
 const scale=page.getByRole('combobox',{name:'UI scale',exact:true}),original=await scale.inputValue(),results=[];
 try {
  for(const size of [{width:1280,height:800},{width:900,height:1000}]) {
   await page.setViewportSize(size);
   for(const value of ['80','100','120','150']) {
    await scale.selectOption(value);
    await page.waitForFunction(expected=>Math.abs(Number(getComputedStyle(document.documentElement).zoom)-expected)<.001,Number(value)*.012);
    const measured=await page.locator('.obsidian-workspace').evaluate(el=>{const r=el.getBoundingClientRect();return {top:r.top,left:r.left,right:r.right,bottom:r.bottom,width:innerWidth,height:innerHeight}});
    if(Math.abs(measured.top)>1||Math.abs(measured.left)>1||Math.abs(measured.right-measured.width)>1||Math.abs(measured.bottom-measured.height)>1)throw new Error('Workspace leaves empty space: '+JSON.stringify({value,...measured}));
    results.push({scale:value+'%',size:size.width+'×'+size.height,filled:true});
   }
  }
  return results;
 } finally {
  await page.setViewportSize({width:1280,height:800});
  await Promise.all([page.waitForResponse(r=>r.url().endsWith('/me/preferences')&&r.request().method()==='PUT'&&r.status()===200),scale.selectOption(original)]);
  await page.screenshot({path:'/tmp/netsidian-mcp/window-fill.png'});
 }
}
