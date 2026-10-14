// A real 5,001-note / 1,000-attachment fixture is mounted at /large-vault.
async (page) => {
 const origin=new URL(page.url()).origin,original=page.url(),headers={'X-Netsidian-Requested':'1'};
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 const start=Date.now(),response=await page.request.post(origin+'/api/v1/local/vaults?background=true',{headers,data:{path:'/large-vault'}});
 assert(response.status()===202,'Vault attachment did not return immediately: '+await response.text());
 const attached=await response.json(),space=attached.space.id,acceptedMs=Date.now()-start;
 assert(acceptedMs<3000,'Vault attachment held its request for '+acceptedMs+'ms');
 let gaps=[],requests=0,stage='navigate';
 const onRequest=request=>{if(new RegExp('/spaces/'+space+'/documents/[^/?]+$').test(request.url()))requests++};page.on('request',onRequest);
 try{
  await page.goto(origin+'/s/'+space);
  stage='progress bar';await page.getByRole('progressbar',{name:'Vault loading progress'}).waitFor();
  await page.evaluate(()=>{window.__vaultGaps=[];let last=performance.now();window.__vaultTimer=setInterval(()=>{const now=performance.now();window.__vaultGaps.push(now-last);last=now},50)});
  stage='settings during import';const clicked=Date.now();await page.getByRole('button',{name:'Settings and appearance',exact:true}).click();await page.getByRole('tab',{name:'Appearance',exact:true}).waitFor();
  assert(Date.now()-clicked<2500,'UI could not open settings during vault import');
  stage='background completion';await page.waitForFunction(async space=>{const r=await fetch('/api/v1/spaces/'+space+'/load'),data=await r.json();if(data.load?.phase==='error')throw new Error(data.load.error);return data.load?.phase==='ready'},space,{polling:1000,timeout:120000});
  gaps=await page.evaluate(()=>{clearInterval(window.__vaultTimer);return window.__vaultGaps});
  assert(Math.max(...gaps)<2000,'Main thread froze during background import: '+Math.max(...gaps)+'ms');
  assert(requests===0,'Workspace eagerly fetched document bodies during vault loading');
  stage='load complete file tree';await page.goto(origin+'/s/'+space);
  const tree=page.getByRole('tree',{name:'Documents and folders',exact:true});await tree.waitFor();
  await page.waitForFunction(()=>Number(document.querySelector('.file-tree')?.dataset.totalRows)>=6300);
  const total=Number(await tree.getAttribute('data-total-rows')),rendered=await tree.locator('.file-row').count();
  assert(rendered<150,'Large vault rendered '+rendered+' DOM rows');
  stage='virtual scroll';await tree.evaluate(element=>{element.scrollTop=element.scrollHeight});
  await page.waitForFunction(()=>document.querySelector('.file-tree .file-row:last-of-type')?.textContent.includes('Anchor')||[...document.querySelectorAll('.file-tree .file-row')].at(-1)?.textContent.includes('Anchor'));
  const scrolled=await tree.evaluate(element=>element.scrollTop);assert(scrolled>10000,'Virtual scroll did not expose the full file tree');
  stage='keyboard home/end';await tree.focus();await page.keyboard.press('Home');await page.waitForFunction(()=>document.activeElement?.classList.contains('file-row'));
  await page.keyboard.press('End');await page.waitForFunction(()=>document.activeElement?.textContent.includes('Anchor'));
  stage='filter and open';await page.getByRole('textbox',{name:'Find in document tree',exact:true}).fill('Note-04999');
  const last=tree.locator('.file-row:not(.folder-row)').filter({hasText:'Note-04999'});await last.click();
  await page.getByRole('heading',{name:'Note 4999',exact:true}).waitFor();
  assert(requests===1,'Opening one document should fetch only its body; got '+requests);
  stage='clear filter and reveal';await page.getByRole('textbox',{name:'Find in document tree',exact:true}).fill('');
  await page.waitForFunction(()=>Number(document.querySelector('.file-tree')?.dataset.totalRows)>=6300);
  await page.getByRole('button',{name:'Reveal active document',exact:true}).click();await last.waitFor();assert(await last.isVisible(),'Reveal did not reach a virtualized row');
  await page.screenshot({path:'/tmp/netsidian-mcp/large-vault.png'});
  return {notes:5001,attachments:1000,totalRows:total,renderedRows:rendered,acceptedMs,loadedMs:Date.now()-start,maxUiGapMs:Math.round(Math.max(...gaps)),bodyReads:requests,settingsDuringImport:'responsive',keyboardAndReveal:'passed'};
 }catch(error){await page.screenshot({path:'/tmp/netsidian-mcp/large-vault-failure.png'});const details=await page.evaluate(()=>({totalRows:document.querySelector('.file-tree')?.dataset.totalRows,lastRows:[...document.querySelectorAll('.file-tree .file-row')].slice(-3).map(e=>e.textContent),active:document.activeElement?.textContent?.slice(0,100),status:document.querySelector('.vault-load-status')?.textContent}));throw new Error(stage+': '+error.message+' '+JSON.stringify(details))}finally{
  page.off('request',onRequest);await page.goto(original);
  const detached=await page.request.delete(origin+'/api/v1/spaces/'+space,{headers,data:{confirmation:attached.space.name}});
  if(!detached.ok())throw new Error('Fixture vault cleanup failed: '+await detached.text());
 }
}
