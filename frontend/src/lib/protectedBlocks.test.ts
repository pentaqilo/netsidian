import {expect,it} from 'vitest';
import {splitProtectedBlocks,stripProtectedBlocks,reinsertProtectedBlocks} from './protectedBlocks';
const block='<!-- netsidian:mindmap-links -->\n- [Idea](/Idea.md)\n<!-- /netsidian:mindmap-links -->\n';
it('hides only complete application membership blocks and preserves ordinary comments',()=>{
 expect(stripProtectedBlocks('Body\n'+block+'<!-- user comment -->')).toBe('Body\n<!-- user comment -->');
 expect(stripProtectedBlocks('<!-- netsidian:user-example -->\nKeep\n<!-- /netsidian:user-example -->')).toContain('Keep');
 expect(stripProtectedBlocks('Body\n<!-- netsidian:mindmap-links -->\nIncomplete')).toContain('Incomplete');
});
it('preserves fenced, indented and frontmatter examples',()=>{
 for(const src of ['```md\n'+block+'```\n','~~~md\n'+block+'~~~\n',block.split('\n').map(l=>'    '+l).join('\n'),'---\nexample: |\n'+block.split('\n').map(l=>'  '+l).join('\n')+'\n---\nBody'])expect(stripProtectedBlocks(src)).toBe(src);
});
it('keeps untouched source byte-for-byte and restores metadata after edits',()=>{
 for(const src of [block+'Body','---\ntitle: Test\n---\nBody\n'+block,'Before\n'+block+'After\n',block+block]){const p=splitProtectedBlocks(src);expect(reinsertProtectedBlocks(p.rest,p.blocks)).toBe(src)}
 const p=splitProtectedBlocks('Body\n'+block);expect(reinsertProtectedBlocks('Edited body\n',p.blocks)).toContain(block);
});
