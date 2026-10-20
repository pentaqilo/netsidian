// @vitest-environment jsdom
import {act} from 'react';
import {createRoot} from 'react-dom/client';
import {expect,it} from 'vitest';
import {LinkWritingHint} from './LinkWritingHint';
it('keeps memo guidance closed across focus changes until the explicit disclosure is used',async()=>{
 Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});
 const host=document.createElement('div');document.body.append(host);const root=createRoot(host);
 try{
  await act(async()=>root.render(<div><LinkWritingHint disclosure/><textarea aria-label="Body"/></div>));
  await act(()=>host.querySelector('textarea')!.focus());
  expect(host.querySelector('[role="note"]')).toBeNull();
  const toggle=host.querySelector<HTMLButtonElement>('.link-writing-tip-toggle')!;
  expect(toggle.getAttribute('aria-expanded')).toBe('false');
  await act(()=>toggle.click());expect(toggle.getAttribute('aria-expanded')).toBe('true');expect(host.querySelector('[role="note"]')).toBeTruthy();
  await act(()=>toggle.click());expect(toggle.getAttribute('aria-expanded')).toBe('false');
  await act(()=>host.querySelector('textarea')!.focus());expect(host.querySelector('[role="note"]')).toBeNull();
  await act(async()=>root.render(<div><LinkWritingHint disclosure enabled={false}/><textarea/></div>));
  expect(host.querySelector('.link-writing-tip-toggle')).toBeNull();
 }finally{await act(async()=>root.unmount());host.remove()}
});
