import {Decoration,EditorView,ViewPlugin,type DecorationSet, type ViewUpdate} from '@codemirror/view';
import {syntaxTree} from '@codemirror/language';
import {codeTokens} from './codeHighlight';

function decorations(view:EditorView):DecorationSet {
 const marks:ReturnType<ReturnType<typeof Decoration.mark>['range']>[]=[];
 syntaxTree(view.state).iterate({enter(node){
  if(node.name!=='FencedCode')return;
  const info=node.node.getChild('CodeInfo'),text=node.node.getChild('CodeText');
  if(!info||!text)return false;
  const code=view.state.doc.sliceString(text.from,text.to),language=view.state.doc.sliceString(info.from,info.to);
  for(const token of codeTokens(code,language))marks.push(Decoration.mark({class:token.className}).range(text.from+token.from,text.from+token.to));
  return false;
 }});
 return Decoration.set(marks,true);
}
export const codeSourceHighlight=ViewPlugin.fromClass(class {
 decorations:DecorationSet;
 constructor(view:EditorView){this.decorations=decorations(view)}
 update(update:ViewUpdate){if(update.docChanged||update.viewportChanged||syntaxTree(update.startState)!==syntaxTree(update.state))this.decorations=decorations(update.view)}
},{decorations:plugin=>plugin.decorations});
