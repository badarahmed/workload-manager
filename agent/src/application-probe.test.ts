import {test} from 'node:test';
import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import {applicationProbe,applicationProbeScript} from './application-probe.js';
import type {Recipe} from './contract.js';

test('application HTTP comparisons are bounded, strict, and support JSON/text/metrics',async()=>{
 const run=(body:string,format:string,field:string,equals:string)=>new Promise<{ready:boolean}>((resolve,reject)=>{
  const prefix=`import io,urllib.request\nbody=${JSON.stringify(body)}.encode()\nclass Response(io.BytesIO):\n status=200\nclass Opener:\n def open(self,*args,**kwargs): return Response(body)\nurllib.request.build_opener=lambda *args: Opener()\n`;
  const p=execFile('python3',['-c',prefix+applicationProbeScript],{timeout:5000},(e,out)=>e?reject(e):resolve(JSON.parse(out)));
  p.stdin!.end(JSON.stringify({url:'http://127.0.0.1/health',format,field,equals}));
 });
 assert.equal((await run('{"status":"healthy"}','json','status','healthy')).ready,true);
 assert.equal((await run('{"database":"ok"}','json','database','ok')).ready,true);
 assert.equal((await run('{"database":"error"}','json','database','ok')).ready,false);
 assert.equal((await run('ok\n','text','status','ok')).ready,true);
 assert.equal((await run('collect_success 1.0\n','prometheus','collect_success','1')).ready,true);
 assert.equal((await run('collect_success 0\n','prometheus','collect_success','1')).ready,false);
 assert.equal((await run('collect_success NaN\n','prometheus','collect_success','1')).ready,false);
 assert.equal((await run('x'.repeat(262145),'text','status','ok')).ready,false);
});
test('container probe uses exact captured ID and fixed read-only program',async()=>{
 const recipe={observedContainerId:'a'.repeat(64),endpoint:'http://127.0.0.1:9810',inferencePath:'/metrics',probe:{location:'container',format:'prometheus',field:'collect_success',equals:'1'}} as Recipe;
 let seen:string[]=[];
 assert.equal((await applicationProbe(recipe,async argv=>{seen=argv;return '{"ready":true}';})).ready,true);
 assert.deepEqual(seen.slice(0,6),['docker','exec','-i','a'.repeat(64),'python3','-c']);
 assert.equal(seen[6],applicationProbeScript);
});
