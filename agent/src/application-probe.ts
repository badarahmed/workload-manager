import type {Command} from './driver.js';
import type {Recipe} from './contract.js';

/** Bounded read-only HTTP probe; no caller-supplied code, headers or credentials. */
export const applicationProbeScript=`import json,sys,urllib.request,math
p=json.load(sys.stdin)
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs): raise ValueError('Redirect forbidden')
try:
 opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect)
 with opener.open(p['url'],timeout=10) as r:
  data=r.read(262145)
  if r.status!=200 or len(data)>262144: raise ValueError('Unexpected HTTP response')
 if p['format']=='json':
  result=json.loads(data)
  ok=isinstance(result,dict) and result.get(p['field'])==p['equals']
 elif p['format']=='text': ok=data.decode().strip()==p['equals']
 else:
  samples=[]
  for line in data.decode().splitlines():
   if line.startswith('#') or not line.strip(): continue
   parts=line.split()
   if len(parts)>=2 and parts[0]==p['field']: samples.append(float(parts[1]))
  expected=float(p['equals'])
  ok=len(samples)==1 and math.isfinite(expected) and math.isfinite(samples[0]) and samples[0]==expected
 print(json.dumps({'ready':bool(ok),'error':None if ok else 'Application probe condition not met'}))
except Exception: print(json.dumps({'ready':False,'error':'Application HTTP probe failed'}))
`;
export async function applicationProbe(recipe:Recipe,command:Command) {
 const probe=recipe.probe??{location:'host',format:'json',field:'status',equals:'OK'};
 const argv=probe.location==='container'
  ?['docker','exec','-i',recipe.observedContainerId,'python3','-c',applicationProbeScript]
  :['python3','-c',applicationProbeScript];
 const raw=JSON.parse(await command(argv,JSON.stringify({...probe,url:recipe.endpoint!+recipe.inferencePath!}),15000));
 if(typeof raw.ready!=='boolean')throw new Error('Invalid application probe result');
 return raw.ready?{ready:true}:{ready:false,error:'Application HTTP probe failed or condition not met'};
}
