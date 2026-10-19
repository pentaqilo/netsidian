import {useEffect,useRef,useState} from 'react';
import {createPortal} from 'react-dom';
import {type AccentPreset, isHex, resolveAccent, resolveMemoAccent} from '../lib/accent';
import {Icon} from './Icon';
import {overlayPoint,uiZoom} from '../lib/overlayPosition';
export interface ThemeColors{preset:AccentPreset;accent:string;hColor:string;linkColor:string;quoteColor:string;codeColor:string;memoColor:string}
export interface Appearance {theme:'dark'|'light'|'system';fontSize:number;lineWidth:number;zoom:number;navLabels:boolean;restoreTabs:boolean;dark:ThemeColors;light:ThemeColors}
const theme=(preset:AccentPreset='default'):ThemeColors=>({preset,accent:'',hColor:'',linkColor:'',quoteColor:'',codeColor:'',memoColor:''});
export const defaults:Appearance={theme:'dark',fontSize:16,lineWidth:720,zoom:100,navLabels:true,restoreTabs:true,dark:theme(),light:theme()};
// Built-in Markdown palette per scheme: light-friendly hues on dark, deep readable tones on
// light, and links lean blue in both. A user-picked color wins; Reset returns to these.
export const paletteDefaults:Record<'dark'|'light',Omit<ThemeColors,'preset'|'accent'>>={
  dark:{hColor:'#ffffff',linkColor:'#6ea8fe',quoteColor:'#83c9a6',codeColor:'#e8a55f',memoColor:'#a6c6ab'},
  light:{hColor:'#000000',linkColor:'#2563db',quoteColor:'#2e7d51',codeColor:'#994515',memoColor:'#41714e'},
};
// Stored prefs merge defensively. Legacy flat fields (single accent + md colors) seed both
// schemes equally, so an upgrade keeps the previous look until each mode is tuned apart.
function mergeAppearance(saved:unknown):Appearance{
 const p=(saved&&typeof saved==='object'?saved:{}) as Record<string,unknown>;
 const hexOr=(v:unknown)=>typeof v==='string'&&isHex(v)?v:'';
 const presetOf=(v:unknown):AccentPreset=>v==='blue'||v==='violet'||v==='neutral'||v==='custom'?v:'default';
 const legacy:ThemeColors={preset:presetOf(p.accentPreset)==='default'&&isHex(hexOr(p.accent))?'custom':presetOf(p.accentPreset),accent:hexOr(p.accent),hColor:hexOr(p.hColor),linkColor:hexOr(p.linkColor),quoteColor:hexOr(p.quoteColor),codeColor:hexOr(p.codeColor),memoColor:hexOr(p.memoColor)};
 const themeOf=(v:unknown):ThemeColors=>{if(!v||typeof v!=='object')return{...legacy};const t=v as Partial<ThemeColors>;return{preset:presetOf(t.preset)==='default'&&isHex(t.accent||'')?'custom':presetOf(t.preset),accent:hexOr(t.accent),hColor:hexOr(t.hColor),linkColor:hexOr(t.linkColor),quoteColor:hexOr(t.quoteColor),codeColor:hexOr(t.codeColor),memoColor:hexOr(t.memoColor)}};
 return{restoreTabs:p.restoreTabs!==false,navLabels:p.navLabels!==false,theme:p.theme==='light'||p.theme==='system'?p.theme:'dark',fontSize:typeof p.fontSize==='number'?p.fontSize:defaults.fontSize,lineWidth:typeof p.lineWidth==='number'?p.lineWidth:defaults.lineWidth,zoom:typeof p.zoom==='number'?Math.min(200,Math.max(60,Math.round(p.zoom/5)*5)):100,dark:themeOf(p.dark),light:themeOf(p.light)};
}
export function useAppearance(user:string){
 const [appearance,setAppearance]=useState<Appearance>(()=>{try{return mergeAppearance(JSON.parse(localStorage.getItem('netsidian:appearance:'+user)||'{}'))}catch{return defaults}});const [loaded,setLoaded]=useState(false);const edits=useRef(0),writes=useRef<Promise<unknown>>(Promise.resolve()),alive=useRef(false),currentUser=useRef(user);currentUser.current=user;
 const cacheKey='netsidian:appearance:'+user,pendingKey=cacheKey+':pending';
 const queue=(next:Appearance)=>{const owner=user;writes.current=writes.current.catch(()=>{}).then(async()=>{
  if(!alive.current||currentUser.current!==owner)return;const r=await fetch('/api/v1/me/preferences');if(!r.ok)return;const p=await r.json();if(!alive.current||currentUser.current!==owner)return;
  const saved=await fetch('/api/v1/me/preferences',{method:'PUT',keepalive:true,headers:{'Content-Type':'application/json','X-Netsidian-Requested':'1'},body:JSON.stringify({...p,appearance:next})});
  if(saved.ok)try{const pending=localStorage.getItem(pendingKey);if(pending&&JSON.stringify(mergeAppearance(JSON.parse(pending)))===JSON.stringify(mergeAppearance(next)))localStorage.removeItem(pendingKey)}catch{}
 }).catch(()=>{})};
 useEffect(()=>{let active=true;alive.current=true;edits.current=0;let pending:Appearance|null=null;
  try{const raw=localStorage.getItem(pendingKey);pending=raw?mergeAppearance(JSON.parse(raw)):null;setAppearance(pending||mergeAppearance(JSON.parse(localStorage.getItem(cacheKey)||'{}')))}catch{setAppearance(defaults)}
  if(pending){queue(pending);setLoaded(true);}
  else fetch('/api/v1/me/preferences').then(r=>r.ok?r.json():{}).then((p:any)=>{if(active&&edits.current===0&&p.appearance)setAppearance(mergeAppearance(p.appearance))}).catch(()=>{}).finally(()=>{if(active)setLoaded(true)});
  return()=>{active=false;alive.current=false};
 },[user]);
 useEffect(()=>{const media=matchMedia('(prefers-color-scheme: dark)');const apply=()=>{
  const dark=appearance.theme==='dark'||(appearance.theme==='system'&&media.matches);document.documentElement.dataset.theme=dark?'dark':'light';
  // Every color below resolves from the scheme that is currently on screen; flipping
  // the theme (manually or via the system) re-runs apply and swaps the whole set.
  const t=dark?appearance.dark:appearance.light;
  const accent=resolveAccent(dark?'dark':'light',t.accent,t.preset);
  document.documentElement.style.setProperty('--accent',accent);document.documentElement.style.setProperty('--editor-size',appearance.fontSize+'px');document.documentElement.style.setProperty('--line-width',appearance.lineWidth+'px');
  // Whole-app UI zoom: root `zoom` scales every layer; --ui-zoom lets viewport-unit sizes divide back (see styles.css).
  document.documentElement.dataset.navLabels=String(appearance.navLabels);const z=(appearance.zoom||100)*1.2;
  document.documentElement.style.setProperty('--ui-zoom',String(z/100));
  document.documentElement.style.setProperty('--document-ui-compensation',String(1/1.2));
  document.documentElement.style.zoom=z===100?'':String(z/100);
  // Markdown colors always follow the scheme on screen: the user's pick when set, else the
  // built-in palette for this scheme (dark schemes get lighter hues, light schemes deep ones).
  const pal=paletteDefaults[dark?'dark':'light'];
  const md=(attr:string,varName:string,v:string,d:string)=>{document.documentElement.style.setProperty(varName,isHex(v)?v:d);document.documentElement.setAttribute(attr,'')};
  md('data-ws-hcolor','--ws-hcolor',t.hColor,pal.hColor);md('data-ws-linkcolor','--ws-linkcolor',t.linkColor,pal.linkColor);md('data-ws-quotecolor','--ws-quotecolor',t.quoteColor,pal.quoteColor);md('data-ws-codecolor','--ws-codecolor',t.codeColor,pal.codeColor);
  // Memos inherit the app accent for the scheme on screen; an explicit memo
  // color pick overrides it, and Reset returns to inheriting.
  const memo=resolveMemoAccent(dark?'dark':'light',t.memoColor,accent);document.documentElement.style.setProperty('--memo-accent',memo.accent);document.documentElement.style.setProperty('--memo-accent-text',memo.text);
 };apply();media.addEventListener('change',apply);return()=>media.removeEventListener('change',apply)},[appearance]);
 const update=(patch:Partial<Appearance>)=>{edits.current++;const next={...appearance,...patch};setAppearance(next);try{localStorage.setItem(cacheKey,JSON.stringify(next));localStorage.setItem(pendingKey,JSON.stringify(next))}catch{}queue(next)};
 return {appearance,update,loaded};
}
export function AppearancePanel({value,onChange}:{value:Appearance;onChange:(v:Partial<Appearance>)=>void}){
 // Which scheme's custom colors the pickers edit — dark and light are stored separately.
 const [scheme,setScheme]=useState<'dark'|'light'>(()=>document.documentElement.dataset.theme==='light'?'light':'dark');
 const t=value[scheme];const [hex,setHex]=useState(t.accent);useEffect(()=>setHex(t.accent),[t.accent]);
 const patch=(p:Partial<ThemeColors>)=>onChange({[scheme]:{...t,...p}} as unknown as Partial<Appearance>);
  const md=(lbl:string,key:'hColor'|'linkColor'|'quoteColor'|'codeColor'|'memoColor',fallback=paletteDefaults[scheme][key])=><MarkdownColor key={key} label={lbl} value={t[key]} fallback={fallback} onChange={color=>patch({[key]:color} as Partial<ThemeColors>)}/>;
 return <div className="appearance-panel"><h3>Appearance</h3><label>Theme<select aria-label="Theme" value={value.theme} onChange={e=>onChange({theme:e.target.value as Appearance['theme']})}><option value="dark">Dark</option><option value="light">Light</option><option value="system">Use system setting</option></select></label>
 <div className="scheme-tabs" role="group" aria-label="Color scheme to edit">
  <button aria-pressed={scheme==='dark'} className={scheme==='dark'?'active':''} onClick={()=>setScheme('dark')}><Icon name="moon" size={13}/> Dark colors</button>
  <button aria-pressed={scheme==='light'} className={scheme==='light'?'active':''} onClick={()=>setScheme('light')}><Icon name="sun" size={13}/> Light colors</button>
 </div>
 <p className="muted scheme-note">You are editing the colors used in <b>{scheme}</b> mode. Each mode keeps its own accent and Markdown colors.</p>
 <label>Accent color<div className="accent-controls"><select aria-label="Accent preset" value={t.preset} onChange={e=>{const p=e.target.value as AccentPreset;if(p!=='custom')patch({preset:p,accent:''});else patch({preset:'custom',accent:isHex(t.accent)?t.accent:'#b7a0ff'})}}><option value="default">Blue (default)</option><option value="violet">Violet</option><option value="neutral">Neutral</option><option value="custom">Custom</option></select>
 {t.preset==='custom'&&<><input aria-label="Choose accent color" type="color" value={isHex(t.accent)?t.accent:'#b7a0ff'} onChange={e=>patch({accent:e.target.value,preset:'custom'})}/><input aria-label="Accent hex color" placeholder="#b7a0ff" value={hex} onChange={e=>{setHex(e.target.value);if(/^#[\da-f]{6}$/i.test(e.target.value))patch({accent:e.target.value,preset:'custom'})}}/></>}
 <button className="btn" onClick={()=>patch({preset:'default',accent:''})}>Reset</button></div></label>
 <p className="muted">Headings default to white in dark mode and black in light mode. Neutral keeps the gray accent scheme. Custom colors are kept as saved and adapted only when they fail the readability contrast gate.</p>
 <label>Document font size <span>{value.fontSize}px</span><input aria-label="Document font size" type="range" min="13" max="22" value={value.fontSize} onChange={e=>onChange({fontSize:+e.target.value})}/></label><label>UI scale<div className="zoom-controls"><select aria-label="UI scale" value={value.zoom} onChange={e=>onChange({zoom:Number(e.target.value)})}>{[...new Set([60,70,80,90,100,110,120,125,150,175,200,value.zoom])].sort((a,b)=>a-b).map(scale=><option key={scale} value={scale}>{scale}%</option>)}</select><button className="btn small" disabled={value.zoom===100} onClick={()=>onChange({zoom:100})}>Reset</button></div></label>
 <p className="muted">100% uses the larger default interface. Markdown keeps its existing document scale. Scroll or pinch over a graph to zoom that graph.</p>
 <label>Readable line width <span>{value.lineWidth}px</span><input aria-label="Readable line width" type="range" min="500" max="1000" step="20" value={value.lineWidth} onChange={e=>onChange({lineWidth:+e.target.value})}/></label>
 <label>Label main navigation<input type="checkbox" checked={value.navLabels} onChange={e=>onChange({navLabels:e.target.checked})}/></label><label>Reopen tabs on startup<input aria-label="Reopen tabs on startup" type="checkbox" checked={value.restoreTabs} onChange={e=>onChange({restoreTabs:e.target.checked})}/></label><p className="muted">Remembers open documents, pinned tabs, Graph and Memos per vault. Closing every tab leaves the next session empty. Turn off to start with no tabs.</p><h4>Markdown colors</h4>
  {md('Headings','hColor')}{md('Links','linkColor')}{md('Quotes','quoteColor')}{md('Code','codeColor')}
  <p className="muted">Recolor headings, links, quotes and code in the reader and the visual editor — for this mode only. Defaults adapt to the scheme (dark schemes get lighter hues, light schemes deep ones, links blue); Reset returns each to that default.</p>
   <h4>Memos</h4>
   {md('Memos','memoColor',resolveAccent(scheme,t.accent,t.preset))}
   <p className="muted">The Memos accent follows the app accent for this mode by default. Pick a color to give Memos their own; the on-accent text adapts for readability. Reset returns to the app accent.</p></div>;
}
function MarkdownColor({label,value,fallback,onChange}:{label:string;value:string;fallback:string;onChange:(color:string)=>void}){
 const color=isHex(value)?value:fallback;
 const [hex,setHex]=useState(color);
 const [open,setOpen]=useState(false);
 const [position,setPosition]=useState({left:0,top:0});
 const trigger=useRef<HTMLButtonElement>(null),popup=useRef<HTMLDivElement>(null);
 useEffect(()=>setHex(color),[color]);
 useEffect(()=>{if(!open)return;const close=(e:PointerEvent)=>{if(!popup.current?.contains(e.target as Node)&&!trigger.current?.contains(e.target as Node))setOpen(false)};const escape=(e:KeyboardEvent)=>{if(e.key==='Escape')setOpen(false)};const scroll=()=>setOpen(false);document.addEventListener('pointerdown',close);document.addEventListener('keydown',escape);window.addEventListener('scroll',scroll,true);return()=>{document.removeEventListener('pointerdown',close);document.removeEventListener('keydown',escape);window.removeEventListener('scroll',scroll,true)}},[open]);
 const toggle=()=>{if(open){setOpen(false);return}const rect=trigger.current!.getBoundingClientRect();const width=272,height=220,z=uiZoom();setPosition(overlayPoint(rect.left,rect.top>=height*z+8?rect.top-(height+6)*z:rect.bottom+6*z,width,height));setOpen(true)};
 const choose=(next:string)=>{onChange(next);setOpen(false);trigger.current?.focus()};
 const shades=['#60a5fa','#34d399','#fde047','#fb923c','#f87171','#c084fc','#c4a484','#f5f5f5','#3b82f6','#10b981','#facc15','#f97316','#ef4444','#a855f7','#a88365','#d4d4d4','#2563eb','#059669','#eab308','#ea580c','#dc2626','#9333ea','#806247','#a3a3a3','#1d4ed8','#047857','#ca8a04','#c2410c','#b91c1c','#7e22ce','#654321','#737373'];
 return <div className="md-color"><span>{label}</span><button ref={trigger} type="button" className="md-color-swatch" aria-label={label+' color'} aria-expanded={open} aria-haspopup="dialog" onClick={toggle}><i style={{background:color}}/></button><input className="md-color-hex" aria-label={label+' hex color'} value={hex} maxLength={7} spellCheck={false} onChange={e=>{const next=e.target.value;setHex(next);if(isHex(next))onChange(next)}} onBlur={()=>{if(!isHex(hex))setHex(color)}}/><button className="btn small" disabled={!isHex(value)} onClick={()=>onChange('')}>Reset</button>{open&&createPortal(<div ref={popup} className="md-color-popup" role="dialog" aria-label={label+' color picker'} style={position}><div className="md-color-popup-title">{label} color</div><div className="md-color-palette">{shades.map(shade=><button type="button" key={shade} aria-label={shade} aria-pressed={color.toLowerCase()===shade} style={{background:shade}} onClick={()=>choose(shade)}/>)}</div><div className="md-color-popup-bottom"><span>Custom</span><input aria-label={label+' custom hex color'} value={hex} maxLength={7} spellCheck={false} onChange={e=>{const next=e.target.value;setHex(next);if(isHex(next))onChange(next)}}/><button type="button" className="btn small" disabled={!isHex(hex)} onClick={()=>choose(hex)}>Select</button></div></div>,document.body)}</div>;
}
