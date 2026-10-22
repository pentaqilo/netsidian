import {afterEach,expect,it,vi} from 'vitest';
import {randomId} from './randomId';
afterEach(()=>vi.unstubAllGlobals());
it('creates unique version 4 UUIDs when randomUUID is unavailable on HTTP',()=>{
 const getRandomValues=globalThis.crypto.getRandomValues.bind(globalThis.crypto);
 vi.stubGlobal('crypto',{getRandomValues});
 const values=Array.from({length:100},()=>randomId());
 expect(new Set(values).size).toBe(100);
 for(const value of values)expect(value).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
});
it('uses native randomUUID when available',()=>{
 const native=vi.fn(()=> 'native-id');vi.stubGlobal('crypto',{randomUUID:native});
 expect(randomId()).toBe('native-id');expect(native).toHaveBeenCalledOnce();
});
