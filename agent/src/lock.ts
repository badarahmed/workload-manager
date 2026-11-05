import {homedir} from 'node:os';
import {join} from 'node:path';
import {mkdirSync} from 'node:fs';
import {createHash} from 'node:crypto';
import type {Recipe} from './contract.js';
import {spawn} from 'node:child_process';

/** Kernel releases this lock on EOF/process death; no stale PID-file guessing. */
export async function acquireOperationLock(file:string):Promise<()=>Promise<void>> {
 const code="import fcntl,sys\nf=open(sys.argv[1],'a')\nfcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)\nprint('locked',flush=True)\nsys.stdin.read()\n";
 const child=spawn('python3',['-u','-c',code,file],{stdio:['pipe','pipe','pipe']});
 let errors='';child.stderr.on('data',chunk=>{errors+=chunk.toString();});
 const done=new Promise<void>(resolve=>child.once('close',()=>resolve()));
 await new Promise<void>((resolve,reject)=>{
  child.once('error',reject);
  child.once('exit',()=>reject(new Error('Operation lock unavailable: '+errors.slice(-1000))));
  child.stdout.once('data',chunk=>chunk.toString().trim()==='locked'?resolve():reject(new Error('Invalid lock response')));
 });
 return async()=>{child.stdin.end();await done;};
}

/** One controller OS user, across all checkouts, intents and operation IDs. */
export function workloadLockPath(recipe:Recipe,root=join(homedir(),'.local/state/wm/locks')) {
 mkdirSync(root,{recursive:true,mode:0o700});
 const identity=JSON.stringify([recipe.ssh?.hostKeyAlias??recipe.local?.hostname,recipe.driver,recipe.observedContainerId]);
 return join(root,createHash('sha256').update(identity).digest('hex')+'.lock');
}
