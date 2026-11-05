import {readFileSync} from 'node:fs';
import {homedir} from 'node:os';
import {join} from 'node:path';
import {createModels,createProvider,getSupportedThinkingLevels} from '@earendil-works/pi-ai/models';
import {openAICompletionsApi} from '@earendil-works/pi-ai/api/openai-completions.lazy';
import {loadModelConfig,modelConfigDigest,type ModelConfig} from './model-config.js';
import {openaiProvider} from '@earendil-works/pi-ai/providers/openai';
import {openaiCodexProvider} from '@earendil-works/pi-ai/providers/openai-codex';

export function codexCredential(document:string,now=Date.now()) {
 let parsed:any,claims:any;
 try {
  parsed=JSON.parse(document);
  const token=parsed.tokens?.access_token;
  if(typeof token!=='string') throw new Error();
  claims=JSON.parse(Buffer.from(token.split('.')[1],'base64url').toString());
 }catch {throw new Error('Codex ChatGPT credential unavailable or malformed; run codex login on the control host');}
 if(!Number.isFinite(claims.exp)||claims.exp*1000<now+35*60*1000) throw new Error('Codex credential expires too soon; refresh the Codex sign-in before apply');
 return {type:'oauth' as const,access:parsed.tokens.access_token as string,refresh:'',expires:claims.exp*1000};
}

/** Read access credentials on demand. Never copy, refresh or rewrite Codex auth. */
export async function workloadModels(config:ModelConfig|undefined=loadModelConfig()) {
 if(config?.backend==='openai-compatible') return compatibleModels(config);
 const modelId=config?.model??'gpt-6.1-sol';
 const thinkingLevel=config?.thinkingLevel;
 const mode=config?(config.backend==='codex'?'codex':'api'):process.env.WM_MODEL_AUTH??'auto';
 if(!['auto','codex','api'].includes(mode)) throw new Error('WM_MODEL_AUTH must be auto, codex or api');
 if(mode==='api'||(mode==='auto'&&process.env.OPENAI_API_KEY)) {
  if(!process.env.OPENAI_API_KEY) throw new Error('API authentication selected but OPENAI_API_KEY is absent');
  const models=createModels();models.setProvider(openaiProvider());
  if(!models.getModel('openai',modelId))throw new Error('Configured model is not in the OpenAI provider catalog');
  if(thinkingLevel&&!getSupportedThinkingLevels(models.getModel('openai',modelId)!).includes(thinkingLevel))throw new Error('Configured thinkingLevel is not supported by the selected model');
  return {models,thinkingLevel,model:{provider:'openai',modelId},source:'OPENAI_API_KEY',configDigest:modelConfigDigest({backend:'openai',model:modelId,thinkingLevel})};
 }
 const file=join(process.env.CODEX_HOME??join(homedir(),'.codex'),'auth.json');
 const read=async(provider:string)=>{
  if(provider!=='openai-codex') return undefined;
  let document:string;
  try {document=readFileSync(file,'utf8');}catch{throw new Error('Cannot read Codex file authentication; configure codex login on the control host or use API authentication');}
  return codexCredential(document);
 };
 const models=createModels({credentials:{
  read,
  list:async()=>[{providerId:'openai-codex',type:'oauth' as const}],
  modify:async()=>{throw new Error('Codex owns credential refresh; refresh through Codex and retry');},
  delete:async()=>{throw new Error('wm does not modify Codex credentials');},
 }});
 models.setProvider(openaiCodexProvider());
 // Fail before admitting mutations; never log the resolved auth object.
 await read('openai-codex');
 await models.getAuth('openai-codex');
 if(!models.getModel('openai-codex',modelId))throw new Error('Configured model is not in the Codex provider catalog');
  if(thinkingLevel&&!getSupportedThinkingLevels(models.getModel('openai-codex',modelId)!).includes(thinkingLevel))throw new Error('Configured thinkingLevel is not supported by the selected model');
 return {models,thinkingLevel,model:{provider:'openai-codex',modelId},source:'Codex ChatGPT sign-in',configDigest:modelConfigDigest({backend:'codex',model:modelId,thinkingLevel})};
}

async function compatibleModels(config:Extract<ModelConfig,{backend:'openai-compatible'}>) {
 const providerId='wm-compatible';
 const models=createModels();
 // Explicit auth resolver: never fall back to Codex or OPENAI_API_KEY for a LAN server.
 const resolve=async()=>{
  const key=config.apiKeyEnv?process.env[config.apiKeyEnv]:'wm-local-no-auth';
  if(!key)throw new Error('Configured agent apiKeyEnv is unset or empty');
  return {auth:{apiKey:key},source:config.apiKeyEnv??'Local endpoint (no secret)'};
 };
 await resolve();
 models.setProvider(createProvider({
  id:providerId,name:'Configured wm agent',baseUrl:config.baseUrl,
  auth:{apiKey:{name:'Configured endpoint authentication',resolve}},
  models:[{id:config.model,name:config.model,api:'openai-completions',provider:providerId,
   baseUrl:config.baseUrl,input:['text'],reasoning:config.reasoning,
   contextWindow:config.contextWindow,maxTokens:config.maxOutputTokens,
   cost:{input:0,output:0,cacheRead:0,cacheWrite:0},
   compat:{supportsStore:false,supportsDeveloperRole:false,supportsReasoningEffort:false,
    supportsStrictMode:false,supportsOpenAIGrammarTools:false,maxTokensField:'max_tokens',...config.compat},
  }],api:openAICompletionsApi(),
 }));
 return {models,thinkingLevel:undefined,model:{provider:providerId,modelId:config.model},source:'Configured compatible endpoint',configDigest:modelConfigDigest(config)};
}
