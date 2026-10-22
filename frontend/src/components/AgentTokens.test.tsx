// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import * as api from '../api/client';
import {AgentTokens} from './AgentTokens';
vi.mock('../AppState',()=>({useApp:()=>({spaces:[{space:{id:'vault',name:'Personal'}}]})}));
vi.mock('../api/client',()=>({listKeys:vi.fn(),createKey:vi.fn(),revokeKey:vi.fn()}));
let root:Root,host:HTMLDivElement;
const key={id:'key',label:'Research',spaceIds:['vault'],scopes:['documents:read'],createdAt:'2026-10-22',expiresAt:'2026-11-22'};
beforeEach(()=>{vi.clearAllMocks();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});vi.mocked(api.listKeys).mockResolvedValue({keys:[]});vi.mocked(api.createKey).mockResolvedValue({...key,secret:'nsd_test_not_real'});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const mount=()=>act(async()=>root.render(<AgentTokens initialVault="vault"/>));
const name=async()=>act(async()=>{const el=host.querySelector<HTMLInputElement>('#new-token-label')!;Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(el,'Research');el.dispatchEvent(new Event('input',{bubbles:true}))});
const submit=()=>act(async()=>{host.querySelector('form')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))});
it('creates scoped read-only access and keeps the one-time secret inside the same card',async()=>{
 await mount();await name();const card=host.querySelector('.agent-token-create');await submit();
 expect(api.createKey).toHaveBeenCalledWith('Research',expect.objectContaining({spaceIds:['vault'],expiresInDays:90,scopes:expect.not.arrayContaining(['documents:write'])}));
 expect(host.querySelector('.agent-token-create')).toBe(card);expect(card?.querySelector<HTMLInputElement>('#created-agent-token')?.value).toBe('nsd_test_not_real');
 await act(async()=>[...host.querySelectorAll('button')].find(b=>b.textContent==='Done — hide token')!.click());expect(host.querySelector('#created-agent-token')).toBeNull();
});
it('retains the created secret when refreshing the token list fails',async()=>{await mount();await name();vi.mocked(api.listKeys).mockRejectedValue(new Error('Offline'));await submit();expect(host.querySelector<HTMLInputElement>('#created-agent-token')?.value).toBe('nsd_test_not_real');expect(host.textContent).toContain('Could not load tokens: Offline')});
it('keeps creation errors beside the form and preserves the entered name',async()=>{await mount();await name();vi.mocked(api.createKey).mockRejectedValue(new Error('Not allowed'));await submit();expect(host.querySelector('[role="alert"]')?.textContent).toBe('Not allowed');expect(host.querySelector<HTMLInputElement>('#new-token-label')?.value).toBe('Research')});
