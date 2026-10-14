import {uploadAttachment} from '../api/client';

// Editor-side image embeds: validate, pick a collision-free attachment path
// under attachments/<note>/, upload, and return the vault path to embed.
const imageExt=/\.(png|jpe?g|gif|webp|svg)$/i;

export const imageFileOk=(f:File)=>imageExt.test(f.name||'')&&(f.type===''||f.type==='application/octet-stream'||f.type.startsWith('image/'));

export async function uploadImage(file:File,space:string,existing:{path:string}[],notePath:string,issued?:Set<string>):Promise<string>{
 const base=(notePath.split('/').pop()||'note').replace(/\.md$/i,'').toLowerCase().replace(/[^a-z0-9 _-]+/g,'-').replace(/[ _]+/g,'-').replace(/^-+|-+$/g,'')||'note';
 const ext=(file.name.match(/\.[a-z0-9]+$/i)?.[0]||'.png').toLowerCase().slice(1);
 const taken=new Set(existing.map(a=>a.path.toLowerCase()));
 issued?.forEach(p=>taken.add(p));
 let n=1,path=`attachments/${base}/${base}-${n}.${ext}`;
 while(taken.has(path.toLowerCase()))path=`attachments/${base}/${base}-${++n}.${ext}`;
 issued?.add(path.toLowerCase());
 await uploadAttachment(space,path,file);
 return path;
}
