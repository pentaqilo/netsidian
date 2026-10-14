import type {DocMeta} from '../api/types';
export type TreeRow=
 {kind:'folder';key:string;path:string;name:string;depth:number}
 |{kind:'doc';key:string;doc:DocMeta;name:string;depth:number}
 |{kind:'att';key:string;path:string;name:string;depth:number};
type Directory={path:string;name:string;children:Directory[];docs:DocMeta[];atts:{path:string}[]};
const collator=new Intl.Collator('en',{numeric:true});

// Build adjacency once. Expanding a folder never scans every file in the vault.
export function indexFileTree(docs:DocMeta[],folders:string[],attachments:{path:string}[],sortBy?:'name'|'mtime'){
 const root:Directory={path:'',name:'',children:[],docs:[],atts:[]},dirs=new Map<string,Directory>([['',root]]);
 const directory=(path:string)=>{
  let parent=root,current='';
  for(const name of path.split('/').filter(Boolean)){
   current=current?current+'/'+name:name;let node=dirs.get(current);
   if(!node){node={path:current,name,children:[],docs:[],atts:[]};dirs.set(current,node);parent.children.push(node)}parent=node;
  }return parent;
 };
 const parent=(path:string)=>directory(path.includes('/')?path.slice(0,path.lastIndexOf('/')):'');
 for(const folder of folders)directory(folder);
 for(const doc of docs)parent(doc.path).docs.push(doc);
 for(const attachment of attachments)parent(attachment.path).atts.push(attachment);
 for(const node of dirs.values()){
  node.children.sort((a,b)=>collator.compare(a.name,b.name));
  node.docs.sort((a,b)=>sortBy==='mtime'?b.updatedAt.localeCompare(a.updatedAt):collator.compare(a.path,b.path));
  node.atts.sort((a,b)=>collator.compare(a.path,b.path));
 }
 return {root,folders:[...dirs.keys()].filter(Boolean)};
}
export function flattenFileTree(index:ReturnType<typeof indexFileTree>,collapsed:Set<string>,filter=false){
 const rows:TreeRow[]=[];
 const walk=(node:Directory,depth:number)=>{
  for(const child of node.children){rows.push({kind:'folder',key:'f:'+child.path,path:child.path,name:child.name,depth});if(filter||!collapsed.has(child.path))walk(child,depth+1)}
  for(const doc of node.docs)rows.push({kind:'doc',key:'d:'+doc.id,doc,name:doc.tags?.includes('memo')?(doc.title||doc.path):doc.path.split('/').pop()?.replace(/\.md$/i,'')||doc.path,depth});
  for(const att of node.atts)rows.push({kind:'att',key:'a:'+att.path,path:att.path,name:att.path.split('/').pop()||att.path,depth});
 };walk(index.root,0);return rows;
}
