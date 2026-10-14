// Browser-provided relative paths must stay inside the chosen destination.
export function importRelativePath(file:Pick<File,'name'> & {webkitRelativePath?:string}):string {
 const path=(file.webkitRelativePath||file.name).normalize('NFC');
 if(!path||path.startsWith('/')||path.length>1024||/[\\\x00-\x1f\x7f:*?"<>|%]/.test(path)||
  path.split('/').some(part=>!part||part.startsWith('.')||part!==part.trimEnd()))
  throw new Error('Unsafe or reserved file path');
 return path;
}
export function unusedImportPath(path:string,taken:ReadonlySet<string>):string {
 if(!taken.has(path.toLowerCase()))return path;
 const dot=path.lastIndexOf('.'),cut=dot>path.lastIndexOf('/')?dot:path.length;
 const base=path.slice(0,cut),ext=path.slice(cut);let n=1;
 for(;;){const candidate=base+' copy'+(n===1?'':' '+n)+ext;if(!taken.has(candidate.toLowerCase()))return candidate;n++}
}
