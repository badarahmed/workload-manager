import {existsSync,readFileSync,openSync,writeFileSync,fsyncSync,closeSync,renameSync,mkdirSync} from 'node:fs';
import {join} from 'node:path';
import {randomUUID} from 'node:crypto';

interface Checkpoint {version:1; intent:string; generation:string; startedAt:number; phase:'active'|'converged'|'failed';attempt:number;input:string|null;commands?:number;modelCalls?:number;modelConfigDigest?:string}

/** Admission checkpoint; Pi SQLite remains the execution journal. */
export class Operation {
 private readonly file:string;
 private state:Checkpoint;
 constructor(readonly root:string,intent:string,now=Date.now()) {
  mkdirSync(root,{recursive:true,mode:0o700});this.file=join(root,'operation.json');
  const previous=existsSync(this.file)?JSON.parse(readFileSync(this.file,'utf8')) as Checkpoint:null;
  if(previous && (previous.version!==1||previous.intent!==intent)) throw new Error('Operation checkpoint intent/version mismatch');
  this.state=previous?.phase==='active'?previous:{version:1,intent,generation:randomUUID(),startedAt:now,phase:'active',attempt:0,input:null};
  this.save();
 }
 bindModel(digest:string){
  if(this.state.modelConfigDigest&&this.state.modelConfigDigest!==digest) throw new Error('Agent model configuration changed during an active operation; restore its configuration before resuming');
  if(!this.state.modelConfigDigest&&existsSync(join(this.directory,'pi.sqlite'))) throw new Error('Legacy active journal has no model configuration pin; do not switch its model implicitly');
  this.state.modelConfigDigest=digest;this.save();
 }
 get directory(){return join(this.root,this.state.generation);}
 get attempt(){return this.state.attempt;}
 remaining(now=Date.now()){return Math.max(0,this.state.startedAt+30*60*1000-now);}
 consumeCommand(){
  if(this.remaining()===0) throw new Error('Operation deadline exhausted');
  const commands=this.state.commands??0;
  if(commands>=80) throw new Error('Persistent command budget exhausted');
  this.state.commands=commands+1;this.save();
 }
 consumeModelCall(){
  if(this.remaining()===0) throw new Error('Operation deadline exhausted');
  const calls=this.state.modelCalls??0;
  if(calls>=40) throw new Error('Persistent model-call budget exhausted');
  this.state.modelCalls=calls+1;this.save();
 }
 submission(content:string) {
  if(this.state.input===null){this.state.input=content;this.save();}
  return {requestId:`reconcile-${this.state.attempt}`,type:'input' as const,content:this.state.input};
 }
 advance(){this.state.attempt++;this.state.input=null;this.save();}
 finish(converged:boolean){this.state.phase=converged?'converged':'failed';this.save();}
 private save(){
  const temp=this.file+'.tmp';const fd=openSync(temp,'w',0o600);
  try {writeFileSync(fd,JSON.stringify(this.state));fsyncSync(fd);}finally{closeSync(fd);}
  renameSync(temp,this.file);
  const dir=openSync(this.root,'r');try{fsyncSync(dir);}finally{closeSync(dir);}
 }
}
