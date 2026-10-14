import {describe,it,expect,vi} from 'vitest';
import {LiveRefresh} from './liveRefresh';
const deferred=()=>{let resolve!:(value:number)=>void;const promise=new Promise<number>(r=>resolve=r);return {promise,resolve}};
describe('live graph snapshots',()=>{
 it('shows in-flight snapshots and coalesces a burst into a fresh follow-up',async()=>{
  const first=deferred(),last=deferred(),load=vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(last.promise),receive=vi.fn();
  const refresh=new LiveRefresh(load,receive,vi.fn());refresh.refresh();
  for(let i=0;i<20;i++)refresh.refresh();
  expect(load).toHaveBeenCalledTimes(1);first.resolve(1);await first.promise;
  expect(receive).toHaveBeenCalledWith(1);expect(load).toHaveBeenCalledTimes(2);
  last.resolve(2);await last.promise;expect(receive).toHaveBeenLastCalledWith(2);expect(load).toHaveBeenCalledTimes(2);
 });
 it('ignores stale results and queued refreshes after a vault switch',async()=>{
  const next=deferred(),load=vi.fn(()=>next.promise),receive=vi.fn();const refresh=new LiveRefresh(load,receive,vi.fn());
  refresh.refresh();refresh.refresh();refresh.dispose();next.resolve(1);await next.promise;
  expect(receive).not.toHaveBeenCalled();expect(load).toHaveBeenCalledTimes(1);
 });
 it('can recover after a failed snapshot',async()=>{
  const load=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(2),receive=vi.fn(),fail=vi.fn();
  const refresh=new LiveRefresh(load,receive,fail);refresh.refresh();await Promise.resolve();await Promise.resolve();refresh.refresh();await Promise.resolve();
  expect(fail).toHaveBeenCalledTimes(1);expect(receive).toHaveBeenCalledWith(2);
 });
});
