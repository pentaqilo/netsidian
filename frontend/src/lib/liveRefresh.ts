// Serialize snapshot reads and coalesce events received during a read into
// one follow-up. Frequent changes must not starve the visible snapshot.
export class LiveRefresh<T> {
 private running=false;
 private pending=false;
 private disposed=false;
 constructor(private load:()=>Promise<T>,private receive:(value:T)=>void,private fail:(error:unknown)=>void){}
 refresh(){if(this.disposed)return;this.pending=true;if(!this.running)void this.drain()}
 private async drain(){
  this.running=true;
  while(this.pending&&!this.disposed){
   this.pending=false;
   try{const value=await this.load();if(!this.disposed)this.receive(value)}
   catch(error){if(!this.disposed)this.fail(error)}
  }
  this.running=false;
 }
 dispose(){this.disposed=true;this.pending=false}
}
