// @vitest-environment jsdom
import {describe,it,expect} from 'vitest';
import {highlightCode,codeTokens} from './codeHighlight';
import {renderInto,makeResolver} from './markdown';
const samples:Record<string,string>={
 bash:'#!/bin/bash\necho "$HOME" # comment',yaml:'enabled: true\nname: "test"',jaml:'enabled: true',javascript:'const name = "test";',js:'const name = "test";',c:'int main(void) { return 0; }','c++':'class Example { public: int value = 1; };',cpp:'class Example {};',java:'public class Example { int value = 1; }','c#':'public class Example { string Name = "test"; }',cs:'public class Example {}',csharp:'public class Example {}',toml:'[server]\nport = 3000\nname = "test"',config:'[server]\nport=3000\nname="test"',properties:'server.port=3000',json:'{"enabled":true}',ts:'const value: number = 1;',python:'def example():\n    return True',html:'<div class="test">Hello</div>',css:'.test { color: red; }',go:'package main\nfunc main() {}',sql:'SELECT * FROM users;',
};
describe('Markdown code highlighting',()=>{
 for(const [language,code] of Object.entries(samples))it('colors '+language+' and preserves code text',()=>{
  const el=document.createElement('div');renderInto(el,'```'+language+'\n'+code+'\n```',makeResolver([]),()=>{});
  expect(el.querySelector('pre code')?.textContent).toBe(code+'\n');expect(el.querySelector('pre code span[class^="hljs-"]')).not.toBeNull();
 });
 it('keeps unknown and unlabelled code plain and inert',()=>{
  for(const language of ['','unknown','mermaid','plantuml'])expect(highlightCode('text',language)).toBe('');
  const el=document.createElement('div');renderInto(el,'```unknown\n<script>alert(1)</script>\n```',makeResolver([]),()=>{});
  expect(el.querySelector('script')).toBeNull();expect(el.querySelector('pre code')?.textContent).toBe('<script>alert(1)</script>\n');
 });
 it('returns safe decoration offsets for escaped HTML, Unicode and nested scopes',()=>{
  const code='const 名 = "<tag> & emoji 😀";\n// comment';
  const ranges=codeTokens(code,'javascript');expect(ranges.length).toBeGreaterThan(2);
  for(const token of ranges){expect(token.from).toBeGreaterThanOrEqual(0);expect(token.to).toBeLessThanOrEqual(code.length);expect(token.className).toMatch(/hljs-/)}
  expect(ranges.some(t=>code.slice(t.from,t.to)==='"<tag> & emoji 😀"')).toBe(true);
 });
 it('does not treat a language info suffix as code or HTML',()=>{expect(highlightCode('const x=1','JavaScript title=example')).toContain('hljs-keyword')});
});
