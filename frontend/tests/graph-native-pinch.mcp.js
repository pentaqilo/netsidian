// Exercise the DOM event bridge used by the Linux GTK capture handler.
// Physical GDK touchpad event delivery requires a native desktop session.
async (page) => {
 await page.getByRole('button',{name:'Open graph view',exact:true}).click();
 const canvas=page.locator('.g2-canvas');await canvas.waitFor();
 await page.waitForFunction(()=>Number(document.querySelector('.g2-canvas')?.dataset.zoom)>0);
 const bounds=await canvas.boundingBox(),anchor={clientX:bounds.x+bounds.width/2,clientY:bounds.y+bounds.height/2};
 const baseline=await page.evaluate(()=>({zoom:getComputedStyle(document.documentElement).zoom,viewport:visualViewport.scale,width:innerWidth,height:innerHeight}));
 const pinch=async(phase,factor=1,point=anchor)=>page.evaluate(detail=>document.dispatchEvent(new CustomEvent('netsidian-native-pinch',{detail})),{phase,factor,...point});
 const before=Number(await canvas.getAttribute('data-zoom'));
 await pinch('start');await pinch('change',1.2);await pinch('change',1.25);await pinch('end');
 await page.waitForFunction(previous=>Number(document.querySelector('.g2-canvas').dataset.zoom)>previous*1.4,before);
 const after=Number(await canvas.getAttribute('data-zoom'));
 if(Math.abs(after/before-1.5)>.02)throw new Error('Native incremental pinch did not zoom the graph by 1.5');
 // Starting on the ribbon must never zoom a graph that happens to be visible.
 const ribbon=await page.locator('.ribbon').boundingBox();
 await pinch('start',1,{clientX:ribbon.x+ribbon.width/2,clientY:ribbon.y+10});await pinch('change',2);await pinch('end');
 if(Number(await canvas.getAttribute('data-zoom'))!==after)throw new Error('Outside pinch changed graph camera');
 await page.mouse.move(anchor.clientX,anchor.clientY);await page.keyboard.down('Control');await page.mouse.wheel(0,-100);await page.keyboard.up('Control');
 await page.waitForFunction(previous=>Number(document.querySelector('.g2-canvas').dataset.zoom)>previous,after);
 const final=await page.evaluate(()=>({zoom:getComputedStyle(document.documentElement).zoom,viewport:visualViewport.scale,width:innerWidth,height:innerHeight}));
 if(JSON.stringify(final)!==JSON.stringify(baseline))throw new Error('Gesture changed whole-app scale or viewport');
 return {nativeBridge:'pinch scales only graph; outside gestures ignored',ctrlWheel:'graph zoom works',appScale:'unchanged'};
}
