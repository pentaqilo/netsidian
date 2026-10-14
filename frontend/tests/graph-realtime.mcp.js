// Playwright MCP browser_run_code_unsafe scenario for an authenticated test vault.
async (page) => {
 const origin=new URL(page.url()).origin,headers={'X-Netsidian-Requested':'1'};
 const spaces=await (await page.request.get(origin+'/api/v1/spaces')).json(),space=spaces.spaces[0].space.id;
 const me=await (await page.request.get(origin+'/api/v1/me')).json();
 await page.evaluate(({user,space})=>localStorage.removeItem(`netsidian:graph2:${user}:${space}:vault:settings`),{user:me.userId,space});
 await page.goto(origin+'/s/'+space);
 let snapshot=null;
 const observe=async response=>{if(response.url().endsWith('/spaces/'+space+'/graph2')&&response.ok())snapshot=await response.json()};
 page.on('response',observe);
 const assert=(ok,message)=>{if(!ok)throw new Error(message)};
 const stamp=Date.now().toString(36),notes=[];
 const poll=async check=>{const deadline=Date.now()+15000;while(Date.now()<deadline){if(snapshot&&check(snapshot))return;await page.waitForTimeout(50)}throw new Error('Graph did not update from the document event')};
 const mutate=async (method,path,data,revision)=>{
  const r=await page.request.fetch(origin+'/api/v1/spaces/'+space+path,{method,headers:{...headers,...(revision?{'If-Match':revision}:{})},...(data?{data}:{})});
  assert(r.ok(),method+' '+path+': '+await r.text());return r;
 };
 try {
  await page.getByRole('button',{name:'Open graph view',exact:true}).click();
  const canvas=page.locator('.g2-canvas:visible');await canvas.waitFor();await poll(()=>true);
  const count=snapshot.nodes.length;
  const create=async name=>{const r=await mutate('POST','/documents',{path:'Live-'+stamp+'/'+name+'.md',content:'# '+name});const note=await r.json();notes.push(note);return note};
  const a=await create('A');await poll(g=>g.nodes.some(n=>n.id===a.id));
  await page.waitForFunction(n=>Number(document.querySelector('.g2-canvas').dataset.nodes)===n,count+1);
  const b=await create('B');await poll(g=>g.nodes.some(n=>n.id===b.id));
  await page.waitForFunction(n=>Number(document.querySelector('.g2-canvas').dataset.nodes)===n,count+2);
  const box=await canvas.boundingBox();await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.mouse.wheel(0,-100);
  await page.waitForTimeout(250);const camera=await canvas.getAttribute('data-camera');
  await mutate('PUT','/documents/'+a.id+'/content',{markdown:'# Updated A\n\n[[Live-'+stamp+'/B]]'},a.currentRevision);
  await poll(g=>g.nodes.some(n=>n.id===a.id&&n.title==='Updated A')&&g.edges.some(e=>e.source===a.id&&e.target===b.id));
  await page.waitForFunction(n=>Number(document.querySelector('.g2-canvas').dataset.edges)===n,snapshot.edges.length);
  const fresh=await (await page.request.get(origin+'/api/v1/spaces/'+space+'/documents/'+a.id)).json();
  await mutate('POST','/documents/'+a.id+'/rename',{path:'Live-moved-'+stamp+'/A.md'},fresh.revision);
  await poll(g=>g.nodes.some(n=>n.id===a.id&&n.path==='Live-moved-'+stamp+'/A.md'));
  await mutate('POST','/documents/'+b.id+'/trash',null,b.currentRevision);notes.splice(notes.indexOf(b),1);
  await poll(g=>!g.nodes.some(n=>n.id===b.id));
  await page.waitForFunction(n=>Number(document.querySelector('.g2-canvas').dataset.nodes)===n,count+1);
  assert(camera===await canvas.getAttribute('data-camera'),'A live refresh changed graph pan/zoom');
  return {created:'nodes appear without reopening graph',modified:'title and link appear',moved:'path updates',deleted:'node removed',camera:'preserved'};
 } finally {
  page.off('response',observe);
  for(const note of notes){const r=await page.request.get(origin+'/api/v1/spaces/'+space+'/documents/'+note.id);if(r.ok()){const current=await r.json();await mutate('POST','/documents/'+note.id+'/trash',null,current.revision)}}
 }
}
