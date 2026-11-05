import { spawn } from 'node:child_process';

export interface SshTarget { hostname: string; user: string; hostKeyAlias: string; knownHostsFile?: string }
export interface CommandResult { stdout: string; stderr: string; exitCode: number | null }
export function shellQuote(value: string): string {
  if (value.includes('\0')) throw new Error('NUL in argument');
  return "'" + value.replaceAll("'", "'\\''") + "'";
}
export function sshArgs(target: SshTarget, argv: string[]): string[] {
  for (const value of [target.hostname,target.user,target.hostKeyAlias]) {
    if (!/^[a-zA-Z0-9._:@-]+$/.test(value) || value.startsWith('-')) throw new Error('Invalid SSH identity');
  }
  if (!argv.length) throw new Error('Empty command');
  if(target.knownHostsFile && (!target.knownHostsFile.startsWith('/') || /[\r\n\0]/.test(target.knownHostsFile))) throw new Error('Invalid known hosts file');
  return ['-T', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes', '-o', 'ConnectTimeout=10',
    '-o', 'ServerAliveInterval=15', '-o', 'ServerAliveCountMax=2', '-o', `HostKeyAlias=${target.hostKeyAlias}`,
    ...(target.knownHostsFile?['-o',`UserKnownHostsFile=${target.knownHostsFile}`]:[]),
    `${target.user}@${target.hostname}`, argv.map(shellQuote).join(' ')];
}
/** A transport error is an unknown remote outcome, never evidence that a mutation failed. */
export async function ssh(target: SshTarget, argv: string[], input = '', timeoutMs = 30_000, signal?:AbortSignal): Promise<CommandResult> {
  return runCommand('ssh',sshArgs(target,argv),input,timeoutMs,signal);
}
export async function runCommand(binary:string,argv:string[],input='',timeoutMs=30000,signal?:AbortSignal,env=process.env):Promise<CommandResult> {
  signal?.throwIfAborted();
  return new Promise((resolve, reject) => {
    const child = spawn(binary,argv,{stdio:['pipe','pipe','pipe'],env});
    const out: Buffer[] = [], err: Buffer[] = [];
    let size = 0, failure: Error | undefined;
    let escalation:ReturnType<typeof setTimeout>|undefined;
    const stop=(reason:string)=>{
      if(failure) return;
      failure=new Error(reason+': remote outcome unknown');child.kill('SIGTERM');
      escalation=setTimeout(()=>child.kill('SIGKILL'),1000);
    };
    const abort=()=>stop('SSH cancelled');
    signal?.addEventListener('abort',abort,{once:true});
    if(signal?.aborted) abort();
    const timer = setTimeout(()=>stop('SSH timeout'),timeoutMs);
    const cleanup=()=>{clearTimeout(timer);clearTimeout(escalation);signal?.removeEventListener('abort',abort);};
    for (const [stream, chunks] of [[child.stdout, out], [child.stderr, err]] as const) {
      stream.on('data', (chunk: Buffer) => {
        size += chunk.length;
        if (size > 2 * 1024 * 1024) { stop('SSH output limit'); }
        else chunks.push(chunk);
      });
    }
    child.on('error', e => { cleanup(); reject(e); });
    child.on('close', code => {
      cleanup();
      if (failure) reject(failure);
      else resolve({ stdout: Buffer.concat(out).toString(), stderr: Buffer.concat(err).toString(), exitCode: code });
    });
    child.stdin.on('error', () => {});
    child.stdin.end(input);
  });
}
