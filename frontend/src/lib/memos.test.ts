import {describe,it,expect} from 'vitest';import {load} from 'js-yaml';import {memoMarkdown,dateBounds,localDate,legacyMemoPath} from './memos';
describe('memo document contract',()=>{
 it('always writes an indexed memo marker while preserving the body',()=>{expect(memoMarkdown('A #project thought')).toBe('---\ntags:\n  - memo\n  - project\n---\nA #project thought\n')});
 it('preserves existing frontmatter and avoids duplicate memo tags',()=>{const result=memoMarkdown('---\ntitle: Test\ntags: [memo, work]\ncustom: true\n---\n\nA thought');const yaml=result.match(/^---\n([\s\S]*?)---/)!;expect(load(yaml[1])).toEqual({title:'Test',tags:['memo','work'],custom:true});expect(result.endsWith('A thought\n')).toBe(true)});
 it('refuses malformed frontmatter instead of losing metadata',()=>{expect(()=>memoMarkdown('---\ntags: [oops\n---\nbody')).toThrow()});
 it('defines creation date boundaries in the local timezone',()=>{const bounds=dateBounds('2026-10-09');expect(localDate(new Date(bounds.from!))).toBe('2026-10-09');expect(localDate(new Date(bounds.before!))).toBe('2026-10-10')});
 it('recognizes only the earlier capture filename format',()=>{expect(legacyMemoPath.test('Inbox/2026-10-09T20-22-10-123Z-a1b2c3d4.md')).toBe(true);expect(legacyMemoPath.test('Inbox/ordinary.md')).toBe(false)});
});
