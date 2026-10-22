// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {CreateBoardDialog} from './CreateBoardDialog';
import * as api from '../api/client';
vi.mock('../api/client',()=>({createBoard:vi.fn(),ApiError:{isConflict:()=>false}}));
let host:HTMLDivElement,root:Root;
beforeEach(()=>{Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});HTMLDialogElement.prototype.showModal=vi.fn();host=document.createElement('div');document.body.append(host);root=createRoot(host);vi.mocked(api.createBoard).mockResolvedValue({path:'Projects/Release'} as api.Board)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});
const change=(el:HTMLInputElement,value:string)=>act(async()=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(el,value);el.dispatchEvent(new Event('input',{bubbles:true}))});
it('selects the initial title and follows it with the folder until location is customized',async()=>{
 await act(async()=>root.render(<CreateBoardDialog space="vault" boards={[]} folders={[]} initialPath="Projects/New board" onClose={()=>{}} onError={()=>{}} onCreated={()=>{}}/>));
 const title=host.querySelector<HTMLInputElement>('[data-autofocus]')!,folder=host.querySelector<HTMLInputElement>('[list="board-folder-suggestions"]')!;
 expect(document.activeElement).toBe(title);expect(title.value.slice(title.selectionStart!,title.selectionEnd!)).toBe('New board');
 await change(title,'Release');expect(folder.value).toBe('Projects/Release');await change(folder,'Custom/Folder');await change(title,'Another');expect(folder.value).toBe('Custom/Folder');
});
