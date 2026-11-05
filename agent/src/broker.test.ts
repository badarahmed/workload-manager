import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,rmSync,readFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {Broker} from './broker.js';
import type {Recipe} from './contract.js';
import type {ssh} from './ssh.js';
import {parseReceipt,sanitize,sanitizeValue} from './results.js';
import {validateResult} from './protocol.js';

function fixture() {
 const directory=mkdtempSync(join(tmpdir(),'wm-broker-'));
 const recipe:Recipe=JSON.parse(readFileSync('agent/testdata/model-recipe.json','utf8'));
 const image='sha256:'+'b'.repeat(64);
 let health='healthy';let running=true;let validModel=false;let validImage=true;let identity=true;
 let helperPhase='verified';let calls=0;let mutations=0;
 const actions:string[]=[];
 const transport:typeof ssh=async(_target,argv)=>{
  calls++;actions.push(argv.slice(0,2).join(' '));
  let result:unknown;
  if(argv[0]==='docker'&&argv[1]==='inspect') result=[{Id:recipe.observedContainerId,Name:identity?'/'+recipe.container:'/unowned',Image:image,State:{Health:{Status:health},Running:running,Status:running?'running':'exited',ExitCode:0,OOMKilled:false,StartedAt:'2020-01-01T00:00:00Z'}}];
  else if(argv[0]==='docker'&&argv[1]==='image') result=[{Id:image,RepoDigests:validImage?[recipe.imageDigestFromHostDocumentation]:[]}];
  else if(argv[1]==='logs') return {stdout:'API_KEY=synthetic-secret Authorization: Bearer synthetic-token',stderr:'',exitCode:0};
  else if(argv[2]?.includes('manifest_present')) result={revisions:validModel?[recipe.revision]:[],manifest_present:validModel};
  else if(argv[2]?.includes('def execute')) {mutations++;running=false;result={version:1,phase:helperPhase,container_id:recipe.observedContainerId,changed:true};}
  else result={ready:true};
  return {stdout:JSON.stringify(result),stderr:'',exitCode:0};
 };
 return {recipe,directory,transport,actions,get calls(){return calls;},get mutations(){return mutations;},setHealth:(v:string)=>{health=v;},setModel:(v:boolean)=>{validModel=v;},setImage:(v:boolean)=>{validImage=v;},setIdentity:(v:boolean)=>{identity=v;},setPhase:(v:string)=>{helperPhase=v;},close:()=>rmSync(directory,{recursive:true,force:true})};
}

test('model/image drift remains observable and does not block logs or authorized stop',async()=>{
 const f=fixture();try {
  const running=new Broker(f.recipe,f.directory,'running',false,undefined,undefined,f.transport);
  assert.equal((await running.observe()).converged,false);
  const logs=await running.logs();assert.equal(logs.untrusted,true);assert.ok(!logs.text.includes('synthetic'));
  f.setImage(false);
  const stopped=new Broker(f.recipe,f.directory,'stopped',true,undefined,undefined,f.transport);
  const result=await stopped.setRunning(false);assert.equal(result.running,false);
  assert.equal((await stopped.observe()).converged,true);
 }finally{f.close();}
});
test('start/restart remain blocked on model drift; diagnostics cannot bypass ownership',async()=>{
 const f=fixture();try {
  const b=new Broker(f.recipe,f.directory,'running',true,undefined,undefined,f.transport);
  await assert.rejects(b.restart(),/drift/);
  assert.equal(f.mutations,0);
  f.setIdentity(false);await assert.rejects(b.logs(),/identity drift/);
 }finally{f.close();}
});
test('manifest digest is checked against RepoDigests, never image config ID',async()=>{
 const f=fixture();try {
  f.setModel(true);
  const b=new Broker(f.recipe,f.directory,'running',false,undefined,undefined,f.transport);
  assert.equal((await b.observe()).converged,true);
  f.setImage(false);assert.equal((await b.observe()).converged,false);
 }finally{f.close();}
});
test('failed receipt on successful process exit is rejected',async()=>{
 const f=fixture();try {
  f.setPhase('failed');const b=new Broker(f.recipe,f.directory,'stopped',true,undefined,undefined,f.transport);
  await assert.rejects(b.setRunning(false),/not verified/);
  assert.throws(()=>parseReceipt(JSON.stringify({version:1,phase:'verified',container_id:'wrong'}),f.recipe.observedContainerId));
 }finally{f.close();}
});
test('stdout helper errors survive nonzero exit and secrets are sanitized',async()=>{
 const f=fixture();try {
  const transport:typeof ssh=async()=>({exitCode:1,stdout:JSON.stringify({phase:'rejected',error:'Runtime identity changed; password=synthetic-secret'}),stderr:''});
  const b=new Broker(f.recipe,f.directory,'running',false,undefined,undefined,transport);
  await assert.rejects(b.inspect(),e=>e instanceof Error&&e.message.includes('Runtime identity changed')&&!e.message.includes('synthetic-secret'));
 }finally{f.close();}
});
test('budget exhaustion terminates outside tools; cancellation aborts wait and prevents commands',async()=>{
 const f=fixture();try {
  let terminated=false;
  const b=new Broker(f.recipe,f.directory,'running',true,()=>{throw new Error('budget exhausted');},()=>{terminated=true;},f.transport);
  await assert.rejects(b.inspect(),/budget exhausted/);assert.equal(terminated,true);assert.equal(f.calls,0);
  const waiting=new Broker(f.recipe,f.directory,'running',false,undefined,undefined,f.transport);
  const promise=waiting.wait(60);waiting.cancel();await assert.rejects(promise,/abort/i);
  await assert.rejects(waiting.inspect(),/abort/i);assert.equal(f.calls,0);
 }finally{f.close();}
});
test('log opt-out prevents retrieval; literal patterns and common credentials are filtered',async()=>{
 const f=fixture();try {
  f.recipe.diagnostics={logs:false};const b=new Broker(f.recipe,f.directory,'running',false,undefined,undefined,f.transport);
  assert.equal((await b.logs()).text,'');assert.equal(f.calls,0);
  assert.equal(sanitize('password=abc api_key="def" tenant-value',['tenant-value']),'password=[REDACTED] api_key=[REDACTED] [REDACTED]');
  assert.throws(()=>validateResult('observe',{converged:true}),/Invalid/);
 }finally{f.close();}
});

test('redaction preserves JSON structure and filters credential fields',()=>{
 const value={text:'password=abc',nested:{api_key:'synthetic-secret'},other:'safe'};
 assert.deepEqual(JSON.parse(JSON.stringify(sanitizeValue(value))),{text:'password=[REDACTED]',nested:{api_key:'[REDACTED]'},other:'safe'});
});

test('application readiness uses Docker health without model artifacts; unhealthy is not ready',async()=>{
 const f=fixture();try{
  f.recipe.engine='application';delete f.recipe.model;delete f.recipe.revision;
  f.recipe.requirements={api:'docker-health'};
  const broker=new Broker(f.recipe,f.directory,'running',false,undefined,undefined,f.transport);
  assert.equal((await broker.observe()).converged,true);
  assert.ok(!f.actions.some(action=>action==='python3 -c'));
  f.setHealth('unhealthy');assert.equal((await broker.observe()).converged,false);
  f.setImage(false);assert.equal((await broker.observe()).converged,false);
 }finally{f.close();}
});
