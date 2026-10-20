// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {FileTree} from './FileTree';
import type {DocMeta} from '../api/types';

const doc=(id:string,path:string,tags?:string[]):DocMeta=>({id,path,kind:'note',title:path.split('/').pop()!.replace(/\.md$/,''),updatedAt:'2026-01-01T00:00:00Z',currentRevision:'1',tags});
const docs=[doc('b','Boards/board.md'),doc('m','Maps/mindmap.md'),doc('n','Notes/plain.md'),doc('t','Notes/thought.md',['memo'])];

let root:Root,host:HTMLDivElement;
const noop=()=>{};
const handlers={onSelect:noop,onPreview:noop,onOpen:noop,onFolder:noop,onMenu:noop,onMenuFolder:noop,onMoveDocs:noop,onMoveFolder:noop};

beforeEach(()=>{Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true,ResizeObserver:class{observe(){}unobserve(){}disconnect(){}},matchMedia:(q:string)=>({matches:false,media:q,addEventListener(){},removeEventListener(){},addListener(){},removeListener(){},onchange:null,dispatchEvent:()=>false})});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove();document.querySelectorAll('.folder-create-menu').forEach(n=>n.remove())});

const mount=(props:Partial<Parameters<typeof FileTree>[0]>={})=>act(async()=>root.render(
  <FileTree docs={docs} folders={['Boards','Maps','Notes']} selected="" selection={{docs:[],folders:[]}} canCreate
    boardPaths={['Boards']} mindMapPaths={['Maps']} onOpenBoard={noop} onOpenMindMap={noop} {...handlers} {...props}/>));

it('offers board, map and memo creation in the folder create menu',async()=>{
  const onCreateBoard=vi.fn(),onCreateMap=vi.fn(),onCreateMemo=vi.fn();
  await mount({onCreateNote:noop,onCreateFolder:noop,onCreateBoard,onCreateMap,onCreateMemo});
  const add=host.querySelector<HTMLButtonElement>('[aria-label="Create in Boards"]')!;
  await act(async()=>add.click());
  const menu=document.querySelector('.folder-create-menu')!;
  const items=[...menu.querySelectorAll('button')].map(b=>b.textContent?.trim());
  expect(items).toEqual(['New document','New folder','New board','New map','New memo']);
  const board=[...menu.querySelectorAll('button')].find(b=>b.textContent?.trim()==='New board')!;
  await act(async()=>board.click());
  expect(onCreateBoard).toHaveBeenCalledWith('Boards');
  expect(onCreateMap).not.toHaveBeenCalled();
});

it('hides specialized create items when the handlers are absent',async()=>{
  await mount({onCreateNote:noop,onCreateFolder:noop});
  const add=host.querySelector<HTMLButtonElement>('[aria-label="Create in Notes"]')!;
  await act(async()=>add.click());
  const menu=document.querySelector('.folder-create-menu')!;
  expect([...menu.querySelectorAll('button')].map(b=>b.textContent?.trim())).toEqual(['New document','New folder']);
});

it('gives board and map rows a single-click open button on folders and config docs',async()=>{
  const onOpenBoard=vi.fn(),onOpenMindMap=vi.fn();
  await mount({onOpenBoard,onOpenMindMap});
  const folderOpen=host.querySelector<HTMLButtonElement>('[aria-label="Open board Boards"]')!;
  const docOpen=host.querySelector<HTMLButtonElement>('[aria-label="Open map Maps"]')!;
  expect(host.querySelector('[aria-label="Open board Maps"]')).toBeNull();
  await act(async()=>folderOpen.click());
  expect(onOpenBoard).toHaveBeenCalledWith('Boards');
  await act(async()=>docOpen.click());
  expect(onOpenMindMap).toHaveBeenCalledWith('Maps');
});

it('marks memo documents and specialized config files with recognizable icons',async()=>{
  await mount();
  const rowOf=(path:string)=>[...host.querySelectorAll<HTMLElement>('.file-row')].find(r=>r.querySelector('.file-name')?.textContent===path.split('/').pop()?.replace(/\.md$/,''))!;
  const glyph=(row:HTMLElement)=>row.querySelector('svg path')?.getAttribute('d');
  expect(glyph(rowOf('Notes/thought.md'))).not.toBe(glyph(rowOf('Notes/plain.md')));
  expect(glyph(rowOf('board.md'))).toBe(host.querySelector('.folder-row .folder-icon svg path')?.getAttribute('d'));
});
