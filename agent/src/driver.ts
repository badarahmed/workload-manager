import {readFileSync} from 'node:fs';
import {parseReceipt} from './results.js';
import type {CommandResult} from './ssh.js';
import type {Recipe} from './contract.js';

export type Command=(argv:string[],input?:string,timeout?:number)=>Promise<string>;
export interface RuntimeState {
 id:string;status:string;running:boolean;exitCode:number;oom:boolean;
 startedAt:string;health:string|null;image:string;
}

/** Docker CLI adapter. Exact captured identity gates every diagnostic/mutation. */
export class DockerDriver {
 constructor(private readonly recipe:Recipe,private readonly command:Command,private readonly execute:(argv:string[])=>Promise<CommandResult>) {}
 async logs() {return this.execute(['docker','logs','--tail','60',this.recipe.observedContainerId]);}
 async setRunning(state:RuntimeState,operation:string,running:boolean,restart=false) {
  const helper=readFileSync(new URL('../remote/lifecycle.py',import.meta.url),'utf8');
  const request={container_id:state.id,container_name:this.recipe.container,image:state.image,operation,running,...(restart?{action:'restart'}:{})};
  return parseReceipt(await this.command(['python3','-c',helper],JSON.stringify(request),restart?120000:65000),state.id);
 }
 async inspect():Promise<RuntimeState> {
  const items:unknown=JSON.parse(await this.command(['docker','inspect',this.recipe.observedContainerId]));
  if(!Array.isArray(items)||items.length!==1) throw new Error('Invalid Docker inspection');
  const item=items[0];const state=item?.State;
  if(item?.Id!==this.recipe.observedContainerId||item?.Name!==`/${this.recipe.container}`||
     !/^sha256:[a-f0-9]{64}$/.test(item?.Image)||
     (this.recipe.observedImageId&&item.Image!==this.recipe.observedImageId)) throw new Error('Runtime identity drift; replan required');
  if(!state||typeof state.Running!=='boolean'||typeof state.Status!=='string'||
     typeof state.StartedAt!=='string'||!Number.isInteger(state.ExitCode)||typeof state.OOMKilled!=='boolean') throw new Error('Invalid Docker state');
  return {id:item.Id,status:state.Status,running:state.Running,exitCode:state.ExitCode,
   oom:state.OOMKilled,startedAt:state.StartedAt,health:state.Health?.Status??null,image:item.Image};
 }
 async validateImage(imageId:string) {
  const items:unknown=JSON.parse(await this.command(['docker','image','inspect',imageId]));
  if(!Array.isArray(items)||items.length!==1||items[0]?.Id!==imageId||
     (items[0].RepoDigests!==null&&!Array.isArray(items[0].RepoDigests))) throw new Error('Invalid Docker image inspection');
  const repoDigests:string[]=items[0].RepoDigests??[];
  return {valid:repoDigests.includes(this.recipe.imageDigestFromHostDocumentation),
   imageId,desiredReference:this.recipe.imageDigestFromHostDocumentation,repoDigests,
   reason:'Desired repository manifest digest must belong to the captured image; image ID is a separate config digest'};
 }
}
