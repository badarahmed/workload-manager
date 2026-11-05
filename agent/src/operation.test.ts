import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {Operation} from './operation.js';

test('interruption reuses admission, completed operations start a new generation',()=>{
 const root=mkdtempSync(join(tmpdir(),'wm-operation-'));
 try {
  const first=new Operation(root,'intent',1000);
  const draft=first.submission('original evidence');
  const resumed=new Operation(root,'intent',2000);
  assert.equal(resumed.directory,first.directory);
  assert.deepEqual(resumed.submission('new evidence must not change pending input'),draft);
  assert.equal(resumed.remaining(2000),30*60*1000-1000);
  resumed.advance();assert.equal(resumed.submission('fresh followup').requestId,'reconcile-1');
  resumed.finish(true);
  const next=new Operation(root,'intent',3000);
  assert.notEqual(next.directory,first.directory);
  assert.equal(next.attempt,0);
  assert.throws(()=>new Operation(root,'different intent'),/mismatch/);
 } finally {rmSync(root,{recursive:true,force:true});}
});

test('command budget survives restart and resets only for a new generation',()=>{
 const root=mkdtempSync(join(tmpdir(),'wm-budget-'));
 try {
  const first=new Operation(root,'intent');
  for(let i=0;i<79;i++) first.consumeCommand();
  const resumed=new Operation(root,'intent');
  resumed.consumeCommand();
  assert.throws(()=>resumed.consumeCommand(),/budget exhausted/);
  resumed.finish(false);
  new Operation(root,'intent').consumeCommand();
 }finally{rmSync(root,{recursive:true,force:true});}
});

test('model-call budget persists across retries independently of command budget',()=>{
 const root=mkdtempSync(join(tmpdir(),'wm-model-budget-'));
 try {
  const first=new Operation(root,'intent');
  for(let i=0;i<40;i++) first.consumeModelCall();
  const resumed=new Operation(root,'intent');
  assert.throws(()=>resumed.consumeModelCall(),/model-call budget exhausted/);
  resumed.consumeCommand();
 }finally{rmSync(root,{recursive:true,force:true});}
});

test('active operations pin model configuration; new generations can switch',()=>{
 const root=mkdtempSync(join(tmpdir(),'wm-model-pin-'));
 try {
  const operation=new Operation(root,'intent');operation.bindModel('first');
  const resumed=new Operation(root,'intent');resumed.bindModel('first');
  assert.throws(()=>resumed.bindModel('different'),/configuration changed/);
  resumed.finish(true);
  new Operation(root,'intent').bindModel('different');
 }finally{rmSync(root,{recursive:true,force:true});}
});
