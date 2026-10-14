import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import yaml from 'highlight.js/lib/languages/yaml';
import javascript from 'highlight.js/lib/languages/javascript';
import typescript from 'highlight.js/lib/languages/typescript';
import c from 'highlight.js/lib/languages/c';
import cpp from 'highlight.js/lib/languages/cpp';
import java from 'highlight.js/lib/languages/java';
import csharp from 'highlight.js/lib/languages/csharp';
import ini from 'highlight.js/lib/languages/ini';
import properties from 'highlight.js/lib/languages/properties';
import json from 'highlight.js/lib/languages/json';
import xml from 'highlight.js/lib/languages/xml';
import css from 'highlight.js/lib/languages/css';
import python from 'highlight.js/lib/languages/python';
import go from 'highlight.js/lib/languages/go';
import sql from 'highlight.js/lib/languages/sql';
import markdown from 'highlight.js/lib/languages/markdown';

hljs.registerLanguage('bash',bash);
hljs.registerLanguage('yaml',yaml);
hljs.registerLanguage('javascript',javascript);
hljs.registerLanguage('typescript',typescript);
hljs.registerLanguage('c',c);
hljs.registerLanguage('cpp',cpp);
hljs.registerLanguage('java',java);
hljs.registerLanguage('csharp',csharp);
hljs.registerLanguage('ini',ini);
hljs.registerLanguage('properties',properties);
hljs.registerLanguage('json',json);
hljs.registerLanguage('xml',xml);
hljs.registerLanguage('css',css);
hljs.registerLanguage('python',python);
hljs.registerLanguage('go',go);
hljs.registerLanguage('sql',sql);
hljs.registerLanguage('markdown',markdown);
hljs.registerAliases(['jaml','yml'],{languageName:'yaml'});
hljs.registerAliases(['toml','config','conf','cfg'],{languageName:'ini'});
hljs.registerAliases(['c#','cs'],{languageName:'csharp'});
hljs.registerAliases(['c++'],{languageName:'cpp'});

export function highlightCode(code:string,info:string):string {
 const language=info.trim().split(/\s+/)[0].toLowerCase();
 // Unknown/unlabelled blocks retain escaped plain text; diagrams retain their renderer.
 if(!language||!hljs.getLanguage(language)||code.length>200000)return '';
 try{return hljs.highlight(code,{language,ignoreIllegals:true}).value}catch{return ''}
}
export interface CodeToken {from:number;to:number;className:string}
export function codeTokens(code:string,info:string):CodeToken[] {
 const html=highlightCode(code,info);if(!html)return [];
 const template=document.createElement('template');template.innerHTML=html;
 const tokens:CodeToken[]=[];let offset=0;
 const walk=(node:Node,classes:string[])=>{
  if(node.nodeType===Node.TEXT_NODE){const end=offset+(node.textContent||'').length;if(end>offset&&classes.length)tokens.push({from:offset,to:end,className:classes.join(' ')});offset=end;return}
  const next=node instanceof Element?[...classes,...node.classList]:classes;
  node.childNodes.forEach(child=>walk(child,next));
 };
 walk(template.content,[]);return tokens;
}
