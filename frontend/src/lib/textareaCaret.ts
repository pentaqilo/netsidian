// A styled mirror reproduces wrapping without changing the textarea or its selection.
export function textareaCaretRect(element:HTMLTextAreaElement,position=element.selectionStart){
 const style=getComputedStyle(element),box=element.getBoundingClientRect(),mirror=document.createElement('div');
 const properties=['boxSizing','fontFamily','fontSize','fontWeight','fontStyle','fontVariant','lineHeight','letterSpacing','wordSpacing','textTransform','textIndent','textAlign','tabSize','paddingTop','paddingRight','paddingBottom','paddingLeft','borderTopWidth','borderRightWidth','borderBottomWidth','borderLeftWidth','borderStyle','direction','wordBreak'] as const;
 for(const key of properties)mirror.style[key]=style[key];
 Object.assign(mirror.style,{position:'fixed',left:'0',top:'0',width:element.offsetWidth+'px',height:'auto',whiteSpace:'pre-wrap',overflowWrap:'break-word',visibility:'hidden',pointerEvents:'none'});
 mirror.textContent=element.value.slice(0,position);const marker=document.createElement('span');marker.textContent=element.value.slice(position)||'\u200b';mirror.append(marker);document.body.append(mirror);
 try{const origin=mirror.getBoundingClientRect(),caret=marker.getClientRects()[0]||marker.getBoundingClientRect(),scale=box.width/element.offsetWidth||1,mirrorScale=origin.width/mirror.offsetWidth||1;
  const left=box.left+((caret.left-origin.left)/mirrorScale-element.scrollLeft)*scale,top=box.top+((caret.top-origin.top)/mirrorScale-element.scrollTop)*scale;
  const height=(parseFloat(style.lineHeight)||parseFloat(style.fontSize)*1.5)*scale;
  return{left:Math.max(box.left,Math.min(box.right-2,left)),top:Math.max(box.top,Math.min(box.bottom-height,top)),bottom:Math.max(box.top+height,Math.min(box.bottom,top+height))};
 }finally{mirror.remove()}
}
