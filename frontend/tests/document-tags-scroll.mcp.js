// Playwright MCP regression: document tags are content, never a pinned row.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'},spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const content='---\ntags: [scroll/test]\n---\n# Scrolling tags\n\n'+Array.from({length:100},(_,i)=>'Paragraph '+i+'. This is scrollable document content.').join('\n\n');
 const created=await page.request.post(origin+'/api/v1/spaces/'+space+'/documents',{headers,data:{path:'Tags-scroll-'+Date.now()+'.md',content}});
 if(!created.ok())throw new Error(await created.text());const note=await created.json();
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 const scrollsAway=async (container,tags)=>{
  await tags.waitFor();await container.evaluate(el=>el.scrollTop=0);await page.waitForTimeout(100);const initial=await tags.boundingBox(),viewport=await container.boundingBox();
  await container.evaluate(el=>el.scrollTop=400);await page.waitForTimeout(100);
  const after=await tags.boundingBox();assert(after.y<initial.y-200&&after.y+after.height<viewport.y,'Tag list remains pinned while the document scrolls');
 };
 try {
  await page.goto(origin+'/s/'+space+'/d/'+note.id);await page.getByRole('button',{name:'Reading mode',exact:true}).click();
  const reader=page.locator('.document-views.read>.markdown-preview');await scrollsAway(reader,reader.locator('.document-tag-summary'));
  await page.getByRole('button',{name:'Source mode',exact:true}).click();
  assert(await page.locator('.document-tag-summary:visible').count()===0,'Source mode still shows a tag row');
  await page.getByRole('button',{name:'Document tags',exact:true}).click();
  await page.getByRole('dialog',{name:'Document tags',exact:true}).getByRole('button',{name:'Search documents tagged scroll/test',exact:true}).waitFor();await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'Visual Markdown editor',exact:true}).click();await page.locator('.ProseMirror').waitFor();
  const visual=page.locator('.visual-content'),toolbar=page.locator('.visual-toolbar'),before=await toolbar.boundingBox();
  const metadata=await visual.locator('.document-tag-summary').boundingBox(),viewport=await visual.boundingBox(),first=await visual.locator('.editor>:first-child').boundingBox();assert((await visual.locator('.document-tag-summary button').first().boundingBox()).x-viewport.x<=16,'Visual tags are not left-aligned');assert(first.y-metadata.y-metadata.height<=6,'Too much space below visual tags');
  await scrollsAway(visual,visual.locator('.document-tag-summary'));
  const after=await toolbar.boundingBox();assert(Math.abs(after.y-before.y)<1,'Visual formatting toolbar scrolls with the tags');
  assert(await visual.locator('.ProseMirror .document-tag-summary').count()===0,'Metadata is editable document body content');
  await page.getByRole('button',{name:'Split editor and preview',exact:true}).click();
  assert(await page.locator('.document-views.split>.markdown-preview .document-tag-summary').count()===1,'Split preview tags are missing');
  assert(await page.locator('.note-editor .document-tag-summary').count()===0,'Split source editor shows a tag row');
  return {reading:'tags scroll with document',visual:'tags scroll; formatting toolbar stays',source:'tags hidden; header management available',split:'tags only in reading pane'};
 } finally {
  const current=await (await page.request.get(origin+'/api/v1/spaces/'+space+'/documents/'+note.id)).json();
  await page.request.post(origin+'/api/v1/spaces/'+space+'/documents/'+note.id+'/trash',{headers:{...headers,'If-Match':current.revision}});
 }
}
