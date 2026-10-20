// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';

const memos=vi.fn();
const createDocument=vi.fn();
vi.mock('../api/client',()=>({
 memos:(...args:unknown[])=>memos(...args),
 getDocument:vi.fn(()=>new Promise(()=>{})),
 createDocument:(...args:unknown[])=>createDocument(...args),
 saveContent:vi.fn(),
 trashDocument:vi.fn(),
 restoreDocument:vi.fn(),
}));
vi.mock('../AppState',()=>({useApp:()=>({me:{userId:'tester'}})}));
vi.mock('../components/VisualEditor',()=>({VisualEditor:({draft,onSourceMode}:{draft:{text:string;change:(t:string)=>void};onSourceMode:()=>void})=>{
 const ref=(el:HTMLTextAreaElement|null)=>{if(el){el.value=draft.text;el.oninput=()=>draft.change(el.value)}};
 return <div className="visual-editor"><textarea ref={ref} aria-label="Visual memo body"/><button type="button" onClick={onSourceMode}>Source mode</button></div>;
}}));
import MemosPage from './MemosPage';

let host:HTMLDivElement,root:Root;
beforeEach(()=>{
 vi.clearAllMocks();localStorage.clear();
 Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true,IntersectionObserver:class{observe(){}unobserve(){}disconnect(){}}});
 memos.mockResolvedValue({memos:[],total:0,tags:[]});
 createDocument.mockResolvedValue({id:'m1',path:'Memos/x.md',title:'x',revision:'r1'});
 host=document.createElement('div');document.body.append(host);root=createRoot(host);
});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const mount=async(source=true)=>{await act(async()=>root.render(<MemosPage space="v" docs={[]} folders={['Memos']} writable onOpen={()=>{}} onSaved={async()=>{}}/>));if(source)await act(async()=>{[...host.querySelectorAll<HTMLButtonElement>('.memo-composer-mode button')].find(b=>b.textContent==='Markdown')!.click()})};
const type=(el:HTMLTextAreaElement,value:string)=>act(()=>{Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value')!.set!.call(el,value);el.dispatchEvent(new Event('input',{bubbles:true}))});
const blur=()=>act(()=>{host.querySelector('textarea')!.dispatchEvent(new FocusEvent('focusout',{bubbles:true}))});
const click=(el:HTMLElement)=>act(async()=>{el.dispatchEvent(new MouseEvent('click',{bubbles:true}))});
const key=(el:HTMLElement,k:string)=>act(()=>{el.dispatchEvent(new KeyboardEvent('keydown',{key:k,bubbles:true,cancelable:true}))});

it('drops the promo, the density dropdown and card avatars',async()=>{
 memos.mockResolvedValue({memos:[{id:'m1',path:'Memos/x.md',title:'x',revision:'r1',createdAt:new Date().toISOString(),updatedAt:new Date().toISOString(),tags:['memo','idea'],preview:'hello'}],total:1,tags:[{tag:'idea',count:1}]});
 await mount();
 expect(host.textContent).not.toContain('YOUR PERSONAL TIMELINE');
 expect(host.textContent).not.toContain('A place for your thoughts');
 expect(host.querySelector('[aria-label="Memo card density"]')).toBeNull();
 expect(host.querySelector('.memo-card')).toBeTruthy();
 expect(host.querySelector('.memo-avatar')).toBeNull();
});

it('keeps search and the folder filter inside the filters sidebar',async()=>{
 await mount();
 expect(host.querySelector('.memo-sidebar [aria-label="Search memos"]')).toBeTruthy();
 expect(host.querySelector('.memo-sidebar [aria-label="Filter memo folders"]')).toBeTruthy();
 expect(host.querySelector('.memo-timeline-toolbar')).toBeNull();
});

it('removes the tag editor from the composer but still stores #memo and written tags',async()=>{
 await mount();
 await type(host.querySelector('textarea')!,'Hello #idea');
 expect(host.querySelector('.memo-composer-v2 .document-tags')).toBeNull();
 await act(async()=>{host.querySelector<HTMLFormElement>('.memo-composer-v2')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))});
 await act(async()=>{});
 expect(createDocument).toHaveBeenCalledTimes(1);
 const content=createDocument.mock.calls[0][2] as string;
 expect(content).toMatch(/-\s*memo/);
 expect(content).toMatch(/-\s*idea/);
 expect(content).toContain('Hello');
});

it('places the filters sidebar and its resize separator left of the composer and timeline',async()=>{
 await mount();
 const aside=host.querySelector('.memo-sidebar')!,resizer=host.querySelector('.memo-sidebar-resizer')!,main=host.querySelector('.memo-main')!;
 expect(resizer.getAttribute('role')).toBe('separator');
 expect(aside.compareDocumentPosition(resizer)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
 expect(resizer.compareDocumentPosition(main)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
 expect(host.querySelector('.memos-v2')!.getAttribute('style')).toContain('--memo-sidebar-width: 238px');
});

it('resizes the sidebar with the keyboard within limits and persists the width',async()=>{
 await mount();
 const resizer=host.querySelector('.memo-sidebar-resizer') as HTMLElement;
 expect(resizer.getAttribute('aria-valuenow')).toBe('238');
 await key(resizer,'ArrowRight');
 expect(resizer.getAttribute('aria-valuenow')).toBe('254');
 expect(localStorage.getItem('netsidian:memos:tester:v:sidebar-width')).toBe('254');
 for(let i=0;i<20;i++)await key(resizer,'ArrowLeft');
 expect(resizer.getAttribute('aria-valuenow')).toBe('200');
 await key(resizer,'End');
 expect(resizer.getAttribute('aria-valuenow')).toBe('420');
 await key(resizer,'Home');
 expect(resizer.getAttribute('aria-valuenow')).toBe('200');
 // The width survives a remount of the page.
 await act(async()=>root.unmount());
 root=createRoot(host);await mount();
 expect(host.querySelector('.memos-v2')!.getAttribute('style')).toContain('--memo-sidebar-width: 200px');
});

it('toggles Visual/Markdown without submitting and round-trips the same draft',async()=>{
 await mount();
 const markdown=host.querySelector('.memo-composer-v2 textarea') as HTMLTextAreaElement;
 await type(markdown,'Draft with **bold** #idea');
 const visualBtn=[...host.querySelectorAll<HTMLButtonElement>('.memo-composer-mode button')].find(b=>b.textContent==='Visual')!;
 await click(visualBtn);
 expect(visualBtn.getAttribute('aria-pressed')).toBe('true');
 const visual=host.querySelector('[aria-label="Visual memo body"]') as HTMLTextAreaElement;
 expect(visual.value).toContain('Draft with **bold** #idea');
 // Editing in visual mode updates the same draft and never submits.
 await act(async()=>{Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value')!.set!.call(visual,'Edited visually #idea');visual.dispatchEvent(new Event('input',{bubbles:true}))});
 expect(createDocument).not.toHaveBeenCalled();
 const mdBtn=[...host.querySelectorAll<HTMLButtonElement>('.memo-composer-mode button')].find(b=>b.textContent==='Markdown')!;
 await click(mdBtn);
 expect((host.querySelector('.memo-composer-v2 textarea') as HTMLTextAreaElement).value).toContain('Edited visually #idea');
 expect(createDocument).not.toHaveBeenCalled();
 // The draft is also kept in localStorage for reload safety.
 expect(localStorage.getItem('netsidian:memos:tester:v:draft')).toContain('Edited visually');
});

it('defaults to a stable visual composer and never replaces it on blur',async()=>{
 await mount(false);
 const editor=host.querySelector<HTMLTextAreaElement>('[aria-label="Visual memo body"]')!;
 expect(editor).toBeTruthy();expect(host.querySelector('.memo-composer-mode [aria-pressed="true"]')?.textContent).toBe('Visual');
 await type(editor,'Live #idea **bold**');
 await act(()=>editor.dispatchEvent(new FocusEvent('focusout',{bubbles:true})));
 expect(host.querySelector('[aria-label="Visual memo body"]')).toBe(editor);
 expect(editor.value).toBe('Live #idea **bold**');expect(host.querySelector('.memo-composer-preview')).toBeNull();
 expect(createDocument).not.toHaveBeenCalled();
 await act(async()=>host.querySelector('form')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true})));
 expect(createDocument.mock.calls[0][2]).toContain('Live #idea **bold**');
 expect(createDocument.mock.calls[0][2]).toMatch(/-\s*idea/);
});

it('keeps explicit Markdown source mounted across focus changes with its selection intact',async()=>{
 await mount();const editor=host.querySelector('textarea')!;
 await type(editor,'Keep #this **format**');editor.setSelectionRange(2,6);await blur();
 expect(host.querySelector('textarea')).toBe(editor);expect(editor.selectionStart).toBe(2);expect(editor.selectionEnd).toBe(6);
 expect(host.querySelector('.memo-composer-preview')).toBeNull();
 expect(editor.value).toBe('Keep #this **format**');expect(createDocument).not.toHaveBeenCalled();
});

it('does not preview an empty composer and keeps Save on the footer',async()=>{
 await mount();
 await blur();
 expect(host.querySelector('.memo-composer-preview')).toBeNull();
 const save=host.querySelector<HTMLButtonElement>('.memo-composer-v2 .memo-save');
 expect(save?.disabled).toBe(true);
 expect(host.querySelector('.memo-composer-v2 footer small')).toBeNull();
});
