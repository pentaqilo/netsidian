// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import BoardsPage from './BoardsPage';
import * as api from '../api/client';
vi.mock('../api/client',async importOriginal=>({...await importOriginal<typeof import('../api/client')>(),createBoard:vi.fn()}));

const board=(path:string,title:string,columns:number):api.BoardSummary=>({path,title,columns});
const boards=[board('Tasks/Website launch','Website launch',3),board('Notes/Reading','Reading queue',2)];

// jsdom's <dialog> predates showModal() in some builds; the page only needs the
// modal open for these assertions.
if(typeof HTMLDialogElement!=='undefined'&&!HTMLDialogElement.prototype.showModal)HTMLDialogElement.prototype.showModal=function(){this.setAttribute('open','')};

let root:Root,host:HTMLDivElement;
const openBoard=vi.fn(),changed=vi.fn(),retry=vi.fn();

const mount=(props:Partial<Parameters<typeof BoardsPage>[0]>&{boards:api.BoardSummary[]})=>act(async()=>root.render(
  <BoardsPage space="vault" folders={['Tasks']} writable onOpenBoard={openBoard} onChanged={changed} {...props}/>));

beforeEach(()=>{vi.clearAllMocks();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true,CSS:{escape:(s:string)=>s}});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove()});

const text=()=>host.textContent||'';
const buttons=(sel:string)=>[...host.querySelectorAll<HTMLButtonElement>(sel)];

it('centres one clear create action in the remaining workspace when the vault has no boards',async()=>{
  await mount({boards:[],boardsStatus:'ready'});
  const state=host.querySelector<HTMLElement>('.boards-state');
  expect(state).not.toBeNull();
  expect(host.querySelector('.boards-body')?.className).toContain('boards-body-centered');
  expect(state?.querySelector('h2')?.textContent).toBe('No boards yet');
  // The empty state carries the single primary action; the header stays quiet.
  expect(buttons('.btn.primary').map(b=>b.textContent?.trim())).toEqual(['Create board']);
  expect(host.querySelector('.boards-header button')).toBeNull();
  await act(async()=>buttons('.btn.primary')[0].click());
  expect(host.querySelector('dialog[aria-label="New board"]')).not.toBeNull();
});

it('explains read-only access instead of offering a create action',async()=>{
  await mount({boards:[],boardsStatus:'ready',writable:false});
  expect(host.querySelector('.boards-readonly')).not.toBeNull();
  expect(buttons('.btn.primary')).toHaveLength(0);
  expect(buttons('button')).toHaveLength(0);
  expect(text()).toContain('Read-only vault');
});

it('shows a loading state rather than claiming an empty vault',async()=>{
  await mount({boards:[],boardsStatus:'loading'});
  expect(host.querySelector('[role="status"]')?.textContent).toContain('Loading boards');
  expect(host.querySelectorAll('.boards-skeleton')).toHaveLength(3);
  expect(text()).not.toContain('No boards yet');
  expect(host.querySelector('.boards-state')).toBeNull();
  // Wait for discovery before presenting creation or a genuinely empty state.
  expect(buttons('.btn.primary')).toHaveLength(0);
});

it('reports a failed board list and retries it when nothing was loaded',async()=>{
  await mount({boards:[],boardsStatus:'error',boardsError:'Vault is offline',onRetryBoards:retry});
  expect(host.querySelector('[role="alert"]')?.textContent).toContain('Vault is offline');
  expect(text()).not.toContain('No boards yet');
  const again=buttons('.btn.primary').find(b=>b.textContent?.trim()==='Try again');
  expect(again).toBeDefined();
  await act(async()=>again!.click());
  expect(retry).toHaveBeenCalledTimes(1);
});

it('keeps already loaded boards when a background refresh fails',async()=>{
  await mount({boards,boardsStatus:'error',boardsError:'Connection lost',onRetryBoards:retry});
  expect(host.querySelectorAll('.board-summary')).toHaveLength(2);
  const warning=host.querySelector('.board-banner.warning');
  expect(warning?.getAttribute('role')).toBe('status');
  expect(warning?.textContent).toContain('Connection lost');
  expect(text()).not.toContain('Could not load boards');
  await act(async()=>buttons('.btn.small').find(b=>b.textContent?.trim()==='Retry')!.click());
  expect(retry).toHaveBeenCalledTimes(1);
});

it('lists boards as accessible cards that open the board',async()=>{
  await mount({boards,boardsStatus:'ready'});
  const cards=host.querySelectorAll<HTMLButtonElement>('.board-summary');
  expect(cards).toHaveLength(2);
  expect(cards[0].getAttribute('aria-label')).toBe('Open board Website launch');
  expect(cards[0].querySelector('strong')?.textContent).toBe('Website launch');
  expect(cards[0].textContent).toContain('Tasks/Website launch');
  expect(cards[0].textContent).toContain('3 columns');
  expect(host.querySelector('.boards-count')?.textContent).toContain('2 boards');
  expect(host.querySelector('.boards-header button.btn.primary')).not.toBeNull();
  await act(async()=>cards[1].click());
  expect(openBoard).toHaveBeenCalledWith('Notes/Reading');
});

it('keeps the existing create dialog and templates working from the empty state',async()=>{
  vi.mocked(api.createBoard).mockResolvedValue({path:'Tasks/New board',title:'New board',revision:'1',config:{version:1,title:'New board',columns:[]},columns:[]});
  await mount({boards:[],boardsStatus:'ready'});
  await act(async()=>buttons('.btn.primary')[0].click());
  const dialog=host.querySelector('dialog')!;
  expect(dialog.querySelector('select')?.textContent).toContain('— blank board —');
  expect(dialog.querySelectorAll('.board-column-row')).toHaveLength(3);
  await act(async()=>dialog.querySelector<HTMLButtonElement>('footer .btn.primary')!.click());
  expect(api.createBoard).toHaveBeenCalledWith('vault',expect.objectContaining({path:'Tasks/New board',title:'New board'}),expect.any(String));
  expect(openBoard).toHaveBeenCalledWith('Tasks/New board');
  expect(changed).toHaveBeenCalledTimes(1);
});
