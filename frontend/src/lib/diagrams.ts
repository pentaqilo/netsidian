import DOMPurify from 'dompurify';

// Lazy client-side diagram engines. Fences stay plain code blocks until a
// matching pre>code exists in a rendered container, then the engine chunk is
// imported on demand (vite code-splitting keeps the base bundle untouched).
export type DiagramKind='mermaid'|'puml';

export const diagramKind=(className:string):DiagramKind|''=>{
 const m=className.match(/(?:^|\s)language-(mermaid|puml|plantuml)(?:\s|$)/);
 return m?(m[1]==='mermaid'?'mermaid':'puml'):'';
};

// PlantUML output is plain SVG shapes+text, fully expressible through the
// svg sanitizer profile, so it gets DOMPurify directly.
export const sanitizeSvg=(svg:string)=>DOMPurify.sanitize(svg,{USE_PROFILES:{svg:true,svgFilters:true}});

// Mermaid renders every label as escaped text in foreignObject HTML, which
// DOMPurify always empties (mutation-XSS countermeasure). securityLevel
// 'strict' already escaped all user input inside the generator, so the
// output only needs a structural sweep for scripts, handlers and URLs.
export const scrubSvg=(svg:string):string=>{
 const t=document.createElement('template');
 t.innerHTML=svg;
 t.content.querySelectorAll('script,iframe,object,embed,form,input,button,link,meta,base').forEach(n=>n.remove());
 t.content.querySelectorAll('*').forEach(el=>{
  for(const a of [...el.attributes]){
   const n=a.name.toLowerCase(),v=a.value;
   if(n.startsWith('on')||n==='formaction'||((n==='href'||n==='xlink:href')&&/^\s*(javascript|data):/i.test(v)))el.removeAttribute(a.name);
  }
 });
 return t.innerHTML;
};

const svgCache=new Map<string,string>();

const isDark=()=>document.documentElement.dataset.theme!=='light';

let mermaidSeq=0;
async function renderMermaid(src:string,dark:boolean):Promise<string>{
 if(!src.trim())throw new Error('empty diagram');
 const mermaid=(await import('mermaid')).default;
 // 'base' is the only v12 theme that honors themeVariables; without it dark
 // labels render dark-on-dark inside the themed container.
 mermaid.initialize({startOnLoad:false,securityLevel:'strict',theme:dark?'base':'default',flowchart:{htmlLabels:false},themeVariables:dark?{primaryTextColor:'#e8e8e8',primaryColor:'#2d2d2d',primaryBorderColor:'#9a9a9a',lineColor:'#a0a0a0',textColor:'#e8e8e8',secondaryColor:'#333338',tertiaryColor:'#26262a'}:undefined});
 const{svg}=await mermaid.render('nsd-diagram-'+(++mermaidSeq).toString(36)+Date.now().toString(36),src);
 return svg;
}

let pumlMod:Promise<typeof import('@plantuml/core/plantuml.js')>|null=null;
// viz-global.js is a classic UMD script; injected as a blob script so its
// globals exist before the ES module engine starts its worker.
function ensurePuml(){
 if(!pumlMod)pumlMod=(async()=>{
  if(!document.querySelector('script[data-nsd-puml-viz]')){
   const raw=(await import('@plantuml/core/viz-global.js?raw')).default;
   await new Promise<void>((res,rej)=>{const url=URL.createObjectURL(new Blob([raw],{type:'text/javascript'}));const s=document.createElement('script');s.src=url;s.dataset.nsdPumlViz='1';s.onload=()=>{URL.revokeObjectURL(url);res()};s.onerror=()=>rej(new Error('PlantUML layout engine failed to load'));document.head.appendChild(s)});
  }
  return import('@plantuml/core/plantuml.js');
 })();
 return pumlMod;
}

// The PlantUML engine shares internal state: renders must be serialized.
let pumlChain:Promise<unknown>=Promise.resolve();
// The TeaVM engine's dark flag leaves label colors engine-default, so dark
// skinparams are injected into the source itself; any user override wins.
function pumlTheme(src:string,dark:boolean):string{
 if(!dark)return src;
 const inject='skinparam ArrowColor #c8c8c8\nskinparam ArrowFontColor #e8e8e8\nskinparam backgroundColor transparent\n';
 const at=src.match(/^@start\w+/);
 return at?at[0]+'\n'+inject+src.slice(at[0].length):inject+src;
}

function renderPuml(src:string,dark:boolean):Promise<string>{
 const run=async()=>{
  if(!src.trim())throw new Error('empty diagram');
  const m=await ensurePuml();
  return new Promise<string>((res,rej)=>{let done=false;m.renderToString(pumlTheme(src,dark).split('\n'),svg=>{if(!done){done=true;res(svg)}},msg=>{if(!done){done=true;rej(new Error(String(msg)))}})});
 };
 const out=pumlChain.then(run,run);
 pumlChain=out.catch(()=>{});
 return out;
}

// Replaces matching <pre> blocks with rendered SVG plus a collapsible source.
// Container re-renders (debounced preview) detach stale nodes; isConnected
// guards abort writes into DOM that React/DocPage already replaced.
export async function renderDiagrams(container:HTMLElement):Promise<void>{
 const dark=isDark();
 const pres=[...container.querySelectorAll<HTMLPreElement>('pre')].filter(pre=>diagramKind(((pre.firstElementChild as HTMLElement|null)?.className)||'')!=='');
 for(const pre of pres){
  if(!pre.isConnected)continue;
  const code=pre.firstElementChild as HTMLElement;
  const kind=diagramKind(code.className) as DiagramKind;
  const src=(code.textContent||'').replace(/\n$/,'');
  const box=document.createElement('div');
  box.className='diagram diagram-'+kind;
  try{
   const key=kind+'\u0000'+(dark?'d':'l')+'\u0000'+src;
   let svg=svgCache.get(key);
   if(svg===undefined){svg=kind==='mermaid'?await renderMermaid(src,dark):await renderPuml(src,dark);svgCache.set(key,svg)}
   if(!pre.isConnected)continue;
    box.innerHTML=kind==='mermaid'?scrubSvg(svg):sanitizeSvg(svg);
   const details=document.createElement('details');details.className='diagram-source';
   const sum=document.createElement('summary');sum.textContent='Source';
   const dup=document.createElement('pre');const dupCode=document.createElement('code');dupCode.textContent=src;dup.append(dupCode);
   details.append(sum,dup);box.append(details);
  }catch(e){
   if(!pre.isConnected)continue;
   box.classList.add('diagram-error');
   const msg=document.createElement('div');msg.className='diagram-error-msg';
   msg.textContent='Diagram render failed: '+(e instanceof Error?e.message:String(e));
   box.append(msg,pre.cloneNode(true));
  }
  if(!pre.isConnected)continue;
  pre.replaceWith(box);
 }
}
