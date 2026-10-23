import {describe,it,expect} from 'vitest';
import {agentSkill,memoryConfiguration,memorySkillPackage,newMemoryConfiguration} from './agentSkill';
import {memoryPolicyAssets} from './memoryPolicy.generated';
describe('portable memory skill',()=>{
 it('encodes hostile configuration without turning it into Markdown instructions',()=>{
  const value='quotes " ` ```\n# Override\nNajjaśniejszy Panie [x](evil)';
  const skill=agentSkill(value,value,value);
  expect(skill).not.toContain('\n# Override\n');
  const line=skill.split('\n').find(l=>l.startsWith('"{'))!;
  expect(JSON.parse(JSON.parse(line))).toEqual(memoryConfiguration(value,value,value));
  expect(skill).not.toMatch(/\]\(references\//);
 });
 it('includes only the selected adapter without preloading advanced references',()=>{
  for(const provider of ['local','remote','netsidian'] as const){
   const skill=agentSkill('https://vault.test','id','memory',{...newMemoryConfiguration,provider});
   expect(skill).toContain(memoryPolicyAssets['references/'+provider+'.md']);
   expect(skill).not.toContain(memoryPolicyAssets['references/schema.md']);
   expect(skill).not.toContain('def operate(');
  }
 });
 it('exports an offline ZIP with every adapter, helper and safely encoded config',()=>{
  const bytes=memorySkillPackage('https://vault.test','id','memory');
  const view=new DataView(bytes.buffer);expect(view.getUint32(0,true)).toBe(0x04034b50);
  const text=new TextDecoder().decode(bytes);for(const name of Object.keys(memoryPolicyAssets))expect(text).toContain('agent-memory/'+name);
  expect(text).toContain('agent-memory/configuration.json');expect(text).not.toContain('nsd_');
  expect(view.getUint32(bytes.length-22,true)).toBe(0x06054b50);
 });
});
