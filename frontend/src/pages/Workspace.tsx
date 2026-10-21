import {importRelativePath,unusedImportPath} from '../lib/fileImports';
import {attachmentTabId,boardTabId,mapTabId,moveMapTabs,moveAttachmentTabs,moveBoardTabs,removePathTabs,workspaceTabURL} from '../lib/workspaceTabs';
import {AppLogo} from '../components/AppLogo';
import {useCallback,useEffect,useMemo,useLayoutEffect,useRef,useState,lazy,Suspense,startTransition} from 'react';
import {useLocation,useNavigate,useParams} from 'react-router-dom';
import * as api from '../api/client';
import type {DocMeta,NoteContent,SearchHit,Backlink,RevisionMeta,WorkspaceState} from '../api/types';
import {useApp} from '../AppState';
import {fitWorkspacePanes,readSession,sessionKey,type WorkspaceSession} from '../lib/workspaceState';
import {overlayPoint,uiZoom} from '../lib/overlayPosition';
import {Draft,loadSharedDraft} from '../lib/drafts';
import {headingId,makeResolver,renderInto} from '../lib/markdown';
import {imageFileOk,uploadImage} from '../lib/images';
import {TextFileEditor} from '../components/TextFileEditor';
import {Icon,Tool} from '../components/Icon';
import {FileTree, PREVIEWABLE, DRAG_KEY, parseDragPayload, type Selection, type DragPayload} from '../components/FileTree';
import {AttachmentPane} from '../components/AttachmentPane';
import {extractPdfText,buildPdfMarkdown,readPdfBytes} from '../lib/pdfText';
import {DocumentTagMenu,DocumentTagSummary} from '../components/DocumentTagMenu';
import {DocumentPath} from '../components/DocumentPath';
import {rankLinkDocuments,repairMissingWikilink,rewriteMovedLinks} from '../lib/documentLinks';
import {NoteEditor} from '../components/NoteEditor';
const LazyGraph2 = lazy(()=>import('../components/Graph2'));
function Graph2(props:{minimapDefault?:boolean;space:string;root?:string;onOpen:(id:string)=>void;epoch?:number}){return <Suspense fallback={<p className="muted">Loading graph…</p>}><LazyGraph2 {...props}/></Suspense>}

const LazyVisualEditor=lazy(()=>import('../components/VisualEditor').then(m=>({default:m.VisualEditor})));
import {StoragePanel} from '../components/StoragePanel';
import {AccountMenu} from '../components/AccountMenu';
import {Dialog} from '../components/Dialog';
import {FolderPicker} from '../components/FolderPicker';
import {AppearancePanel,useAppearance} from '../components/Appearance';
import {InteractionPanel,useInteraction,type DocMode} from '../components/Interaction';
import SettingsPage from './SettingsPage';
import {VaultSharing} from '../components/VaultSharing';
import MemosPage from './MemosPage';
import BoardsPage from './BoardsPage';
import MindMapsPage from './MindMapsPage';
import {CreateBoardDialog} from '../components/CreateBoardDialog';
import {CreateMapDialog} from '../components/CreateMapDialog';
import {memoMarkdown} from '../lib/memos';
import {MindMapView} from '../components/MindMapView';
import {flushMindMapDraft,flushMindMapDrafts} from '../lib/mindmapDraft';
import {BoardView} from '../components/BoardView';
import {MoveToBoardDialog} from '../components/MoveToBoardDialog';

  type Tab={id:string;path:string;space:string;pinned?:boolean;transient?:boolean;att?:string;board?:string;map?:string};
type Prompt={title:string;label:string;value:string;dir?:string;confirm?:boolean;yes?:string;no?:string;action:(s:string)=>Promise<void>};
const defaultPaneWidths={sidebar:312,inspector:340};
const title=(path:string)=>path.split('/').pop()?.replace(/\.md$/i,'')||'Untitled';
const msg=(e:unknown)=>e instanceof Error?e.message:String(e);
function download(path:string,text:string){const url=URL.createObjectURL(new Blob([text],{type:'text/markdown;charset=utf-8'}));const a=document.createElement('a');a.href=url;a.download=path.split('/').pop()||'document.md';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
export default function Workspace(){
 const {spaceId=''}=useParams();const loc=useLocation(),navigate=useNavigate();const {spaces,me,settings,refresh,signOut,spacesError}=useApp();
  const {appearance,update,loaded:appearanceLoaded}=useAppearance(me?.userId||'local');
  const {interaction,update:updateInteraction}=useInteraction(me?.userId||'local');
   const attMatch=loc.pathname.match(/\/a\/(.+)$/);
   let activeAtt='';try{activeAtt=attMatch?decodeURIComponent(attMatch[1]):''}catch{/* Invalid links stay in the workspace. */}
   const boardMatch=loc.pathname.match(/\/b\/(.+)$/);
   let activeBoard='';try{activeBoard=boardMatch?decodeURIComponent(boardMatch[1]):''}catch{/* Invalid board links stay in the workspace. */}
   const mapMatch=loc.pathname.match(/\/m\/(.+)$/);
   let activeMap='';try{activeMap=mapMatch?decodeURIComponent(mapMatch[1]):''}catch{}
   const activeId=loc.pathname.match(/\/d\/([^/]+)/)?.[1]||(activeAtt?attachmentTabId(activeAtt):activeBoard?boardTabId(activeBoard):activeMap?mapTabId(activeMap):'');
  const page=activeBoard||activeMap?'document':loc.pathname.endsWith('/graph2')?'graph2':loc.pathname.endsWith('/memos')?'memos':loc.pathname.endsWith('/graph')?'graph2':loc.pathname.endsWith('/boards')?'boards':loc.pathname.endsWith('/maps')?'maps':/\/settings(?:\/mcp)?$/.test(loc.pathname)?'settings':'document';
  const [toolTabs,setToolTabs]=useState<{space:string;page:'memos'|'graph2'|'boards'|'maps'}[]>([]);
  useEffect(()=>{if(page==='memos'||page==='graph2'||(page==='boards'||page==='maps'))setToolTabs(old=>old.some(t=>t.space===spaceId&&t.page===page)?old:[...old,{space:spaceId,page}])},[page,spaceId]);
 const [docs,setDocs]=useState<DocMeta[]>([]),[docsLoaded,setDocsLoaded]=useState(false),[explorerLoaded,setExplorerLoaded]=useState(false),[tabs,setTabs]=useState<Tab[]>([]),[loading,setLoading]=useState(false),[error,setError]=useState('');
 const [vaultLoad,setVaultLoad]=useState<api.VaultLoad|null>(null),[loadRetry,setLoadRetry]=useState(0),[listProgress,setListProgress]=useState<{done:number;total:number}|null>(null);
 const vaultIndexing=!!vaultLoad&&['queued','scanning','indexing','resolving'].includes(vaultLoad.phase);
 const [sidebar,setSidebar]=useState(!loc.pathname.endsWith('/graph2')&&!loc.pathname.endsWith('/memos')),[nav,setNav]=useState<'files'|'search'|'bookmarks'|'tags'|'trash'>('files');
 const [folder,setFolder]=useState(''),[folders,setFolders]=useState<string[]>([]);
  const [mode,setMode]=useState<'source'|'read'|'split'|'visual'>('read');
  // A document opens in the mode from Settings → Interaction. Switching modes
  // inside an open document is remembered per document for the session, so a
  // preference change never silently drops a reader out of the mode they chose.
  const modeRef=useRef(mode);modeRef.current=mode;
  const explicitModes=useRef(new Map<string,DocMode>());
  const setDocMode=(next:DocMode|((prev:DocMode)=>DocMode))=>{const value=typeof next==='function'?next(modeRef.current):next;if(activeId)explicitModes.current.set(spaceId+':'+activeId,value);setMode(value)};
  // The global shortcut listener is registered once, so it reaches the current
  // closure through a ref instead of capturing the first activeId.
  useEffect(()=>{explicitModes.current.clear()},[me?.userId]);
 const setDocModeRef=useRef(setDocMode);setDocModeRef.current=setDocMode;
  useEffect(()=>{if(page!=='document'||!activeId)return;setMode(explicitModes.current.get(spaceId+':'+activeId)??interaction.defaultDocumentMode)},[activeId,page,spaceId,me?.userId,interaction.defaultDocumentMode]);
 const [wide,setWide]=useState(false);
 const [accountRequest,setAccountRequest]=useState(0);
 const [panel,setPanel]=useState<''|'graph'|'outline'|'backlinks'>('');
 const [quick,setQuick]=useState(false),[quickQuery,setQuickQuery]=useState(''),[quickIndex,setQuickIndex]=useState(0),[quickLimit,setQuickLimit]=useState(100);
 const [prompt,setPrompt]=useState<Prompt|null>(null),[busy,setBusy]=useState(false);
  const [menu,setMenu]=useState<{doc:DocMeta;x:number;y:number}|null>(null);
  const [tabMenu,setTabMenu]=useState<{tab:Tab;x:number;y:number}|null>(null);
  const [tabOverflow,setTabOverflow]=useState(false),[tabListOpen,setTabListOpen]=useState(false),[tabListPos,setTabListPos]=useState({x:200,y:44});
  const tabStrip=useRef<HTMLDivElement|null>(null);
  const [sel,setSel]=useState<Selection>({docs:[],folders:[]});
   const [folderMenu,setFolderMenu]=useState<{path:string;x:number;y:number}|null>(null);
   const [attMenu,setAttMenu]=useState<{path:string;x:number;y:number}|null>(null);
   const [selMenu,setSelMenu]=useState<{x:number;y:number}|null>(null);
   // Board/map creation requested from the file explorer's create menu; the
   // value is the target folder ('' = vault root), null closes the dialog.
   const [createBoardIn,setCreateBoardIn]=useState<string|null>(null);
   const [createMapIn,setCreateMapIn]=useState<string|null>(null);
    const [attView,setAttView]=useState<string|null>(null);
    const [pdfConverting,setPdfConverting]=useState(false);
    const [trashHot,setTrashHot]=useState(false);
 const [fileMoveActive,setFileMoveActive]=useState(false);
 useEffect(()=>{const end=()=>{setFileMoveActive(false);setTrashHot(false)};window.addEventListener('drop',end,true);window.addEventListener('dragend',end,true);return()=>{window.removeEventListener('drop',end,true);window.removeEventListener('dragend',end,true)}},[]);
  const [ftrashes,setFTrashes]=useState<api.FolderTrashEntry[]>([]);
  const [atts,setAtts]=useState<api.AttachmentMeta[]>([]);
  const [impPending,setImpPending]=useState<{items:{target:string;rel:string;file:File;md:boolean}[];skipped:string[];docs:Map<string,DocMeta>;atts:Map<string,api.AttachmentMeta>;embed?:boolean}|null>(null);
 const [query,setQuery]=useState(''),[hits,setHits]=useState<SearchHit[]>([]),[searching,setSearching]=useState(false),[searchError,setSearchError]=useState(''),[searchRetry,setSearchRetry]=useState(0),[searchIndex,setSearchIndex]=useState(0);
 const [treeQuery,setTreeQuery]=useState(''),[treeTool,setTreeTool]=useState<'filter'|'goto'>('filter');
 const [findOpen,setFindOpen]=useState(false),[findQuery,setFindQuery]=useState(''),[findStep,setFindStep]=useState(0),[findCount,setFindCount]=useState(0);
 const findInput=useRef<HTMLInputElement>(null);
 const [tags,setTags]=useState<{tag:string;count:number}[]>([]),[backlinks,setBacklinks]=useState<Backlink[]>([]);
 const [history,setHistory]=useState<RevisionMeta[]|null>(null),[historyText,setHistoryText]=useState<string|null>(null);
 const [conflict,setConflict]=useState<{draft:Draft;text:string;revision:string;base:string}|null>(null);
 const [share,setShare]=useState(false),[agentRequest,setAgentRequest]=useState(0);
  const [graphEpoch,refreshGraph]=useState(0);const [epoch,tick]=useState(0),[notice,setNotice]=useState('');
  const [picker,setPicker]=useState(false);
  const native=settings?.runtime.kind==='desktop';
  const localPick=settings?.runtime.storage.folderPicker==='server';
  const [folderBrowse,setFolderBrowse]=useState(false);
  const [newVault,setNewVault]=useState(false);
  const restore=useRef<{active?:string;openDocs?:string[];session?:WorkspaceSession|null}>({});
  const wsReady=useRef(false);const [workspaceReady,setWorkspaceReady]=useState('');
  const [collapsedFolders,setCollapsedFolders]=useState<string[]>([]);
  const [sortMode,setSortMode]=useState<'name'|'mtime'>('name');
  const [bookmarks,setBookmarks]=useState<string[]>([]);
  const [widths,setWidths]=useState(defaultPaneWidths);const [viewport,setViewport]=useState(innerWidth);
 useEffect(()=>{const resize=()=>setViewport(innerWidth);window.addEventListener('resize',resize);return()=>window.removeEventListener('resize',resize)},[]);
 const paneLayout=fitWorkspacePanes(viewport,appearance.zoom,widths,sidebar,!!panel);
   const showInspector=!!panel&&page==='document'&&!!activeId&&!activeAtt&&!activeBoard&&!activeMap;
  const [revealTick,setRevealTick]=useState(0),[revealDoc,setRevealDoc]=useState('');
  const [help,setHelp]=useState(false);const [missingLink,setMissingLink]=useState(''),[missingSource,setMissingSource]=useState('');const [repairQuery,setRepairQuery]=useState('');
  const navStack=useRef<{i:number;ids:string[]}>({i:-1,ids:[]});
  const closedStack=useRef<Tab[]>([]);
  const tabDrag=useRef<Tab|null>(null);
  const autoName=useRef<{id:string;path:string}|null>(null);
  const previewSequence=useRef(0);
  const drafts=useRef(new Map<string,Draft>()),notes=useRef(new Map<string,NoteContent>()),preview=useRef<HTMLDivElement>(null),files=useRef<HTMLInputElement>(null),mounted=useRef(true);
 const currentSpace=useRef(spaceId);currentSpace.current=spaceId;
  const userRole=spaces.find(s=>s.space.id===spaceId)?.role;const writable=(userRole==='owner'||userRole==='editor')&&!vaultIndexing;
 const writableNow=useRef(writable);writableNow.current=writable;
 const registeredDrafts=useRef(new WeakSet<Draft>());
 const workspaceDraft=useCallback(async(id:string,refresh=false)=>{
  const scope=spaceId;
  const bundle=await loadSharedDraft(drafts.current,scope,id,me?.userId||'local',refresh,async(s,d)=>{
   const n=await api.getDocument(s,d);
   if(!mounted.current||currentSpace.current!==scope)throw new Error('The vault changed while this document was loading.');
   return n;
  });
  if(!mounted.current||currentSpace.current!==scope)throw new Error('The vault changed while this document was loading.');
  notes.current.set(scope+'/'+id,bundle.note);
  bundle.draft.canEdit=bundle.note.canEdit&&writableNow.current;
  if(!registeredDrafts.current.has(bundle.draft)){
   registeredDrafts.current.add(bundle.draft);
   bundle.draft.subscribe(()=>{if(mounted.current)tick(x=>x+1)});
  }
  return bundle;
 },[spaceId,me?.userId]);
 const previewDraft=useCallback((id:string)=>workspaceDraft(id,true),[workspaceDraft]);
 useEffect(()=>{if(me&&spaceId&&!userRole)navigate('/',{replace:true});let changed=false;for(const d of drafts.current.values())if(d.space===spaceId&&d.canEdit!==writable){d.canEdit=writable;changed=true}if(changed)tick(x=>x+1)},[userRole,writable,spaceId,me?.userId]);
  const curName=spaces.find(s=>s.space.id===spaceId)?.space.name||'Vault';
 // Trash only applies to vaults stored in the server-managed local store; folder-attached vaults recycle nothing.
 const externalVault=(spaces.find(s=>s.space.id===spaceId)?.space as {storageBinding?:string}|undefined)?.storageBinding?.startsWith('local-folder:')??false;
  const key=spaceId+'/'+activeId,draft=drafts.current.get(key),note=notes.current.get(key);
  // Folder-backed boards: discovered via /boards, opened as workspace tabs, and
  // moved into with the same draft-flush + If-Match discipline as renames.
   const [boards,setBoards]=useState<api.BoardSummary[]>([]),[boardMove,setBoardMove]=useState<{id:string;path:string;title:string}|null>(null),[reloadTick,setReloadTick]=useState(0);
  // The board list has its own loading/error state so the Boards tab can tell
  // "still fetching", "no boards" and "could not read" apart. A failed refresh
  // keeps the boards already on screen; responses from a previous vault or an
  // superseded request are dropped by the sequence guard.
  const [boardsStatus,setBoardsStatus]=useState<'loading'|'ready'|'error'>('loading'),[boardsError,setBoardsError]=useState('');const boardsSeq=useRef(0);
  const refreshBoards=useCallback(()=>{const seq=++boardsSeq.current;setBoardsStatus('loading');setBoardsError('');api.listBoards(spaceId).then(r=>{if(currentSpace.current!==spaceId||boardsSeq.current!==seq)return;setBoards(r.boards);setBoardsStatus('ready');setBoardsError('')}).catch(e=>{if(currentSpace.current!==spaceId||boardsSeq.current!==seq)return;setBoardsStatus('error');setBoardsError(e instanceof Error?e.message:'Could not load boards.')})},[spaceId]);
  useEffect(()=>{boardsSeq.current++;setBoards([]);setBoardsStatus('loading');setBoardsError('')},[spaceId]);
  const openBoard=useCallback((path:string)=>{const id=boardTabId(path);setTabs(old=>old.some(t=>t.id===id&&t.space===spaceId)?old.map(t=>t.id===id&&t.space===spaceId?{...t,transient:false}:t):[...old,{id,path,space:spaceId,board:path}]);navigate(`/s/${spaceId}/b/${encodeURIComponent(path)}`);if(innerWidth<700)setSidebar(false)},[spaceId,navigate]);
  const [mindMaps,setMindMaps]=useState<api.MindMapSummary[]>([]),[mapsStatus,setMapsStatus]=useState<'loading'|'ready'|'error'>('loading'),[mapsError,setMapsError]=useState('');
  const mapsSeq=useRef(0);
  const refreshMaps=useCallback(()=>{const seq=++mapsSeq.current;setMapsStatus('loading');api.listMindMaps(spaceId).then(r=>{if(currentSpace.current===spaceId&&mapsSeq.current===seq){setMindMaps(r.mindMaps);setMapsStatus('ready');setMapsError('')}}).catch(e=>{if(currentSpace.current===spaceId&&mapsSeq.current===seq){setMapsStatus('error');setMapsError(msg(e))}})},[spaceId]);
  useEffect(()=>{mapsSeq.current++;setMindMaps([]);setMapsError('');setMapsStatus('loading')},[spaceId]);
  const openMap=useCallback((path:string)=>{const id=mapTabId(path);setTabs(old=>old.some(t=>t.id===id&&t.space===spaceId)?old:[...old,{id,path,space:spaceId,map:path}]);navigate(`/s/${spaceId}/m/${encodeURIComponent(path)}`);if(innerWidth<700)setSidebar(false)},[spaceId,navigate]);
  const invalidateDoc=(_id:string)=>{let retained=false;for(const [key,d] of drafts.current){if(d.space!==spaceId)continue;if(d.dirty){retained=true;continue}drafts.current.delete(key)}for(const key of notes.current.keys())if(key.startsWith(spaceId+'/'))notes.current.delete(key);if(retained)setError('A document draft changed during the move. Keep that draft and resolve any save conflict.');else if(activeId&&!activeBoard&&!activeMap&&!activeAtt)setReloadTick(x=>x+1)};
  const flushBoardDrafts=async(id:string)=>{await flush(id);for(const d of drafts.current.values())if(d.space===spaceId&&d.id!==id){await d.flush();if(d.dirty)throw new Error('Save or export document drafts before moving a task.')}};
  const moveDocToBoard=async(docId:string,boardPath:string,columnId:string)=>{
   if(!writable)throw new Error('This vault is read-only.');
   await flushBoardDrafts(docId);
   const fresh=await api.getDocument(spaceId,docId);
   if(boardMove?.id===docId&&boardMove.path!==fresh.path)throw new Error('This document moved elsewhere. Reopen the move dialog.');
   const moved=await api.boardMoveTask(spaceId,boardPath,{docId,columnId,fromPath:fresh.path},fresh.revision);
   if(moved.warnings?.length)setError(moved.warnings.join("; "));
   if(currentSpace.current!==spaceId)return;
   invalidateDoc(docId);
   await refreshDocs();void refreshBoards();void refreshMaps();tick(x=>x+1);
   notify(`Moved ${baseOf(fresh.path)} to board ${boardPath}.`);
  };

 const activeMeta=docs.find(d=>d.id===activeId);
 useEffect(()=>{setRevealDoc('');requestAnimationFrame(()=>document.querySelector('.document-tab.active')?.scrollIntoView({inline:'nearest',block:'nearest'}))},[activeId,page]);
 const measureTabs=useCallback(()=>{const el=tabStrip.current;if(el)setTabOverflow(el.scrollWidth>el.clientWidth+2)},[]);
 useEffect(()=>{const raf=requestAnimationFrame(measureTabs);return()=>cancelAnimationFrame(raf)},[measureTabs,tabs,page,sidebar,panel]);
 useEffect(()=>{const el=tabStrip.current;if(!el||typeof ResizeObserver==='undefined')return;const ro=new ResizeObserver(()=>measureTabs());ro.observe(el);return()=>ro.disconnect()},[measureTabs]);
 useEffect(()=>{if(!tabListOpen)return;const h=(e:KeyboardEvent)=>{if(e.key==='Escape')setTabListOpen(false)};window.addEventListener('keydown',h);return()=>window.removeEventListener('keydown',h)},[tabListOpen]);
 const scrollTabs=(dir:number)=>{const el=tabStrip.current;if(!el)return;setTabListOpen(false);el.scrollBy({left:dir*Math.max(180,el.clientWidth*0.7),behavior:'smooth'})};
 const openTabList=()=>{const r=document.querySelector('.tabbar-nav')?.getBoundingClientRect();setTabListPos(r?{x:r.left,y:r.bottom}:{x:200,y:44});setTabListOpen(o=>!o)};
  const navRef=useRef(nav);navRef.current=nav;
 const listTask=useRef<{key:string;controller:AbortController;promise:Promise<void>;again:boolean;extras:boolean}|null>(null);
 const catalog=useRef<{scope:string;trashed:boolean;documents:DocMeta[]}>({scope:'',trashed:false,documents:[]});
  const refreshDocs=useCallback((extras=true):Promise<void>=>{
   if(!spaceId)return Promise.resolve();
   const scope=spaceId,trashed=navRef.current==='trash',taskKey=scope+':'+trashed;
  if(listTask.current?.key===taskKey){listTask.current.again=true;listTask.current.extras ||= extras;return listTask.current.promise}
  listTask.current?.controller.abort();
  const task={key:taskKey,controller:new AbortController(),promise:Promise.resolve(),again:false,extras};listTask.current=task;
  task.promise=(async()=>{try{
   do{
    task.again=false;
    await api.loadDocumentPages(scope,trashed,(documents,total,done)=>{
     if(currentSpace.current!==scope||task.controller.signal.aborted)return;
     catalog.current={scope,trashed,documents};
     startTransition(()=>{setDocs(documents);if(done)setDocsLoaded(true)});
     setListProgress(done?null:{done:documents.length,total});
    },task.controller.signal,!task.extras&&catalog.current.scope===scope&&catalog.current.trashed===trashed?catalog.current.documents:[]);
    if(task.extras&&!task.controller.signal.aborted){task.extras=false;
     const [f,a]=await Promise.all([api.listFolders(scope),api.listAttachments(scope)]);
     if(currentSpace.current===scope&&!task.controller.signal.aborted){setFolders(f.folders||[]);setAtts(a.attachments);setExplorerLoaded(true)}
    }
   }while(task.again&&!task.controller.signal.aborted&&currentSpace.current===scope);
   }catch(e){if(!task.controller.signal.aborted&&currentSpace.current===scope)setError(msg(e))}
  finally{if(listTask.current===task){listTask.current=null;setListProgress(null)}}})();return task.promise;
 },[spaceId]);
 useEffect(()=>()=>listTask.current?.controller.abort(),[]);
 useEffect(()=>{
   let stopped=false,timer:ReturnType<typeof setTimeout>|undefined,last=-1,wasActive=false;setVaultLoad(null);
   if(!spaceId)return;
   const poll=async()=>{try{
    const {load}=await api.vaultLoad(spaceId);if(stopped)return;setVaultLoad(load);
    const active=!!load&&['queued','scanning','indexing','resolving'].includes(load.phase);
    if(active){wasActive=true;if(load.completed!==last){last=load.completed;void refreshDocs(false)}timer=setTimeout(poll,1000)}
    else if(wasActive){void refreshDocs();refreshGraph(x=>x+1);tick(x=>x+1)}
   }catch(e){if(!stopped){setError(msg(e));timer=setTimeout(poll,3000)}}};void poll();
  return()=>{stopped=true;clearTimeout(timer)};
 },[spaceId,refreshDocs,loadRetry]);
  const open=useCallback((id:string)=>{previewSequence.current++;setTextFile(null);setTabs(old=>old.map(t=>t.space===spaceId&&t.id===id?{...t,transient:false}:t));navigate(`/s/${spaceId}/d/${id}`);if(innerWidth<700)setSidebar(false)},[spaceId,navigate]);
  // PDFs open as real workspace tabs (not a throwaway modal) so they can be pinned,
  // reordered, restored, and converted to Markdown from a stable location.
  const openAttachmentTab=useCallback((path:string)=>{const id='att:'+encodeURIComponent(path);
   setTabs(old=>old.some(t=>t.id===id&&t.space===spaceId)?old.map(t=>t.id===id&&t.space===spaceId?{...t,transient:false}:t):[...old,{id,path,space:spaceId,att:path}]);
   navigate(`/s/${spaceId}/a/${encodeURIComponent(path)}`);if(innerWidth<700)setSidebar(false)},[spaceId,navigate]);
 const previewDoc=useCallback(async(id:string)=>{
  const sequence=++previewSequence.current;
  const previous=tabs.find(t=>t.space===spaceId&&t.transient);
  if(previous&&previous.id!==id){const pending=drafts.current.get(spaceId+'/'+previous.id);if(pending){try{await pending.flush();if(pending.dirty)throw new Error('Save or export unsaved changes before replacing this preview.')}catch(e){setError(msg(e));return}}}
  if(sequence!==previewSequence.current)return;
  setTabs(old=>{if(old.some(t=>t.space===spaceId&&t.id===id))return old;return [...old.filter(t=>t.space!==spaceId||!t.transient),{id,path:docs.find(d=>d.id===id)?.path||'Loading…',space:spaceId,transient:true}]});
  navigate(`/s/${spaceId}/d/${id}`);if(innerWidth<700)setSidebar(false)
 },[tabs,spaceId,docs,navigate]);
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[]);
   useEffect(()=>{let cancelled=false;setDocs([]);setFolders([]);setAtts([]);setBoards([]);setBoardMove(null);setDocsLoaded(false);setExplorerLoaded(false);setError('');setFolder('');setSel({docs:[],folders:[]});setRevealDoc('');restore.current={};wsReady.current=false;setWorkspaceReady('');setCollapsedFolders([]);setSidebar(innerWidth>=700&&page!=='graph2'&&page!=='memos');navStack.current={i:-1,ids:[]};
   try{const ls=JSON.parse(localStorage.getItem('netsidian:ws:'+(me?.userId||'local')+':'+spaceId)||'null');if(ls){if(typeof ls.sidebar==='boolean'&&innerWidth>=700&&page!=='graph2'&&page!=='memos')setSidebar(ls.sidebar);if(Array.isArray(ls.collapsed))setCollapsedFolders(ls.collapsed)}}catch{}
   try{const rk='netsidian:vault-recents:'+(me?.userId||'local');const r=JSON.parse(localStorage.getItem(rk)||'[]');localStorage.setItem(rk,JSON.stringify([spaceId,...r.filter((x:string)=>x!==spaceId)].slice(0,6)))}catch{}
   api.getWorkspace(spaceId).then(w=>{if(cancelled)return;let local:unknown=null;try{local=JSON.parse(localStorage.getItem(sessionKey(me?.userId||'local',spaceId))||'null')}catch{}const c={...(w.custom||{}),...(readSession(local,spaceId)?local as Record<string,unknown>:{})} as {collapsed?:string[];sidebar?:boolean;panel?:string;mode?:string;sort?:string;bookmarks?:string[];wide?:boolean;widths?:{sidebar?:number;inspector?:number}};
    if(Array.isArray(c.collapsed))setCollapsedFolders(c.collapsed);if(typeof c.sidebar==='boolean'&&innerWidth>=700&&page!=='graph2'&&page!=='memos')setSidebar(c.sidebar);setPanel(['graph','outline','backlinks'].includes(c.panel||'')?c.panel as typeof panel:'');
    if(typeof c.wide==='boolean')setWide(c.wide);if(c.sort==='mtime'||c.sort==='name')setSortMode(c.sort);if(Array.isArray(c.bookmarks))setBookmarks(c.bookmarks);
    setWidths({sidebar:c.widths?.sidebar||defaultPaneWidths.sidebar,inspector:c.widths?.inspector||defaultPaneWidths.inspector});
    let legacy:WorkspaceSession|null=null;try{const raw=localStorage.getItem('netsidian:tabs:'+me?.userId);if(raw!==null)legacy=readSession({version:2,tabs:JSON.parse(raw),toolTabs:JSON.parse(localStorage.getItem('netsidian:tool-tabs:'+me?.userId)||'[]'),activeDoc:w.activeDoc,activePage:'document'},spaceId)}catch{}
    restore.current={active:w.activeDoc||undefined,openDocs:w.openDocs||[],session:readSession(local,spaceId)||readSession(c,spaceId)||legacy}}).catch(()=>{}).finally(()=>{if(!cancelled){wsReady.current=true;setWorkspaceReady(spaceId)}});
   const es=new EventSource(`/api/v1/spaces/${spaceId}/events`);let eventTimer:ReturnType<typeof setTimeout>|undefined;let connected=false;
    es.addEventListener('change',(event)=>{try{if(JSON.parse((event as MessageEvent).data).eventType==='space.members')void refresh().catch(()=>{})}catch{}if(eventTimer)return;eventTimer=setTimeout(()=>{eventTimer=undefined;if(cancelled)return;void refreshDocs();void refreshBoards();void refreshMaps();refreshGraph(x=>x+1);tick(x=>x+1)},100)});
    es.onopen=()=>{if(!cancelled&&connected){void refreshDocs();void refreshBoards();void refreshMaps();refreshGraph(x=>x+1)}connected=true};
   es.onerror=()=>{if(cancelled)return;void api.vaultSharing(spaceId).catch(e=>{if(!cancelled&&e instanceof api.ApiError&&(e.status===403||e.status===404)){es.close();void refresh().catch(()=>{})}})};
 void refreshBoards();void refreshMaps();return()=>{cancelled=true;clearTimeout(eventTimer);es.close()}},[spaceId,refreshDocs,refreshBoards,refreshMaps,me?.userId]);
  useEffect(()=>{void refreshDocs()},[nav,refreshDocs]);
  useEffect(()=>{if(nav==='trash')api.listFolderTrashes(spaceId).then(r=>setFTrashes(r.folders)).catch(()=>setFTrashes([]))},[nav,spaceId,epoch]);
  // A deep-linked or restored attachment URL gets a real tab too, so the tab strip,
  // tab list and workspace persistence all know about the open PDF.
  useEffect(()=>{if(!activeId)return;const scope=spaceId,k=scope+'/'+activeId;
  if(activeAtt){setTabs(old=>old.some(t=>t.id===activeId&&t.space===scope)?old:[...old,{id:activeId,path:activeAtt,space:scope,att:activeAtt}]);return}
  if(activeBoard){setTabs(old=>old.some(t=>t.id===activeId&&t.space===scope)?old:[...old,{id:activeId,path:activeBoard,space:scope,board:activeBoard}]);return}
  if(activeMap){setTabs(old=>old.some(t=>t.id===activeId&&t.space===scope)?old:[...old,{id:activeId,path:activeMap,space:scope,map:activeMap}]);return}
  setTabs(old=>old.some(t=>t.id===activeId&&t.space===scope)?old:[...old,{id:activeId,path:docs.find(d=>d.id===activeId)?.path||notes.current.get(k)?.path||'Loading…',space:scope}]);
  if(drafts.current.has(k))return;let cancelled=false;setLoading(true);
  workspaceDraft(activeId).then(({note:n})=>{if(cancelled)return;setTabs(old=>old.map(t=>t.id===activeId&&t.space===scope?{...t,path:n.path}:t));tick(x=>x+1);setLoading(false)}).catch(e=>{if(!cancelled){setError(msg(e));setLoading(false)}});return()=>{cancelled=true};
 },[activeId,spaceId,reloadTick,workspaceDraft]);
 useEffect(()=>{setTabs(old=>old.map(t=>{const d=t.space===spaceId?docs.find(d=>d.id===t.id):null;return d?{...t,path:d.path}:t}))},[docs,spaceId]);
 // Restore this vault's saved workspace once its documents are known:
 // reopen tabs, then focus the active document. Server already dropped
 // ids that do not belong to this vault. Guarded so it runs per vault.
  const restoredFor=useRef('');
 useEffect(()=>{if(restoredFor.current===spaceId||!docsLoaded||workspaceReady!==spaceId||!appearanceLoaded)return;restoredFor.current=spaceId;
  const saved=restore.current;restore.current={};const session=saved.session;
  const ids=new Set(docs.filter(d=>!d.tombstoneAt).map(d=>d.id));
   const attTab=(id:string):Tab=>{const path=decodeURIComponent(id.slice(4));return{id,space:spaceId,path,att:path}};
   const mapTab=(id:string):Tab=>{const path=decodeURIComponent(id.slice(4));return{id,space:spaceId,path,map:path}};
   const boardTab=(id:string):Tab=>{const path=decodeURIComponent(id.slice(6));return{id,space:spaceId,path,board:path}};
    const restored=appearance.restoreTabs?(session?session.tabs:(saved.openDocs||[]).map((id):Tab=>id.startsWith('att:')?attTab(id):id.startsWith('board:')?boardTab(id):id.startsWith('map:')?mapTab(id):{id,space:spaceId,path:docs.find(d=>d.id===id)?.path||'Loading…'})).filter(t=>ids.has(t.id)||!!t.att||!!t.board||!!t.map):[];
   if(activeId&&ids.has(activeId)&&!restored.some(t=>t.id===activeId))restored.push({id:activeId,space:spaceId,path:docs.find(d=>d.id===activeId)!.path});
   if(activeAtt&&activeId&&!restored.some(t=>t.id===activeId))restored.push(attTab(activeId));
   if(activeBoard&&activeId&&!restored.some(t=>t.id===activeId))restored.push(boardTab(activeId));
   if(activeMap&&activeId&&!restored.some(t=>t.id===activeId))restored.push(mapTab(activeId));
   setTabs(old=>[...old.filter(t=>t.space!==spaceId),...restored]);
   const restoredTools=appearance.restoreTabs?[...(session?.toolTabs||[])]:[];if((page==='graph2'||page==='memos'||(page==='boards'||page==='maps'))&&!restoredTools.some(t=>t.page===page))restoredTools.push({space:spaceId,page});
   setToolTabs(old=>[...old.filter(t=>t.space!==spaceId),...restoredTools]);
   if(!activeId&&page==='document'&&appearance.restoreTabs){const active=session?session.activeDoc:saved.active;
    if(session?.activePage==='graph2'||session?.activePage==='memos'||session?.activePage==='boards'||session?.activePage==='maps')navigate(`/s/${spaceId}/${session.activePage}`,{replace:true});
    else if(active&&active.startsWith('att:')&&restored.some(t=>t.id===active))navigate(`/s/${spaceId}/a/${active.slice(4)}`,{replace:true});
    else if(active&&active.startsWith('board:')&&restored.some(t=>t.id===active))navigate(`/s/${spaceId}/b/${active.slice(6)}`,{replace:true});
    else if(active&&active.startsWith('map:')&&restored.some(t=>t.id===active))navigate(`/s/${spaceId}/m/${active.slice(4)}`,{replace:true});
    else if(active&&ids.has(active))navigate(`/s/${spaceId}/d/${active}`,{replace:true});
   }
  },[docs,docsLoaded,spaceId,activeId,activeAtt,activeBoard,activeMap,page,navigate,workspaceReady,appearanceLoaded]);
 const pendingWorkspace=useRef<{scope:string;state:WorkspaceState}|null>(null),workspaceWrites=useRef<Promise<unknown>>(Promise.resolve());
 useLayoutEffect(()=>{if(restoredFor.current!==spaceId||workspaceReady!==spaceId)return;
  const openTabs=tabs.filter(t=>t.space===spaceId).map(t=>({...t,transient:false}));
   const session:WorkspaceSession={version:2,tabs:openTabs,toolTabs:toolTabs.filter(t=>t.space===spaceId),activeDoc:openTabs.some(t=>t.id===activeId)?activeId:page==='settings'?(openTabs.at(-1)?.id||''):'',activePage:page==='graph2'||page==='memos'||(page==='boards'||page==='maps')?page:'document'};
  const custom={...session,collapsed:collapsedFolders,sidebar,panel,nav,sort:sortMode,bookmarks,wide,widths};
  try{localStorage.setItem(sessionKey(me?.userId||'local',spaceId),JSON.stringify(custom))}catch{}
  pendingWorkspace.current={scope:spaceId,state:{openDocs:openTabs.map(t=>t.id),activeDoc:session.activeDoc,expandedFolders:[],paneWidths:{sidebar:Math.round(widths.sidebar),inspector:Math.round(widths.inspector)},custom}};
 },[tabs,toolTabs,activeId,page,collapsedFolders,sidebar,panel,mode,nav,spaceId,workspaceReady,me?.userId,sortMode,bookmarks,wide,widths]);
 useEffect(()=>{if(workspaceReady!==spaceId)return;const t=setTimeout(()=>{const pending=pendingWorkspace.current;if(!pending||pending.scope!==spaceId)return;workspaceWrites.current=workspaceWrites.current.catch(()=>{}).then(()=>api.putWorkspace(pending.scope,pending.state)).catch(()=>{})},350);return()=>clearTimeout(t)},[tabs,toolTabs,activeId,page,collapsedFolders,sidebar,panel,mode,nav,spaceId,workspaceReady,sortMode,bookmarks,wide,widths]);
 useEffect(()=>{const flush=()=>{const pending=pendingWorkspace.current;if(pending)void fetch(`/api/v1/spaces/${encodeURIComponent(pending.scope)}/workspace`,{method:'PUT',keepalive:true,headers:{'Content-Type':'application/json','X-Netsidian-Requested':'1'},body:JSON.stringify(pending.state)}).catch(()=>{})};window.addEventListener('pagehide',flush);return()=>window.removeEventListener('pagehide',flush)},[]);
  // Navigation history for the document header back/forward buttons.
    useEffect(()=>{if(!activeId||activeAtt||activeBoard||activeMap||page!=='document')return;const s=navStack.current;if(s.ids[s.i]===activeId)return;s.ids=[...s.ids.slice(0,s.i+1),activeId].slice(-50);s.i=s.ids.length-1},[activeId,page]);
  const goBack=()=>{const s=navStack.current;if(s.i>0){s.i--;navigate(`/s/${spaceId}/d/${s.ids[s.i]}`)}};
  const goForward=()=>{const s=navStack.current;if(s.i<s.ids.length-1){s.i++;navigate(`/s/${spaceId}/d/${s.ids[s.i]}`)}};
  const startResize=(which:'sidebar'|'inspector')=>(e:React.PointerEvent)=>{e.preventDefault();const startX=e.clientX,startW=paneLayout[which];
   const move=(ev:PointerEvent)=>{const dw=(which==='sidebar'?ev.clientX-startX:startX-ev.clientX)/uiZoom();setWidths(w=>({...w,[which]:Math.max(which==='sidebar'?180:260,Math.min(which==='sidebar'?paneLayout.maxSidebar:paneLayout.maxInspector,startW+dw))}))};
   const up=()=>{window.removeEventListener('pointermove',move);window.removeEventListener('pointerup',up)};
   window.addEventListener('pointermove',move);window.addEventListener('pointerup',up)};
  useEffect(()=>{const handler=(e:BeforeUnloadEvent)=>{if([...drafts.current.values()].some(d=>d.dirty)){e.preventDefault();e.returnValue=''}};window.addEventListener('beforeunload',handler);return()=>window.removeEventListener('beforeunload',handler)},[]);
   useEffect(()=>{const l=(e:KeyboardEvent)=>{if(e.key==='Escape'){setSelMenu(null);setTabMenu(null);setAttView(null);setAttMenu(null);setFolderMenu(null);setMenu(null)}};window.addEventListener('keydown',l);return()=>window.removeEventListener('keydown',l)},[]);
 useEffect(()=>{if(nav!=='search'||!query.trim()){setHits([]);setSearching(false);setSearchError('');return}if(query.startsWith('tag:')){const tag=query.slice(4).trim().toLocaleLowerCase();setHits(docs.filter(d=>!d.tombstoneAt&&d.tags?.includes(tag)).map(d=>({documentId:d.id,path:d.path,title:title(d.path),snippet:'',score:0})));setSearching(false);setSearchError('');setSearchIndex(0);return}let cancelled=false;setSearching(true);setSearchError('');setSearchIndex(0);const timer=setTimeout(()=>api.search(spaceId,query,100).then(r=>{if(!cancelled)setHits(r.hits)}).catch(e=>{if(!cancelled)setSearchError(msg(e))}).finally(()=>{if(!cancelled)setSearching(false)}),220);return()=>{cancelled=true;clearTimeout(timer)}},[query,spaceId,nav,epoch,searchRetry,docs]);
 useEffect(()=>{if(nav==='search')document.querySelector('.explorer-results [aria-current=true]')?.scrollIntoView({block:'nearest'})},[nav,searchIndex]);
 useEffect(()=>{if(nav==='tags')api.tags(spaceId).then(r=>setTags(r.tags)).catch(e=>setError(msg(e)))},[nav,spaceId,docs]);
  useEffect(()=>{if(panel==='backlinks'&&activeId&&!activeAtt)api.getBacklinks(spaceId,activeId).then(r=>setBacklinks(r.backlinks)).catch(e=>setError(msg(e)))},[panel,activeId,spaceId,docs]);
 useEffect(()=>{if(!draft||!preview.current||(mode==='source'||mode==='visual')||page!=='document')return;
   renderInto(preview.current,draft.text,makeResolver(docs.map(d=>d.path),activeMeta?.path||note?.path,atts.map(a=>({path:a.path,url:api.attachmentURL(spaceId,a.path)}))),(path,frag)=>{const d=docs.find(d=>d.path===path);if(d){open(d.id);if(frag)navigate(`/s/${spaceId}/d/${d.id}#${encodeURIComponent(frag)}`)}},draft.canEdit?(index,checked)=>{let n=0,fenced=false;const text=draft.text.split('\n').map(line=>{if(/^\s*(```|~~~)/.test(line))fenced=!fenced;if(!fenced&&/^\s*[-*+] \[[ xX]\] /.test(line)){if(n++===index)return line.replace(/\[[ xX]\]/,checked?'[x]':'[ ]')}return line}).join('\n');draft.change(text)}:undefined);
   if(loc.hash){const id=headingId(decodeURIComponent(loc.hash.slice(1)));preview.current.querySelector('#'+CSS.escape(id))?.scrollIntoView({block:'start'})}
  },[draft,epoch,mode,docs,page,loc.hash,activeId,atts,spaceId]);
 const notify=(s:string)=>{setNotice(s);setTimeout(()=>setNotice(''),5000)};
  const ask=(title:string,label:string,value:string,action:(s:string)=>Promise<void>)=>{setError('');setPrompt({title,label,value,action})};
  const confirmAsk=(title:string,label:string,action:()=>Promise<void>,yes='Confirm',no='Cancel')=>{setError('');setPrompt({title,label,value:'',confirm:true,yes,no,action:async()=>{await action()}})};
  const dirOf=(p:string)=>p.includes('/')?p.slice(0,p.lastIndexOf('/')):'';
  const baseOf=(p:string)=>p.split('/').pop()||p;
  // Name-only dialog: the parent path is shown fixed, only the last segment is editable.
  const askName=(title:string,label:string,dir:string,value:string,action:(full:string)=>Promise<void>)=>{setError('');setPrompt({title,label,value,dir,action})};
 const create=()=>{if(window.event&&((window.event as KeyboardEvent).shiftKey||(window.event as MouseEvent).altKey))return createUntitledWithPrompt();createUntitled()};
 const dailyNote=async()=>{try{const now=new Date();const date=[now.getFullYear(),String(now.getMonth()+1).padStart(2,'0'),String(now.getDate()).padStart(2,'0')].join('-');const path='Daily/'+date+'.md';let d=docs.find(d=>!d.tombstoneAt&&d.path===path);if(!d){if(!writable)return;d=await api.createDocument(spaceId,path,'# '+date+'\n\n## Notes\n\n## Tasks\n\n- [ ] \n','daily:'+date);await refreshDocs()}open(d.id)}catch(e){setError(msg(e))}};
 const createUntitledWithPrompt=()=>ask('New document','File path', (folder?folder+'/':'')+'Untitled.md',async p=>{if(!p.toLowerCase().endsWith('.md'))p+='.md';const d=await api.createDocument(spaceId,p,'# '+title(p)+'\n\n');await refreshDocs();explicitModes.current.set(spaceId+':'+d.id,'source');setMode('source');open(d.id);autoName.current={id:d.id,path:p}});
 // One-click new note: no prompt, lands in the selected folder as Untitled / Untitled 2 / …
 const createUntitled=(destination=folder)=>{if(!writable)return;void (async()=>{try{
  const base=destination?destination+'/':'';const used=new Set(docs.map(d=>d.path));let name='Untitled';
  for(let n=1;n<500;n++){name=n===1?'Untitled':`Untitled ${n}`;if(!used.has(base+name+'.md'))break}
   const d=await api.createDocument(spaceId,base+name+'.md','# '+name+'\n\n');await refreshDocs();explicitModes.current.set(spaceId+':'+d.id,'source');setMode('source');open(d.id);autoName.current={id:d.id,path:base+name+'.md'}}catch(e){setError(msg(e))}})()};
  const createFolder=(destination=folder)=>askName('New folder','Folder name',destination,'New folder',async n=>{if(!n||n==='.'||n==='..')throw new Error('Pick a folder name without dots or slashes.');const p=n;await api.createFolder(spaceId,p);setFolder(p);await refreshDocs()});
  // One-click new memo from the explorer's create menu: same timestamped path
  // and memo tag as the Memos composer, opened for editing right away.
  const createMemoIn=(destination:string)=>{if(!writable)return;void (async()=>{try{const id=crypto.randomUUID(),stamp=new Date().toISOString().replace(/[:.]/g,'-');const path=(destination?destination+'/':'')+stamp+'-'+id.slice(0,8)+'.md';const d=await api.createDocument(spaceId,path,memoMarkdown(''),id);await refreshDocs();explicitModes.current.set(spaceId+':'+d.id,interaction.memoComposerMode==='visual'?'visual':'source');open(d.id)}catch(e){setError(msg(e))}})()};
 const flush=async(id:string)=>{const d=drafts.current.get(spaceId+'/'+id);if(d){await d.flush();if(d.dirty)throw new Error('Resolve the unsaved changes before changing this file.')}};
  const rename=async(d:DocMeta,path:string,automatic=false)=>{
   await flush(d.id);if(automatic&&autoName.current?.id!==d.id)return;const incoming=await api.getBacklinks(spaceId,d.id);const sources=[...new Set([d.id,...incoming.backlinks.map(b=>b.documentId)])];const paths=docs.filter(x=>!x.tombstoneAt).map(x=>x.path);
   const updates:{id:string;note:NoteContent;text:string}[]=[];
   for(const id of sources){await flush(id);const n=await api.getDocument(spaceId,id);const text=rewriteMovedLinks(n.markdown,n.path,d.path,path,paths,id===d.id);if(text!==n.markdown)updates.push({id,note:n,text})}
   const n=await api.getDocument(spaceId,d.id);await api.renameDocument(spaceId,d.id,path,n.revision);notes.current.set(spaceId+'/'+d.id,{...n,path});
   const failed:string[]=[];for(const update of updates){try{const live=drafts.current.get(spaceId+'/'+update.id);if(live){if(live.text!==update.note.markdown)throw new Error('Draft changed during move; original link retained.');live.change(update.text);await flush(update.id)}else{const fresh=await api.getDocument(spaceId,update.id);if(fresh.markdown!==update.note.markdown)throw new Error('Document changed during move; original link retained.');await api.saveContent(spaceId,update.id,update.text,fresh.revision);notes.current.delete(spaceId+'/'+update.id)}}catch(e){failed.push(update.note.path+': '+msg(e))}}
   await refreshDocs();if(failed.length)throw new Error('Document moved, but some incoming links could not be updated: '+failed.join('; '));
  };
  const renameAsk=(d:DocMeta)=>askName('Rename document','File name',dirOf(d.path),baseOf(d.path).replace(/\.md$/i,''),async n=>{if(!n||n==='.'||n==='..')throw new Error('Pick a name without dots or slashes.');if(!n.toLowerCase().endsWith('.md'))n+='.md';await rename(d,n)});
  // A brand-new note follows its own heading: while the (still auto-owned) name matches,
  // editing the H1 renames the file alongside it. A manual rename detaches automatically.
  useEffect(()=>{const an=autoName.current;if(!an||an.id!==activeId||!draft)return;const t=setTimeout(()=>{const cur=docs.find(d=>d.id===an.id);if(autoName.current?.id!==an.id||!cur||cur.path!==an.path)return;const m=/^#\s+([^\n]+)/.exec(draft.text);const clean=(m?.[1]||'').replace(/[\\/:*?"<>|#^\[\]]+/g,'').replace(/\s+/g,' ').trim().replace(/\.+$/,'').slice(0,80);if(!clean)return;const dir=dirOf(cur.path);const next=(dir?dir+'/':'')+clean+'.md';if(next===cur.path||docs.some(d=>d.id!==an.id&&d.path===next&&!d.tombstoneAt))return;void rename(cur,next,true).then(()=>{if(autoName.current?.id===an.id)autoName.current={id:an.id,path:next}}).catch(e=>setError('Could not auto-rename: '+msg(e)))},500);return()=>clearTimeout(t)});
  const moveDocsTo=async(ids:string[],to:string)=>{if(!writable)return;const target=to.replace(/\/+$/,'');let moved=0;const failures:string[]=[];for(const id of ids){const d=docs.find(d=>d.id===id);if(!d)continue;const next=(target?target+'/':'')+d.path.split('/').pop();if(next===d.path)continue;try{if(autoName.current?.id===id)autoName.current=null;await rename(d,next);moved++}catch(e){failures.push(d.path+': '+msg(e))}}await refreshDocs();setSel({docs:[],folders:[]});if(moved)notify(`Moved ${moved} document${moved===1?'':'s'}${target?' to '+target:' to the root'}.`);if(failures.length)setError(failures.join('\n'))};
   const remapFolderTabs=(from:string,to:string)=>{setTabs(old=>moveMapTabs(moveBoardTabs(moveAttachmentTabs(old,spaceId,from,to,true),spaceId,from,to),spaceId,from,to));closedStack.current=moveMapTabs(moveBoardTabs(moveAttachmentTabs(closedStack.current,spaceId,from,to,true),spaceId,from,to),spaceId,from,to);if(activeAtt.startsWith(from+'/')&&currentSpace.current===spaceId)navigate(`/s/${spaceId}/a/${encodeURIComponent(to+activeAtt.slice(from.length))}`);if((activeBoard===from||activeBoard.startsWith(from+'/'))&&currentSpace.current===spaceId)navigate(`/s/${spaceId}/b/${encodeURIComponent(to+activeBoard.slice(from.length))}`);if((activeMap===from||activeMap.startsWith(from+'/'))&&currentSpace.current===spaceId)navigate(`/s/${spaceId}/m/${encodeURIComponent(to+activeMap.slice(from.length))}`)};
 const moveFolderTo=async(from:string,to:string)=>{if(!writable)return;const name=from.split('/').pop()||from;const target=to?to+'/'+name:name;if(target===from)return;try{await flushMindMapDrafts(me?.userId||'local',spaceId);const r=await api.renameFolder(spaceId,from,target);remapFolderTabs(from,r.path);if(folder===from||folder.startsWith(from+'/'))setFolder(r.path);await refreshDocs();notify(`Moved ${name}${r.moved?` with ${r.moved} document${r.moved===1?'':'s'}`:''}.`);if(r.failures?.length)setError(r.failures.join('\n'))}catch(e){setError(msg(e))}};
   const renameFolderAsk=(path:string)=>askName('Rename folder','Folder name',dirOf(path),baseOf(path),async n=>{if(!n||n==='.'||n==='..')throw new Error('Pick a name without dots or slashes.');const target=n;await flushMindMapDrafts(me?.userId||'local',spaceId);const r=await api.renameFolder(spaceId,path,target);remapFolderTabs(path,r.path);if(folder===path||folder.startsWith(path+'/'))setFolder(r.path);await refreshDocs();setSel({docs:[],folders:[r.path]});notify(`Folder renamed to ${r.path}; ${r.moved} document${r.moved===1?'':'s'} moved with it.`);if(r.failures?.length)setError(r.failures.join('\n'))});
  const trashFolderAsk=(path:string)=>confirmAsk('Move folder to trash',`The whole “${path}” folder and its notes will be moved to trash. You can restore the folder later from Trash.`,async()=>{for(const d of docs){if(d.path.startsWith(path+'/')&&drafts.current.get(spaceId+'/'+d.id)?.dirty)throw new Error('Save or export drafts under '+path+' before trashing the folder.')}for(const d of docs)if(d.path.startsWith(path+'/')&&drafts.current.has(spaceId+'/'+d.id)){await drafts.current.get(spaceId+'/'+d.id)!.flush();drafts.current.delete(spaceId+'/'+d.id)}await flushMindMapDrafts(me?.userId||'local',spaceId);const r=await api.trashFolder(spaceId,path);setTabs(old=>removePathTabs(old,spaceId,path));if(folder===path||folder.startsWith(path+'/'))setFolder('');if((activeMeta&&activeMeta.path.startsWith(path+'/'))||activeAtt.startsWith(path+'/')||activeBoard===path||activeBoard.startsWith(path+'/')||activeMap===path||activeMap.startsWith(path+'/'))navigate(`/s/${spaceId}`);await refreshDocs();notify(`Trashed ${r.trashed} document${r.trashed===1?'':'s'} from ${path}. Restore trashed folders from the trash view.`);if(r.failures?.length)setError(r.failures.join('\n'))});
  const bulkMove=()=>ask('Move selection','Destination folder path',folder,async p=>{if(p&&p.split('/').some(s=>s==='.'||s==='..'))throw new Error('Invalid folder path.');await moveDocsTo(sel.docs,p)});
  const bulkTrash=()=>confirmAsk('Move selection to trash',`${sel.docs.length} selected document${sel.docs.length===1?'':'s'} will be moved to trash. You can restore them later from Trash.`,async()=>{let count=0;const failures:string[]=[];for(const id of sel.docs){const d=docs.find(d=>d.id===id);if(!d)continue;try{await flush(id);const n=await api.getDocument(spaceId,id);await api.trashDocument(spaceId,id,n.revision);setTabs(old=>old.filter(t=>t.id!==id));drafts.current.delete(spaceId+'/'+id);if(activeId===id)navigate(`/s/${spaceId}`);count++}catch(e){failures.push(d.path+': '+msg(e))}}await refreshDocs();setSel({docs:[],folders:[]});notify(`Trashed ${count} document${count===1?'':'s'}.`);if(failures.length)setError(failures.join('\n'))});
 const closeTabs=async(targets:Tab[])=>{const eligible=targets.filter(t=>!t.pinned&&t.space===spaceId);if(!eligible.length)return;
  try{for(const t of eligible){if(t.map){await flushMindMapDraft(me?.userId||'local',spaceId,t.map);for(const draft of drafts.current.values())if(draft.space===spaceId){await draft.flush();if(draft.dirty)throw new Error('Save or export the document preview before closing this map.')}}const d=drafts.current.get(t.space+'/'+t.id);if(d){await d.flush();if(d.dirty)throw new Error('Resolve or export unsaved changes before closing tabs.')}}}
  catch(e){setError(msg(e));return}
  for(const t of eligible)explicitModes.current.delete(t.space+':'+t.id);
  const ids=new Set(eligible.map(t=>t.id));setTabs(old=>old.filter(t=>t.space!==spaceId||!ids.has(t.id)));
   // Attachment tabs join the reopen stack like ordinary note tabs; only transient
   // hover-previews stay out of it.
    closedStack.current=[...eligible.filter(t=>!t.transient).slice().reverse().map(t=>({id:t.id,path:t.path,att:t.att,board:t.board,map:t.map,space:t.space})),...closedStack.current].slice(0,8);
    if(ids.has(activeId)){const next=tabs.filter(t=>t.space===spaceId&&!ids.has(t.id)).pop();navigate(next?workspaceTabURL(next):`/s/${spaceId}`)}
 };
 const closeTab=async(t:Tab)=>{if(t.pinned){setError('Unpin this tab before closing it.');return}await closeTabs([t])};
 const tabAction=(kind:string,t:Tab)=>{setTabMenu(null);const local=tabs.filter(x=>x.space===spaceId),i=local.findIndex(x=>x.id===t.id);
  if(kind==='pin'){setTabs(old=>old.map(x=>x.space===t.space&&x.id===t.id?{...x,pinned:!x.pinned}:x));return}
  if(kind==='close')void closeTabs([t]);
  if(kind==='others')void closeTabs(local.filter(x=>x.id!==t.id));
  if(kind==='right')void closeTabs(local.slice(i+1));
  if(kind==='all')void closeTabs(local);
    if(kind==='reveal'){setNav('files');setSidebar(true);if(t.map)openMap(t.map);else if(t.board)openBoard(t.board);else if(t.att)navigate(`/s/${spaceId}/a/${encodeURIComponent(t.att)}`);else{open(t.id);setRevealTick(x=>x+1)}}
  if(kind==='copy')void navigator.clipboard.writeText(t.path).catch(e=>setError(msg(e)));
 };
   const reopenClosed=()=>{const i=closedStack.current.findIndex(t=>t.space===spaceId&&(t.att?atts.some(a=>a.path===t.att):t.board?boards.some(b=>b.path===t.board):t.map?mindMaps.some(m=>m.path===t.map):docs.some(d=>d.id===t.id&&!d.tombstoneAt)));if(i<0)return;const [last]=closedStack.current.splice(i,1);if(last.att)openAttachmentTab(last.att);else if(last.board)openBoard(last.board);else if(last.map)openMap(last.map);else open(last.id)};
 const exportNote=async(d:DocMeta)=>{const buffer=drafts.current.get(spaceId+'/'+d.id);const content=buffer?.text??(await api.getDocument(spaceId,d.id)).markdown;download(d.path,content)};
  const menuAction=async(action:string,d:DocMeta)=>{setMenu(null);try{
    if(action==='rename')renameAsk(d);
   if(action==='board'){setBoardMove({id:d.id,path:d.path,title:d.title||title(d.path)});return}
  if(action==='duplicate')ask('Duplicate document','Copy path',d.path.replace(/\.md$/i,' copy.md'),async p=>{const n=await api.getDocument(spaceId,d.id);const c=await api.createDocument(spaceId,p,drafts.current.get(spaceId+'/'+d.id)?.text??n.markdown);await refreshDocs();open(c.id)});
  if(action==='export')await exportNote(d);
  if(action==='trash'){confirmAsk('Move document to trash',`“${title(d.path)}” will be moved to trash. You can restore it later from Trash.`,async()=>{await flush(d.id);const n=await api.getDocument(spaceId,d.id);await api.trashDocument(spaceId,d.id,n.revision);setTabs(old=>old.filter(t=>t.id!==d.id));drafts.current.delete(spaceId+'/'+d.id);if(activeId===d.id)navigate(`/s/${spaceId}`);await refreshDocs()})}
 }catch(e){setError(msg(e))}};
  const embedNames=(names:string[])=>{if(!names.length)return;if(activeId&&draft?.canEdit){draft.change(draft.text.replace(/\s+$/,'')+'\n\n'+names.map(n=>'![['+n+']]').join('\n')+'\n');notify(`Embedded ${names.length} attachment${names.length===1?'':'s'} in this note.`)}else notify(`Uploaded ${names.length} attachment${names.length===1?'':'s'}. Open a note to embed them, or insert ![[name]] manually.`)};
  // Editor paste/drop/toolbar uploads: unique attachments/<note>/N paths,
  // tracked across a burst of drops so a stale atts list cannot collide.
  const [textFile,setTextFile]=useState<string|null>(null);
  const issuedUploads=useRef(new Set<string>());
  useEffect(()=>{issuedUploads.current.clear()},[spaceId]);
  const uploadImages=useCallback(async(files:File[],notePath?:string)=>{
   if(!writable)throw new Error('This vault is read-only.');
   const out:string[]=[];
   for(const f of files){
    if(!imageFileOk(f)){setError('Only PNG, JPEG, GIF, WebP and SVG images can be embedded.');continue}
    out.push(await uploadImage(f,spaceId,atts,notePath??activeMeta?.path??note?.path??'',issuedUploads.current));
   }
   if(out.length)await refreshDocs();
   return out;
  },[writable,spaceId,atts,activeMeta?.path,note?.path,refreshDocs]);
  const runImport=async(pending:{items:{target:string;rel:string;file:File;md:boolean}[];skipped:string[];docs:Map<string,DocMeta>;atts:Map<string,api.AttachmentMeta>;embed?:boolean},mode:'both'|'skip'|'overwrite')=>{setImpPending(null);let made=0,over=0,skipped=pending.skipped.length;const failures:string[]=[];const embeds:string[]=[];const taken=new Set([...pending.docs.keys(),...pending.atts.keys()]);
   for(const it of pending.items){const key=it.target.toLowerCase();const dup=it.md?pending.docs.get(key):pending.atts.get(key);const collision=taken.has(key);
    if(collision&&mode==='skip'){skipped++;continue}
    let target=it.target;
    if(collision&&(mode==='both'||!dup))target=unusedImportPath(target,taken);
    try{
     if(it.md){const text=await it.file.text();if(dup&&mode==='overwrite'){await api.saveContent(spaceId,pending.docs.get(key)!.id,text,pending.docs.get(key)!.currentRevision);over++}else{await api.createDocument(spaceId,target,text);taken.add(target.toLowerCase())}made++}
     else{await api.uploadAttachment(spaceId,target,await it.file.arrayBuffer(),mode!=='overwrite'||!dup);embeds.push(target.split('/').pop()||target);taken.add(target.toLowerCase());made++}
    }catch(e){failures.push(it.file.name+': '+msg(e))}
   }
   await refreshDocs();
   notify(`Imported ${made} file${made===1?'':'s'}${over?`, replaced ${over}`:''}${skipped?`, skipped ${skipped}`:''}.`);if(failures.length)setError(failures.join('\n'));if(pending.embed!==false)embedNames(embeds)};
  const importFiles=async(list:FileList|File[],dest=folder,embed=true)=>{if(!writable)return;const arr=Array.from(list);const docMap=new Map(docs.filter(d=>!d.tombstoneAt).map(d=>[d.path.toLowerCase(),d]));let attMap=new Map<string,api.AttachmentMeta>();try{attMap=new Map((await api.listAttachments(spaceId)).attachments.map(a=>[a.path.toLowerCase(),a]))}catch(e){setError('Could not check existing attachments: '+msg(e));return}
   const items:{target:string;rel:string;file:File;md:boolean}[]=[];const skipped:string[]=[];
    for(const f of arr){const isMd=/\.md$/i.test(f.name);const isAtt=/\.(png|jpe?g|gif|webp|svg|pdf|mp4)$/i.test(f.name);if(!isMd&&!isAtt){skipped.push(f.name+': unsupported type');continue}let rel:string;try{rel=importRelativePath(f)}catch(e){skipped.push(f.name+': '+msg(e));continue}items.push({rel,file:f,md:isMd,target:(dest?dest+'/':'')+rel})}
   if(!items.length){if(skipped.length)setError(skipped.join('\n'));return}
   const seen=new Set([...docMap.keys(),...attMap.keys()]);let clash=false;for(const it of items){const key=it.target.toLowerCase();if(seen.has(key))clash=true;seen.add(key)}
   const pending={items,skipped,docs:docMap,atts:attMap,embed};
    if(clash)setImpPending(pending);else void runImport(pending,'skip')};
   // Open an attachment from the tree or its row menu: text-editable formats go to the
   // text editor, PDFs become workspace tabs, other media preview, the rest open raw.
   const openAttachmentPath=useCallback((path:string)=>{
    if(/\.(puml|plantuml|mmd)$/i.test(path)){setTextFile(path);navigate(`/s/${spaceId}`);return}
    if(/\.pdf$/i.test(path)){openAttachmentTab(path);return}
    if(PREVIEWABLE.test(path)){setAttView(path);return}
    window.open(api.attachmentURL(spaceId,path),'_blank','noopener')
   },[spaceId,navigate,openAttachmentTab]);
   const trashAttachmentAsk=(path:string)=>confirmAsk('Move attachment to trash',`“${baseOf(path)}” will be moved to trash. You can restore it later from Trash.`,async()=>{
    await api.trashAttachment(spaceId,path);
    const tabId='att:'+encodeURIComponent(path);
    setTabs(old=>old.filter(t=>t.id!==tabId||t.space!==spaceId));
    if(activeAtt===path)navigate(`/s/${spaceId}`);
    await refreshDocs();tick(x=>x+1);
    notify(`Trashed ${baseOf(path)}. Restore it from the trash view.`);
   });
   // Drag an attachment onto a folder row (or a file row: its parent folder). The server
   // refuses occupied targets, so a clash surfaces as an error instead of overwriting.
   const moveAttsTo=async(paths:string[],to:string)=>{if(!writable)return;const target=to.replace(/\/+$/,'');let moved=0;const failures:string[]=[];
    for(const path of paths){const name=baseOf(path);const next=(target?target+'/':'')+name;if(next===path)continue;
     try{await api.moveAttachment(spaceId,path,next);
      // The tab id is derived from the path, so a move must re-key it; otherwise the
      // deep-link effect would open a duplicate tab and trash actions would miss it.
      setTabs(old=>moveAttachmentTabs(old,spaceId,path,next));closedStack.current=moveAttachmentTabs(closedStack.current,spaceId,path,next);
      if(activeAtt===path&&currentSpace.current===spaceId)navigate(`/s/${spaceId}/a/${encodeURIComponent(next)}`);
      moved++}catch(e){failures.push(path+': '+msg(e))}}
    await refreshDocs();
    if(moved)notify(`Moved ${moved} attachment${moved===1?'':'s'}${target?' to '+target:' to the vault root'}.`);
    if(failures.length)setError(failures.join('\n'))};
   // Drop notes/folders/attachments onto the trash tool: same draft-preserving rules as
   // the menus — flush note drafts, refuse folders holding unsaved drafts, then trash.
   const dragTrashAsk=(payload:DragPayload)=>{
    const selDocs=payload.docs.map(id=>docs.find(d=>d.id===id&&!d.tombstoneAt)).filter((d):d is DocMeta=>!!d);
    const knownFolders=payload.folders.filter(f=>folders.includes(f));const selectedFolders=knownFolders.filter(f=>!knownFolders.some(parent=>parent!==f&&f.startsWith(parent+'/')));const selectedAtts=payload.atts.filter(p=>atts.some(a=>a.path===p)&&!selectedFolders.some(f=>p.startsWith(f+'/')));
    // A note inside a dropped folder is trashed with the folder; don't trash it twice.
    const standalone=selDocs.filter(d=>!selectedFolders.some(f=>d.path.startsWith(f+'/')));
    const parts=[standalone.length?`${standalone.length} note${standalone.length===1?'':'s'}`:'',selectedFolders.length?`${selectedFolders.length} folder${selectedFolders.length===1?'':'s'}`:'',selectedAtts.length?`${selectedAtts.length} attachment${selectedAtts.length===1?'':'s'}`:''].filter(Boolean);
    if(!parts.length)return;
    confirmAsk('Move to trash',`${parts.join(', ')} will be moved to trash. You can restore them later from Trash.`,async()=>{
     const failures:string[]=[];let notes=0,foldersN=0,attsN=0;
     for(const f of selectedFolders){for(const d of docs){if(d.path.startsWith(f+'/')&&drafts.current.get(spaceId+'/'+d.id)?.dirty)throw new Error('Save or export drafts under '+f+' before trashing the folder.')}
      for(const d of docs)if(d.path.startsWith(f+'/')){const dr=drafts.current.get(spaceId+'/'+d.id);if(dr){await dr.flush();drafts.current.delete(spaceId+'/'+d.id)}}}
     for(const d of standalone){try{await flush(d.id);const n=await api.getDocument(spaceId,d.id);await api.trashDocument(spaceId,d.id,n.revision);setTabs(old=>old.filter(t=>t.id!==d.id));drafts.current.delete(spaceId+'/'+d.id);if(activeId===d.id)navigate(`/s/${spaceId}`);notes++}catch(e){failures.push(d.path+': '+msg(e))}}
      for(const f of selectedFolders){try{await flushMindMapDrafts(me?.userId||'local',spaceId);const r=await api.trashFolder(spaceId,f);setTabs(old=>removePathTabs(old,spaceId,f));if(folder===f||folder.startsWith(f+'/'))setFolder('');const am=docs.find(d=>d.id===activeId);if((am&&am.path.startsWith(f+'/'))||activeAtt.startsWith(f+'/')||activeBoard===f||activeBoard.startsWith(f+'/')||activeMap===f||activeMap.startsWith(f+'/'))navigate(`/s/${spaceId}`);foldersN++;if(r.failures?.length)failures.push(...r.failures)}catch(e){failures.push(f+': '+msg(e))}}
     for(const p of selectedAtts){try{await api.trashAttachment(spaceId,p);const tabId='att:'+encodeURIComponent(p);setTabs(old=>old.filter(t=>t.id!==tabId||t.space!==spaceId));if(activeAtt===p)navigate(`/s/${spaceId}`);attsN++}catch(e){failures.push(p+': '+msg(e))}}
     await refreshDocs();tick(x=>x+1);setSel({docs:[],folders:[]});
     const total=notes+foldersN+attsN;
     notify(`Trashed ${total} item${total===1?'':'s'}. Restore them from the trash view.`);
     if(failures.length)setError(failures.join('\n'));
    });
   };
   // Convert a PDF attachment into a new Markdown note beside it. The original PDF is never
   // modified or removed; extraction is best-effort and the note links to the source PDF.
   const convertPdfToNote=async(path:string)=>{
    if(!writable){setError('This vault is read-only.');return}
    const scope=spaceId;
    setPdfConverting(true);
    try{
     const res=await fetch(api.attachmentURL(scope,path));if(!res.ok)throw new Error('Could not read the PDF.');
     const ex=await extractPdfText(await readPdfBytes(res));
     const md=buildPdfMarkdown(path,ex);
     const dir=dirOf(path),base=baseOf(path).replace(/\.pdf$/i,'')||'document';
     const taken=new Set([...docs.filter(d=>!d.tombstoneAt).map(d=>d.path.toLowerCase()),...atts.map(a=>a.path.toLowerCase())]);
     let target=(dir?dir+'/':'')+base+'.md',n=2;while(taken.has(target.toLowerCase())){target=(dir?dir+'/':'')+base+' '+n+'.md';n++}
     // A fresh nonce per attempt: a constant key would let a second conversion replay the
     // first request's cached response instead of creating the new note.
     const nonce=(crypto as Crypto&{randomUUID?:()=>string}).randomUUID?.()??Date.now().toString(36)+Math.random().toString(36).slice(2);
     const created=await api.createDocument(scope,target,md,`pdf-convert:${nonce}`);
     await refreshDocs();
     // The extraction takes seconds — if the user switched vaults meanwhile, stay put.
     if(currentSpace.current!==scope||!mounted.current)return;
     open(created.id);
     notify(`Converted ${target} (${ex.charCount} characters extracted). The original PDF is preserved.`);
    }catch(e){if(currentSpace.current===scope&&mounted.current)setError(msg(e))}
    finally{setPdfConverting(false)}
   };
 const showHistory=async(id=activeId)=>{if(!id)return;try{setHistoryText(null);const r=await api.listRevisions(spaceId,id);setHistory(r.revisions||[])}catch(e){setError(msg(e))}};
 const resolveConflictFor=async(id:string)=>{const d=drafts.current.get(spaceId+'/'+id);if(!d)return;const scope=spaceId;try{const n=await api.getDocument(scope,id);if(currentSpace.current===scope)setConflict({draft:d,text:n.markdown,revision:n.revision,base:d.base})}catch(e){setError(msg(e))}};
 const resolveConflict=()=>resolveConflictFor(activeId);
 const headings=(draft?.text||'').split('\n').filter(s=>/^#{1,6} /.test(s)).map(s=>({level:s.match(/^#+/)![0].length,label:s.replace(/^#+\s+/,'')}));
 const route=(name:string)=>{if(name==='graph2'||name==='memos'||name==='boards'||name==='maps')setSidebar(false);navigate(`/s/${spaceId}/${name}`)};
 const togglePanel=(name:typeof panel)=>setPanel(old=>old===name?'':name);
  const kbRef=useRef({create, reopenClosed, writable});
  kbRef.current={create, reopenClosed, writable};
  useEffect(()=>{const listener=(e:KeyboardEvent)=>{if(!(e.ctrlKey||e.metaKey))return;if(e.key==='o'||e.key==='p'){e.preventDefault();setQuickQuery(e.key==='p'?'>':'');setQuick(true)}if(e.key==='n'||e.key==='N'){e.preventDefault();if(kbRef.current.writable)kbRef.current.create()}if(e.key==='f'){e.preventDefault();if(e.shiftKey){setSidebar(true);setNav('search')}else{setFindOpen(true);requestAnimationFrame(()=>findInput.current?.focus())}}if(e.key==='e'){e.preventDefault();setDocModeRef.current(m=>m==='source'?'read':'source')}if((e.key==='t'||e.key==='T')&&e.shiftKey){e.preventDefault();kbRef.current.reopenClosed()}};window.addEventListener('keydown',listener);return()=>window.removeEventListener('keydown',listener)},[]);
 useEffect(()=>{if(!findOpen||!findQuery){setFindCount(0);return}const body=mode==='visual'?document.querySelector('.visual-content'):preview.current;const source=draft?.text||'';const q=findQuery.toLocaleLowerCase();let count=0;for(let pos=0;(pos=source.toLocaleLowerCase().indexOf(q,pos))>=0;pos+=q.length)count++;setFindCount(count);
  if(mode==='source'||!body)return;const walker=document.createTreeWalker(body,NodeFilter.SHOW_TEXT);const matches:{node:Node;at:number}[]=[];while(walker.nextNode()){const node=walker.currentNode,value=node.textContent?.toLocaleLowerCase()||'';for(let at=0;(at=value.indexOf(q,at))>=0;at+=q.length)matches.push({node,at})}if(!matches.length)return;const m=matches[((findStep%matches.length)+matches.length)%matches.length];const range=document.createRange();range.setStart(m.node,m.at);range.setEnd(m.node,m.at+findQuery.length);const selection=window.getSelection();selection?.removeAllRanges();selection?.addRange(range);(m.node.parentElement as HTMLElement|null)?.scrollIntoView({block:'center'});
 },[findOpen,findQuery,findStep,mode,activeId,epoch,draft]);
 const logout=async()=>{for(const d of drafts.current.values()){await d.flush();if(d.dirty){setError('Save or export your unsaved documents before signing out.');return}}await signOut();navigate('/auth')};
 const request=async(path:string,method='GET',body?:unknown)=>{const r=await fetch('/api/v1'+path,{method,headers:{'Content-Type':'application/json','X-Netsidian-Requested':'1'},body:body===undefined?undefined:JSON.stringify(body)});const v=await r.json();if(!r.ok)throw new Error(v.error?.message||'Request failed');return v};
 const loadShare=async()=>setShare(true);
 const attachLocal=async()=>{try{const r=await api.attachNativeVault();if(!r.cancelled){await refresh();setPicker(false);setNewVault(false);navigate('/s/'+r.space.id);notify(r.loading?'Vault opened. Notes are loading in the background.':`Attached ${r.documents} Markdown documents from the selected folder.`)}}catch(e){setError(msg(e))}};
 const attachFolder=async(path:string)=>{setFolderBrowse(false);try{const r=await api.attachLocalVault(path);if(r.cancelled)return;await refresh();setPicker(false);setNewVault(false);navigate('/s/'+r.space.id);notify(r.loading?'Vault opened. Notes are loading in the background.':`Attached ${r.documents} Markdown documents from the selected folder.`)}catch(e){setError(msg(e))}};
  const deleteVault=()=>{const detach=native||externalVault;const run=async()=>{for(const [k,d] of drafts.current)if(k.startsWith(spaceId+'/')){await d.flush();if(d.dirty)throw new Error(`Save or export unsaved changes before ${detach?'detaching':'deleting'} this vault.`)}await request('/spaces/'+spaceId,'DELETE',{confirmation:curName});setTabs(old=>old.filter(t=>t.space!==spaceId));await refresh();navigate('/s/'+(spaces.find(s=>s.space.id!==spaceId)?.space.id||''))};if(detach)confirmAsk('Detach local vault',`Detach “${curName}”? The vault is removed from Netsidian; its files stay on disk and can be opened again.`,run,'Yes','No');else ask('Delete vault','Type the vault name to permanently delete it','',async name=>{if(name!==curName)throw new Error('Type '+curName+' to confirm.');await run()})};
 const visible=useMemo(()=>docs.filter(d=>nav==='trash'?!!d.tombstoneAt:!d.tombstoneAt),[docs,nav]);
 const treeMatches=useMemo(()=>{const q=treeQuery.trim().toLocaleLowerCase();return q?visible.filter(d=>d.path.toLocaleLowerCase().includes(q)):visible},[visible,treeQuery]);
 const treeFilter=treeTool==='filter'?treeQuery.trim().toLocaleLowerCase():'';
 const treeDocs=treeTool==='filter'?treeMatches:visible;
 const treeAtts=treeFilter?atts.filter(a=>a.path.toLocaleLowerCase().includes(treeFilter)):atts;
 const treeFolders=treeFilter?folders.filter(f=>f.toLocaleLowerCase().includes(treeFilter)||treeDocs.some(d=>d.path.startsWith(f+'/'))||treeAtts.some(a=>a.path.startsWith(f+'/'))):folders;
 const explorerEmpty=<div className="explorer-empty"><Icon name={treeFilter?'search':'file'} size={30}/><strong>{treeFilter?'No files match this filter':'No files yet'}</strong>{treeFilter?<><p>Try a different filter to find your files.</p><button className="btn small" onClick={()=>setTreeQuery('')}>Clear filter</button></>:writable?<><p>Create a note or import Markdown documents and attachments. You can also drop files here.</p><div className="explorer-empty-actions"><button className="btn small primary" onClick={()=>createUntitled('')}><Icon name="plus" size={13}/> New note</button><button className="btn small" onClick={()=>files.current?.click()}><Icon name="upload" size={13}/> Import files</button></div></>:<p>This vault is read-only. Its owner or an editor can add files.</p>}</div>;
  useEffect(()=>{const handler=(event:Event)=>{const detail=(event as CustomEvent<{target:string;documentId:string}>).detail;setMissingLink(detail.target);setMissingSource(detail.documentId);setRepairQuery('')};window.addEventListener('netsidian:missing-link',handler);return()=>window.removeEventListener('netsidian:missing-link',handler)},[]);
  const commandResults=[['New document',()=>create()],['Search documents',()=>{setNav('search');setSidebar(true)}],['Open graph',()=>route('graph2')],['Open memos',()=>route('memos')],['Open boards',()=>route('boards')],['Open maps',()=>route('maps')],['Open today’s daily note',()=>void dailyNote()],['Toggle reading / source',()=>setDocMode(m=>m==='read'?'source':'read')],['Toggle local graph',()=>togglePanel('graph')],['Settings and appearance',()=>route('settings')],['Reopen closed tab',()=>reopenClosed()],['Import Markdown files',()=>files.current?.click()]] as [string,()=>void][];
 const quickDocs=useMemo(()=>quick&&!quickQuery.startsWith('>')?rankLinkDocuments(docs.filter(d=>!d.tombstoneAt),quickQuery):[],[quick,quickQuery,docs]);
 const quickResults=quickQuery.startsWith('>')?commandResults.filter(([label])=>label.toLowerCase().includes(quickQuery.slice(1).trim().toLowerCase())).map(([label,action])=>({id:label,label,path:'',action})):quickDocs.map(d=>({id:d.id,label:d.title||title(d.path),path:d.path,action:()=>open(d.id)}));
 useEffect(()=>{if(!quick){setQuickIndex(0);setQuickLimit(100)}},[quick]);
return <div className={'obsidian-workspace '+(!sidebar?'sidebar-hidden':'')+(paneLayout.compact?' inspector-compact':'')} onDragStart={e=>{if(e.dataTransfer.types.includes(DRAG_KEY))setFileMoveActive(true)}} onDragOver={e=>{if(e.dataTransfer.types.includes('Files'))e.preventDefault()}} onDrop={e=>{if(e.defaultPrevented)return;if(e.dataTransfer.types.includes('Files')){e.preventDefault();if(writable&&e.dataTransfer.files.length)void importFiles(e.dataTransfer.files);else if(!writable)setError('This vault is read-only.')}}}>
  <aside className="ribbon" aria-label="Workspace tools"><Tool caption="Files" icon="sidebar" label="Toggle file explorer" active={sidebar} onClick={()=>setSidebar(!sidebar)}/><Tool caption="New" icon="plus" label="New document (Ctrl+N; hold Shift to choose path)" onClick={create} disabled={!writable}/><Tool caption="Find" icon="search" label="Quick switcher (Ctrl+O)" onClick={()=>{setQuickQuery('');setQuick(true)}}/><Tool caption="Graph" icon="graph" label="Open graph view" active={page==='graph2'} onClick={()=>route('graph2')}/><Tool caption="Memos" icon="text" label="Open memos" active={page==='memos'} onClick={()=>route('memos')}/><Tool caption="Boards" icon="group" label="Open boards" active={page==='boards'} onClick={()=>route('boards')}/><Tool caption="Maps" icon="mindmap" label="Open maps" active={page==='maps'} onClick={()=>route('maps')}/><div className="spacer"/><Tool caption="Help" icon="help" label="Help and keyboard shortcuts" onClick={()=>setHelp(true)}/><Tool caption="Settings" icon="settings" label="Settings and appearance" active={page==='settings'} onClick={()=>route('settings')}/><span className="ribbon-logo" role="img" aria-label="Netsidian"><AppLogo size={28}/></span></aside>
  {sidebar&&<><button className="mobile-scrim" aria-label="Close file explorer" onClick={()=>setSidebar(false)}/><aside className="explorer" style={{width:`min(${paneLayout.sidebar}px, 85vw)`}}><div className="explorer-tabs"><Tool icon="folder" label="Files" active={nav==='files'} onClick={()=>setNav('files')}/><Tool icon="star" label="Bookmarks" active={nav==='bookmarks'} onClick={()=>setNav('bookmarks')}/><Tool icon="tag" label="Tags" active={nav==='tags'} onClick={()=>setNav('tags')}/>{/* Drag-to-trash: attached vaults recycle nothing, read-only vaults can't trash —
       so the drop zone only exists where trashing is actually allowed. */}
     {!externalVault&&<span className={'trash-drop'+(fileMoveActive&&writable?' available':'')+(trashHot&&writable?' hot':'')} title="Drag notes, folders or files here to move them to trash" onDragOver={e=>{if(!writable||!e.dataTransfer.types.includes(DRAG_KEY))return;e.preventDefault();e.dataTransfer.dropEffect='move';if(!trashHot)setTrashHot(true)}} onDragLeave={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node|null))setTrashHot(false)}} onDrop={e=>{setTrashHot(false);if(!writable)return;const payload=parseDragPayload(e.dataTransfer.getData(DRAG_KEY));if(!payload)return;e.preventDefault();e.stopPropagation();dragTrashAsk(payload)}}><Tool icon="bin" label="Open trash" active={nav==='trash'} onClick={()=>setNav('trash')}/></span>}<div className="spacer"/><Tool icon="sidebar" label="Collapse explorer" onClick={()=>setSidebar(false)}/></div>
    {nav==='files'&&<><div className="tree-search"><input aria-label="Find in document tree" placeholder={treeTool==='filter'?'Filter files…':'Go to file…'} value={treeQuery} onChange={e=>setTreeQuery(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'&&treeMatches[0]){setRevealDoc(treeMatches[0].id);void previewDoc(treeMatches[0].id);setRevealTick(x=>x+1)}if(e.key==='Escape')setTreeQuery('')}}/><Tool icon="eye" label="Go to first matching document" active={treeTool==='goto'} onClick={()=>{setTreeTool('goto');if(treeMatches[0]){setRevealDoc(treeMatches[0].id);void previewDoc(treeMatches[0].id);setRevealTick(x=>x+1)}}}/><Tool icon="sort" label="Filter document tree" active={treeTool==='filter'} onClick={()=>setTreeTool('filter')}/></div><div className="file-actions"><Tool icon="fileplus" label="New document (Untitled)" onClick={()=>createUntitled()} disabled={!writable}/><Tool icon="folderplus" label="New folder" onClick={()=>createFolder()} disabled={!writable}/><Tool icon="upload" label="Import Markdown files and attachments" onClick={()=>files.current?.click()} disabled={!writable}/><Tool icon="sort" label={'Sort: '+(sortMode==='name'?'by name — click for modified time':'by modified time — click for name')} onClick={()=>setSortMode(s=>s==='name'?'mtime':'name')}/><Tool icon="eye" label="Reveal active document" onClick={()=>{setSidebar(true);setRevealTick(x=>x+1)}}/></div>
      <FileTree mindMapPaths={mindMaps.map(m=>m.path)} onOpenMindMap={openMap} boardPaths={boards.map(b=>b.path)} onOpenBoard={openBoard} onCreateNote={path=>createUntitled(path)} onCreateFolder={path=>createFolder(path)} onCreateBoard={path=>setCreateBoardIn(path)} onCreateMap={path=>setCreateMapIn(path)} onCreateMemo={path=>createMemoIn(path)} canCreate={writable} docs={treeDocs} folders={treeFolders} selected={revealDoc||activeId} selection={sel} onSelect={setSel} onPreview={id=>void previewDoc(id)} onOpen={id=>{const d=docs.find(d=>d.id===id);const b=boards.find(b=>d?.path===b.path+'/board.md');const m=mindMaps.find(m=>d?.path===m.path+'/mindmap.md');m?openMap(m.path):b?openBoard(b.path):open(id)}} onFolder={setFolder} onMenu={(doc,x,y)=>setMenu({doc,x,y})} onMenuFolder={(path,x,y)=>setFolderMenu({path,x,y})} onMoveDocs={(ids,f)=>void moveDocsTo(ids,f)} onMoveFolder={(from,to)=>void moveFolderTo(from,to)} onRename={(doc,fp)=>{if(doc)renameAsk(doc);else if(fp)renameFolderAsk(fp)}} onSelMenu={(x,y)=>setSelMenu({x,y})} onBlank={()=>setFolder('')} initialCollapsed={collapsedFolders} resetKey={spaceId} onCollapse={setCollapsedFolders} filterQuery={treeTool==='filter'?treeQuery:''} sortBy={sortMode} revealTick={revealTick} attachments={treeAtts} onOpenAttachment={openAttachmentPath} onMenuAttachment={(path,x,y)=>setAttMenu({path,x,y})} onMoveAtts={(paths,f)=>void moveAttsTo(paths,f)} canImport={writable} onImportFiles={(files,dest)=>void importFiles(files,dest,false)} emptyState={vaultIndexing?<div className="explorer-empty" role="status">Loading vault files…</div>:docsLoaded&&explorerLoaded?explorerEmpty:<div className="explorer-empty" role="status">Loading files…</div>}/></>}
   {nav==='search'&&<div className="explorer-results"><input aria-label="Search documents" autoFocus placeholder="Search this vault…" value={query} onChange={e=>setQuery(e.target.value)} onKeyDown={e=>{if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();setSearchIndex(i=>Math.max(0,Math.min(hits.length-1,i+(e.key==='ArrowDown'?1:-1))))}if(e.key==='Enter'&&hits[searchIndex]){e.preventDefault();open(hits[searchIndex].documentId)}}}/>{searchError?<div role="alert"><p>Search could not load. {searchError}</p><button className="btn" onClick={()=>setSearchRetry(n=>n+1)}>Retry search</button></div>:<small role="status">{searching?'Searching…':`${hits.length} results`}</small>}{!searchError&&hits.map((h,i)=><button key={h.documentId} aria-current={i===searchIndex?'true':undefined} onClick={()=>open(h.documentId)}><strong>{h.title||title(h.path)}</strong><small>{h.path}</small><SearchExcerpt text={h.snippet} query={query}/></button>)}</div>}
   {nav==='bookmarks'&&<div className="explorer-results"><small>{bookmarks.length} bookmarked in this vault</small>{bookmarks.map(id=>{const d=docs.find(x=>x.id===id&&!x.tombstoneAt);return d&&<div className="bookmark-row" key={id}><button onClick={()=>open(id)}><Icon name="star" size={12}/><span>{title(d.path)}</span></button><button className="btn small" aria-label={'Remove bookmark for '+title(d.path)} disabled={!bookmarks.length} onClick={()=>setBookmarks(b=>b.filter(x=>x!==id))}>Remove</button></div>})}{!bookmarks.length&&<p className="muted">No bookmarks yet. Use the star in a document's toolbar to save it here.</p>}</div>}
   {nav==='tags'&&<div className="explorer-results"><h3>Tags</h3>{tags.map(t=><button key={t.tag} onClick={()=>{setQuery('tag:'+t.tag);setNav('search')}}><span>#{t.tag}</span><small>{t.count}</small></button>)}{!tags.length&&<p className="muted">Add #tags to your documents to organize them.</p>}</div>}
    {nav==='trash'&&<div className="explorer-results"><h3>Trash</h3>{visible.map(d=><div key={d.id} className="trash-row"><span>{title(d.path)}</span><small>{d.path}</small><button className="btn small" disabled={!writable} onClick={()=>void api.restoreDocument(spaceId,d.id).then(()=>refreshDocs()).catch(e=>setError(msg(e)))}>Restore</button></div>)}{ftrashes.map(f=>{const isAtt=f.kind==='attachment';return <div key={f.trashId} className={'trash-row '+(isAtt?'attachment-trash':'folder-trash')}><span>{isAtt&&<Icon name="file" size={13}/>}{title(f.path)}</span><small>{isAtt?'file':`folder · ${f.docIds?.length||0} note${(f.docIds?.length||0)===1?'':'s'}`} · {new Date(f.trashedAt).toLocaleString()}</small><button className="btn small" disabled={!writable} onClick={()=>void api.restoreFolder(spaceId,f.trashId).then(r=>{refreshDocs();const problems=[...(r.conflicts?.length?[`Kept existing files; trashed copies stay in Trash so you can retry: ${r.conflicts.join(', ')}`]:[]),...(r.failures||[])];problems.length?api.listFolderTrashes(spaceId).then(x=>setFTrashes(x.folders)).catch(()=>{}):setFTrashes(old=>old.filter(x=>x.trashId!==f.trashId));notify(problems.length?'Some items remain in Trash; resolve the reported conflicts and retry.':isAtt?`Restored ${f.path}.`:`Restored ${f.path} (${r.trashed} note${r.trashed===1?'':'s'}${r.restored?`, ${r.restored} file${r.restored===1?'':'s'}`:''}).`);if(problems.length)setError(problems.join('\n'))}).catch(e=>setError(msg(e)))}>{isAtt?'Restore file':'Restore folder'}</button></div>})}{!visible.length&&!ftrashes.length&&<p className="muted">Trash is empty.</p>}</div>}
   <div className="space-footer"><button id="space-switcher" className="vault-switch" title="Open vault picker" onClick={()=>setPicker(true)}><Icon name="book" size={14}/><span>{curName}</span></button><Tool icon="plus" label="New vault" onClick={()=>setNewVault(true)}/>{!native&&userRole==='owner'&&<Tool icon="trash" label={externalVault?"Detach vault":"Delete vault"} onClick={deleteVault}/>}<Tool icon="settings" label="Vault settings and sharing" onClick={()=>void loadShare()}/></div>
  </aside><div className="pane-resizer" role="separator" aria-valuemin={180} aria-valuemax={Math.round(paneLayout.maxSidebar)} aria-valuenow={Math.round(paneLayout.sidebar)} aria-orientation="vertical" tabIndex={0} title="Drag to resize explorer (or arrow keys)" aria-label="Resize explorer" onPointerDown={startResize('sidebar')} onKeyDown={e=>{if(e.key==='ArrowLeft'||e.key==='ArrowRight')setWidths(w=>({...w,sidebar:Math.max(180,Math.min(paneLayout.maxSidebar,paneLayout.sidebar+(e.key==='ArrowRight'?16:-16)))}))}}/></>}
  <main className="workspace-main"><div className="tabbar-row"><div className="tabbar" ref={tabStrip} role="navigation" aria-label="Open documents" onKeyDown={e=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;const buttons=[...e.currentTarget.querySelectorAll<HTMLButtonElement>('.document-tab>button:first-child')];const i=buttons.indexOf(document.activeElement as HTMLButtonElement);if(i<0)return;e.preventDefault();buttons[e.key==='Home'?0:e.key==='End'?buttons.length-1:(i+(e.key==='ArrowRight'?1:buttons.length-1))%buttons.length]?.focus()}} onWheel={e=>{const el=e.currentTarget;if(Math.abs(e.deltaY)>Math.abs(e.deltaX))el.scrollLeft+=e.deltaY}}>{tabs.filter(t=>t.space===spaceId).map(t=><div className={'document-tab '+(t.id===activeId&&page==='document'?'active':'')+(t.transient?' transient':'')} key={t.space+t.id} onContextMenu={e=>{e.preventDefault();setTabMenu({tab:t,x:e.clientX,y:e.clientY})}} onKeyDown={e=>{if(e.key==='ContextMenu'||(e.shiftKey&&e.key==='F10')){e.preventDefault();const r=e.currentTarget.getBoundingClientRect();setTabMenu({tab:t,x:r.left,y:r.bottom})}}} draggable onDragStart={()=>{tabDrag.current=t}} onDragOver={e=>{if(tabDrag.current)e.preventDefault()}} onDrop={()=>{const from=tabDrag.current;tabDrag.current=null;if(!from||from===t)return;setTabs(old=>{const i=old.indexOf(from),j=old.indexOf(t);if(i<0||j<0)return old;const c=[...old];c.splice(i,1);c.splice(j,0,from);return c})}} title="Drag to reorder"><button aria-current={t.id===activeId&&page==='document'?'page':undefined} onClick={()=>navigate(workspaceTabURL(t))}><Icon name={t.map?"mindmap":t.board?"group":t.pinned?"pin":"file"} size={14}/><span>{title(t.path)}</span>{drafts.current.get(t.space+'/'+t.id)?.dirty&&<span title="Unsaved changes">•</span>}</button>{t.transient&&<button className="keep-tab" title="Keep this tab" aria-label={'Keep '+title(t.path)+' open'} onClick={()=>setTabs(old=>old.map(x=>x.space===t.space&&x.id===t.id?{...x,transient:false}:x))}><Icon name="pin" size={12}/></button>}<button className="close-tab" aria-label={'Close '+title(t.path)} disabled={!!t.pinned} onClick={()=>void closeTab(t)}><Icon name="close" size={12}/></button></div>)}{toolTabs.filter(t=>t.space===spaceId).map(t=><div key={t.page} className={'document-tab tool-tab '+(page===t.page?'active':'')}><button aria-label={'Switch to '+(t.page==='memos'?'Memos':t.page==='maps'?'Maps':t.page==='boards'?'Boards':'Graph')+' tab'} aria-current={page===t.page?'page':undefined} onClick={()=>route(t.page)}><Icon name={t.page==='memos'?'text':t.page==='maps'?'mindmap':t.page==='boards'?'group':'graph'} size={14}/><span>{t.page==='memos'?'Memos':t.page==='maps'?'Maps':t.page==='boards'?'Boards':'Graph'}</span></button><button className="close-tab" aria-label={'Close '+(t.page==='memos'?'Memos':t.page==='maps'?'Maps':t.page==='boards'?'Boards':'Graph')+' tab'} onClick={()=>{setToolTabs(old=>old.filter(x=>x!==t));if(page===t.page){const doc=tabs.filter(x=>x.space===spaceId).pop(),other=toolTabs.find(x=>x.space===spaceId&&x!==t);navigate(doc?workspaceTabURL(doc):other?'/s/'+spaceId+'/'+other.page:'/s/'+spaceId)}}}><Icon name="close" size={12}/></button></div>)}{page!=='document'&&page!=='memos'&&page!=='graph2'&&page!=='boards'&&page!=='maps'&&<div className="document-tab active"><span>{'Settings'}</span><Tool icon="close" label="Close tool tab" onClick={()=>{const t=tabs.filter(t=>t.space===spaceId).pop();navigate(t?workspaceTabURL(t):`/s/${spaceId}`)}}/></div>}<Tool icon="plus" label="New document" onClick={()=>createUntitled()} disabled={!writable}/></div>{tabOverflow&&<div className="tabbar-nav"><Tool icon="chevronL" label="Scroll tabs left" onClick={()=>scrollTabs(-1)}/><Tool icon="list" label="List open tabs" active={tabListOpen} onClick={openTabList}/><Tool icon="chevron" label="Scroll tabs right" onClick={()=>scrollTabs(1)}/></div>}<div className="spacer"/><AccountMenu onSettings={()=>{setAccountRequest(n=>n+1);route('settings')}} onSignOut={logout}/></div>
  {spacesError&&<div className="workspace-error" role="alert"><span>Could not refresh vaults: {spacesError}</span><button className="btn small" onClick={()=>void refresh()}>Retry</button></div>}
  {error&&<div className="workspace-error" role="alert"><span>{error}</span><Tool icon="close" label="Dismiss error" onClick={()=>setError('')}/></div>}
  <div className="workspace-body"><section className="document-pane">
   {toolTabs.some(t=>t.space===spaceId&&t.page==='memos')&&<div className="workspace-tool-pane" hidden={page!=='memos'}><MemosPage composerMode={interaction.memoComposerMode} key={spaceId} space={spaceId} docs={docs} folders={folders} writable={writable} trashable={!externalVault} onOpen={open} onSaved={refreshDocs} onMoveToBoard={m=>setBoardMove({id:m.id,path:m.path,title:m.title})}/></div>}
    {toolTabs.some(t=>t.space===spaceId&&t.page==='graph2')&&<div className="workspace-tool-pane" hidden={page!=='graph2'}><Graph2 minimapDefault={interaction.canvasMinimap} space={spaceId} onOpen={open} epoch={graphEpoch}/></div>}
    {toolTabs.some(t=>t.space===spaceId&&t.page==='boards')&&<div className="workspace-tool-pane" hidden={page!=='boards'}><BoardsPage key={spaceId} space={spaceId} boards={boards} folders={folders} docs={docs} writable={writable} boardsStatus={boardsStatus} boardsError={boardsError} onRetryBoards={refreshBoards} onOpenBoard={openBoard} onChanged={()=>{void refreshBoards();void refreshMaps();void refreshDocs();tick(x=>x+1)}}/></div>}
    {toolTabs.some(t=>t.space===spaceId&&t.page==='maps')&&<div className="workspace-tool-pane" hidden={page!=='maps'}><MindMapsPage key={spaceId} space={spaceId} mindMaps={mindMaps} folders={folders} docs={docs} writable={writable} mapsStatus={mapsStatus} mapsError={mapsError} onRetryMaps={refreshMaps} onOpenMap={openMap} onChanged={()=>{refreshMaps();void refreshDocs()}}/></div>}
       {page==='memos'||page==='graph2'||(page==='boards'||page==='maps')?null:page==='settings'?<div className="settings-scroll"><SettingsPage accountRequest={accountRequest} onAccountOpened={()=>setAccountRequest(0)} appearance={<AppearancePanel value={appearance} onChange={update}/>} interaction={<InteractionPanel value={interaction} onChange={updateInteraction}/>} initialVault={spaceId} agentRequest={agentRequest} onAgentOpened={()=>setAgentRequest(0)}/></div>:activeAtt?<AttachmentPane space={spaceId} path={activeAtt} writable={writable} converting={pdfConverting} onConvert={()=>void convertPdfToNote(activeAtt)} onClose={()=>{const t=tabs.find(t=>t.space===spaceId&&t.id===activeId);if(t)void closeTab(t);else navigate(`/s/${spaceId}`)}}/>:textFile&&!activeId?<TextFileEditor space={spaceId} path={textFile} onClose={()=>setTextFile(null)} onMutated={()=>void refreshDocs()}/>:activeMap?<MindMapView previewActivation={interaction.mapPreviewClick} inlineEditing={interaction.mapInlineEditing} minimapDefault={interaction.canvasMinimap} key={spaceId+':'+activeMap} space={spaceId} onUploadImages={uploadImages} mapPath={activeMap} writable={writable} docs={docs} attachments={atts} epoch={graphEpoch} principal={me?.userId||'local'} onPreviewDraft={previewDraft} onResolveConflict={resolveConflictFor} onOpenDoc={open} onFlushDraft={flushBoardDrafts} onInvalidateDoc={invalidateDoc} onChanged={()=>{void refreshDocs();refreshMaps();refreshGraph(x=>x+1)}} onRevealFolder={p=>{setSidebar(true);setNav('files');setFolder(p)}}/>:activeBoard?<BoardView minimapDefault={interaction.canvasMinimap} key={spaceId+':'+activeBoard} space={spaceId} boardPath={activeBoard} writable={writable} docs={docs} attachments={atts} epoch={graphEpoch} principal={me?.userId||'local'} onPreviewDraft={previewDraft} onResolveConflict={resolveConflictFor} onUploadImages={uploadImages} onOpenDoc={open} onFlushDraft={flushBoardDrafts} onInvalidateDoc={invalidateDoc} onChanged={()=>{void refreshDocs();void refreshBoards();void refreshMaps();tick(x=>x+1)}} onMoveToBoard={id=>{const d=docs.find(x=>x.id===id);setBoardMove({id,path:d?.path||'',title:d?.title||title(d?.path||'')})}} onRevealFolder={p=>{setSidebar(true);setNav('files');setFolder(p)}}/>:draft&&note?<>
    <div className="document-toolbar"><div className="nav-history"><Tool icon="arrow" label="Back in navigation history" onClick={goBack}/><Tool icon="arrowR" label="Forward in navigation history" onClick={goForward}/></div><DocumentPath key={activeId} path={activeMeta?.path||note.path} folders={folders} docs={docs} id={activeId!} editable={writable&&draft.canEdit} vaultName={curName} onOpen={open} onRename={()=>{if(activeMeta)renameAsk(activeMeta)}} onMove={async folder=>{const d=activeMeta;if(!d)throw new Error('Document is still loading.');autoName.current=null;await rename(d,(folder?folder+'/':'')+baseOf(d.path));notify('Document moved.')}}/><div className="toolbar-group view-modes" role="group" aria-label="Document editing mode"><Tool icon="code" label="Source mode" active={mode==='source'} onClick={()=>setDocMode('source')}/><Tool icon="book" label="Reading mode" active={mode==='read'} onClick={()=>setDocMode('read')}/><Tool icon="split" label="Split editor and preview" active={mode==='split'} onClick={()=>setDocMode('split')}/><Tool icon="edit" label="Visual Markdown editor" active={mode==='visual'} onClick={()=>setDocMode('visual')}/></div><div className="toolbar-group" role="group" aria-label="Document layout"><Tool icon={wide?"contract":"expand"} label={wide?"Use readable document width":"Use 90% document width"} active={wide} onClick={()=>setWide(x=>!x)}/><Tool icon="graph" label="Local graph" active={panel==='graph'} onClick={()=>togglePanel('graph')}/><Tool icon="sidebar" label="Toggle right sidebar" active={!!panel} onClick={()=>togglePanel('outline')}/></div><div className="toolbar-group" role="group" aria-label="Document actions"><DocumentTagMenu key={activeId} text={draft.text} onChange={text=>draft.change(text)} editable={writable&&draft.canEdit} known={[...new Set([...tags.map(t=>t.tag),...docs.flatMap(d=>d.tags||[])])]} onSearch={tag=>{setQuery('tag:'+tag);setNav('search');setSidebar(true)}}/><Tool icon="star" label={activeId&&bookmarks.includes(activeId)?'Remove bookmark':'Bookmark this document'} active={!!activeId&&bookmarks.includes(activeId)} onClick={()=>setBookmarks(b=>!activeId?b:b.includes(activeId)?b.filter(x=>x!==activeId):[...b,activeId])}/><Tool icon="group" label="Move document to a board" disabled={!writable} onClick={()=>{const d=activeMeta;if(d)setBoardMove({id:d.id,path:d.path,title:d.title||title(d.path)})}}/><Tool icon="more" label="Document actions" onClick={()=>{const d=activeMeta;if(d){const r=document.querySelector('button[aria-label="Document actions"]')?.getBoundingClientRect();setMenu({doc:d,x:r?r.right-220*uiZoom():8,y:r?r.bottom+4:8})}}}/></div></div>
    {findOpen&&<div className="document-find" role="search"><input ref={findInput} aria-label="Find in document" placeholder="Find in document…" value={findQuery} onChange={e=>{setFindQuery(e.target.value);setFindStep(0)}} onKeyDown={e=>{if(e.key==='Enter'){e.preventDefault();setFindStep(x=>x+(e.shiftKey?-1:1))}if(e.key==='Escape')setFindOpen(false)}}/><small>{findQuery?findCount+' matches':''}</small><Tool icon="arrow" label="Previous match" onClick={()=>setFindStep(x=>x-1)}/><Tool icon="arrowR" label="Next match" onClick={()=>setFindStep(x=>x+1)}/><Tool icon="close" label="Close document search" onClick={()=>setFindOpen(false)}/></div>}
    {(draft.status==='error'||draft.status==='conflict')&&<div className="save-error" role="alert">{draft.error}<button className="btn" onClick={()=>draft.status==='conflict'?void resolveConflict():void draft.flush()}>{draft.status==='conflict'?'Resolve conflict':'Retry save'}</button><button className="btn" onClick={()=>download(note.path,draft.text)}>Export draft</button></div>}
    <div className={'document-views '+mode+(wide?' wide':'')}>{mode==='visual'?<Suspense fallback={<p className="muted">Loading Visual editor…</p>}><LazyVisualEditor autoFocus draft={draft} docs={docs} onTagSearch={tag=>{setQuery('tag:'+tag);setNav('search');setSidebar(true)}} onSourceMode={()=>setDocMode('source')} onUploadImages={uploadImages} attachments={atts.map(a=>({path:a.path,url:api.attachmentURL(spaceId,a.path)}))} notePath={activeMeta?.path||note?.path}/></Suspense>:mode!=='read'&&<NoteEditor draft={draft} docs={docs} notePath={activeMeta?.path||note.path} selectTitle={autoName.current?.id===activeId} find={findOpen?{query:findQuery,step:findStep}:undefined} onUploadImages={uploadImages} onImageError={m=>setError(m)}/>}<article className="markdown-preview" onDoubleClick={e=>{if(mode!=='read'||!writable||!draft.canEdit||interaction.readingDoubleClick==='none'||(e.target as Element).closest('a,button,input,select,textarea,label,[role="button"],.tag-chip'))return;setDocMode(interaction.readingDoubleClick)}} style={{display:mode==='source'||mode==='visual'?'none':undefined}}>{(mode==='read'||mode==='split')&&<DocumentTagSummary text={draft.text} onSearch={tag=>{setQuery('tag:'+tag);setNav('search');setSidebar(true)}}/>}<div data-document-id={activeId} ref={preview}/></article></div>
    <footer className="document-status"><span><b className="editor-mode-name">{mode==='read'?'Reading':mode==='source'?'Source':mode==='visual'?'Visual':'Split'}</b> · {draft.canEdit?'':'Read only · '}{draft.status==='saved'?'All changes saved':draft.status==='saving'?'Saving…':draft.status==='conflict'?'Conflict':draft.status==='error'?'Save failed':'Unsaved changes'}</span><div className="spacer"/><button onClick={()=>togglePanel('backlinks')}>Backlinks</button><span>{draft.text.trim()?draft.text.trim().split(/\s+/).length:0} words</span><span>{draft.text.length} characters</span></footer>
   </>:loading?<div className="workspace-empty">Opening document…</div>:<div className="workspace-empty"><Icon name="book" size={44}/><h1>Your documents, connected.</h1><p>Open a note from the file explorer, or start writing.</p><div><button className="btn" onClick={create} disabled={!writable}><Icon name="plus"/> New document</button><button className="btn" onClick={()=>{setQuickQuery('');setQuick(true)}}><Icon name="search"/> Open document</button></div><p className="shortcuts"><kbd>Ctrl O</kbd> Quick switcher <kbd>Ctrl N</kbd> New note <kbd>Ctrl P</kbd> Commands</p><button className="text-button" onClick={()=>files.current?.click()} disabled={!writable}>Import Markdown files</button></div>}
  </section>
  {showInspector&&<><div className="pane-resizer inspector-resizer" role="separator" aria-valuemin={260} aria-valuemax={Math.round(paneLayout.maxInspector)} aria-valuenow={Math.round(paneLayout.inspector)} aria-orientation="vertical" tabIndex={0} title="Drag to resize inspector (or arrow keys)" aria-label="Resize inspector" onPointerDown={startResize('inspector')} onKeyDown={e=>{if(e.key==='ArrowLeft'||e.key==='ArrowRight')setWidths(w=>({...w,inspector:Math.max(260,Math.min(paneLayout.maxInspector,paneLayout.inspector+(e.key==='ArrowLeft'?16:-16)))}))}}/><aside className="inspector" style={{width:`min(${paneLayout.inspector}px, 90vw)`}}><div className="inspector-tabs"><Tool icon="list" label="Document outline" active={panel==='outline'} onClick={()=>setPanel('outline')}/><Tool icon="link" label="Backlinks" active={panel==='backlinks'} onClick={()=>setPanel('backlinks')}/><Tool icon="graph" label="Local graph" active={panel==='graph'} onClick={()=>setPanel('graph')}/><div className="spacer"/><Tool icon="close" label="Close right sidebar" onClick={()=>setPanel('')}/></div>{panel==='graph'?<Graph2 minimapDefault={interaction.canvasMinimap} space={spaceId} root={activeId||undefined} onOpen={open} epoch={graphEpoch}/>:panel==='outline'?<div className="outline"><h3>Outline</h3>{headings.map((h,i)=><button key={i} style={{paddingLeft:12+(h.level-1)*12}} onClick={()=>{setDocMode('read');navigate(loc.pathname+'#'+encodeURIComponent(h.label))}}>{h.label}</button>)}{!headings.length&&<p className="muted">Headings in your document appear here.</p>}</div>:<div className="outline"><h3>Linked mentions</h3>{backlinks.map(b=><button key={b.documentId} onClick={()=>open(b.documentId)}>{b.title||title(b.path)}<small>{b.path}</small></button>)}{!backlinks.length&&<p className="muted">No backlinks yet. Link here from another document using [[{title(activeMeta?.path||'note.md')}]].</p>}</div>}</aside></>}
  </div>
  {(vaultIndexing||vaultLoad?.phase==='error'||listProgress)&&<footer className="vault-load-status" aria-label="Vault loading status">
   <span role="status">{vaultLoad?.phase==='error'?'Vault loading stopped: '+vaultLoad.error:vaultIndexing?vaultLoad?.phase==='queued'?'Waiting to load vault…':vaultLoad?.phase==='scanning'?`Discovering notes… ${vaultLoad.total} found`:vaultLoad?.phase==='resolving'?'Resolving document links…':`Loading notes… ${vaultLoad?.completed.toLocaleString()} / ${vaultLoad?.total.toLocaleString()}`:`Loading file list… ${listProgress?.done.toLocaleString()} / ${listProgress?.total.toLocaleString()}`}</span>
   {vaultLoad?.phase==='error'?<button className="btn small" onClick={()=>void api.retryVaultLoad(spaceId).then(()=>setLoadRetry(x=>x+1)).catch(e=>setError(msg(e)))}>Retry</button>:<progress aria-label="Vault loading progress" max={vaultIndexing?vaultLoad?.total||1:listProgress?.total||1} value={vaultIndexing&&['queued','scanning','resolving'].includes(vaultLoad?.phase||'')?undefined:vaultIndexing?vaultLoad?.completed:listProgress?.done}/>}
   {!!vaultLoad?.skipped&&<small>{vaultLoad.skipped} skipped</small>}
  </footer>}
  </main>
   <input ref={files} className="sr-only" aria-label="Import documents and attachments" tabIndex={-1} type="file" accept=".md,.png,.jpg,.jpeg,.gif,.webp,.svg,.pdf,.mp4" multiple onChange={e=>{if(e.target.files)void importFiles(e.target.files);e.target.value=''}}/>
  {notice&&<div className="toast" role="status">{notice}</div>}
  {tabListOpen&&<><div className="menu-dismiss" onClick={()=>setTabListOpen(false)}/><div className="file-menu tab-list-pop" role="menu" style={overlayPoint(tabListPos.x,tabListPos.y,252,330)}><small>{'Open tabs — '+tabs.filter(t=>t.space===spaceId).length}</small>{tabs.filter(t=>t.space===spaceId).map(t=><div className={'tab-list-row'+(t.id===activeId&&page==='document'?' current':'')} key={t.space+t.id}><button role="menuitem" onClick={()=>{setTabListOpen(false);navigate(workspaceTabURL(t))}}><Icon name={t.map?'mindmap':t.board?'group':t.pinned?'pin':'file'} size={13}/><span>{title(t.path)}</span></button>{t.pinned?<span className="tab-pin-hint" title="Pinned"><Icon name="pin" size={11}/></span>:<button className="close-tab" aria-label={'Close '+title(t.path)} onClick={()=>void closeTab(t)}><Icon name="close" size={12}/></button>}</div>)}</div></>}
  {tabMenu&&<><div className="menu-dismiss" onClick={()=>setTabMenu(null)}/><div className="file-menu" role="menu" style={overlayPoint(tabMenu.x,tabMenu.y,220,340)}><small>{title(tabMenu.tab.path)}</small>{[
   ['beside','Open beside (unavailable)',true],['pin',tabMenu.tab.pinned?'Unpin tab':'Pin tab',false],['close','Close',!!tabMenu.tab.pinned],['others','Close others',!tabs.some(t=>t.space===spaceId&&t.id!==tabMenu.tab.id&&!t.pinned)],['right','Close tabs to the right',!tabs.filter(t=>t.space===spaceId).slice(tabs.filter(t=>t.space===spaceId).findIndex(t=>t.id===tabMenu.tab.id)+1).some(t=>!t.pinned)],['all','Close all unpinned',!tabs.some(t=>t.space===spaceId&&!t.pinned)],['reveal','Reveal in explorer',false],['copy','Copy document path',false]
  ].map(([action,label,disabled])=><button key={String(action)} role="menuitem" disabled={!!disabled} title={action==='beside'?'Document side-by-side pane is not implemented yet':undefined} onClick={()=>tabAction(String(action),tabMenu.tab)}>{String(label)}</button>)}</div></>}
  {menu&&<><div className="menu-dismiss" onClick={()=>setMenu(null)}/><div className="file-menu" role="menu" style={overlayPoint(menu.x,menu.y,220,300)}><small>{title(menu.doc.path)}</small>{boards.some(b=>menu.doc.path===b.path+'/board.md')&&<button role="menuitem" onClick={()=>{const b=boards.find(b=>menu.doc.path===b.path+'/board.md')!;openBoard(b.path);setMenu(null)}}><Icon name="group"/>Open board</button>}{mindMaps.some(m=>menu.doc.path===m.path+'/mindmap.md')&&<button role="menuitem" onClick={()=>{const m=mindMaps.find(m=>menu.doc.path===m.path+'/mindmap.md')!;openMap(m.path);setMenu(null)}}><Icon name="mindmap"/>Open map</button>}{[['edit','Rename / move','rename'],['file','Duplicate','duplicate'],['download','Export Markdown','export'],['group','Move to board…','board'],['trash','Move to trash','trash']].map(([icon,label,action])=><button role="menuitem" key={action} disabled={(action!=='export'&&!writable)||(action==='trash'&&externalVault)} onClick={()=>void menuAction(action,menu.doc)}><Icon name={icon}/>{label}</button>)}<button role="menuitem" onClick={()=>{open(menu.doc.id);void showHistory(menu.doc.id);setMenu(null)}}><Icon name="history"/>Version history</button></div></>}
   {boardMove&&<MoveToBoardDialog key={spaceId+':'+boardMove.id} space={spaceId} doc={boardMove} boards={boards} onClose={()=>setBoardMove(null)} onMoved={async(bp,ci)=>{await moveDocToBoard(boardMove.id,bp,ci);setBoardMove(null)}}/>}
   {createBoardIn!==null&&<CreateBoardDialog space={spaceId} boards={boards} folders={folders} initialPath={createBoardIn?createBoardIn+'/New board':'New board'} onClose={()=>setCreateBoardIn(null)} onCreated={path=>{setCreateBoardIn(null);void refreshBoards();void refreshMaps();void refreshDocs();tick(x=>x+1);openBoard(path)}} onError={setError}/>}
   {createMapIn!==null&&<CreateMapDialog space={spaceId} mindMaps={mindMaps} folders={folders} initialPath={createMapIn?createMapIn+'/Untitled map':'Untitled map'} onClose={()=>setCreateMapIn(null)} onCreated={path=>{setCreateMapIn(null);void refreshMaps();void refreshDocs();openMap(path)}} onError={setError}/>}
  {folderMenu&&<><div className="menu-dismiss" onClick={()=>setFolderMenu(null)}/><div className="file-menu" role="menu" style={overlayPoint(folderMenu.x,folderMenu.y,220,210)}><small>{title(folderMenu.path)}</small>{mindMaps.some(m=>m.path===folderMenu.path)&&<button role="menuitem" onClick={()=>{openMap(folderMenu.path);setFolderMenu(null)}}><Icon name="mindmap"/>Open mind map</button>}{boards.some(b=>b.path===folderMenu.path)&&<button role="menuitem" onClick={()=>{openBoard(folderMenu.path);setFolderMenu(null)}}><Icon name="group"/>Open board</button>}<button role="menuitem" disabled={!writable} onClick={()=>{const p=folderMenu.path;setFolderMenu(null);askName('New subfolder','Folder name',p,'New folder',async n=>{if(!n||n==='.'||n==='..')throw new Error('Pick a name without dots or slashes.');await api.createFolder(spaceId,n);await refreshDocs()})}}><Icon name="plus"/>New subfolder</button><button role="menuitem" disabled={!writable} onClick={()=>{const p=folderMenu.path;setFolderMenu(null);renameFolderAsk(p)}}><Icon name="edit"/>Rename folder</button><button role="menuitem" onClick={()=>{const p=folderMenu.path;setFolderMenu(null);setSel({docs:docs.filter(d=>!d.tombstoneAt&&d.path.startsWith(p+'/')).map(d=>d.id),folders:[p]})}}><Icon name="file"/>Select notes inside</button><button role="menuitem" disabled={!writable||externalVault} onClick={()=>{const p=folderMenu.path;setFolderMenu(null);trashFolderAsk(p)}}><Icon name="trash"/>Trash folder</button></div></>}
   {attMenu&&<><div className="menu-dismiss" onClick={()=>setAttMenu(null)}/><div className="file-menu" role="menu" style={overlayPoint(attMenu.x,attMenu.y,220,190)}><small>{baseOf(attMenu.path)}</small><button role="menuitem" onClick={()=>{const p=attMenu.path;setAttMenu(null);openAttachmentPath(p)}}><Icon name="eye"/>Open</button><a role="menuitem" className="menu-link" href={api.attachmentURL(spaceId,attMenu.path)} download onClick={()=>setAttMenu(null)}><Icon name="download"/>Download</a><button role="menuitem" disabled={!writable||externalVault} title={externalVault?'Vaults attached to a folder do not support app trash':undefined} onClick={()=>{const p=attMenu.path;setAttMenu(null);trashAttachmentAsk(p)}}><Icon name="trash"/>Move to trash</button></div></>}
   {selMenu&&sel.docs.length+sel.folders.length>0&&<><div className="menu-dismiss" onClick={()=>setSelMenu(null)}/><div className="file-menu" role="menu" style={overlayPoint(selMenu.x-220*uiZoom(),selMenu.y,220,320)}><small>{[sel.docs.length?`${sel.docs.length} note${sel.docs.length===1?'':'s'}`:'',sel.folders.length?`${sel.folders.length} folder${sel.folders.length===1?'':'s'}`:''].filter(Boolean).join(' · ')}</small><button role="menuitem" disabled={!writable||!sel.docs.length} onClick={()=>{setSelMenu(null);bulkMove()}}><Icon name="folder"/>Move notes…</button>{sel.folders.length===1&&!sel.docs.length&&<><button role="menuitem" disabled={!writable} onClick={()=>{setSelMenu(null);renameFolderAsk(sel.folders[0])}}><Icon name="edit"/>Rename folder</button><button role="menuitem" disabled={!writable} onClick={()=>{setSelMenu(null);setSel({docs:docs.filter(d=>!d.tombstoneAt&&d.path.startsWith(sel.folders[0]+'/')).map(d=>d.id),folders:[]})}}><Icon name="file"/>Select notes inside</button><button role="menuitem" disabled={!writable||externalVault} onClick={()=>{setSelMenu(null);trashFolderAsk(sel.folders[0])}}><Icon name="trash"/>Trash folder…</button></>}<button role="menuitem" disabled={!writable||externalVault||!sel.docs.length} onClick={()=>{setSelMenu(null);bulkTrash()}}><Icon name="trash"/>Trash selected notes…</button><button role="menuitem" onClick={()=>{setSelMenu(null);setSel({docs:[],folders:[]})}}><Icon name="close"/>Clear selection</button></div></>}
   {prompt&&<Dialog title={prompt.title} close={()=>{if(!busy)setPrompt(null)}}><form onSubmit={e=>{e.preventDefault();const name=prompt.value.trim();if(prompt.dir!==undefined&&(name.includes('/')||name==='.'||name==='..')){setError('Names cannot contain slashes — drag items in the explorer to move them between folders.');return}const full=prompt.dir!==undefined?(prompt.dir?prompt.dir+'/':'')+name:name;setBusy(true);prompt.action(full).then(()=>setPrompt(null)).catch(e=>setError(msg(e))).finally(()=>setBusy(false))}}>{prompt.confirm?<p className="prompt-text">{prompt.label}</p>:<label>{prompt.label}{prompt.dir!==undefined?<div className="path-input"><span className="path-prefix" title={prompt.dir||'Vault root'}>{prompt.dir||'Vault root'}</span><span className="path-sep">/</span><input autoFocus data-autofocus required value={prompt.value} onFocus={e=>e.currentTarget.select()} onChange={e=>setPrompt({...prompt,value:e.target.value})}/></div>:<input autoFocus data-autofocus required value={prompt.value} onFocus={e=>e.currentTarget.select()} onChange={e=>setPrompt({...prompt,value:e.target.value})}/>}</label>}
   {error&&<p className="inline-error" role="alert">{error}</p>}<footer><button type="button" className="btn" onClick={()=>setPrompt(null)} disabled={busy}>{prompt.no||'Cancel'}</button><button className="btn primary" disabled={busy}>{busy?'Working…':prompt.yes||'Confirm'}</button></footer></form></Dialog>}
  {missingLink&&<Dialog title="Unresolved document link" close={()=>setMissingLink('')}><p><strong>{missingLink}</strong> does not exist yet.</p>{writable&&<button className="btn primary" onClick={()=>{const target=missingLink.split('#')[0].replace(/\.md$/i,'');setMissingLink('');ask('Create linked document','Document path',(target.includes('/')?'':folderOfPath(activeMeta?.path||''))+target+'.md',async path=>{const d=await api.createDocument(spaceId,path,'# '+title(path)+'\n');await refreshDocs();open(d.id)})}}>Create document…</button>}<p>Select an existing document to repair the link in the current note.</p><input data-autofocus aria-label="Find existing link target" value={repairQuery} onChange={e=>setRepairQuery(e.target.value)}/><div className="quick-results">{rankLinkDocuments(docs.filter(d=>!d.tombstoneAt),repairQuery).slice(0,30).map(d=><button key={d.id} onClick={async()=>{try{if(draft?.canEdit&&(!missingSource||missingSource===activeId)){draft.change(repairMissingWikilink(draft.text,missingLink,d.path));setMissingLink('')}else if(missingSource&&writable){const source=await api.getDocument(spaceId,missingSource);if(source.canEdit){await api.saveContent(spaceId,missingSource,repairMissingWikilink(source.markdown,missingLink,d.path),source.revision);await refreshDocs();tick(v=>v+1);notify('Document link repaired.')}else open(d.id);setMissingLink('')}else{setMissingLink('');open(d.id)}}catch(e){setError(msg(e))}}}><span>{d.title||title(d.path)}<small>{d.path}</small></span></button>)}</div></Dialog>}
  {quick&&<Dialog title={quickQuery.startsWith('>')?'Command palette':'Open document'} close={()=>setQuick(false)}><input data-autofocus role="combobox" aria-expanded="true" aria-controls="quick-options" aria-activedescendant={quickResults.length?'quick-option-'+quickIndex:undefined} aria-label="Find document or command" placeholder="Find a document… (type > for commands)" value={quickQuery} onChange={e=>{setQuickQuery(e.target.value);setQuickIndex(0);setQuickLimit(100)}} onKeyDown={e=>{if(['ArrowDown','ArrowUp','Home','End','Enter'].includes(e.key)){e.preventDefault();e.stopPropagation();if(e.key==='Enter'){quickResults[quickIndex]?.action();setQuick(false)}else{const next=e.key==='Home'?0:e.key==='End'?quickResults.length-1:Math.max(0,Math.min(quickResults.length-1,quickIndex+(e.key==='ArrowDown'?1:-1)));setQuickIndex(next);setQuickLimit(n=>Math.max(n,next+1));requestAnimationFrame(()=>document.getElementById('quick-option-'+next)?.scrollIntoView({block:'nearest'}))}}}}/><div className="quick-results" id="quick-options" role="listbox" aria-label="Matching documents or commands">{quickResults.slice(0,quickLimit).map((item,i)=><button key={item.id} id={'quick-option-'+i} role="option" aria-selected={i===quickIndex} onMouseDown={e=>e.preventDefault()} onClick={()=>{setQuick(false);item.action()}}><Icon name={quickQuery.startsWith('>')?'command':'file'}/><span>{item.label}{item.path&&<small>{item.path}</small>}</span></button>)}</div>{!quickResults.length&&<p>No matching results.</p>}{quickResults.length>quickLimit&&<button onClick={()=>setQuickLimit(n=>n+100)}>Show more results</button>}</Dialog>}
  {history&&<Dialog title="Version history" close={()=>setHistory(null)}>{historyText===null?<div className="history-list">{history.map(r=><button key={r.id} onClick={()=>void api.getRevision(spaceId,activeId,r.id).then(r=>setHistoryText(r.markdown)).catch(e=>setError(msg(e)))}>{new Date(r.createdAt).toLocaleString()}<small>{r.byteLength} bytes</small></button>)}</div>:<><pre>{historyText}</pre><footer><button className="btn" onClick={()=>setHistoryText(null)}>Back</button><button className="btn primary" disabled={!draft?.canEdit} onClick={()=>{if(draft){draft.change(historyText);setHistory(null);setDocMode('source');void draft.flush()}}}>Restore as new version</button></footer></>}</Dialog>}
  {impPending&&<Dialog title="Files already exist" close={()=>setImpPending(null)}><p>{impPending.items.filter(it=>(it.md?impPending.docs:impPending.atts).has(it.target.toLowerCase())).length} of {impPending.items.length} files already exist in this vault. Choose what to do with them.</p><h3>Existing targets</h3><ul className="break-word">{impPending.items.filter(it=>(it.md?impPending.docs:impPending.atts).has(it.target.toLowerCase())).slice(0,12).map(it=><li key={it.target}>{it.target}</li>)}</ul><footer><button className="btn" onClick={()=>setImpPending(null)}>Cancel</button><button className="btn" onClick={()=>void runImport(impPending,'skip')}>Skip existing</button><button className="btn" onClick={()=>void runImport(impPending,'both')}>Keep both</button><button className="btn primary" onClick={()=>void runImport(impPending,'overwrite')}>Overwrite</button></footer></Dialog>}
  {conflict&&<Dialog title="Resolve conflicting edits" close={()=>setConflict(null)}><p>Your draft is preserved. Compare it with the server before choosing.</p><details><summary>Base version</summary><pre>{conflict.base}</pre></details><h3>Server version</h3><pre>{conflict.text}</pre><h3>Your draft</h3><pre>{conflict.draft.text}</pre><footer><button className="btn" onClick={()=>{download(notes.current.get(conflict.draft.space+'/'+conflict.draft.id)?.path||'draft.md',conflict.draft.text)}}>Export draft</button><button className="btn" onClick={()=>{conflict.draft.useServer(conflict.text,conflict.revision);setConflict(null)}}>Use server</button><button className="btn primary" onClick={()=>void conflict.draft.keepLocal(conflict.revision).then(()=>setConflict(null))}>Save my draft</button></footer></Dialog>}
  {newVault&&<Dialog title="New vault" close={()=>setNewVault(false)}>
   <StoragePanel createOnly onFolderBrowse={localPick?()=>{setNewVault(false);setFolderBrowse(true)}:undefined} onCreated={id=>{setNewVault(false);setPicker(false);setNav('files');navigate('/s/'+id)}}/>
  </Dialog>}
  {folderBrowse&&<FolderPicker onPick={path=>void attachFolder(path)} onClose={()=>setFolderBrowse(false)}/>}
  {share&&<VaultSharing space={spaceId} name={curName} onClose={()=>setShare(false)} onAgent={()=>{setShare(false);setAgentRequest(n=>n+1);route('settings')}}/>}
  {picker&&<Dialog title="Vaults" close={()=>setPicker(false)}>
   <div className="vault-picker" role="listbox" aria-label="Your vaults">{(()=>{let rec:string[]=[];try{rec=JSON.parse(localStorage.getItem('netsidian:vault-recents:'+(me?.userId||'local'))||'[]')}catch{}
    const rank=(id:string)=>id===spaceId?-1:rec.indexOf(id)<0?99:rec.indexOf(id);
    return [...spaces].sort((a,b)=>rank(a.space.id)-rank(b.space.id)).map(s=><button key={s.space.id} role="option" aria-selected={s.space.id===spaceId} className={'vault-pick '+(s.space.id===spaceId?'active':'')} onClick={()=>{setPicker(false);setNav('files');navigate('/s/'+s.space.id)}}><Icon name="book" size={14}/><span>{s.space.name}</span><small>{s.role}{s.space.id!==spaceId&&rec.includes(s.space.id)?' · recent':''}</small></button>)})()}</div>
   <h3>{curName}</h3>
   <div className="vault-actions">
    <button className="btn primary" onClick={()=>{setPicker(false);setNewVault(true)}}><Icon name="plus"/> Add vault…</button>
    {native&&<button className="btn" onClick={()=>void attachLocal()}><Icon name="folder"/> Open local folder as vault…</button>}
    {localPick&&!native&&<button className="btn" onClick={()=>{setPicker(false);setFolderBrowse(true)}}><Icon name="folder"/> Open local folder as vault…</button>}
    {native&&userRole==='owner'&&<button className="btn" onClick={()=>{setPicker(false);deleteVault()}}><Icon name="trash"/> Detach vault</button>}
    {userRole==='owner'&&<button className="btn" onClick={()=>{setPicker(false);ask('Rename vault','Vault name',curName,async n=>{await api.renameVault(spaceId,n);await refresh()})}}>Rename vault</button>}
    <button className="btn" onClick={()=>void api.exportVault(spaceId,curName).catch(e=>setError(msg(e)))}>Export vault</button>
    <button className="btn" onClick={()=>{setPicker(false);void loadShare()}}>Sharing</button>
   </div>
   <p className="muted">Closing a vault just switches away; nothing is deleted. Export downloads Markdown, attachments and folders with relative paths plus a separable metadata manifest. Archive and destructive actions stay in vault settings.</p>
  </Dialog>}
  {attView&&<div className="att-preview" role="dialog" aria-modal="true" aria-label={'Preview of '+attView} onClick={()=>setAttView(null)}><div className="att-preview-card" onClick={e=>e.stopPropagation()}><header><span title={attView}>{attView}</span>{/\.(svg|puml|plantuml|mmd)$/i.test(attView)&&<button className="btn small" onClick={()=>{setTextFile(attView);setAttView(null);navigate(`/s/${spaceId}`)}}>Edit</button>}<a className="btn small" href={api.attachmentURL(spaceId,attView)} download>Download</a><button className="btn small" onClick={()=>setAttView(null)}>Close</button></header><div className="att-preview-body">{/\.(png|jpe?g|gif|webp|avif|ico|bmp|svg|tiff?)$/i.test(attView)?<img src={api.attachmentURL(spaceId,attView)} alt={attView}/>:/\.pdf$/i.test(attView)?<iframe title={attView} src={api.attachmentURL(spaceId,attView)}/>:/\.(mp4|webm|ogv|mov|m4v)$/i.test(attView)?<video controls src={api.attachmentURL(spaceId,attView)}/>:/\.(mp3|wav|ogg|m4a|flac|aac)$/i.test(attView)?<audio controls src={api.attachmentURL(spaceId,attView)}/>:<p>No inline preview for this file. <a href={api.attachmentURL(spaceId,attView)} download>Download it</a> to open.</p>}</div></div></div>}
  {help&&<Dialog title="Help and shortcuts" close={()=>setHelp(false)}><p>On boards, arrows navigate cards; Alt+Left/Right changes status and Alt+Up/Down changes order. M opens card actions.</p><h3>Keyboard</h3><ul className="help-list"><li><kbd>Ctrl/⌘ O</kbd> quick switcher · <kbd>Ctrl P</kbd> command palette · <kbd>Ctrl N</kbd> new note</li><li><kbd>Ctrl Shift F</kbd> search vault · <kbd>Ctrl E</kbd> reading / source · <kbd>Ctrl Shift T</kbd> reopen closed tab</li><li>File tree: <kbd>↑ ↓</kbd> move · <kbd>→ ←</kbd> expand / collapse · <kbd>Enter</kbd> open · <kbd>F2</kbd> rename · <kbd>Space</kbd> add to selection · <kbd>Shift F10</kbd> or the menu key opens the context menu</li><li><kbd>Ctrl</kbd>/<kbd>⌘</kbd>-click toggles selection, <kbd>Shift</kbd>-click selects a visible range; drag notes, folders or attachments onto a folder to move them (dropping on a file moves them into its folder; holding on a collapsed folder opens it); drag them onto the highlighted trash button to move them to trash. The ⋯ button on a row or selection opens its actions; clicking empty space deselects.</li><li>PDFs open in workspace tabs; images, audio and video files open in a preview (Esc or a click outside closes it); anything else opens in a new tab.</li><li>Boards: open the Boards tab from the ribbon. Drag cards between columns or onto a spot inside a column; on a focused card <kbd>←</kbd> <kbd>→</kbd> navigate between columns, <kbd>Alt ← →</kbd> changes status, <kbd>Alt ↑ ↓</kbd> reorders within the column, <kbd>Enter</kbd> opens the note, <kbd>M</kbd> / <kbd>Del</kbd> or <kbd>Shift F10</kbd> opens the card menu. Board settings configure columns, labels, completion, soft WIP limits and new-task defaults; a column folder with cards must be emptied before it is removed or renamed.</li></ul><h3>Vaults</h3><p className="muted">Use the vault name at the bottom of the sidebar to switch, rename, or export a vault. Each vault keeps its own tree expansion, layout, bookmarks, and open note — per user.</p><h3>Document links</h3><p className="muted">Type [[ or press Ctrl/⌘ Space in Source, Visual, or a memo to search document titles and folder paths. Use ↑ ↓ to choose and Enter or Tab to insert; Esc closes suggestions.</p><button className="btn small" onClick={()=>{try{localStorage.removeItem('netsidian:link-hints:disabled');window.dispatchEvent(new Event('netsidian:link-hints-enabled'))}catch{}}}>Enable linking tips</button><h3>Graph</h3><p className="muted">Open the graph from the ribbon, the status bar, or a document's toolbar to view it full-pane or beside the note. The note's cursor, draft, and scroll position are kept.</p></Dialog>}
 </div>
}

function folderOfPath(path:string){const i=path.lastIndexOf('/');return i<0?'':path.slice(0,i+1)}
function SearchExcerpt({text,query}:{text:string;query:string}){const clean=text.replace(/<\/?(?:mark|b)>/g,'').replace(/^[#>*-]+\s*/gm,'').replace(/\[([^\]]+)\]\([^)]+\)/g,'$1').replace(/[`*_]/g,'');const terms=query.match(/[\p{L}\p{N}]+/gu)||[];if(!terms.length)return <p>{clean}</p>;const pattern=new RegExp('('+terms.map(t=>t.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')).join('|')+')','ig');return <p>{clean.split(pattern).map((part,i)=>i%2?<mark key={i}>{part}</mark>:part)}</p>}
