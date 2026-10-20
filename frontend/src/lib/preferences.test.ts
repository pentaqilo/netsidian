import {expect,it} from 'vitest';
import {queuePreferenceWrite} from './preferences';
it('prevents concurrent preference sections from losing one another during read-modify-write',async()=>{
 let release!:()=>void;const barrier=new Promise<void>(r=>release=r);let stored:Record<string,unknown>={other:'preserved'};let secondStarted=false;
 const appearance=queuePreferenceWrite(async()=>{const read={...stored};await barrier;stored={...read,appearance:{theme:'light'}}});
 const interaction=queuePreferenceWrite(async()=>{secondStarted=true;stored={...stored,interaction:{defaultDocumentMode:'read'}}});
 await Promise.resolve();await Promise.resolve();expect(secondStarted).toBe(false);release();await Promise.all([appearance,interaction]);expect(stored).toEqual({other:'preserved',appearance:{theme:'light'},interaction:{defaultDocumentMode:'read'}});
});
it('continues subsequent writes after a rejected write',async()=>{await expect(queuePreferenceWrite(async()=>{throw new Error('offline')})).rejects.toThrow('offline');let saved=false;await queuePreferenceWrite(async()=>{saved=true});expect(saved).toBe(true)});
