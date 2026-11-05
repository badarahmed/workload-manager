import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createProgress,outcome} from './progress.js';

test('progress reports actions/readiness without raw logs, model text or error secrets',()=>{
 const dir=mkdtempSync(join(tmpdir(),'wm-progress-test-'));
 try {
  const file=join(dir,'events.jsonl');const progress=createProgress('d3\n\x1b',file);
  progress.step('start','begin');
  progress.step('logs','done',{text:'password=synthetic-secret'});
  progress.step('probe','done',{ready:false,error:'Connection reset by peer password=synthetic-secret'});
  progress.step('start','error',new Error('password=synthetic-secret'));
  progress.step('wait','done',{runtime:{running:true},probe:{ready:true},converged:true});
  const text=readFileSync(file,'utf8');assert.ok(!text.includes('synthetic-secret'));
  const records=text.trim().split('\n').map(line=>JSON.parse(line));
  assert.equal(records.length,5);assert.equal(records[0].message,'Starting captured container');
  assert.ok(records.at(-1).message.includes('desired state reached'));
  assert.equal(outcome('probe',{ready:false,error:'Connection reset by peer'}),'Not ready: connection reset by peer');
 }finally{rmSync(dir,{recursive:true,force:true});}
});
test('absent or unavailable progress destination does not fail reconciliation',()=>{
 assert.doesNotThrow(()=>createProgress('d3','').write('hello'));
 assert.doesNotThrow(()=>createProgress('d3','/nonexistent-wm-dir/progress').step('start','begin'));
});
