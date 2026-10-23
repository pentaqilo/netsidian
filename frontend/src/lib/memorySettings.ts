import {newMemoryConfiguration,type MemoryConfiguration,memoryPolicyVersion} from './agentSkill';
export function restoredMemoryConfiguration(value:unknown,user:string,legacy=false):{configuration:MemoryConfiguration;adopted:boolean}{
 const defaults={...newMemoryConfiguration,scopeId:user,mode:legacy?'explicit-only' as const:'selective' as const};
 if(!value||typeof value!=='object')return {configuration:defaults,adopted:false};
 const v=value as Record<string,unknown>;
 const valid=v.policyVersion===memoryPolicyVersion&&['netsidian','local','remote'].includes(String(v.provider))&&['local','remote'].includes(String(v.location))&&['personal','shared'].includes(String(v.audience))&&['user','project','environment','session'].includes(String(v.scope))&&['selective','explicit-only'].includes(String(v.mode))&&typeof v.scopeId==='string'&&!!v.scopeId.trim()&&(v.destination===undefined||typeof v.destination==='string');
 if(!valid)return {configuration:defaults,adopted:false};
 return {configuration:{provider:v.provider,location:v.location,audience:v.audience,scope:v.scope,scopeId:v.scopeId,mode:v.mode,...(v.destination!==undefined?{destination:v.destination}:{})} as MemoryConfiguration,adopted:true};
}
