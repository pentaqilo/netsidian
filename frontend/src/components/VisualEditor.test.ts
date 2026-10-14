import {describe,it,expect} from 'vitest';
import {Schema} from 'prosemirror-model';
import type {Node} from 'prosemirror-model';
import {EditorState,TextSelection} from 'prosemirror-state';
import {visualSafe,prepareVisual,computeUi,linkHrefAllowed,toggleTaskList,moveBlock,convertList,insertMarkdownTable} from './VisualEditor';

describe('Visual editor source safety gate',()=>{
 it('allows basic Markdown and fenced code',()=>{
  expect(visualSafe('# A\n\nSome **bold** text.\n\n```js\nconst x = 1\n```\n')).toBe(true);
 });
 it.each([
  '---\nunknown: value\n---\n# Note',
  'A [[wikilink|alias]] here',
  '%%private comment%%',
  '> [!note] Callout',
  '[^source]: Footnote text',
  '[target]: https://example.com',
  '==highlight==',
  '<custom-widget mode="x">data</custom-widget>',
  '<!-- an HTML comment -->',
  'A paragraph with CRLF\r\n',
  'A paragraph with trailing spaces  \n',
  ':::custom directive\ncontent\n:::',
 ])('rejects unsupported syntax before an editor can serialize it: %s',text=>{
  expect(visualSafe(text)).toBe(false);
 });
});

describe('Visual editor protected Obsidian syntax',()=>{
 it('opens a normal vault note and restores metadata and wikilinks exactly',()=>{
  const source='---\ntags: [notes]\n---\n# Title\n\nRead [[Other Note|alias]] and ![[image.png]].\n';
  const prepared=prepareVisual(source);
  expect(prepared.safe).toBe(true);
  expect(prepared.markdown).toContain('[alias](#netsidian-protected-0)');
  expect(prepared.restore(prepared.markdown)).toBe(source);
  expect(prepared.restore(prepared.markdown.replace('# Title','# Edited'))).toContain('# Edited');
  expect(prepared.restore(prepared.markdown.replace('alias','changed'))).toBeNull();
 });
 it('keeps source-only constructs as protected islands while editing other paragraphs',()=>{
  const source='---\nkind: note\n---\n# Title\n\n> [!note] Keep me\n\nEditable text.\n';
  const prepared=prepareVisual(source);expect(prepared.safe).toBe(true);
  expect(prepared.restore(prepared.markdown.replace('Editable text.','Changed text.'))).toBe(source.replace('Editable text.','Changed text.'));
   expect(prepared.restore(prepared.markdown.replace('Source syntax','Changed source'))).toBeNull();
  });
 });

describe('Visual toolbar state',()=>{
 it('reports the current heading level and paragraph blocks',()=>{
  const fake=(chain:{name:string;attrs?:Record<string,unknown>;before:number}[],parent:{name:string;attrs?:Record<string,unknown>})=>{
   const depth=chain.length+1;
   const $from={depth,
    parent:{type:{name:parent.name},attrs:parent.attrs??{}},
    node:(d:number)=>d===depth?{type:{name:parent.name},attrs:parent.attrs??{}}:{type:{name:chain[d-1].name},attrs:chain[d-1].attrs??{}},
    before:(d:number)=>d===depth?101:chain[d-1].before};
   return {selection:{$from}} as any;
  };
  expect(computeUi(fake([],{name:'heading',attrs:{level:4}})).block).toBe('h4');
  expect(computeUi(fake([{name:'blockquote',before:1}],{name:'paragraph'})).block).toBe('paragraph');
  const inTable=computeUi(fake([{name:'table',before:1},{name:'table_row',before:2},{name:'table_cell',before:3}],{name:'paragraph'}));
  expect(inTable.inTable).toBe(true);expect(inTable.block).toBe('other');
  const code=computeUi(fake([],{name:'code_block',attrs:{language:'js'}}));
  expect(code.block).toBe('code');expect(code.code).toEqual({pos:101,lang:'js'});
 });
 it('accepts web URLs and vault paths but rejects fragments, protocols and spaces',()=>{
  expect(linkHrefAllowed('https://example.com/a?b=1')).toBe(true);
  expect(linkHrefAllowed('http://host')).toBe(true);
  expect(linkHrefAllowed('folder/note.md')).toBe(true);
  expect(linkHrefAllowed('/vault/root.md')).toBe(true);
  expect(linkHrefAllowed('note#section')).toBe(true);
  expect(linkHrefAllowed('#anchor')).toBe(false);
  expect(linkHrefAllowed('mailto:a@b.c')).toBe(false);
  expect(linkHrefAllowed('two words')).toBe(false);
 });
});

describe('Visual toolbar document commands',()=>{
 const d=new Schema({
  nodes:{
   doc:{content:'block+'},paragraph:{content:'inline*',group:'block'},text:{inline:true,group:'inline'},
   heading:{content:'text*',group:'block',attrs:{level:{default:1}}},
   blockquote:{content:'block+',group:'block'},hr:{group:'block'},
   bullet_list:{content:'list_item+',group:'block'},
   ordered_list:{content:'list_item+',group:'block'},
   list_item:{content:'paragraph block*',attrs:{checked:{default:null}}},
   table:{content:'table_row+',group:'block'},table_row:{content:'(table_cell|table_header)*'},
   table_header_row:{content:'table_header+'},table_header:{content:'paragraph+'},table_cell:{content:'paragraph+'},
  },marks:{},
 });
 const para=(t:string)=>d.node('paragraph',null,t?[d.text(t)]:undefined);
 const item=(t:string,attrs?:Record<string,unknown>)=>d.node('list_item',attrs??null,[para(t)]);
 const docOf=(...kids:NodeLike[])=>d.node('doc',null,kids as never);
 type NodeLike=ReturnType<typeof para>;
 const view=(doc:ReturnType<typeof docOf>,anchor:number,head?:number)=>{let st=EditorState.create({schema:d,doc,selection:head===undefined?TextSelection.create(doc,anchor):TextSelection.create(doc,anchor,head)});
  return {get state(){return st},dispatch:(tr:never)=>{st=st.apply(tr)},focus:()=>{}} as any;};
 it('moveBlock swaps top-level siblings and refuses at the borders',()=>{
  const doc=docOf(para('one'),para('two'));
  const v=view(doc,2);
  expect(moveBlock(v,1)).toBe(true);
  expect(v.state.doc.textBetween(0,v.state.doc.content.size,'','')).toBe('twoone');
  const edge=view(docOf(para('one'),para('two')),2);
  expect(moveBlock(edge,-1)).toBe(false);
 });
 it('task toggle gives plain bullet items checkboxes',()=>{
  const doc=docOf(para('plain text'));
  const v=view(doc,3);
  expect(toggleTaskList(v)).toBe(true);
  const list=v.state.doc.firstChild;
  expect(list!.type.name).toBe('bullet_list');
  expect(list!.child(0).attrs.checked).toBe(false);
 });
 it('task toggle adds checkboxes to unchecked items and removes them when all have one',()=>{
  const doc=docOf(d.node('bullet_list',null,[item('a'),item('b')]));
  const positions:number[]=[];doc.descendants((n,p)=>{if(n.type.name==='list_item')positions.push(p);return true});
  const v=view(doc,positions[0]+3,positions[1]+3);
  expect(toggleTaskList(v)).toBe(true);
  expect(v.state.doc.child(0).child(0).attrs.checked).toBe(false);
  expect(v.state.doc.child(0).child(1).attrs.checked).toBe(false);
  expect(toggleTaskList(v)).toBe(true);
  expect(v.state.doc.child(0).child(0).attrs.checked).toBeNull();
  expect(v.state.doc.child(0).child(1).attrs.checked).toBeNull();
 });
  it('convertList switches list type in place and unwraps when already that type',()=>{
   const doc=docOf(d.node('bullet_list',null,[item('aaa'),item('bbb')]));
   const v=view(doc,4);
   expect(convertList(v,'ordered_list')).toBe(true);
   expect(v.state.doc.child(0).type.name).toBe('ordered_list');
   expect(v.state.doc.textBetween(0,v.state.doc.content.size,'','')).toBe('aaabbb');
   expect(convertList(v,'ordered_list')).toBe(true);
   expect(v.state.doc.childCount).toBe(2);
   expect(v.state.doc.child(0).type.name).toBe('paragraph');
   expect(v.state.doc.textBetween(0,v.state.doc.content.size,'','')).toBe('aaabbb');
   expect(convertList(v,'bullet_list')).toBe(false);
  });
  it('insertMarkdownTable builds a header row plus body rows',()=>{
  const doc=docOf(para('outside'));
  const v=view(doc,2);
   expect(insertMarkdownTable(v,3,2)).toBe(true);
   let table:Node|null=null;
   v.state.doc.forEach((n:Node)=>{if(n.type.name==='table')table=n});
   expect(table!.type.name).toBe('table');
  expect(table!.child(0).type.name).toBe('table_header_row');
  expect(table!.child(0).childCount).toBe(2);
  expect(table!.childCount).toBe(3);
 });
});


describe('visual document link insertion',()=>{
 it('allows undoing a newly inserted link without blocking later edits',()=>{const p=prepareVisual('Body');p.addWiki('/Other','Other');expect(p.restore('Edited body')).toBe('Edited body')});
 it('protects empty frontmatter and does not treat body separators as metadata',()=>{for(const source of ['---\n---\nBody','# Title\n\n---\n\nBody\n\n---\n']){const p=prepareVisual(source);expect(p.restore(p.markdown)).toBe(source)}});
 it('restores a newly inserted link as an indexed wikilink alongside existing frontmatter',()=>{const p=prepareVisual('---\ntags: [work]\n---\nBody');const link=p.addWiki('/Projects/Research','Research notes');expect(p.restore(p.markdown+'\n\n['+link.label+']('+link.href+')')).toContain('[[/Projects/Research|Research notes]]');expect(p.restore(p.markdown+'\n\n['+link.label+']('+link.href+')')).toContain('tags: [work]')});
});
