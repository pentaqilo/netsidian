// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {MemoryRouter} from 'react-router-dom';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import AuthPage from './AuthPage';
vi.mock('../AppState',()=>({useApp:()=>({ready:true,loading:false,spacesError:'',me:null,spaces:[],refresh:vi.fn(),signOut:vi.fn(),settings:{authMode:'local',needsSetup:false,signupEnabled:false,version:'test',features:{},runtime:{kind:'web',auth:{passwordLogin:true,oauth:false,logout:true},storage:{defaultProvider:'local',providers:['local'],folderPicker:'server'}}}})}));
let root:Root,host:HTMLDivElement;
const mount=()=>act(async()=>root.render(<MemoryRouter initialEntries={['/auth']}><AuthPage/></MemoryRouter>));
beforeEach(()=>{
 Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});
 vi.stubGlobal('fetch',vi.fn(async()=>({json:async()=>({providers:[]})})));
 host=document.createElement('div');document.body.append(host);root=createRoot(host);
});
afterEach(async()=>{await act(async()=>{root.unmount()});host.remove();vi.unstubAllGlobals()});
it('keeps the MCP section out of the login card and anchors a collapsed control instead',async()=>{
 await mount();
 const card=host.querySelector('.auth-card')!;
 expect(card).not.toBeNull();
 expect(card.querySelector('.mcp-setup')).toBeNull();
 const disclosure=host.querySelector('.mcp-disclosure');
 expect(disclosure).not.toBeNull();
 expect(card.contains(disclosure!)).toBe(false);
 expect(disclosure!.querySelector('.mcp-disclosure-panel')).toBeNull();
 expect(disclosure!.querySelector('.mcp-disclosure-toggle')?.getAttribute('aria-expanded')).toBe('false');
});
it('the anchored control expands to the MCP instructions content',async()=>{
 await mount();
 const toggle=host.querySelector<HTMLButtonElement>('.mcp-disclosure-toggle')!;
 await act(async()=>{toggle.click()});
 const panel=host.querySelector('.mcp-disclosure-panel');
 expect(panel?.textContent).toContain('Paste to agent');
 expect(panel?.querySelector('.mcp-setup')).not.toBeNull();
});
