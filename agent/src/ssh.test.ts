import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { shellQuote, sshArgs } from './ssh.js';

test('remote arguments survive shell interpretation without executing substitutions', () => {
  const value = "x' $(touch /tmp/wm-should-not-exist) `id` \n spaces";
  const actual = execFileSync('/bin/sh', ['-c', `printf %s ${shellQuote(value)}`], { encoding: 'utf8' });
  assert.equal(actual, value);
});
test('reject SSH option injection and NUL arguments', () => {
  assert.throws(() => sshArgs({ hostname: '-oProxyCommand=bad', user: 'operator', hostKeyAlias: 'gpu-01' }, ['true']));
  assert.throws(() => shellQuote('a\0b'));
});
test('strict host key checking remains enabled', () => {
  const args = sshArgs({ hostname: '192.0.2.10', user: 'operator', hostKeyAlias: 'gpu-01' }, ['docker','inspect','d3']);
  assert.ok(args.includes('StrictHostKeyChecking=yes'));
  assert.equal(args.at(-1), "'docker' 'inspect' 'd3'");
});

test('SSH cancellation escalates when child ignores SIGTERM and reports unknown outcome',{timeout:5000},async()=>{
 const {mkdtempSync,writeFileSync,rmSync}=await import('node:fs');
 const {tmpdir}=await import('node:os');const {join}=await import('node:path');
 const {ssh}=await import('./ssh.js');
 const directory=mkdtempSync(join(tmpdir(),'wm-ssh-cancel-'));
 const original=process.env.PATH;
 try {
  writeFileSync(join(directory,'ssh'),'#!/usr/bin/env python3\nimport signal,time\nsignal.signal(signal.SIGTERM,signal.SIG_IGN)\ntime.sleep(60)\n',{mode:0o700});
  process.env.PATH=directory+':'+original;
  const abort=new AbortController();
  const pending=ssh({hostname:'fixture',user:'test',hostKeyAlias:'fixture'},['true'],'',3000,abort.signal);
  const timer=setTimeout(()=>abort.abort(),200);
  try {await assert.rejects(pending,/cancelled: remote outcome unknown/);}finally{clearTimeout(timer);}
  await assert.rejects(ssh({hostname:'fixture',user:'test',hostKeyAlias:'fixture'},['true'],'',200),/timeout: remote outcome unknown/);
 }finally{process.env.PATH=original;rmSync(directory,{recursive:true,force:true});}
});
