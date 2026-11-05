import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {acquireOperationLock} from './lock.js';

test('concurrent operation is rejected; release permits subsequent runner',async()=>{
 const root=mkdtempSync(join(tmpdir(),'wm-lock-'));const path=join(root,'lock');
 try {
  const release=await acquireOperationLock(path);
  try {await assert.rejects(acquireOperationLock(path),/lock unavailable/);}finally{await release();}
  await (await acquireOperationLock(path))();
 }finally{rmSync(root,{recursive:true,force:true});}
});

test('workload lock excludes opposing intents across operation directories',async()=>{
 const {workloadLockPath}=await import('./lock.js');
 const {loadRecipe}=await import('./contract.js');
 const {recipe}=loadRecipe('agent/testdata/model-recipe.json');
 const root=mkdtempSync(join(tmpdir(),'wm-workload-lock-'));
 try {
  const path=workloadLockPath(recipe,root);
  const changed={...recipe,name:'renamed',recovery:{allowRestart:false,startupGraceSeconds:600}};
  assert.equal(workloadLockPath(changed,root),path);
  const release=await acquireOperationLock(path);
  try{await assert.rejects(acquireOperationLock(workloadLockPath(changed,root)),/lock unavailable/);}
  finally{await release();}
 }finally{rmSync(root,{recursive:true,force:true});}
});
