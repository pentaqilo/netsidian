// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {beforeEach,afterEach,expect,it,vi} from 'vitest';
import type {Graph2Data} from '../lib/graph2Model';
const data:Graph2Data={total:2,nodes:[{id:'a',title:'Alpha',path:'Notes/Alpha.md',kind:'document',tags:[],links:1},{id:'b',title:'Beta',path:'Notes/Beta.md',kind:'document',tags:[],links:1}],edges:[{source:'a',target:'b'}]};
const engine=vi.hoisted(()=>({inspect:null as null|((id:string,preview?:boolean)=>void),settings:null as null|{layout:string}}));
vi.mock('../AppState',()=>({useApp:()=>({me:{userId:'tester'}})}));
vi.mock('../api/client',()=>({graph2:vi.fn(async()=>data),getDocument:vi.fn(async(_space:string,id:string)=>({id,path:id+'.md',markdown:'# '+id,revision:'r1'}))}));
vi.mock('../lib/graph2Canvas',()=>({Graph2Canvas:class{
 percent=100;maxScale=30;
 constructor(_canvas:unknown,_key:unknown,actions:{inspect:(id:string,preview?:boolean)=>void}){engine.inspect=actions.inspect}
 setScene(_scene:unknown,settings:{layout:string}){engine.settings=settings}
 setDisplay(){}getOverview(){return null}destroy(){}fit(){}reset(){}zoom(){}centerOn(){}
 select(id:string,preview?:boolean,notify=true){if(notify)engine.inspect?.(id,preview)}
 focus(id:string){engine.inspect?.(id)}
}}));
import Graph2 from './Graph2';
let host:HTMLDivElement,root:Root;
const prefix='netsidian:graph2:tester:v:';
beforeEach(()=>{localStorage.clear();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const mount=(local=false,epoch=0)=>act(async()=>root.render(<Graph2 space="v" root={local?'a':undefined} epoch={epoch} onOpen={()=>{}}/>));
const click=(label:string)=>act(async()=>{host.querySelector<HTMLButtonElement>(`[aria-label="${label}"]`)!.click()});
const inspect=(id:string,preview?:boolean)=>act(async()=>engine.inspect?.(id,preview));
it('starts in link layout, omits breadcrumb and legend, and retains the navigation panel',async()=>{
 await mount();expect(engine.settings?.layout).toBe('links');expect(host.querySelector('.g2-breadcrumb')).toBeNull();expect(host.querySelector('.g2-legend')).toBeNull();expect(host.querySelector('.canvas-navigation')).toBeTruthy();
});
it('switches settings and preview exclusively, including reselecting the same document',async()=>{
 await mount();await inspect('a',false);expect(host.querySelector('.g2-inspector')).toBeTruthy();
 await click('Graph settings');expect(host.querySelector('.g2-inspector')).toBeNull();expect(host.querySelector('.g2-settings')).toBeTruthy();
 await inspect('a',false);expect(host.querySelector('.g2-inspector')).toBeTruthy();expect(host.querySelector('.g2-settings')).toBeNull();expect(localStorage.getItem(prefix+'vault:controls')).toBe('false');
 await click('Graph settings');await click('Close graph settings');expect(host.querySelector('.g2-inspector')).toBeNull();expect(host.querySelector('.g2-settings')).toBeNull();
});
it('respects restored settings and does not turn highlight synchronization into an open-preview action',async()=>{
 localStorage.setItem(prefix+'vault:controls','true');localStorage.setItem(prefix+'vault:navigation',JSON.stringify({selected:'a'}));localStorage.setItem(prefix+'vault:settings',JSON.stringify({layout:'atlas'}));
 await mount();expect(engine.settings?.layout).toBe('atlas');expect(host.querySelector('.g2-settings')).toBeTruthy();expect(host.querySelector('.g2-inspector')).toBeNull();
 await mount(false,1);expect(host.querySelector('.g2-settings')).toBeTruthy();expect(host.querySelector('.g2-inspector')).toBeNull();
});
it('opening a search result closes settings and opens its preview',async()=>{
 await mount();await click('Graph settings');await click('Graph document list');
 await act(async()=>host.querySelector<HTMLButtonElement>('[data-graph-result="0"]')!.click());
 expect(host.querySelector('.g2-settings')).toBeNull();expect(host.querySelector('.g2-inspector')).toBeTruthy();
});
it('keeps local highlight clicks separate from explicit local previews',async()=>{
 await mount(true);await click('Graph settings');await inspect('a',false);expect(host.querySelector('.g2-inspector')).toBeNull();expect(host.querySelector('.g2-settings')).toBeTruthy();
 await inspect('a',true);expect(host.querySelector('.g2-settings')).toBeNull();expect(host.querySelector('.g2-inspector')).toBeTruthy();
 await click('Graph settings');await mount(true,1);expect(host.querySelector('.g2-settings')).toBeTruthy();expect(host.querySelector('.g2-inspector')).toBeNull();
});
