// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import MindMapsPage from './MindMapsPage';
let host:HTMLDivElement,root:Root;const open=vi.fn(),retry=vi.fn();
beforeEach(()=>{vi.clearAllMocks();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const mount=(props:Partial<Parameters<typeof MindMapsPage>[0]>={})=>act(async()=>root.render(<MindMapsPage space="v" mindMaps={[]} folders={[]} writable onOpenMap={open} onChanged={()=>{}} {...props}/>));
it('offers one clear empty-state action and explains read-only vaults',async()=>{await mount();expect(host.querySelectorAll('button')).toHaveLength(1);expect(host.textContent).toContain('Create map');await mount({writable:false});expect(host.querySelectorAll('button')).toHaveLength(0);expect(host.textContent).toContain('Ask its owner or an editor')});
it('distinguishes loading and errors from a truly empty vault',async()=>{await mount({mapsStatus:'loading'});expect(host.textContent).toContain('Loading maps');expect(host.textContent).not.toContain('No maps yet');await mount({mapsStatus:'error',mapsError:'Offline',onRetryMaps:retry});expect(host.textContent).toContain('Offline');await act(async()=>{host.querySelector('button')!.click()});expect(retry).toHaveBeenCalledOnce()});
it('opens a discovered map with its existing folder identity',async()=>{await mount({mindMaps:[{path:'Maps/Research',title:'Research',nodes:5,edges:4}]});await act(async()=>{host.querySelector<HTMLButtonElement>('[aria-label="Open map Research"]')!.click()});expect(open).toHaveBeenCalledWith('Maps/Research');expect(host.textContent).toContain('5 nodes · 4 connectors')});

it('keeps map cards compact with a meaningful map icon, counts and one native tile action',async()=>{
 await mount({mindMaps:[{path:'Maps/Research',title:'Research',nodes:5,edges:4}]});
 expect(host.querySelector('h1')?.textContent).toBe('Maps');
 expect(host.textContent).toContain('New map');
 const tile=host.querySelector<HTMLButtonElement>('.map-summary')!;
 expect(tile.tagName).toBe('BUTTON');expect(tile.querySelectorAll('button')).toHaveLength(0);
 expect(tile.querySelector('.map-summary-icon svg')).toBeTruthy();
 expect(tile.querySelector('.board-summary-preview')).toBeNull();
 expect(tile.textContent).not.toContain('Open canvas');
 expect(tile.querySelector('footer')).toBeNull();
 expect(tile.textContent).toContain('5 nodes · 4 connectors');
});

it('groups maps by parent folder and filters by search, folder and dates',async()=>{
  const iso=new Date(Date.now()-3*86400000).toISOString();
  await mount({mindMaps:[{path:'Maps/Research',title:'Research',nodes:5,edges:4},{path:'Ideas',title:'Seed ideas',nodes:2,edges:1}],docs:[{path:'Ideas/mindmap.md',updatedAt:iso}]});
  expect([...host.querySelectorAll('.resource-group-title')].map(t=>t.textContent)).toEqual(['Vault root1 map','Maps1 map']);
  expect(host.querySelector('.board-summary-modified')?.textContent).toContain('3 days ago');
  const search=host.querySelector<HTMLInputElement>('.resource-search input')!;
  const setValue=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!;
  await act(async()=>{setValue.call(search,'nope');search.dispatchEvent(new Event('input',{bubbles:true}))});
  expect(host.querySelectorAll('.map-summary')).toHaveLength(0);
  expect(host.textContent).toContain('No maps match');
  const clear=[...host.querySelectorAll<HTMLButtonElement>('.boards-state button')].find(b=>b.textContent?.trim()==='Clear filters')!;
  await act(async()=>clear.click());
  expect(host.querySelectorAll('.map-summary')).toHaveLength(2);
});
