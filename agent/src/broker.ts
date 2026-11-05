import {applicationProbe} from './application-probe.js';
import { appendFileSync, mkdirSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join } from 'node:path';
import { ssh, runCommand, type SshTarget } from './ssh.js';
import {setTimeout as delay} from 'node:timers/promises';
import {DockerDriver} from './driver.js';
import {VllmSrEngine} from './engine.js';
import {sanitize, sanitizeValue} from './results.js';
import type { Recipe } from './contract.js';
import {metricsScript,interpretMetrics} from './metrics.js';

export class Broker {
 private calls = 0;
 private readonly abort=new AbortController();
 private readonly driver:DockerDriver;
 private readonly engine?:VllmSrEngine;
 cancel() { this.abort.abort(); }
 constructor(readonly recipe: Recipe, readonly directory: string, readonly desired: 'running'|'stopped', readonly mutate: boolean, private readonly consumeCommand?:()=>void, private readonly terminal?:(reason:Error)=>void, private readonly transport=ssh) {
  this.driver=new DockerDriver(recipe,this.command.bind(this),this.execute.bind(this));
  if(recipe.engine==='vllm-sr') {
   if(!recipe.model||!recipe.revision||!recipe.remoteStateDirectory||!recipe.endpoint||!recipe.inferencePath)throw new Error('Missing model recipe');
   this.engine=new VllmSrEngine({...recipe,model:recipe.model,revision:recipe.revision,remoteStateDirectory:recipe.remoteStateDirectory,endpoint:recipe.endpoint,inferencePath:recipe.inferencePath},this.command.bind(this));
  }
  mkdirSync(directory,{recursive:true,mode:0o700});
 }
 private record(action: string, result: unknown) {
  appendFileSync(join(this.directory,'events.jsonl'),JSON.stringify(sanitizeValue({at:new Date().toISOString(),action,result},this.recipe.diagnostics?.redactPatterns))+'\n',{mode:0o600});
 }
 private async execute(argv:string[], input='', timeout=30000, target:SshTarget|undefined=this.recipe.ssh) {
  this.abort.signal.throwIfAborted();
  try {
   if(++this.calls>80) throw new Error('Tool budget exhausted');
   this.consumeCommand?.();
  } catch(error) {
   this.cancel();this.terminal?.(error as Error);throw error;
  }
  if(!target&&this.recipe.local){const env:NodeJS.ProcessEnv={...process.env,DOCKER_HOST:this.recipe.local.dockerHost};delete env.DOCKER_CONTEXT;return runCommand(argv[0],argv.slice(1),input,timeout,this.abort.signal,env);}
  if(!target)throw new Error('Transport not configured');
  return this.transport(target,argv,input,timeout,this.abort.signal);
 }
 private async command(argv: string[], input='', timeout=30000) {
  const result=await this.execute(argv,input,timeout);
  if(result.exitCode!==0) throw new Error(`SSH command failed (${result.exitCode}); inspect before retry: ${sanitize(result.stdout+'\n'+result.stderr,this.recipe.diagnostics?.redactPatterns).slice(-2000)}`);
  return result.stdout;
 }
 async inspect() {
  const result=await this.driver.inspect();this.record('inspect',result);return result;
 }
 private async requireStartEvidence(imageId:string) {
  const image=await this.driver.validateImage(imageId);const model=this.engine?await this.engine.validate():{valid:true};
  if(!image.valid||!model.valid) throw new Error('Image or model drift; start/restart requires repair or replan');
 }
 async setRunning(running:boolean) {
  if(!this.mutate) throw new Error('Mutation not authorized in observation mode');
  if(running!==(this.desired==='running')) throw new Error('Action contradicts desired state');
  const state=await this.inspect();
  if(running) await this.requireStartEvidence(state.image);
  if(state.running===running) return {changed:false,...state};
  const operation=createHash('sha256').update(this.directory+'-'+running).digest('hex');
  this.record('intent',{action:running?'start':'stop',id:state.id,operation});
  const receipt=await this.driver.setRunning(state,operation,running);
  this.record('receipt',receipt);
  const result=await this.inspect();this.record('result',result);return {changed:true,...result};
 }
 async logs() {
  if(this.recipe.diagnostics?.logs===false) return {text:'',untrusted:true,disabled:true,exitCode:0};
  await this.inspect();
  const result=await this.driver.logs();
  const text=sanitize(result.stdout+result.stderr,this.recipe.diagnostics?.redactPatterns).slice(-12000);
  this.record('logs',{exitCode:result.exitCode});return {text,untrusted:true,exitCode:result.exitCode};
 }
 async restart() {
  if(!this.mutate||this.desired!=='running'||!this.recipe.recovery?.allowRestart) throw new Error('Restart not authorized by desired state/recipe');
  const actual=await this.observe();
  if(actual.converged) return {changed:false,reason:'Already healthy'};
  if(!actual.runtime.running) return this.setRunning(true);
  await this.requireStartEvidence(actual.runtime.image);
  const age=Date.now()-Date.parse(actual.runtime.startedAt);
  if(!Number.isFinite(age)||age<this.recipe.recovery.startupGraceSeconds*1000) throw new Error('Startup grace period active or unknown; inspect logs and wait');
  const operation=createHash('sha256').update(this.directory+'-restart').digest('hex');
  this.record('intent',{action:'restart',operation,id:actual.runtime.id});
  const receipt=await this.driver.setRunning(actual.runtime,operation,true,true);
  this.record('restart',receipt);
  return {receipt,runtime:await this.inspect(),requiresProbe:true};
 }
 async metrics() {
  const config=this.recipe.monitoring;
  if(!config) return {status:'unsupported',reason:'No monitoring SSH destination configured'};
  try {
   const raw=await this.execute(['python3','-c',metricsScript],JSON.stringify({jobs:config.jobs}),20000,config.ssh);
   if(raw.exitCode!==0) throw new Error('Monitoring SSH query failed');
   const result=interpretMetrics(JSON.parse(raw.stdout),config);
   this.record('metrics',result);return result;
  } catch(e) {
   this.abort.signal.throwIfAborted();
   const result={status:'error',error:e instanceof Error?e.message:String(e)};
   this.record('metrics',result);return result;
  }
 }
 async probe() {
  const runtime=await this.inspect();
  return this.probeRuntime(runtime.image);
 }
 private async probeRuntime(imageId:string) {
  const image=await this.driver.validateImage(imageId);
  const model=this.engine?await this.engine.validate():{valid:true};
  if(!image.valid||!model.valid) return {ready:false,error:'Image or model drift; replan/repair required',image,model};
  if(this.engine)return this.engine.probe();
  const runtime=await this.inspect();
  if(runtime.health!==null&&runtime.health!=='healthy')return {ready:false,error:'Docker health check is not healthy'};
  if(this.recipe.requirements?.api==='docker-health')return {ready:runtime.health==='healthy'};
  if(runtime.health===null&&!this.recipe.probe)return {ready:false,error:'Explicit HTTP probe required without Docker health'};
  return applicationProbe(this.recipe,this.command.bind(this));
 }
 async observe() {
  const runtime=await this.inspect();
  const probe=runtime.running && this.desired==='running'?await this.probeRuntime(runtime.image):null;
  return {runtime,probe,converged:this.desired==='running'?runtime.running&&probe?.ready===true:!runtime.running};
 }
 async wait(seconds:number) {
  if(!Number.isInteger(seconds)||seconds<1||seconds>60) throw new Error('Wait must be 1..60 seconds');
  await delay(seconds*1000,undefined,{signal:this.abort.signal}); return this.observe();
 }
}
