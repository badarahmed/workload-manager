import {appendFileSync} from 'node:fs';
import type {Action} from './protocol.js';

export type Progress=(action:Action,phase:'begin'|'done'|'error',result?:unknown)=>void;
const descriptions:Record<Action,string>={
 observe:'Inspecting runtime and readiness',start:'Starting captured container',stop:'Stopping captured container',
 restart:'Performing authorized corrective restart',probe:'Checking functional readiness',
 wait:'Waiting 60s for loading, then checking readiness',logs:'Reading scoped diagnostics',metrics:'Reading Prometheus observations',
};
function object(value:unknown):Record<string,unknown> {
 return value!==null&&typeof value==='object'?value as Record<string,unknown>:{};
}
export function outcome(action:Action,value:unknown):string {
 const result=object(value);
 if(action==='probe') {
  if(result.ready===true)return 'Functional probe passed';
  const error=typeof result.error==='string'?result.error:'';
  if(/drift/i.test(error))return 'Not ready: image or model drift requires repair';
  if(/reset/i.test(error))return 'Not ready: connection reset by peer';
  if(/refused/i.test(error))return 'Not ready: connection refused';
  if(/timed? ?out/i.test(error))return 'Not ready: probe timed out';
  return 'Functional probe not ready';
 }
 if(action==='observe'||action==='wait') {
  const runtime=object(result.runtime);
  const state=runtime.running===true?'running':runtime.running===false?'stopped':'unknown';
  const probe=result.probe?'; '+outcome('probe',result.probe).toLowerCase():'';
  return `Container ${state}${probe}${result.converged===true?'; desired state reached':''}`;
 }
 if(action==='start'||action==='stop')return result.changed===false?'Container already in requested state':action==='start'?'Container started; readiness still requires verification':'Container stopped';
 if(action==='restart')return result.changed===false?'Restart unnecessary; already healthy':'Lifecycle step completed; readiness still requires verification';
 if(action==='logs')return result.disabled===true?'Raw logs disabled by policy':'Diagnostics returned to agent (content omitted)';
 const status=['observed','missing','error','unsupported'].includes(String(result.status))?result.status:'unknown';
 return `Prometheus observations: ${status}`;
}

/** Best-effort UI telemetry only. Never writes raw logs, errors, code or reasoning. */
export function createProgress(workload:string,file=process.env.WM_PROGRESS_FILE) {
 const label=workload.replace(/[^a-zA-Z0-9_.-]/g,'_').slice(0,80);
 const write=(message:string)=>{
  if(!file)return;
  try {appendFileSync(file,JSON.stringify({workload:label,message})+'\n',{mode:0o600});}
  catch { /* Loss of UI telemetry must not change reconciliation outcomes. */ }
 };
 const step:Progress=(action,phase,result)=>write(phase==='begin'?descriptions[action]:phase==='done'?outcome(action,result):`${action} failed; see operation journal for details`);
 return {write,step};
}
