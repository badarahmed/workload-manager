import type {SshTarget} from './ssh.js';

export interface Monitoring {ssh:SshTarget; jobs:string[]; scrapeIntervalSeconds:number}
type Series={metric:Record<string,string>;values:[number,string][]};
export function summarizeMetrics(series:Series[],now:number,interval:number) {
 return series.map(s=>{
  const latest=s.values.at(-1);
  const value=latest?Number(latest[1]):NaN;
  const age=latest?now-latest[0]:null;
  const status=!latest?'missing':!Number.isFinite(value)||!Number.isFinite(age)?'error':age! < -5?'error':age! > 2*interval+15?'stale':'fresh';
  return {labels:s.metric,status,value:Number.isFinite(value)?value:null,ageSeconds:age,sourceTimestamp:latest?.[0]??null,samples:s.values};
 });
}

// Fixed selectors only: the model cannot supply arbitrary PromQL or HTTP paths.
// A range-vector instant query preserves original scrape timestamps.
export const metricsScript=`import json,sys,time,urllib.request,urllib.parse
p=json.load(sys.stdin)
jobs='|'.join(p['jobs'])
selector='{job=~"'+jobs+'",__name__=~"up|node_memory_MemAvailable_bytes|node_memory_MemTotal_bytes|node_scrape_collector_success|nvidia_smi_last_collect_success"}[5m]'
url='http://127.0.0.1:9090/api/v1/query?'+urllib.parse.urlencode({'query':selector,'timeout':'10s'})
try:
 with urllib.request.urlopen(url,timeout=12) as r:
  raw=r.read(1048577)
  if len(raw)>1048576: raise Exception('metrics response exceeds limit')
  result=json.loads(raw)
 print(json.dumps({'evaluatedAt':time.time(),'query':selector,'response':result}))
except Exception as e: print(json.dumps({'error':str(e)}))
`;

export function interpretMetrics(raw:any,config:Monitoring) {
 if(raw.error || raw.response?.status!=='success') return {status:'error',error:raw.error??raw.response?.error??'Invalid Prometheus response',warnings:raw.response?.warnings??[]};
 const data=raw.response.data;
 if(data?.resultType!=='matrix'||!Array.isArray(data.result)||!Number.isFinite(raw.evaluatedAt)) throw new Error('Invalid metrics response');
 if(data.result.length>100) throw new Error('Metrics series limit exceeded; coverage incomplete');
 for(const series of data.result) {
  if(!series||!series.metric||typeof series.metric!=='object'||Object.values(series.metric).some(v=>typeof v!=='string')||!Array.isArray(series.values)||series.values.length>1000||series.values.some((v:unknown)=>!Array.isArray(v)||v.length!==2||!Number.isFinite(v[0])||typeof v[1]!=='string')) throw new Error('Invalid metrics series');
 }
 const observations=summarizeMetrics(data.result,raw.evaluatedAt,config.scrapeIntervalSeconds);
 return {status:observations.length?'observed':'missing',evaluatedAt:raw.evaluatedAt,query:raw.query,windowSeconds:300,jobs:config.jobs,observations,warnings:raw.response.warnings??[],infos:raw.response.infos??[],missingJobs:config.jobs.filter(job=>!observations.some(s=>s.labels.job===job)),readinessEvidence:false};
}
