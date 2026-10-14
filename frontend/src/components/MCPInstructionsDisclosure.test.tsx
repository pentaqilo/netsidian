// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {MCPInstructionsDisclosure} from './MCPInstructionsDisclosure';
vi.mock('../AppState',()=>({useApp:()=>({ready:true,loading:false,spacesError:'',me:null,spaces:[],refresh:vi.fn(),signOut:vi.fn(),settings:{authMode:'local',needsSetup:false,signupEnabled:false,version:'test',features:{},runtime:{kind:'web',auth:{passwordLogin:true,oauth:false,logout:true},storage:{defaultProvider:'local',providers:['local'],folderPicker:'server'}}}})}));
let root:Root,host:HTMLDivElement,writeText:ReturnType<typeof vi.fn>;
const mount=()=>act(()=>root.render(<MCPInstructionsDisclosure/>));
const toggle=()=>host.querySelector<HTMLButtonElement>('.mcp-disclosure-toggle')!;
const panel=()=>host.querySelector<HTMLElement>('.mcp-disclosure-panel');
beforeEach(()=>{
 Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});
 writeText=vi.fn().mockResolvedValue(undefined);
 Object.defineProperty(globalThis.navigator,'clipboard',{configurable:true,value:{writeText}});
 host=document.createElement('div');document.body.append(host);root=createRoot(host);
});
afterEach(async()=>{await act(async()=>{root.unmount()});host.remove()});
it('starts collapsed with disclosure semantics so nothing covers the login form',async()=>{
 await mount();
 expect(panel()).toBeNull();
 expect(toggle().getAttribute('aria-expanded')).toBe('false');
 expect(toggle().getAttribute('aria-controls')).toMatch(/^mcp-panel-/);
 expect(toggle().textContent).toBe('MCP instructions');
});
it('expands on activation, focuses the panel and reuses MCPSetup content and copy actions',async()=>{
 await mount();
 await act(async()=>{toggle().click()});
 const opened=panel();
 expect(opened).not.toBeNull();
 expect(toggle().getAttribute('aria-expanded')).toBe('true');
 expect(opened!.getAttribute('role')).toBe('region');
 expect(opened!.getAttribute('aria-labelledby')).toBe(toggle().id);
 expect(toggle().getAttribute('aria-controls')).toBe(opened!.id);
 expect(document.activeElement).toBe(opened);
 expect(opened!.textContent).toContain('Paste to agent');
 expect(opened!.querySelector('a[href="/settings/mcp"]')).not.toBeNull();
 const copy=[...opened!.querySelectorAll('button')].find(b=>b.textContent==='Copy agent instructions')!;
 await act(async()=>{copy.click()});
 expect(writeText).toHaveBeenCalledWith(expect.stringContaining('Paste to agent: add'));
 expect(copy.textContent).toBe('Copied');
});
it('Escape collapses the panel and returns focus to the toggle',async()=>{
 await mount();
 await act(async()=>{toggle().click()});
 expect(panel()).not.toBeNull();
 await act(async()=>{document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))});
 expect(panel()).toBeNull();
 expect(toggle().getAttribute('aria-expanded')).toBe('false');
 expect(document.activeElement).toBe(toggle());
});
it('collapsing by clicking the toggle keeps focus on the button',async()=>{
 await mount();
 await act(async()=>{toggle().click()});
 await act(async()=>{toggle().click()});
 expect(panel()).toBeNull();
 expect(document.activeElement).toBe(toggle());
});

it('outside dismissal lets the login input keep focus',async()=>{
 await mount();await act(async()=>toggle().click());
 const input=document.createElement('input');host.append(input);
 await act(async()=>{input.dispatchEvent(new Event('pointerdown',{bubbles:true}));input.focus()});
 expect(panel()).toBeNull();expect(document.activeElement).toBe(input);
});
it('the explicit close control restores focus to its launcher',async()=>{
 await mount();await act(async()=>toggle().click());
 await act(async()=>panel()!.querySelector<HTMLButtonElement>('[aria-label="Close MCP instructions"]')!.click());
 expect(panel()).toBeNull();expect(document.activeElement).toBe(toggle());
});
