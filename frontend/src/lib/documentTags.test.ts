import {describe,it,expect} from 'vitest';
import {documentBody,normalizeTag,readDocumentTags,replaceDocumentBody,writeDocumentTags} from './documentTags';
import {load} from 'js-yaml';
describe('shared document tags',()=>{
 it('unifies legacy frontmatter, strings, arrays and inline Unicode/nested tags',()=>{expect(readDocumentTags('---\ntag: "#Work, PROJECT/One"\ntags: [work, Żółć, 123]\n---\n#Work #project/two #123')).toEqual(['project/one','project/two','work','żółć'])});
 it('excludes fenced, indented, double-backtick code and multiline comments',()=>{expect(readDocumentTags('#real\n`` #fake ` #fake ``\n    #indented\n<!-- #comment\n#hidden -->\n~~~\n#fence\n~~~')).toEqual(['real'])});
 it('renames inline tags without touching code, preserves unrelated metadata and comments',()=>{const source='---\ntitle: "Test"\n"tag":\n- old\n# Keep this comment\ncustom: true\n---\n#old and `#old`';const result=writeDocumentTags(source,['new']);expect(load(result.split('---')[1])).toEqual({title:'Test',custom:true,tags:['new']});expect(result).toContain('# Keep this comment');expect(result).toContain(' and `#old`');expect(readDocumentTags(result)).toEqual(['new'])});
 it('preserves BOM, CRLF and other field formatting',()=>{const result=writeDocumentTags('\uFEFF---\r\ntitle: \'A\'\r\n---\r\nBody',['#Foo']);expect(result).toBe('\uFEFF---\r\ntitle: \'A\'\r\ntags:\r\n  - foo\r\n---\r\nBody')});
 it('accepts empty frontmatter and YAML end markers',()=>{expect(readDocumentTags('---\n---\nBody')).toEqual([]);expect(readDocumentTags('---\ntags: [work]\n...\nBody')).toEqual(['work']);expect(documentBody(writeDocumentTags('---\n---\nBody',['a']))).toBe('Body')});
 it('preserves values referencing an anchored tag property',()=>{const result=writeDocumentTags('---\ntags: &topics [old]\ncustom: *topics\n---\nBody',['new']);expect(load(result.split('---')[1])).toEqual({tags:['new'],custom:['old']})});
 it('refuses malformed metadata and invalid tag names',()=>{expect(()=>writeDocumentTags('---\ntags: [broken\n---\nBody',['a'])).toThrow();for(const value of ['123','two words','a//b','x!'])expect(()=>normalizeTag(value)).toThrow()});
 it('lets a malformed pasted memo be repaired as source text',()=>{expect(documentBody('---\ntags: [broken')).toBe('---\ntags: [broken');expect(replaceDocumentBody('---\ntags: [broken','Fixed body')).toBe('Fixed body')});
 it('keeps memo metadata separate from the composer body',()=>{const source=writeDocumentTags('Thought',['memo','work']);expect(documentBody(source)).toBe('Thought');expect(readDocumentTags(replaceDocumentBody(source,'New thought'))).toEqual(['memo','work']);expect(documentBody(replaceDocumentBody(source,'New thought'))).toBe('New thought')});
});
