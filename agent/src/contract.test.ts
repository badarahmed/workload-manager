import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,readFileSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {loadRecipe} from './contract.js';
test('recipe identity rejects unpinned or remotely redirected probe targets',()=>{
 const dir=mkdtempSync(join(tmpdir(),'wm-contract-'));const file=join(dir,'recipe.json');
 const source=JSON.parse(readFileSync('agent/testdata/model-recipe.json','utf8'));
 try {
  for(const change of [{revision:'main'},{observedContainerId:'router'},{endpoint:'http://example.com:8899'},{driver:'launchd'}]) {
   writeFileSync(file,JSON.stringify({...source,...change}));assert.throws(()=>loadRecipe(file));
  }
  writeFileSync(file,JSON.stringify(source));const before=loadRecipe(file).digest;
  writeFileSync(file,JSON.stringify(source,null,2));assert.notEqual(loadRecipe(file).digest,before);
 } finally {rmSync(dir,{recursive:true});}
});

test('recipe rejects malformed transport, secret-bearing origins and unsafe paths',()=>{
 const dir=mkdtempSync(join(tmpdir(),'wm-contract-'));const file=join(dir,'recipe.json');
 const source=JSON.parse(readFileSync('agent/testdata/model-recipe.json','utf8'));
 try {
  for(const change of [{ssh:{}},{endpoint:'http://user:password@127.0.0.1:8899'},
   {inferencePath:'//evil/path'},{inferencePath:'@evil/path'},{model:'../escape'},
   {diagnostics:{logs:'yes'}},{observedImageId:'manifest-id'}]) {
   writeFileSync(file,JSON.stringify({...source,...change}));assert.throws(()=>loadRecipe(file));
  }
 }finally{rmSync(dir,{recursive:true,force:true});}
});

test('local application binding is pinned to the controller and contains no model',async()=>{
 const {hostname}=await import('node:os');
 const dir=mkdtempSync(join(tmpdir(),'wm-app-contract-'));const file=join(dir,'recipe.json');
 const source={name:'fixture',host:'studio',driver:'docker',engine:'application',container:'fixture',
  local:{hostname:hostname(),dockerHost:'unix:///tmp/fixture.sock'},observedContainerId:'a'.repeat(64),observedImageId:'sha256:'+'b'.repeat(64),
  imageDigestFromHostDocumentation:'example/app@sha256:'+'c'.repeat(64),requirements:{api:'docker-health'}};
 try{
  writeFileSync(file,JSON.stringify(source));assert.equal(loadRecipe(file).recipe.engine,'application');
  writeFileSync(file,JSON.stringify({...source,local:{...source.local,hostname:'another-controller'}}));assert.throws(()=>loadRecipe(file),/hostname/);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
