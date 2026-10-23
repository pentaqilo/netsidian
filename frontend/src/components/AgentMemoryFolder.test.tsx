// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {AgentMemoryFolder} from './AgentMemoryFolder';
import * as api from '../api/client';
vi.mock('../AppState',()=>({useApp:()=>({me:{userId:'user'},spaces:[{space:{id:'vault',name:'Personal'},role:'viewer'},{space:{id:'project',name:'Project'},role:'editor'}]})}));
vi.mock('../api/client',()=>({lookupDocument:vi.fn(),createDocument:vi.fn(),ApiError:class extends Error{status=404}}));
let root:Root,host:HTMLDivElement;
beforeEach(()=>{localStorage.clear();vi.clearAllMocks();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const button=(name:string)=>[...host.querySelectorAll('button')].find(b=>b.textContent===name)!;
const mount=()=>act(async()=>root.render(<AgentMemoryFolder initialVault="vault"/>));
it('shows selective mode and readonly destination without writing or migrating memory',async()=>{
 await mount();expect(host.querySelector<HTMLSelectElement>('[aria-label="Memory retention mode"]')?.value).toBe('selective');expect(host.textContent).toContain('Read-only vault');expect(button('Prepare memory folder').disabled).toBe(true);expect(button('Copy self-contained skill').disabled).toBe(true);
 await act(async()=>button('Adopt memory policy').click());expect(button('Copy self-contained skill').disabled).toBe(false);expect(button('Prepare memory folder').disabled).toBe(true);expect(api.createDocument).not.toHaveBeenCalled();expect(host.textContent).toContain('does not enable automatic memory');
});
it('does not silently adopt a legacy configuration or change its memory folder',async()=>{
 localStorage.setItem('netsidian:agent-folder:user:vault','Existing Notes');await mount();expect(host.querySelector<HTMLSelectElement>('[aria-label="Memory retention mode"]')?.value).toBe('explicit-only');expect(host.querySelector<HTMLInputElement>('[aria-label="Agent memory folder path"]')?.value).toBe('Existing Notes');expect(host.textContent).toContain('existing workflow is unchanged');expect(button('Download skill package').disabled).toBe(true);expect(api.createDocument).not.toHaveBeenCalled();
});
it('revokes adoption when the destination changes',async()=>{await mount();await act(async()=>button('Adopt memory policy').click());const input=host.querySelector<HTMLInputElement>('[aria-label="Agent memory folder path"]')!;await act(async()=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(input,'Other');input.dispatchEvent(new Event('input',{bubbles:true}))});expect(button('Copy self-contained skill').disabled).toBe(true)});
