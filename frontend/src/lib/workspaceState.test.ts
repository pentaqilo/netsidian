import {describe,it,expect} from 'vitest';
import {fitWorkspacePanes,readSession} from './workspaceState';
describe('Workspace restoration',()=>{
 it('treats an empty snapshot as an explicitly closed session',()=>{expect(readSession({version:2,tabs:[],toolTabs:[],activeDoc:'stale'},'v')).toEqual({version:2,tabs:[],toolTabs:[],activeDoc:'',activePage:'document'})});
 it('preserves order and pins, isolates vaults, and rejects duplicate/stale active entries',()=>{const t=(id:string,space='v')=>({id,space,path:id+'.md',pinned:true});const s=readSession({version:2,tabs:[t('b'),t('a'),t('b'),t('foreign','other')],toolTabs:[{space:'v',page:'graph2'},{space:'v',page:'graph2'}],activePage:'graph2'},'v')!;expect(s.tabs.map(t=>t.id)).toEqual(['b','a']);expect(s.tabs[0].pinned).toBe(true);expect(s.toolTabs).toHaveLength(1);expect(s.activePage).toBe('graph2')});
 it('ignores malformed snapshots',()=>{expect(readSession({version:2,tabs:'bad'},'v')).toBeNull()});
 it('preserves board tabs, board tool tabs and a boards active page',()=>{const s=readSession({version:2,tabs:[{id:'board:boards%2Fproject',space:'v',path:'boards/project',board:'boards/project'},{id:'doc_a',space:'v',path:'note.md'}],toolTabs:[{space:'v',page:'boards'},{space:'other',page:'boards'}],activePage:'boards'},'v')!;expect(s.tabs[0].board).toBe('boards/project');expect(s.toolTabs).toEqual([{space:'v',page:'boards'}]);expect(s.activePage).toBe('boards')});
});
describe('Workspace pane budgets',()=>{
 for(const viewport of [720,900,1024,1280,1440,1920])for(const zoom of [80,100,125,200])it(`protects document space at ${viewport}px / ${zoom}%`,()=>{const p=fitWorkspacePanes(viewport,zoom,{sidebar:1120,inspector:1120},true,true);expect(p.sidebar).toBeLessThanOrEqual(480);expect(p.inspector).toBeGreaterThanOrEqual(260);if(!p.compact){const remaining=viewport/(zoom/100)-78-p.sidebar-5;expect(remaining-p.inspector).toBeGreaterThanOrEqual(400);expect(p.inspector).toBeLessThanOrEqual(remaining/2)}});
 it('recovers corrupt widths',()=>{const p=fitWorkspacePanes(1440,100,{sidebar:NaN,inspector:Infinity},true,true);expect(p.sidebar).toBe(312);expect(p.inspector).toBe(340)});
});
it('restores mind map document tabs and the mind map discovery tool',()=>{
 const session=readSession({version:2,tabs:[{id:'map:Maps%2FPlan',space:'v',path:'Maps/Plan',map:'Maps/Plan'}],toolTabs:[{space:'v',page:'maps'}],activeDoc:'map:Maps%2FPlan',activePage:'maps'},'v')!;
 expect(session.tabs[0].map).toBe('Maps/Plan');expect(session.activePage).toBe('maps');expect(session.toolTabs[0].page).toBe('maps');
});
