// Verify natural table column widths, horizontal overflow and themed headers.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'};
 const spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const prose=('Actual authorized Markdown prose with normalized terms, links and membership explanations. ').repeat(12);
 const content='# Table layout\n\n| Mode | Membership and explanation |\n| --- | --- |\n'+['Content','Tags','Folders','Links'].map(word=>'| '+word+' | '+prose+' |').join('\n')+'\n\n| Identifier | Description |\n| --- | --- |\n| '+('x'.repeat(180))+' | Wide table must scroll |\n';
 const response=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'Table-review-'+Date.now()+'.md',content}});if(!response.ok())throw new Error(await response.text());const note=await response.json();
 const oldTheme=await page.evaluate(()=>document.documentElement.dataset.theme);
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 const inspect=async(selector,theme)=>{
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
  return page.locator(selector).evaluate(root=>{
   const table=root.querySelector('table'),cell=table.querySelector('tbody td')||table.querySelector('td'),header=table.querySelector('th');
   const css=getComputedStyle(cell),canvas=document.createElement('canvas'),ctx=canvas.getContext('2d');ctx.font=css.font;
   const range=document.createRange();range.selectNodeContents(cell);
   const brightness=color=>{ctx.fillStyle=color;ctx.fillRect(0,0,1,1);return [...ctx.getImageData(0,0,1,1).data].slice(0,3).reduce((a,v)=>a+v,0)};
   return {wordWidth:ctx.measureText('Content').width+parseFloat(css.paddingLeft)+parseFloat(css.paddingRight),cellWidth:cell.offsetWidth,wordLines:range.getClientRects().length,wrap:css.overflowWrap,headerBrightness:brightness(getComputedStyle(header).backgroundColor),bodyBrightness:brightness(getComputedStyle(document.documentElement).getPropertyValue('--bg')),pageOverflow:document.documentElement.scrollWidth>document.documentElement.clientWidth};
  });
 };
 try{
  await page.goto(origin+'/s/'+space+'/d/'+note.id);await page.getByRole('button',{name:'Reading mode',exact:true}).click();
  for(const width of [1280,600]){
   await page.setViewportSize({width,height:800});
   for(const theme of ['dark','light']){
    const result=await inspect('.document-views.read>.markdown-preview',theme);
    assert(result.cellWidth>=result.wordWidth-1,'Short table column is squeezed: '+JSON.stringify(result));
    assert(result.wrap==='normal','Table inherits emergency word wrapping');
    assert(theme==='dark'?result.headerBrightness>result.bodyBrightness:result.headerBrightness<result.bodyBrightness,'Wrong table header contrast');
    assert(!result.pageOverflow,'Wide table stretches the application');
   }
  }
  await page.setViewportSize({width:1280,height:800});await page.getByRole('button',{name:'Visual Markdown editor',exact:true}).click();await page.locator('.ProseMirror table').first().waitFor();
  for(const theme of ['dark','light']){
   const result=await inspect('.ProseMirror',theme);assert(result.cellWidth>=result.wordWidth-1,'Visual table column squeezed');assert(theme==='dark'?result.headerBrightness>result.bodyBrightness:result.headerBrightness<result.bodyBrightness,'Visual header contrast missing');assert(!result.pageOverflow,'Visual table stretches application');
  }
  await page.screenshot({path:'/tmp/netsidian-mcp/markdown-tables.png'});
  return {reading:'short words stay intact; wide tables scroll',visual:'natural column widths',headers:'lighter in dark mode, darker in light mode',viewports:[1280,600]};
 }finally{
  await page.evaluate(theme=>{if(theme)document.documentElement.dataset.theme=theme},oldTheme);await page.setViewportSize({width:1280,height:800});
  const current=await (await page.request.get(origin+'/api/v1/spaces/'+space+'/documents/'+note.id)).json();await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...headers,'If-Match':current.revision}});
 }
}
