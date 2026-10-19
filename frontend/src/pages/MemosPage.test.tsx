// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';

const memos=vi.fn();
vi.mock('../api/client',()=>({
 memos:(...args:unknown[])=>memos(...args),
 getDocument:vi.fn(()=>new Promise(()=>{})),
 createDocument:vi.fn(),
 saveContent:vi.fn(),
 trashDocument:vi.fn(),
 restoreDocument:vi.fn(),
}));
vi.mock('../AppState',()=>({useApp:()=>({me:{userId:'tester'}})}));
import MemosPage from './MemosPage';

let host:HTMLDivElement,root:Root;
beforeEach(()=>{
 vi.clearAllMocks();localStorage.clear();
 Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true,IntersectionObserver:class{observe(){}unobserve(){}disconnect(){}}});
 memos.mockResolvedValue({memos:[],total:0,tags:[]});
 host=document.createElement('div');document.body.append(host);root=createRoot(host);
});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const mount=()=>act(async()=>root.render(<MemosPage space="v" docs={[]} folders={['Memos']} writable onOpen={()=>{}} onSaved={async()=>{}}/>));
const type=(el:HTMLTextAreaElement,value:string)=>act(()=>{Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value')!.set!.call(el,value);el.dispatchEvent(new Event('input',{bubbles:true}))});
const blur=()=>act(()=>{host.querySelector('textarea')!.dispatchEvent(new FocusEvent('focusout',{bubbles:true}))});

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

it('hides the automatic memo tag from the composer tag editor but keeps written tags editable',async()=>{
 await mount();
 await type(host.querySelector('textarea')!,'Hello #idea');
 const chips=host.querySelector('.memo-composer-v2 .document-tags')!;
 expect(chips.textContent).toContain('#idea');
 expect(chips.textContent).not.toContain('#memo');
});

it('blurred composer renders Markdown with hashtag chips and returns the draft on click',async()=>{
 await mount();
 await type(host.querySelector('textarea')!,'Hello #idea **world**');
 await blur();
 const preview=host.querySelector<HTMLElement>('.memo-composer-preview');
 expect(preview).toBeTruthy();
 expect(preview!.querySelector('.tag-chip')?.textContent).toBe('#idea');
 expect(preview!.querySelector('strong')?.textContent).toBe('world');
 expect(host.querySelector('textarea')).toBeNull();
 await act(async()=>{preview!.click();await new Promise(r=>setTimeout(r,50))});
 const editor=host.querySelector('textarea');
 expect(editor?.value).toBe('Hello #idea **world**');
});

it('typing in the blurred preview resumes editing without losing text',async()=>{
 await mount();
 await type(host.querySelector('textarea')!,'Keep #this');
 await blur();
 const preview=host.querySelector<HTMLElement>('.memo-composer-preview')!;
 await act(async()=>{preview.dispatchEvent(new KeyboardEvent('keydown',{key:'x',bubbles:true,cancelable:true}));await new Promise(r=>setTimeout(r,50))});
 const editor=host.querySelector('textarea');
 expect(editor?.value).toBe('Keep #thisx');
});

it('does not preview an empty composer and keeps Save on the footer',async()=>{
 await mount();
 await blur();
 expect(host.querySelector('.memo-composer-preview')).toBeNull();
 const save=host.querySelector<HTMLButtonElement>('.memo-composer-v2 .memo-save');
 expect(save?.disabled).toBe(true);
 expect(host.querySelector('.memo-composer-v2 footer small')).toBeNull();
});

it('resumes at the previous cursor position instead of appending to the whole draft',async()=>{
 await mount();await type(host.querySelector('textarea')!,'Keep #this');
 host.querySelector('textarea')!.setSelectionRange(2,2);await blur();
 const preview=host.querySelector<HTMLElement>('.memo-composer-preview')!;
 await act(async()=>{preview.dispatchEvent(new KeyboardEvent('keydown',{key:'x',bubbles:true,cancelable:true}));await new Promise(r=>setTimeout(r,50))});
 const editor=host.querySelector('textarea');expect(editor?.value).toBe('Kexep #this');expect(editor?.selectionStart).toBe(3);
});
