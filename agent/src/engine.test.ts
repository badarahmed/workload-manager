import {test} from 'node:test';
import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import {readFileSync,mkdtempSync,rmSync,mkdirSync,writeFileSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {VllmSrEngine} from './engine.js';
import type {Recipe} from './contract.js';
import type {Command} from './driver.js';

test('engine Python scripts handle missing config, reject redirects and bound response size',async()=>{
 const recipe:Recipe=JSON.parse(readFileSync('agent/testdata/model-recipe.json','utf8'));
 const root=mkdtempSync(join(tmpdir(),'wm-engine-'));recipe.remoteStateDirectory=root;
 let response='valid';
 const command:Command=(argv,input='')=>new Promise((resolve,reject)=>{
  // Exercise the actual Python script with urllib replaced at its I/O boundary.
  // No network listener or SSH connection is needed by this fixture.
  const prefix=argv[2].includes('urllib.request')?`import urllib.request,io,json
scenario=${JSON.stringify(response)}
class Opener:
 def __init__(self,handler): self.handler=handler
 def open(self,req,timeout):
  if scenario=='redirect': return self.handler.redirect_request(None,None,None,None,None,None)
  if scenario=='large': return io.BytesIO(b'x'*1048577)
  model=json.loads(req.data)['model']
  return io.BytesIO(json.dumps({'model':model,'answers':{'kind':{'choice':'code','probabilities':{'code':0.8,'math':0.1,'chat':0.1}}}}).encode())
urllib.request.build_opener=lambda handler: Opener(handler)
`:'';
  const child=execFile(argv[0],['-c',prefix+argv[2]],{timeout:5000,maxBuffer:2*1024*1024},(error,stdout)=>error?reject(error):resolve(stdout));
  child.stdin!.end(input);
 });
 const engine=new VllmSrEngine(recipe,command);
 try {
  assert.equal((await engine.validate()).valid,false);
  mkdirSync(join(root,'.vllm-sr'));writeFileSync(join(root,'.vllm-sr/runtime-config.yaml'),Buffer.from([255]));
  const malformed=await engine.validate();assert.equal(malformed.valid,false);assert.match(malformed.reason,/decode/);
  assert.equal((await engine.probe()).ready,true);
  response='redirect';const redirected=await engine.probe();assert.equal(redirected.ready,false);assert.match(redirected.error!,/redirects forbidden/);
  response='large';const large=await engine.probe();assert.equal(large.ready,false);assert.match(large.error!,/exceeds limit/);
 }finally{rmSync(root,{recursive:true,force:true});}
});
