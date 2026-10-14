import {codeSourceHighlight} from '../lib/codeSource';
import {useEffect,useRef,useState} from 'react';
import type {DocMeta} from '../api/types';
import {closingBrackets,documentWikilink,linkTrigger,type LinkRequest} from '../lib/documentLinks';
import {DocumentLinkPicker} from './DocumentLinkPicker';
import {LinkWritingHint} from './LinkWritingHint';
import {EditorState,Compartment} from '@codemirror/state';
import {EditorView,keymap,drawSelection,highlightActiveLine} from '@codemirror/view';
import {defaultKeymap,history,historyKeymap,indentWithTab} from '@codemirror/commands';
import {markdown} from '@codemirror/lang-markdown';
import {syntaxTree,syntaxHighlighting,defaultHighlightStyle} from '@codemirror/language';
import {SearchQuery,setSearchQuery,findNext,findPrevious,highlightSelectionMatches,search} from '@codemirror/search';
import type {Draft} from '../lib/drafts';
import {imageFileOk} from '../lib/images';
export function NoteEditor({draft,find,onUploadImages,onImageError,selectTitle,docs=[],notePath=''}:{docs?:DocMeta[];notePath?:string;draft:Draft;find?:{query:string;step:number};onUploadImages?:(files:File[])=>Promise<string[]>;onImageError?:(message:string)=>void;selectTitle?:boolean}){
 const host=useRef<HTMLDivElement>(null);const view=useRef<EditorView | null>(null);const [request,setRequest]=useState<LinkRequest|null>(null);const dismissed=useRef('');
 const upload=useRef(onUploadImages);upload.current=onUploadImages;
 const report=useRef(onImageError);report.current=onImageError;
 const suggest=(v:EditorView,explicit=false)=>{if(!draft.canEdit)return;const selection=v.state.selection.main,text=v.state.doc.toString(),before=text.slice(0,selection.from),node=syntaxTree(v.state).resolveInner(selection.from,-1);const trigger=!explicit&&/Code/.test(node.name)?null:linkTrigger(before,explicit),key=text+'\0'+selection.from;if(!trigger||(!explicit&&dismissed.current===key)){setRequest(null);return}const from=trigger.from,to=selection.to+closingBrackets(text,selection.to),rect=v.coordsAtPos(selection.head)||v.dom.getBoundingClientRect();setRequest({query:trigger.query,manual:explicit,rect,owner:v.contentDOM,close:()=>{dismissed.current=key;setRequest(null)},choose:doc=>{const insert=documentWikilink(doc);v.dispatch({changes:{from,to,insert},selection:{anchor:from+insert.length}});setRequest(null);v.focus()}})};
 useEffect(()=>{
  // Pasting or dropping image files uploads them and inserts markdown embeds;
  // the upload callback owns vault context, naming and error surfacing.
  const insertFiles=(files:File[],at?:number)=>{
   const up=upload.current;
   if(!up||!draft.canEdit){report.current?.('This vault is read-only.');return}
   void up(files).then(paths=>{
    const v=view.current;if(!v||!paths.length)return;
    const md=paths.map(p=>`\n![${p.split('/').pop()??p}](${p})\n`).join('');
    const from=at??v.state.selection.main.head,to=at??v.state.selection.main.to;
    v.dispatch({changes:{from,to,insert:md},selection:{anchor:from+md.length}});
   }).catch(e=>report.current?.(e instanceof Error?e.message:String(e)));
  };
  const editable=new Compartment();
  // Freshly created notes: preselect the heading text so the first keystrokes
  // both name the note and replace the "Untitled" title (Workspace owns the rename).
  const titleSel=selectTitle&&draft.canEdit&&/^#\s+\S/.test(draft.text)?(()=>{const line=draft.text.split('\n')[0];const lead=/^#+\s+/.exec(line)![0].length;return{anchor:lead,head:line.length}})():undefined;
  const v=new EditorView({parent:host.current!,state:EditorState.create({doc:draft.text,selection:titleSel,extensions:[history(),drawSelection(),highlightActiveLine(),markdown(),codeSourceHighlight,search({top:false}),highlightSelectionMatches(),syntaxHighlighting(defaultHighlightStyle),EditorView.lineWrapping,editable.of([EditorState.readOnly.of(!draft.canEdit),EditorView.editable.of(draft.canEdit)]),EditorView.contentAttributes.of({'aria-label':'Markdown document editor',spellcheck:'true'}),keymap.of([{key:'Ctrl-Space',run:v=>{suggest(v,true);return true}},{key:'Mod-Space',run:v=>{suggest(v,true);return true}},{key:'Mod-s',run:()=>{void draft.flush();return true}},indentWithTab,...defaultKeymap,...historyKeymap]),EditorView.updateListener.of(u=>{if(u.docChanged&&u.state.doc.toString()!==draft.text)draft.change(u.state.doc.toString());if(u.docChanged||u.selectionSet)suggest(u.view)}),EditorView.domEventHandlers({paste(event){const files=Array.from(event.clipboardData?.files??[]).filter(imageFileOk);if(!files.length)return false;event.preventDefault();insertFiles(files);return true},drop(event){const files=Array.from(event.dataTransfer?.files??[]).filter(imageFileOk);if(!files.length)return false;event.preventDefault();const at=event.clientX?view.current?.posAtCoords({x:event.clientX,y:event.clientY}):undefined;insertFiles(files,at??undefined);return true}})]})});
  view.current=v;if(titleSel)v.focus();const off=draft.subscribe(()=>{if(v.state.doc.toString()!==draft.text)v.dispatch({changes:{from:0,to:v.state.doc.length,insert:draft.text}})});
  return()=>{off();v.destroy();void draft.flush()};
 },[draft]);
 useEffect(()=>{const v=view.current;if(!v)return;v.dispatch({effects:setSearchQuery.of(new SearchQuery({search:find?.query||'',caseSensitive:false}))});if(find?.query)(find.step<0?findPrevious:findNext)(v)},[find?.query,find?.step,draft]);
 return <div className="note-editor"><LinkWritingHint enabled={draft.canEdit}/><div className="note-editor-host" ref={host}/><DocumentLinkPicker request={request} docs={docs} currentPath={notePath}/></div>;
}
