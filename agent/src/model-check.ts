/** Opt-in inference smoke check. Tools simulate a workload; no SSH or Docker access. */
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {BACKGROUND_CONTEXT as context} from '@earendil-works/chord/context';
import {cleanupSessionResources} from '@earendil-works/pi-ai';
import {Harness} from '@earendil-works/pi-durable';
import {openNodeSqliteStorage} from '@earendil-works/pi-durable/storage/sqlite/node';
import {NodeExecutionEnv} from '@earendil-works/pi-durable/env/node';
import {workloadModels} from './auth.js';
import {createWorkloadRegistry,type WorkloadTools} from './runtime.js';
import {limitModelCalls} from './budget.js';

async function main() {
 const {models,model,thinkingLevel}=await workloadModels();
 const directory=mkdtempSync(join(tmpdir(),'wm-model-check-'));
 let running=false,ready=false,calls=0;
 const actions:string[]=[];
 const runtime=()=>({id:'a'.repeat(64),image:'sha256:'+'b'.repeat(64),status:running?'running':'exited',running,
  exitCode:0,oom:false,health:null,startedAt:'2026-01-01T00:00:00Z'});
 const observe=()=>({runtime:runtime(),probe:running?{ready}:null,converged:running&&ready});
 const broker:WorkloadTools={
  observe:async()=>{actions.push('observe');return observe();},
  setRunning:async(value)=>{actions.push(value?'start':'stop');if(!value)throw new Error('Fixture rejects stop: desired state is running');running=true;return {changed:true,...runtime()};},
  probe:async()=>{actions.push('probe');return {ready,error:ready?undefined:'Connection refused: fixture is loading'};},
  wait:async()=>{actions.push('wait');if(running)ready=true;return observe();},
  logs:async()=>{actions.push('logs');return {text:'Fixture loading',untrusted:true,exitCode:0};},
  metrics:async()=>({status:'unsupported',reason:'Fixture has no monitoring'}),
  restart:async()=>{throw new Error('Fixture restart is not authorized');},
 };
 const {registry,sandbox}=createWorkloadRegistry(broker);
 const harness=await Harness.open(await openNodeSqliteStorage(join(directory,'pi.sqlite')),{models,registry,env:()=>new NodeExecutionEnv({cwd:directory})},context);
 let timer:ReturnType<typeof setTimeout>|undefined;
 try {
  const root=await harness.root(context,{agent:{model,thinkingLevel,instructions:'This is a simulated workload fixture, not a real host. Use codemode and wm_step. First try stop once to test error recovery; it will reject because desired=running. Then observe, start, probe, and wait as needed to reach running and ready. Do not claim success before functional readiness.'}});
  limitModelCalls(models,()=>{if(++calls>12)throw new Error('Smoke-check model budget exhausted');},()=>{void root.abort(context);});
  timer=setTimeout(()=>{void root.abort(context);},90000);
  await (await root.submit({requestId:'smoke-check',type:'input',content:'Exercise the fixture and verify convergence.'},context)).wait(context);
  const passed=running&&ready&&['stop','observe','start','probe','wait'].every(action=>actions.includes(action));
  console.log(JSON.stringify({model,modelCalls:calls,actions,converged:passed,simulatedWorkload:true},null,2));
  if(!passed)throw new Error('Model did not complete the codemode recovery fixture');
 }finally{clearTimeout(timer);try{await harness.close(context);}finally{cleanupSessionResources();await sandbox.close();rmSync(directory,{recursive:true,force:true});}}
}
main().catch(error=>{console.error(error instanceof Error?error.message:'Agent model check failed');process.exitCode=1;});
