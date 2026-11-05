import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {BACKGROUND_CONTEXT as context} from '@earendil-works/chord/context';
import {createModels} from '@earendil-works/pi-ai/models';
import {fauxProvider,fauxAssistantMessage,fauxToolCall} from '@earendil-works/pi-ai/providers/faux';
import {Harness} from '@earendil-works/pi-durable';
import {openNodeSqliteStorage} from '@earendil-works/pi-durable/storage/sqlite/node';
import {NodeExecutionEnv} from '@earendil-works/pi-durable/env/node';
import {CODEMODE_TOOL_NAME} from '@earendil-works/pi-durable/codemode';
import {createWorkloadRegistry, type WorkloadTools} from './runtime.js';

test('Pi Durable executes scoped operations through codemode and recovers a tool error', {timeout:30000}, async()=>{
 const directory=mkdtempSync(join(tmpdir(),'wm-pi-'));
 const actions:string[]=[];
 let started=false;
 const runtime={id:'a'.repeat(64),status:'running',running:true,exitCode:0,oom:false,startedAt:'2020-01-01',health:null,image:'sha256:'+'b'.repeat(64)};
 const broker={
  observe:async()=>{actions.push('observe');return {converged:started,runtime,probe:{ready:started}};},
  setRunning:async(running:boolean)=>{actions.push(running?'start':'stop');if(!running)throw new Error('Action contradicts desired state');started=true;return {changed:true,...runtime};},
  probe:async()=>{actions.push('probe');return {ready:started};},
  logs:async()=>({text:''}),wait:async()=>({converged:started})
 } as unknown as WorkloadTools;
 const progress:string[]=[];
 const {registry,sandbox}=createWorkloadRegistry(broker,[],(action,phase)=>progress.push(action+':'+phase));
 const faux=fauxProvider();const models=createModels();models.setProvider(faux.provider);
 faux.setResponses([
  fauxAssistantMessage(fauxToolCall(CODEMODE_TOOL_NAME,{code:'await tools.wm_step({action:"stop"});'}),{stopReason:'toolUse'}),
  fauxAssistantMessage(fauxToolCall(CODEMODE_TOOL_NAME,{code:'await tools.wm_step({action:"observe"}); await tools.wm_step({action:"start"}); const p = await tools.wm_step({action:"probe"}); if (!p.ready) throw new Error("not ready"); return p;'}),{stopReason:'toolUse'}),
  fauxAssistantMessage('Verified.')
 ]);
 let harness=await Harness.open(await openNodeSqliteStorage(join(directory,'pi.sqlite')),{models,registry,env:()=>new NodeExecutionEnv({cwd:directory})},context);
 try {
  const model=faux.getModel();
  const root=await harness.root(context,{agent:{model:{provider:model.provider,modelId:model.id},instructions:'Reconcile using codemode.'}});
  const settled=await (await root.submit({requestId:'reconcile-0',type:'input',content:'Reach running and verify.'},context)).wait(context);
  assert.ok(actions.length,JSON.stringify({settled,calls:faux.state.callCount}));
  assert.deepEqual(actions,['stop','observe','start','probe']);
  assert.equal(faux.state.callCount,3);
  assert.equal(started,true);
  assert.deepEqual(progress,['stop:begin','stop:error','observe:begin','observe:done','start:begin','start:done','probe:begin','probe:done']);
  await harness.close(context);
  harness=await Harness.open(await openNodeSqliteStorage(join(directory,'pi.sqlite')),{models,registry,env:()=>new NodeExecutionEnv({cwd:directory})},context);
  const reopened=await harness.root(context);
  await (await reopened.submit({requestId:'reconcile-0',type:'input',content:'Reach running and verify.'},context)).wait(context);
  assert.equal(progress.length,8,'completed replay must not emit historical progress again');
  assert.equal(faux.state.callCount,3,'completed admission must not call model again after reopen');
  assert.deepEqual(actions,['stop','observe','start','probe']);
 } finally {await harness.close(context);await sandbox.close();rmSync(directory,{recursive:true,force:true});}
});
