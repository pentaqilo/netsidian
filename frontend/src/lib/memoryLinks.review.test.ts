// @vitest-environment jsdom
import {expect,it,vi} from 'vitest';
import {makeResolver,renderInto,type Resolved} from './markdown';
const id='doc_06gp0tkay811e473t4tcap49qp';
const resolve=(target:string):Resolved=>({href:target===id?'agent-memory/details/vibrotrack-project.md':'',missing:target!==id});
it('opens a legacy bare-ID detail through internal document navigation',()=>{
 const host=document.createElement('div'),open=vi.fn();
 const source='- memory: '+JSON.stringify({key:'vibrotrack-project',scope:'project',text:'VibroTrack details',detail:id});
 renderInto(host,source,resolve,open);
 const link=host.querySelector<HTMLAnchorElement>('a[data-note]');
 expect(link).not.toBeNull();expect(link?.target).not.toBe('_blank');link?.click();
 expect(open).toHaveBeenCalledWith('agent-memory/details/vibrotrack-project.md','');
 expect(source).toContain('"detail":"'+id+'"');
});
it('keeps memory examples and unrelated identifiers literal',()=>{
 const source='- memory: '+JSON.stringify({key:'example',text:'Keep exact wording',detail:id});
 const host=document.createElement('div');
 renderInto(host,'```json\n'+source+'\n```\n\nOrdinary text '+id,resolve,()=>{});
 expect(host.querySelector('a')).toBeNull();expect(host.querySelector('code')?.textContent).toContain(source);
});
it('links only reference fields, leaving document IDs mentioned in memory text unchanged',()=>{
 const host=document.createElement('div');
 renderInto(host,'- memory: '+JSON.stringify({key:'example',text:'A literal '+id,detail:id}),resolve,()=>{});
 expect(host.querySelectorAll('a[data-note]').length).toBe(1);
 expect(host.textContent).toContain('A literal '+id);
});

it('resolves stable IDs using live vault metadata and never falls back to same-named files',()=>{
 const doc={id,path:'Projects/Renamed.md',title:'VibroTrack documentation'};
 const resolver=makeResolver([doc.path], 'agent-memory/index.md', [], [doc]);
 const host=document.createElement('div'),open=vi.fn();renderInto(host,'[['+id+']]',resolver,open);
 expect(host.querySelector('a')?.textContent).toBe(doc.title);host.querySelector('a')?.click();expect(open).toHaveBeenCalledWith(doc.path,'');
 expect(makeResolver([id+'.md']) (id).missing).toBe(true);
});
