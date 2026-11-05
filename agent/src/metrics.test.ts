import {test} from 'node:test';
import assert from 'node:assert/strict';
import {summarizeMetrics,interpretMetrics} from './metrics.js';

test('metrics preserve missing, stale and nonfinite evidence without inferring readiness',()=>{
 const observations=summarizeMetrics([
  {metric:{job:'host'},values:[[990,'0']]},
  {metric:{job:'host'},values:[[800,'50']]},
  {metric:{job:'host'},values:[[990,'NaN']]},
  {metric:{job:'host'},values:[]},
 ],1000,60);
 assert.deepEqual(observations.map(s=>s.status),['fresh','stale','error','missing']);
 assert.equal(observations[0].value,0);
 assert.equal(observations[2].value,null);
 const config={ssh:{hostname:'studio',user:'operator',hostKeyAlias:'studio'},jobs:['host'],scrapeIntervalSeconds:60};
 const result=interpretMetrics({evaluatedAt:1000,response:{status:'success',data:{resultType:'matrix',result:[]},warnings:['partial']}},config);
 assert.equal(result.status,'missing');
 assert.deepEqual(result.missingJobs,['host']);
 assert.deepEqual(result.warnings,['partial']);
 assert.equal(result.readinessEvidence,false);
});
