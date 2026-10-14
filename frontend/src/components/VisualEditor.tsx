import {DocumentTagSummary} from './DocumentTagMenu';
import {codeTokens} from '../lib/codeHighlight';
import {useEffect,useMemo,useRef,useState} from 'react';
import {Editor,rootCtx,defaultValueCtx,editorViewCtx,editorViewOptionsCtx} from '@milkdown/kit/core';
import {commonmark,imageSchema} from '@milkdown/kit/preset/commonmark';
import {gfm} from '@milkdown/kit/preset/gfm';
import {listener,listenerCtx} from '@milkdown/kit/plugin/listener';
import {toggleMark,setBlockType,wrapIn} from '@milkdown/kit/prose/commands';
import {wrapInList,sinkListItem,liftListItem} from '@milkdown/kit/prose/schema-list';
import {undo,redo} from '@milkdown/kit/prose/history';
import {addRowBefore,addRowAfter,addColumnBefore,addColumnAfter,toggleHeaderRow,deleteRow,deleteColumn,deleteTable} from '@milkdown/kit/prose/tables';
 import {Decoration,DecorationSet} from '@milkdown/kit/prose/view';
 import type {EditorView,NodeViewConstructor} from '@milkdown/kit/prose/view';
import type {Node} from '@milkdown/kit/prose/model';
import {Plugin,PluginKey,TextSelection} from '@milkdown/kit/prose/state';
import {$prose,$view,replaceAll} from '@milkdown/kit/utils';
import type {Draft} from '../lib/drafts';
import type {DocMeta} from '../api/types';
import {closingBrackets,documentLinkTarget,linkTrigger,type LinkRequest} from '../lib/documentLinks';
import {LinkWritingHint} from './LinkWritingHint';
import {DocumentLinkPicker} from './DocumentLinkPicker';
import {diagramKind,renderDiagrams} from '../lib/diagrams';
import {imageFileOk} from '../lib/images';
import {makeResolver} from '../lib/markdown';
import {Icon} from './Icon';

// Live diagram preview: a widget decoration below every mermaid/puml code
// block. Widgets are cached per source text so typing elsewhere (or inside
// the block) never re-renders engines; previews stay inert read-only DOM.
const diagramWidgets=new Map<string,HTMLElement>();
function diagramWidget(src:string,kind:string):HTMLElement{
 const key=kind+'\u0000'+src;
 let el=diagramWidgets.get(key);
 if(!el){
  const w=document.createElement('div');w.className='diagram-widget';w.contentEditable='false';
  const pre=document.createElement('pre');const code=document.createElement('code');
  code.className='language-'+kind;code.textContent=src;pre.append(code);w.append(pre);
  diagramWidgets.set(key,w);
  // The widget is only mounted by ProseMirror after this factory returns;
  // renderDiagrams skips detached nodes, so the render waits one task.
  setTimeout(()=>{void renderDiagrams(w).catch(()=>{})},0);
  el=w;
 }
 return el;
}
function diagramDecorations(doc:Node):DecorationSet{
 const out:Decoration[]=[];
 doc.descendants((node,pos)=>{
  if(node.type.name==='code_block'){
   const language=String(node.attrs.language??'');
   for(const token of codeTokens(node.textContent,language))out.push(Decoration.inline(pos+1+token.from,pos+1+token.to,{class:token.className}));
   const kind=diagramKind('language-'+language);
   if(kind)out.push(Decoration.widget(pos+node.nodeSize,diagramWidget(node.textContent,kind),{side:1,stopEvent:()=>true}));
  }
  return true;
 });
 return DecorationSet.create(doc,out);
}
const diagramKey=new PluginKey<DecorationSet>('nsd-diagram');
// Bridge to the live component: image uploads and attachment URL resolution
// arrive from the most recent render, so the plugin/view never close stale props.
const visualHandlers:{onFiles:null|((files:File[])=>void);resolve:(src:string)=>string}={onFiles:null,resolve:src=>src};
function createVisualPlugin(){
 return new Plugin<DecorationSet>({key:diagramKey,
  state:{init:(_config,instance)=>diagramDecorations(instance.doc),apply:(tr,value)=>tr.docChanged?diagramDecorations(tr.doc):value},
  props:{nodeViews:{list_item:taskItemView},decorations:(state)=>diagramKey.getState(state)??DecorationSet.empty,
   handleDOMEvents:{
    paste:(_view,event)=>{const files=Array.from((event as ClipboardEvent).clipboardData?.files??[]).filter(imageFileOk);if(!files.length)return false;event.preventDefault();visualHandlers.onFiles?.(files);return true},
    drop:(_view,event)=>{const files=Array.from((event as DragEvent).dataTransfer?.files??[]).filter(imageFileOk);if(!files.length)return false;event.preventDefault();visualHandlers.onFiles?.(files);return true}}}});
}
// Task controls stay outside the editable content; toggling is an undoable document transaction.
const taskItemView:NodeViewConstructor=(node,view,getPos)=>{
 const dom=document.createElement('li'),contentDOM=document.createElement('div'),checkbox=document.createElement('input');
 contentDOM.className='task-item-body';checkbox.type='checkbox';checkbox.contentEditable='false';
 const sync=()=>{const task=node.attrs.checked!=null;dom.dataset.itemType=task?'task':'list';dom.dataset.checked=String(node.attrs.checked);checkbox.hidden=!task;checkbox.checked=!!node.attrs.checked;checkbox.disabled=!view.editable;checkbox.setAttribute('aria-label',(checkbox.checked?'Mark incomplete: ':'Complete task: ')+node.textContent.slice(0,100))};
 checkbox.addEventListener('change',()=>{const pos=getPos();if(pos===undefined||!view.editable)return;const current=view.state.doc.nodeAt(pos);if(current)view.dispatch(view.state.tr.setNodeMarkup(pos,undefined,{...current.attrs,checked:checkbox.checked}));view.focus()});
 dom.append(checkbox,contentDOM);sync();return {dom,contentDOM,update(next){if(next.type!==node.type)return false;node=next;sync();return true},stopEvent:e=>e.target===checkbox,ignoreMutation:mutation=>mutation.type!=='selection'&&(mutation.target===dom||mutation.target===checkbox)};
};
// Visual images resolve relative vault paths to served attachment URLs; an
// unresolved source shows a dashed broken marker instead of a silent 404.
const visualImageView:NodeViewConstructor=(node)=>{
 const el=document.createElement('img');
 el.className='visual-image';
 const load=()=>{el.alt=String(node.attrs.alt??'');const src=String(node.attrs.src??'');const url=src?visualHandlers.resolve(src):'';el.classList.toggle('visual-image-broken',!url);if(url)el.src=url;else el.removeAttribute('src')};
 load();
 return{dom:el,ignoreMutation:()=>true,
  update(next){if(next.type.name!=='image')return false;node=next;load();return true},
  selectNode(){el.classList.add('visual-image-selected')},
  deselectNode(){el.classList.remove('visual-image-selected')}};
};

// Until custom syntax nodes and localized serialization exist, refuse to
// parse documents where a stock CommonMark/GFM round trip could drop data.
export function visualSafe(text:string):boolean{
 return !(/\r|^\uFEFF|[ \t]+$|^---\n|\[\[|%%|==|^\s*(?:>\s*)?\[![^\]]+\]|^\[\^[^\]]+\]|^\[[^\]]+\]:|\^[\w-]+\s*$|<!--|<\/?[A-Za-z][^>]*>|^\s*:::\w+/m.test(text));
}
// Keep vault metadata and Obsidian links byte-for-byte while Milkdown edits
// the ordinary Markdown body. A missing/changed sentinel blocks the update.
export function prepareVisual(source:string){
 const crlf=source.includes('\r\n');const bom=source.startsWith('\uFEFF')?'\uFEFF':'';
 const normalized=source.slice(bom.length).replace(/\r\n/g,'\n');
 const fm=normalized.startsWith('---\n')?(normalized.match(/^---\n[\s\S]*?^(?:---|\.\.\.)(?:\n|$)/m)?.[0]||''):'';
 const protectedParts:{original:string;label:string;kind:'wiki'|'line';optional?:boolean}[]=[];
 let fenced=false,comment=false;
 const body=normalized.slice(fm.length).split('\n').map((line,index)=>{
  if(/^\s*(```|~~~)/.test(line)){fenced=!fenced;return line}
  if(fenced)return line;
  const unsafe=comment||/[ \t]+$|%%|==|^\s*(?:>\s*)?\[![^\]]+\]|^\[\^[^\]]+\]|^\[[^\]]+\]:|\^[\w-]+\s*$|<!--|<\/?[A-Za-z][^>]*>|^\s*:::\w+/m.test(line);
  if(line.includes('<!--'))comment=true;if(line.includes('-->'))comment=false;
  if(unsafe){const id=protectedParts.push({original:line,label:`Source syntax · line ${index+1}`,kind:'line'})-1;return `[Source syntax · line ${index+1}](#netsidian-protected-${id})`}
  return line.replace(/(!?)\[\[([^\]\n]+)\]\]/g,(whole,_embed:string,target:string)=>{
   const label=(target.split('|').pop()||target).replace(/[\]\\]/g,'');
   const id=protectedParts.push({original:whole,label,kind:'wiki'})-1;
   return `[${label}](#netsidian-protected-${id})`;
  });
 }).join('\n');
 return {safe:true,markdown:body,addWiki:(target:string,label:string)=>{const clean=label.replace(/[\]\\|\n]/g,'');const id=protectedParts.push({original:'[['+target+'|'+clean+']]',label:clean,kind:'wiki',optional:true})-1;return{href:'#netsidian-protected-'+id,label:clean}},restore:(edited:string):string|null=>{
  let next=edited;
  for(let i=0;i<protectedParts.length;i++){
   const part=protectedParts[i],marker='#netsidian-protected-'+i;
   const escaped=marker.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');
   const re=new RegExp('\\[([^\\]\\n]*)\\]\\('+escaped+'\\)','g');
   const matches=[...next.matchAll(re)];
   if(matches.length===0&&part.optional)continue;
   if(matches.length!==1||matches[0][1].replace(/\\([\\`*_{}[\]()#+.!>-])/g,'$1')!==part.label)return null;
   next=next.replace(re,()=>part.original);
  }
  const result=bom+fm+next;return crlf?result.replace(/\n/g,'\r\n'):result;
 }};
}

type PmCommand=(state:any,dispatch?:any,view?:any)=>boolean;
type VisualUi={block:string;inTable:boolean;code:{pos:number;lang:string}|null};
const initialUi:VisualUi={block:'paragraph',inTable:false,code:null};

// Duck-typed view inspection so the toolbar can reflect the current block.
export function computeUi(state:any):VisualUi{
 const $from=state.selection.$from;let inTable=false;let code:VisualUi['code']=null;
 for(let d=$from.depth;d>0;d--){const name=$from.node(d).type.name;
  if(name==='table'||name==='table_row'||name==='table_header_row'||name==='table_cell'||name==='table_header')inTable=true;
  if(name==='code_block')code={pos:$from.before(d),lang:String(($from.node(d).attrs as {language?:string}).language??'')};}
 const parent=$from.parent;const name=parent.type.name;let block='other';
 if(!inTable){
  if(name==='heading')block='h'+Number(parent.attrs.level??1);
  else if(name==='paragraph'||name==='blockquote')block='paragraph';
  else if(name==='code_block')block='code';
 }
 return {block,inTable,code};
}

// Toolbar task-list toggle: give plain bullet items Obsidian checkboxes
// (`- [ ]`), or remove the checkboxes when every item already has one.
export function toggleTaskList(view:EditorView):boolean{
 const {state}=view;const items:{pos:number;node:any}[]=[];
 const {from,to}=state.selection;
 state.doc.nodesBetween(from,to,(node,pos)=>{if(node.type.name==='list_item')items.push({pos,node});return true});
 if(items.length){
  const all=items.every(item=>item.node.attrs.checked!=null);
  let tr=state.tr;let changed=false;
  for(const item of items){const checked=all?null:false;
   if(item.node.attrs.checked!==checked){tr=tr.setNodeMarkup(item.pos,undefined,{...item.node.attrs,checked});changed=true}}
  if(changed)view.dispatch(tr.scrollIntoView());return changed;
 }
 const list=state.schema.nodes.bullet_list;
 if(!list||!wrapInList(list)(state,view.dispatch,view))return false;
 const next=view.state;let tr=next.tr;let changed=false;
 next.doc.nodesBetween(next.selection.from,next.selection.to,(node,pos)=>{
  if(node.type.name==='list_item'&&node.attrs.checked==null){tr=tr.setNodeMarkup(pos,undefined,{...node.attrs,checked:false});changed=true}});
 if(changed)view.dispatch(tr.scrollIntoView());return true;
}

// Move the top-level block containing the selection between its siblings.
export function moveBlock(view:EditorView,dir:-1|1):boolean{
 const {state}=view;const $from=state.selection.$from;
 if($from.depth===0||state.doc.childCount<2)return false;
 const i=$from.index(0);const t=i+dir;
 if(i<0||i>=state.doc.childCount||t<0||t>=state.doc.childCount)return false;
 const kids:Node[]=[];const sizes:number[]=[];
 state.doc.forEach(n=>{sizes.push(n.nodeSize);kids.push(n)});
 const a=sizes.slice(0,i).reduce((x,y)=>x+y,0);const node=kids[i];
 kids.splice(t,0,kids.splice(i,1)[0]);
 const newStart=1+sizes.slice(0,t).reduce((x,y)=>x+y,0);
 const offset=Math.max(0,Math.min($from.pos-(1+a),node.nodeSize-2));
 const tr=state.tr.replaceWith(0,state.doc.content.size,kids);
 view.dispatch(tr.setSelection(TextSelection.create(tr.doc,newStart+1+offset)).scrollIntoView());return true;
}

export function convertList(view:EditorView,target:'bullet_list'|'ordered_list'):boolean{
 const {state}=view;const {$from}=view.state.selection;
 let depth=-1;for(let d=$from.depth;d>0;d--){const n=$from.node(d);if(n.type.name==='bullet_list'||n.type.name==='ordered_list'){depth=d;break}}
 if(depth<0)return false;
 const list=$from.node(depth);const start=$from.before(depth),end=$from.after(depth);
 if(list.type.name===target){
  const rep:Node[]=[];list.forEach(item=>{item.forEach(ch=>rep.push(ch))});
  if(!rep.length)return false;
  view.dispatch(state.tr.replaceWith(start,end,rep).scrollIntoView());return true;
 }
 view.dispatch(state.tr.setNodeMarkup(start,state.schema.nodes[target],null).scrollIntoView());
 return true;
}

export function insertMarkdownTable(view:EditorView,rows=3,cols=3):boolean{
 const s=view.state.schema.nodes;
 if(!s.table||!s.table_row||!s.table_header_row||!s.table_cell||!s.table_header||!view.state.schema.nodes.paragraph)return false;
 const para=()=>view.state.schema.nodes.paragraph.create();
 const header=s.table_header_row.create(null,Array.from({length:cols},()=>s.table_header.create(null,para())));
 const body=Array.from({length:Math.max(1,rows-1)},()=>s.table_row.create(null,Array.from({length:cols},()=>s.table_cell.create(null,para()))));
 view.dispatch(view.state.tr.replaceSelectionWith(s.table.create(null,[header,...body])).scrollIntoView());
 view.focus();return true;
}

type Action='undo'|'redo'|'bold'|'italic'|'strike'|'code'|'clear'|'quote'|'rule'|'bullet'|'numbered'|'task'|'indent'|'outdent'|'up'|'down'|'codeBlock';
type TableAction='add'|'rowBefore'|'rowAfter'|'colBefore'|'colAfter'|'header'|'deleteRow'|'deleteCol'|'delete';
const LINK_URL=/^(?:https?:\/\/\S+|\/?[^:/?#\s]+(?:[/?#]\S*)?)$/i;
export function linkHrefAllowed(href:string):boolean{return LINK_URL.test(href)}

export function VisualEditor({compact=false,draft,onTagSearch,onSourceMode,onUploadImages,onUploaded,attachments,notePath,docs=[]}:{compact?:boolean;docs?:DocMeta[];draft:Draft;onTagSearch?:(tag:string)=>void;onSourceMode?:()=>void;onUploadImages?:(files:File[])=>Promise<string[]>;onUploaded?:()=>void;attachments?:{path:string;url:string}[];notePath?:string}){
 const host=useRef<HTMLDivElement>(null),editor=useRef<Editor|null>(null),muting=useRef(false),imagesInput=useRef<HTMLInputElement>(null);
 const [ready,setReady]=useState(false),[error,setError]=useState('');
 const [ui,setUi]=useState<VisualUi>(initialUi);
 const [linkEdit,setLinkEdit]=useState<{from:number;to:number;text:string;original:string;href:string}|null>(null);
 const prepared=useMemo(()=>prepareVisual(draft.text),[draft]);const preparedRef=useRef(prepared),lastDraft=useRef(draft.text),lastMarkdown=useRef(prepared.markdown);const [request,setRequest]=useState<LinkRequest|null>(null);const dismissed=useRef('');const suggestRef=useRef<(v:EditorView,explicit?:boolean)=>void>(()=>{});
 const safe=prepared.safe;
 const view=():EditorView|null=>{const instance=editor.current;return instance?instance.action((ctx:any)=>ctx.get(editorViewCtx)):null};
 const sync=(v:EditorView|null)=>{if(v)setUi(computeUi(v.state))};
 const suggest=(v:EditorView,explicit=false)=>{if(!draft.canEdit)return;const {$from,from,to}=v.state.selection;if(!explicit&&($from.parent.type.spec.code||$from.marks().some(m=>m.type.name==='code'))){setRequest(null);return}const before=$from.parent.textBetween(0,$from.parentOffset,'','\ufffc'),trigger=linkTrigger(before,explicit),key=before+'\0'+from;if(!trigger||(!explicit&&dismissed.current===key)){setRequest(null);return}const start=from-(before.length-trigger.from),after=$from.parent.textBetween($from.parentOffset,$from.parent.content.size,'','\ufffc'),end=to+closingBrackets(after,0),rect=v.coordsAtPos(from);setRequest({query:trigger.query,manual:explicit,rect,owner:v.dom,close:()=>{dismissed.current=key;setRequest(null)},choose:doc=>{const link=v.state.schema.marks.link;if(!link)return;const target=documentLinkTarget(doc),label=doc.title||doc.path.split('/').pop()!.replace(/\.md$/i,''),part=preparedRef.current.addWiki(target,label);v.dispatch(v.state.tr.replaceWith(start,end,v.state.schema.text(part.label,[link.create({href:part.href})])).scrollIntoView());setRequest(null);v.focus()}})};
 suggestRef.current=suggest;
 useEffect(()=>{if(!safe||!host.current)return;let active=true;preparedRef.current=prepared;lastDraft.current=draft.text;lastMarkdown.current=prepared.markdown;setReady(false);setError('');setUi(initialUi);
  const instance=Editor.make().config((ctx:any)=>{
   ctx.set(rootCtx,host.current!);ctx.set(defaultValueCtx,prepared.markdown);
   ctx.update(editorViewOptionsCtx,(prev:any)=>({...prev,editable:()=>draft.canEdit}));
   ctx.get(listenerCtx).markdownUpdated((_ctx:any,markdown:string,previous:string)=>{
    if(!active||muting.current||markdown===previous||markdown===draft.text)return;
    const restored=preparedRef.current.restore(markdown);if(restored===null){setError('A protected vault link changed. Undo that edit or use Source mode.');return}
    lastDraft.current=restored;lastMarkdown.current=markdown;draft.change(restored);
   });
   ctx.get(listenerCtx).selectionUpdated((_ctx:any)=>{if(active)sync(view())});
  }).use(commonmark).use(gfm).use(listener).use($prose(()=>new Plugin({props:{handleKeyDown:(v,event)=>{if((event.ctrlKey||event.metaKey)&&event.code==='Space'){event.preventDefault();suggestRef.current(v,true);return true}return false}},view:()=>({update:(v,previous)=>{if(!v.state.doc.eq(previous.doc)||!v.state.selection.eq(previous.selection))suggestRef.current(v)}})}))).use($prose(()=>createVisualPlugin())).use($view(imageSchema.node,()=>visualImageView));
  instance.create().then(()=>{if(!active){void instance.destroy();return}editor.current=instance;setReady(true)}).catch(e=>{if(active)setError(String(e))});
  const off=draft.subscribe(()=>{if(!active||!editor.current)return;
   if(draft.text===lastDraft.current)return;const next=prepareVisual(draft.text);preparedRef.current=next;lastDraft.current=draft.text;if(next.markdown===lastMarkdown.current)return;const current=view(),anchor=current?.state.selection.from||1;muting.current=true;try{editor.current.action(replaceAll(next.markdown));const updated=view();if(updated)updated.dispatch(updated.state.tr.setSelection(TextSelection.near(updated.state.doc.resolve(Math.min(anchor,updated.state.doc.content.size)))));lastMarkdown.current=next.markdown}finally{muting.current=false}
  });
  return()=>{active=false;off();editor.current=null;setLinkEdit(null);setRequest(null);void instance.destroy()};
 },[draft,safe,prepared]);
 // Fresh resolver per attachment-list change: relative markdown image paths
 // resolve exactly like the read view (folder-relative, then fuzzy basename).
 useEffect(()=>{
  const list=attachments??[];
  const resolver=makeResolver([],notePath||'',list);
  visualHandlers.resolve=src=>{if(!src||/^(https?:|data:|blob:)/i.test(src))return src;return resolver(decodeURI(src.split('#')[0])).attach??''};
  return()=>{visualHandlers.resolve=src=>src};
 },[attachments,notePath]);
 function insertImages(paths:string[]){const instance=editor.current;if(!instance||!paths.length)return;
  void instance.action((ctx:any)=>{const v=ctx.get(editorViewCtx) as EditorView;const img=v.state.schema.nodes.image;if(!img)return;
   let tr=v.state.tr;for(const p of paths)tr=tr.replaceSelectionWith(img.create({src:p,alt:p.split('/').pop()??p}),false);
   v.dispatch(tr.scrollIntoView());v.focus();});
 }
 function handleFiles(files:File[]){
  if(!draft.canEdit){setError('This vault is read-only.');return}
  if(!onUploadImages){setError('Image upload is not available here.');return}
  void onUploadImages(files).then(paths=>{if(paths.length){insertImages(paths);onUploaded?.()}}).catch(e=>setError(e instanceof Error?e.message:String(e)));
 }
 useEffect(()=>{visualHandlers.onFiles=files=>handleFiles(files);return()=>{visualHandlers.onFiles=null}});
 function run(action:Action){const instance=editor.current;if(!instance||!draft.canEdit)return;
  instance.action((ctx:any)=>{const v=ctx.get(editorViewCtx) as EditorView;const s=v.state;const {nodes,marks}=s.schema;let handled=false;
   const exec=(cmd:PmCommand)=>{cmd(s,v.dispatch,v)};
   switch(action){
    case 'undo':exec(undo);handled=true;break;
    case 'redo':exec(redo);handled=true;break;
    case 'bold':if(marks.strong){exec(toggleMark(marks.strong));handled=true}break;
    case 'italic':if(marks.emphasis){exec(toggleMark(marks.emphasis));handled=true}break;
    case 'strike':if(marks.strike_through){exec(toggleMark(marks.strike_through));handled=true}break;
    case 'code':if(marks.inlineCode){exec(toggleMark(marks.inlineCode));handled=true}break;
    case 'clear':{const {from,to}=s.selection;if(to>from){v.dispatch(s.tr.removeMark(from,to,null).setStoredMarks([]).scrollIntoView());handled=true}break}
    case 'quote':if(nodes.blockquote){exec(wrapIn(nodes.blockquote));handled=true}break;
    case 'rule':if(nodes.hr){v.dispatch(s.tr.replaceSelectionWith(nodes.hr.create()).scrollIntoView());handled=true}break;
     case 'bullet':if(convertList(v,'bullet_list')){handled=true}else if(nodes.bullet_list){exec(wrapInList(nodes.bullet_list));handled=true}break;
     case 'numbered':if(convertList(v,'ordered_list')){handled=true}else if(nodes.ordered_list){exec(wrapInList(nodes.ordered_list));handled=true}break;
    case 'task':handled=toggleTaskList(v);break;
    case 'indent':if(nodes.list_item){exec(sinkListItem(nodes.list_item));handled=true}break;
    case 'outdent':if(nodes.list_item){exec(liftListItem(nodes.list_item));handled=true}break;
    case 'up':handled=moveBlock(v,-1);break;
    case 'down':handled=moveBlock(v,1);break;
    case 'codeBlock':if(nodes.code_block){exec(setBlockType(nodes.code_block));handled=true}break;
   }
   if(handled){v.focus();sync(v)}
  });
 }
 function runTable(action:TableAction){const v=view();if(!v||!draft.canEdit)return;const s=v.state;
  if(action==='add'){if(insertMarkdownTable(v))sync(v);return}
  const cmd=action==='rowBefore'?addRowBefore:action==='rowAfter'?addRowAfter:action==='colBefore'?addColumnBefore:action==='colAfter'?addColumnAfter:action==='header'?toggleHeaderRow:action==='deleteRow'?deleteRow:action==='deleteCol'?deleteColumn:deleteTable;
  if(cmd(s,v.dispatch,v)){v.focus();sync(v)}
 }
 function setBlock(value:string){const v=view();if(!v||!draft.canEdit)return;const nodes=v.state.schema.nodes;
  if(value==='paragraph'&&nodes.paragraph)setBlockType(nodes.paragraph)(v.state,v.dispatch,v);
  else if(/^h[1-6]$/.test(value)&&nodes.heading)setBlockType(nodes.heading,{level:Number(value.slice(1))})(v.state,v.dispatch,v);
  v.focus();sync(v);
 }
 function setCodeLang(language:string){const v=view();const code=ui.code;if(!v||!draft.canEdit||!code)return;const node=v.state.doc.nodeAt(code.pos);
  if(!node||node.type.name!=='code_block')return;
  v.dispatch(v.state.tr.setNodeMarkup(code.pos,undefined,{...node.attrs,language}).scrollIntoView());v.focus();sync(v);
 }
function expandMark(s:any,link:any,from:number,to:number):{from:number;to:number;href:string}{
 const has=(f:number,t:number)=>s.doc.rangeHasMark(f,t,link);
 let f=from,t=to,href='';
 const probeHref=(p:number)=>{const ms=s.doc.resolve(Math.min(Math.max(p,0),s.doc.content.size)).marks();const hit=ms.find((m:any)=>m.type.name===link.name);return hit?String(hit.attrs.href??''):'';};
 if(from===to){href=probeHref(from)||(from<s.doc.content.size?probeHref(from+1):'')||(from>0?probeHref(from-1):'');
  if(href){f=from;t=from;while(f>0&&has(f-1,f))f--;while(t<s.doc.content.size&&has(t,t+1))t++;}}
 else if(has(from,to)){href=probeHref(from);while(f>0&&has(f-1,f))f--;while(t<s.doc.content.size&&has(t,t+1))t++;}
 return {from:f,to:t,href};
}

function openLink(){const v=view();if(!v||!draft.canEdit)return;const s=v.state;const link=s.schema.marks.link;if(!link)return;
  const r=expandMark(s as never,link as never,s.selection.from,s.selection.to);
  setLinkEdit({from:r.from,to:r.to,text:r.from<r.to?s.doc.textBetween(r.from,r.to,'\n'):'',original:r.from<r.to?s.doc.textBetween(r.from,r.to,'\n'):'',href:r.href});
}
 function applyLink(){const v=view();const edit=linkEdit;if(!v||!edit||!draft.canEdit)return;const link=v.state.schema.marks.link;if(!link)return;
  if(!linkHrefAllowed(edit.href)){setError('Use a web URL or vault-relative path.');return}
  const tr=v.state.tr;const {from,to,text,original,href}=edit;
  if(from===to){if(!text)return;tr.insert(from,v.state.schema.text(text,[link.create({href})]))}
  else if(text!==original){if(!text)return;tr.replaceWith(from,to,v.state.schema.text(text,[link.create({href})]))}
  else tr.addMark(from,to,link.create({href}));
  v.dispatch(tr.scrollIntoView());setLinkEdit(null);setError('');v.focus();
 }
 function removeLink(){const v=view();const edit=linkEdit;if(!v||!edit||!draft.canEdit)return;const link=v.state.schema.marks.link;if(!link)return;
  if(edit.from===edit.to){setLinkEdit(null);return}
  v.dispatch(v.state.tr.removeMark(edit.from,edit.to,link).scrollIntoView());setLinkEdit(null);v.focus();
 }
 if(!safe)return <div className="visual-unavailable" role="status"><h3>Visual editing is unavailable for this document</h3><p>This document uses syntax the visual editor cannot yet preserve. Switch to Source to edit it safely.</p>{onSourceMode&&<button className="btn" onClick={onSourceMode}>Edit as Markdown source</button>}</div>;
 const dis=!ready||!draft.canEdit;
 const btn=(id:Action,label:string,icon:string,extra?:{disabled?:boolean})=><button key={id} title={label} aria-label={label} disabled={dis||extra?.disabled} onMouseDown={e=>e.preventDefault()} onClick={()=>run(id)}><Icon name={icon}/></button>;
 const tableBtn=(id:TableAction,label:string,disabled:boolean)=>{const icons:Record<TableAction,string>={add:'table',rowBefore:'up',rowAfter:'down',colBefore:'arrow',colAfter:'arrowR',header:'table',deleteRow:'minus',deleteCol:'close',delete:'trash'};
  return <button key={id} role="menuitem" title={label} disabled={dis||disabled} onMouseDown={e=>e.preventDefault()} onClick={e=>{runTable(id);e.currentTarget.closest('details')?.removeAttribute('open')}}><Icon name={icons[id]}/>{label}</button>};
 const blockValue=/^h[1-6]$|^paragraph$/.test(ui.block)?ui.block:'other';
 const advancedTools=<>
  <div className="visual-tool-group"><select aria-label="Block style" title="Block style" disabled={dis||ui.inTable||ui.code!==null} value={blockValue} onChange={e=>setBlock(e.target.value)}>{blockValue==='other'&&<option value="other">Other</option>}<option value="paragraph">Paragraph</option><option value="h1">Heading 1</option><option value="h2">Heading 2</option><option value="h3">Heading 3</option><option value="h4">Heading 4</option><option value="h5">Heading 5</option><option value="h6">Heading 6</option></select>{btn('quote','Block quote','quote')}{btn('codeBlock','Code block','braces')}{btn('rule','Horizontal rule','minus')}{onUploadImages&&<><button title="Insert image" aria-label="Insert image" disabled={dis} onMouseDown={e=>e.preventDefault()} onClick={()=>imagesInput.current?.click()}><Icon name="upload"/></button><input ref={imagesInput} type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml" multiple hidden onChange={e=>{const files=Array.from(e.currentTarget.files??[]);if(files.length)handleFiles(files);e.currentTarget.value=''}}/></>}</div>
  <div className="visual-tool-group">{btn('bold','Bold','bold')}{btn('italic','Italic','italic')}{btn('strike','Strikethrough','strike')}{btn('code','Inline code','code')}{btn('clear','Clear formatting','eraser')}</div>
  <div className="visual-tool-group">{btn('bullet','Bulleted list','bullets')}{btn('numbered','Numbered list','numbered')}{btn('task','Task list','check')}{btn('indent','Indent','indent')}{btn('outdent','Outdent','outdent')}{btn('up','Move block up','up')}{btn('down','Move block down','down')}</div>
  <div className="visual-tool-group"><button title="Insert document link" aria-label="Insert document link" disabled={dis} onMouseDown={e=>e.preventDefault()} onClick={()=>{const v=view();if(v)suggest(v,true)}}><Icon name="fileplus"/></button><button title="Link" aria-label="Link" disabled={dis} onMouseDown={e=>e.preventDefault()} onClick={openLink}><Icon name="link"/></button></div>
  {ui.code&&<div className="visual-tool-group"><select aria-label="Code block language" title="Code block language" disabled={dis} value={ui.code.lang} onChange={e=>setCodeLang(e.target.value)}><option value="">Plain</option><option value="js">JavaScript</option><option value="ts">TypeScript</option><option value="json">JSON</option><option value="html">HTML</option><option value="css">CSS</option><option value="bash">Bash</option><option value="py">Python</option><option value="go">Go</option><option value="sql">SQL</option><option value="yaml">YAML</option><option value="c">C</option><option value="cpp">C++</option><option value="java">Java</option><option value="csharp">C#</option><option value="toml">TOML</option><option value="config">Config / INI</option><option value="properties">Properties</option><option value="md">Markdown</option><option value="mermaid">Mermaid</option><option value="plantuml">PlantUML</option></select></div>}
  <details className="visual-more"><summary title="Tables">Table <Icon name="chevron" size={13}/></summary><div role="menu">{tableBtn('add','Insert 3×3 table',false)}<hr className="visual-menu-sep"/>{tableBtn('rowBefore','Add row above',!ui.inTable)}{tableBtn('rowAfter','Add row below',!ui.inTable)}{tableBtn('colBefore','Add column before',!ui.inTable)}{tableBtn('colAfter','Add column after',!ui.inTable)}{tableBtn('header','Toggle header row',!ui.inTable)}{tableBtn('deleteRow','Delete row',!ui.inTable)}{tableBtn('deleteCol','Delete column',!ui.inTable)}{tableBtn('delete','Delete table',!ui.inTable)}</div></details>
 </>;
 return <div className="visual-editor"><div className="visual-toolbar" role="toolbar" aria-label="Visual Markdown formatting">
  <div className="visual-tool-group">{btn('undo','Undo','undoArrow')}{btn('redo','Redo','redoArrow')}</div>
  {compact&&<div className="visual-tool-group">{btn('bold','Bold','bold')}{btn('italic','Italic','italic')}{btn('task','Task list','check')}</div>}
  {compact?<details className="visual-format-more"><summary title="More formatting" aria-label="More formatting"><Icon name="more"/></summary><div className="visual-format-panel" role="group" aria-label="Additional formatting">{advancedTools}</div></details>:advancedTools}
  {onSourceMode&&<button className="visual-source-toggle" title="Edit as Markdown source" aria-label="Edit as Markdown source" disabled={!draft.canEdit} onClick={onSourceMode}><Icon name="code" size={14}/><span>Source</span></button>}
 </div>
 {linkEdit&&<div className="visual-dialog" role="dialog" aria-label="Edit link">
  <input aria-label="Link text" placeholder="Text" value={linkEdit.text} onChange={e=>setLinkEdit({...linkEdit,text:e.target.value})}/>
  <input aria-label="Link URL" placeholder="https://… or vault/path" value={linkEdit.href} onChange={e=>setLinkEdit({...linkEdit,href:e.target.value})} onKeyDown={e=>{if(e.key==='Enter')applyLink();if(e.key==='Escape')setLinkEdit(null)}}/>
  <button onClick={applyLink}>Save</button>{linkEdit.href&&<button onClick={removeLink}>Remove link</button>}<button onClick={()=>setLinkEdit(null)}>Cancel</button>
 </div>}
 {error&&<p role="alert" className="visual-error">{error}</p>}<LinkWritingHint enabled={draft.canEdit}/><div className="visual-content" aria-label="Visual Markdown editor">{onTagSearch&&<DocumentTagSummary text={draft.text} onSearch={onTagSearch}/>}<div className="visual-editor-host" ref={host}/></div>{!ready&&!error&&<p className="visual-loading">Loading Visual editor…</p>}<DocumentLinkPicker request={request} docs={docs} currentPath={notePath}/></div>;
}
