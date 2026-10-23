import {build} from '../frontend/node_modules/esbuild/lib/main.js';
const result=await build({entryPoints:['frontend/src/lib/agentSkill.ts'],bundle:true,platform:'node',format:'esm',write:false});
const mod=await import('data:text/javascript;base64,'+Buffer.from(result.outputFiles[0].text).toString('base64'));
const configurations=[['normal','https://vault.example.test','spc_example','agent-memory'],['unicode','https://vault.example.test/'+ 'ż'.repeat(40),'spc_example','Pamięć/'+ 'projekt'.repeat(12)]];
const surfaces={};
for(const [name,base,vault,folder] of configurations)for(const provider of ['netsidian','local','remote'])surfaces[name+'-'+provider]=mod.agentSkill(base,vault,folder,{...mod.newMemoryConfiguration,provider,scopeId:'project-example',destination:provider==='local'?'/configured/memory':'configured-resource'});
process.stdout.write(JSON.stringify(surfaces));
