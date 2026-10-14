import {expect,it} from 'vitest';
import {attachmentTabId,boardTabId,moveAttachmentTabs,moveBoardTabs,removePathTabs,workspaceTabURL} from './workspaceTabs';
import type {WorkspaceTab} from './workspaceState';
const pdf=(space:string,path:string):WorkspaceTab=>({space,path,id:attachmentTabId(path),att:path,pinned:true});
const board=(space:string,path:string):WorkspaceTab=>({space,path,id:boardTabId(path),board:path});
it('moves a PDF tab with a coherent ID, path, URL and pin while retaining another vault',()=>{
 const tabs=[pdf('one','Folder/a.pdf'),pdf('two','Folder/a.pdf')];
 const moved=moveAttachmentTabs(tabs,'one','Folder/a.pdf','Dest/a.pdf');
 expect(moved[0]).toEqual(pdf('one','Dest/a.pdf'));expect(moved[1]).toBe(tabs[1]);
 expect(workspaceTabURL(moved[0])).toBe('/s/one/a/Dest%2Fa.pdf');
});
it('moves folder attachment tabs without matching similarly named sibling folders',()=>{
 const tabs=[pdf('one','Folder/sub/a.pdf'),pdf('one','Folder2/a.pdf')];
 const moved=moveAttachmentTabs(tabs,'one','Folder','New',true);
 expect(moved[0].att).toBe('New/sub/a.pdf');expect(moved[1]).toBe(tabs[1]);
});
it('trashing a folder closes only its own vault tabs and descendants',()=>{
 const tabs=[pdf('one','Folder/a.pdf'),pdf('two','Folder/a.pdf'),pdf('one','Folder2/a.pdf')];
 expect(removePathTabs(tabs,'one','Folder')).toEqual([tabs[1],tabs[2]]);
});
it('routes ordinary notes with their original document ID',()=>{expect(workspaceTabURL({space:'one',id:'doc_a',path:'note.md'})).toBe('/s/one/d/doc_a')});
it('routes board tabs by encoded board path with a stable ID',()=>{const t=board('one','boards/project');expect(t.id).toBe('board:boards%2Fproject');expect(workspaceTabURL(t)).toBe('/s/one/b/boards%2Fproject')});
it('renames a board tab together with its board folder',()=>{
 const tabs=[board('one','boards/project'),board('two','boards/project')];
 const moved=moveBoardTabs(tabs,'one','boards/project','archive/ship');
 expect(moved[0]).toEqual(board('one','archive/ship'));expect(moved[1]).toBe(tabs[1]);
 expect(workspaceTabURL(moved[0])).toBe('/s/one/b/archive%2Fship');
});
it('moves board tabs with a renamed parent folder without matching sibling folders',()=>{
 const tabs=[board('one','boards/project'),board('one','boards2/project')];
 const moved=moveBoardTabs(tabs,'one','boards','archive');
 expect(moved[0].board).toBe('archive/project');expect(moved[1]).toBe(tabs[1]);
});
it('trashing a folder closes its board tabs and descendants only',()=>{
 const tabs=[board('one','boards/project'),board('two','boards/project'),board('one','boards2/project'),pdf('one','boards/notes.md')];
 expect(removePathTabs(tabs,'one','boards')).toEqual([tabs[1],tabs[2]]);
});
