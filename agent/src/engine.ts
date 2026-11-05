import type {Recipe} from './contract.js';
import type {Command} from './driver.js';

/** Engine evidence is separate from runtime ownership and lifecycle. */
export class VllmSrEngine {
 private readonly recipe:Recipe & {model:string;revision:string;remoteStateDirectory:string;endpoint:string;inferencePath:string};
 constructor(recipe:Recipe,private readonly command:Command) {
  if(!recipe.model||!recipe.revision||!recipe.remoteStateDirectory||!recipe.endpoint||!recipe.inferencePath)throw new Error('Incomplete model profile');
  this.recipe={...recipe,model:recipe.model,revision:recipe.revision,remoteStateDirectory:recipe.remoteStateDirectory,endpoint:recipe.endpoint,inferencePath:recipe.inferencePath};
 }
 async validate() {
  const selectionScript=`import json,pathlib,re,sys
p=json.load(sys.stdin)
try:
 root=pathlib.Path(p['root'])
 config=root/'.vllm-sr/runtime-config.yaml'
 text=config.read_text() if config.is_file() else ''
 revisions=re.findall(r'^\\s*revision:\\s*([a-f0-9]{40})\\s*$',text,re.M)
 manifest=root/'models/model-runtime'/('models--'+p['model'].replace('/','--'))/'snapshots'/p['revision']/'MODEL_MANIFEST.json'
 print(json.dumps({'revisions':revisions,'manifest_present':manifest.is_file()}))
except Exception as e: print(json.dumps({'revisions':[],'manifest_present':False,'error':str(e)}))
`;
  const selection=JSON.parse(await this.command(['python3','-c',selectionScript],JSON.stringify({root:this.recipe.remoteStateDirectory,model:this.recipe.model,revision:this.recipe.revision})));
  if(!Array.isArray(selection.revisions)||selection.revisions.some((r:unknown)=>typeof r!=='string')||typeof selection.manifest_present!=='boolean') throw new Error('Invalid model evidence');
  return {valid:selection.revisions.length===1&&selection.revisions[0]===this.recipe.revision&&selection.manifest_present===true, revisions:selection.revisions,manifestPresent:selection.manifest_present,reason:typeof selection.error==='string'?selection.error:'Model revision and manifest check'};
 }
 async probe() {
  const script=`import json,urllib.request,math\nclass NoRedirect(urllib.request.HTTPRedirectHandler):\n def redirect_request(self,*args,**kwargs): raise ValueError('Probe redirects forbidden')\np=json.load(__import__('sys').stdin)\ntry:\n req=urllib.request.Request(p['url'],data=json.dumps(p['body']).encode(),headers={'Content-Type':'application/json'})\n opener=urllib.request.build_opener(NoRedirect)\n with opener.open(req,timeout=60) as r:\n  raw=r.read(1048577)\n  if len(raw)>1048576: raise ValueError('probe response exceeds limit')\n  d=json.loads(raw)\n a=d.get('answers',{}).get('kind',{}); probs=a.get('probabilities',{})\n ok=d.get('model')==p['body']['model'] and a.get('choice')=='code' and set(probs)=={'code','math','chat'} and all(isinstance(v,(int,float)) and math.isfinite(v) and 0<=v<=1 for v in probs.values()) and abs(sum(probs.values())-1)<0.01\n print(json.dumps({'ready':ok,'model':d.get('model'),'answer':a}))\nexcept Exception as e: print(json.dumps({'ready':False,'error':str(e)}))\n`;
  const payload={url:this.recipe.endpoint+this.recipe.inferencePath,body:{model:this.recipe.model,state:'Write a Python function that merges two sorted lists.',questions:{kind:{type:'choice',instructions:'What kind of work is this?',criteria:{code:'Writing or fixing code',math:'Mathematics',chat:'Anything else'}}}}};
  const result=JSON.parse(await this.command(['python3','-c',script],JSON.stringify(payload),70000));
  if(typeof result.ready!=='boolean'||(result.error!==undefined&&typeof result.error!=='string')) throw new Error('Invalid engine probe response');
  return {ready:result.ready,error:result.error} as {ready:boolean;error?:string};
 }
}
