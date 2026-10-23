import {expect,it} from 'vitest';
import {memoryPolicyVersion,newMemoryConfiguration} from './agentSkill';
import {restoredMemoryConfiguration} from './memorySettings';
it('defaults new configurations to selective without adopting existing workflows',()=>{
 expect(restoredMemoryConfiguration(null,'user').configuration.mode).toBe('selective');
 expect(restoredMemoryConfiguration(null,'user',true)).toMatchObject({adopted:false,configuration:{mode:'explicit-only'}});
});
it('requires current policy adoption and prevents unknown stored fields entering exports',()=>{
 const valid={...newMemoryConfiguration,scopeId:'u',policyVersion:memoryPolicyVersion,secret:'never export'};
 const restored=restoredMemoryConfiguration(valid,'u');expect(restored.adopted).toBe(true);expect(restored.configuration).not.toHaveProperty('secret');
 for(const patch of [{policyVersion:'future'},{scope:'arbitrary'},{mode:'automatic'},{scopeId:''}])expect(restoredMemoryConfiguration({...valid,...patch},'u').adopted).toBe(false);
});
