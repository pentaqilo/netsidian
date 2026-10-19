// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import MindMapsPage from './MindMapsPage';
let host:HTMLDivElement,root:Root;const open=vi.fn(),retry=vi.fn();
beforeEach(()=>{vi.clearAllMocks();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const mount=(props:Partial<Parameters<typeof MindMapsPage>[0]>={})=>act(async()=>root.render(<MindMapsPage space="v" mindMaps={[]} folders={[]} writable onOpenMap={open} onChanged={()=>{}} {...props}/>));
it('offers one clear empty-state action and explains read-only vaults',async()=>{await mount();expect(host.querySelectorAll('button')).toHaveLength(1);expect(host.textContent).toContain('Create mind map');await mount({writable:false});expect(host.querySelectorAll('button')).toHaveLength(0);expect(host.textContent).toContain('Ask its owner or an editor')});
it('distinguishes loading and errors from a truly empty vault',async()=>{await mount({mapsStatus:'loading'});expect(host.textContent).toContain('Loading mind maps');expect(host.textContent).not.toContain('No mind maps yet');await mount({mapsStatus:'error',mapsError:'Offline',onRetryMaps:retry});expect(host.textContent).toContain('Offline');await act(async()=>{host.querySelector('button')!.click()});expect(retry).toHaveBeenCalledOnce()});
it('opens a discovered map with its existing folder identity',async()=>{await mount({mindMaps:[{path:'Maps/Research',title:'Research',nodes:5,edges:4}]});await act(async()=>{host.querySelector<HTMLButtonElement>('[aria-label="Open mind map Research"]')!.click()});expect(open).toHaveBeenCalledWith('Maps/Research');expect(host.textContent).toContain('5 nodes · 4 connectors')});
