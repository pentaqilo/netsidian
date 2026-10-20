// Both appearance and interaction use whole-document preferences. Serialize
// their read/modify/write operations so concurrent local changes cannot replace
// the other section with an older server snapshot.
let writes:Promise<unknown>=Promise.resolve();
export function queuePreferenceWrite(work:()=>Promise<void>):Promise<void>{
 const next=writes.catch(()=>{}).then(work);writes=next;return next;
}
