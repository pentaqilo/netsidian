import type {WorkspaceTab} from './workspaceState';

export const attachmentTabId=(path:string)=>'att:'+encodeURIComponent(path);
export const boardTabId=(path:string)=>'board:'+encodeURIComponent(path);
export const mapTabId=(path:string)=>'map:'+encodeURIComponent(path);
export const workspaceTabURL=(tab:WorkspaceTab)=>tab.att
 ?`/s/${tab.space}/a/${encodeURIComponent(tab.att)}`:tab.board
 ?`/s/${tab.space}/b/${encodeURIComponent(tab.board)}`:tab.map
 ?`/s/${tab.space}/m/${encodeURIComponent(tab.map)}`:`/s/${tab.space}/d/${tab.id}`;
export function moveAttachmentTabs(tabs:WorkspaceTab[],space:string,from:string,to:string,folder=false):WorkspaceTab[]{
 return tabs.map(tab=>{
  if(tab.space!==space||!tab.att||!(folder?tab.att.startsWith(from+'/'):tab.att===from))return tab;
  const path=folder?to+tab.att.slice(from.length):to;
  return {...tab,id:attachmentTabId(path),path,att:path};
 });
}
export function moveBoardTabs(tabs:WorkspaceTab[],space:string,from:string,to:string):WorkspaceTab[]{
 return tabs.map(tab=>{
  if(tab.space!==space||!tab.board||!(tab.board===from||tab.board.startsWith(from+'/')))return tab;
  const path=tab.board===from?to:to+tab.board.slice(from.length);
  return {...tab,id:boardTabId(path),path,board:path};
 });
}
export function moveMapTabs(tabs:WorkspaceTab[],space:string,from:string,to:string):WorkspaceTab[]{
 return tabs.map(tab=>{
  if(tab.space!==space||!tab.map||!(tab.map===from||tab.map.startsWith(from+'/')))return tab;
  const path=tab.map===from?to:to+tab.map.slice(from.length);
  return {...tab,id:mapTabId(path),path,map:path};
 });
}
export function removePathTabs(tabs:WorkspaceTab[],space:string,path:string):WorkspaceTab[]{
 return tabs.filter(tab=>tab.space!==space||(tab.path!==path&&!tab.path.startsWith(path+'/')));
}
