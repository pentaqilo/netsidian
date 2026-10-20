// @vitest-environment jsdom
import {act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {InteractionPanel,interactionDefaults,mergeInteraction,useInteraction} from './Interaction';
let host:HTMLDivElement,root:Root;
beforeEach(()=>{localStorage.clear();Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});host=document.createElement('div');document.body.append(host);root=createRoot(host)});
afterEach(async()=>{await act(async()=>root.unmount());host.remove();vi.unstubAllGlobals()});
it('defaults to Reading and validates unsupported persisted choices',()=>{
 expect(mergeInteraction(null)).toEqual(interactionDefaults);expect(mergeInteraction({defaultDocumentMode:'broken',readingDoubleClick:'source',mapPreviewClick:'single',mapInlineEditing:false,memoComposerMode:'unknown',canvasMinimap:'true'})).toEqual({...interactionDefaults,readingDoubleClick:'source',mapPreviewClick:'single',mapInlineEditing:false});
});
it('provides concrete changes and resets only interaction values',async()=>{
 const change=vi.fn();await act(async()=>root.render(<InteractionPanel value={{...interactionDefaults,mapInlineEditing:false}} onChange={change}/>));
 const select=host.querySelector<HTMLSelectElement>('[aria-label="Default document mode"]')!;await act(async()=>{select.value='visual';select.dispatchEvent(new Event('change',{bubbles:true}))});expect(change).toHaveBeenLastCalledWith({defaultDocumentMode:'visual'});
 await act(async()=>[...host.querySelectorAll('button')].find(b=>b.textContent==='Reset to defaults')!.click());expect(change).toHaveBeenLastCalledWith(interactionDefaults);
});
it('loads and caches server preferences without leaking them into another account',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>({ok:true,json:async()=>({interaction:{defaultDocumentMode:'source'}})})));
 function Probe({user}:{user:string}){const {interaction,loaded}=useInteraction(user);return <span>{loaded?interaction.defaultDocumentMode:'loading'}</span>}
 await act(async()=>root.render(<Probe user="alice"/>));expect(host.textContent).toBe('source');expect(JSON.parse(localStorage.getItem('netsidian:interaction:alice')!).defaultDocumentMode).toBe('source');
 vi.stubGlobal('fetch',vi.fn(async()=>{throw new Error('offline')}));await act(async()=>root.render(<Probe user="bob"/>));expect(host.textContent).toBe('read');
});
