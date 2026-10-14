export type WorkspaceTab={id:string;path:string;space:string;pinned?:boolean;transient?:boolean;att?:string;board?:string};
export type WorkspaceTool={space:string;page:'memos'|'graph2'|'boards'};
export type WorkspaceSession={version:2;tabs:WorkspaceTab[];toolTabs:WorkspaceTool[];activeDoc:string;activePage:'document'|'memos'|'graph2'|'boards'};
export const sessionKey=(user:string,space:string)=>`netsidian:ws:${user}:${space}`;
export function readSession(value:unknown,space:string):WorkspaceSession|null{
 if(!value||typeof value!=='object')return null;const v=value as Partial<WorkspaceSession>;
 if(v.version!==2||!Array.isArray(v.tabs))return null;
  const seen=new Set<string>(),tabs=v.tabs.filter(t=>t&&typeof t.id==='string'&&t.space===space&&!t.transient&&!seen.has(t.id)&&!!seen.add(t.id)).map(t=>({id:t.id,space,path:typeof t.path==='string'?t.path:'Loading…',pinned:t.pinned===true,att:typeof (t as {att?:unknown}).att==='string'?(t as {att:string}).att:undefined,board:typeof (t as {board?:unknown}).board==='string'?(t as {board:string}).board:undefined}));
 const tools=new Set<string>(),toolTabs=(Array.isArray(v.toolTabs)?v.toolTabs:[]).filter(t=>t&&t.space===space&&(t.page==='graph2'||t.page==='memos'||t.page==='boards')&&!tools.has(t.page)&&!!tools.add(t.page));
 const activePage=v.activePage==='graph2'||v.activePage==='memos'||v.activePage==='boards'?v.activePage:'document';
 return{version:2,tabs,toolTabs,activeDoc:tabs.some(t=>t.id===v.activeDoc)?v.activeDoc||'':'',activePage:activePage==='document'||toolTabs.some(t=>t.page===activePage)?activePage:'document'};
}
const finite=(n:unknown,fallback:number)=>typeof n==='number'&&Number.isFinite(n)?n:fallback;
export function fitWorkspacePanes(viewport:number,zoom:number,desired:{sidebar:number;inspector:number},sidebar:boolean,inspector:boolean){
 const available=Math.max(180,viewport/Math.max(.6,zoom/100)-(viewport<=700?44:68)-10);
 const maxSidebar=Math.max(180,Math.min(480,available-400));
 const left=Math.min(maxSidebar,Math.max(180,finite(desired.sidebar,312)));
 const remaining=available-(sidebar&&viewport>700?left+5:0);
 const overlay=viewport<=700||remaining<660;
 const maxInspector=overlay?Math.max(260,Math.min(600,available*.9)):Math.max(260,Math.min(remaining/2,remaining-400));
 return{sidebar:left,inspector:Math.min(maxInspector,Math.max(260,finite(desired.inspector,340))),maxSidebar,maxInspector,compact:inspector&&overlay};
}
