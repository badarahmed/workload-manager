import {parseDocument} from 'yaml';
import {readFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {Type,type Static} from 'typebox';
import {Value} from 'typebox/value';
import {OpenAICompletionsCompatSchema} from '@earendil-works/pi-ai/providers/compat-schema';

const thinkingLevel=Type.Union([Type.Literal('off'),Type.Literal('minimal'),Type.Literal('low'),Type.Literal('medium'),Type.Literal('high'),Type.Literal('xhigh'),Type.Literal('max')]);
const cloud=Type.Object({version:Type.Literal(1),backend:Type.Union([Type.Literal('codex'),Type.Literal('openai')]),
 model:Type.String({minLength:1,maxLength:256}),thinkingLevel:Type.Optional(thinkingLevel)},{additionalProperties:false});
const compatible=Type.Object({version:Type.Literal(1),backend:Type.Literal('openai-compatible'),
 model:Type.String({minLength:1,maxLength:256}),baseUrl:Type.String({minLength:1}),
 apiKeyEnv:Type.Optional(Type.String({pattern:'^[A-Z][A-Z0-9_]*$'})),
 contextWindow:Type.Integer({minimum:4096,maximum:2000000}),
 maxOutputTokens:Type.Integer({minimum:256,maximum:4096}),
 reasoning:Type.Boolean(),compat:Type.Optional(OpenAICompletionsCompatSchema),
},{additionalProperties:false});
const schema=Type.Union([cloud,compatible]);
export type ModelConfig=Static<typeof schema>;
const profilesSchema=Type.Object({version:Type.Literal(1),active:Type.String({pattern:'^[a-z][a-z0-9_-]*$'}),
 profiles:Type.Record(Type.String({pattern:'^[a-z][a-z0-9_-]*$'}),Type.Union([Type.Omit(cloud,['version']),Type.Omit(compatible,['version'])]),{additionalProperties:false}),
},{additionalProperties:false});
export function parseModelConfig(text:string,profile?:string):ModelConfig {
 let value:unknown;
 try {
  if(text.length>100000)throw new Error();
  const document=parseDocument(text,{uniqueKeys:true});
  if(document.errors.length||document.warnings.length)throw new Error();
  value=document.toJS({maxAliasCount:10});
 }catch{throw new Error('Agent config must be valid YAML (duplicate keys and custom tags are not allowed)');}
 if(Value.Check(profilesSchema,value)) {
  const name=profile??value.active;
  if(!Object.hasOwn(value.profiles,name))throw new Error('Selected agent profile does not exist');
  // Validate inactive profiles too, so switching cannot reveal malformed settings.
  const configs=Object.fromEntries(Object.entries(value.profiles).map(([key,entry])=>[key,validateConfig({version:1,...entry})]));
  return configs[name];
 }
 if(profile!==undefined)throw new Error('PROFILE requires a YAML configuration with named profiles');
 return validateConfig(value); // Backwards-compatible single-model JSON/YAML.
}
function validateConfig(value:unknown):ModelConfig {
 if(!Value.Check(schema,value)) throw new Error('Invalid agent config; see docs/configuration.md for supported fields (credentials must use apiKeyEnv)');
 const config=value as ModelConfig;
 if(config.backend==='openai-compatible') {
  const url=new URL(config.baseUrl);
  if(!['http:','https:'].includes(url.protocol)||url.username||url.password||url.search||url.hash) throw new Error('Agent baseUrl must be an HTTP(S) endpoint without credentials, query or fragment');
  if(config.maxOutputTokens>=config.contextWindow)throw new Error('Agent output limit must be smaller than its context window');
  config.baseUrl=url.href.replace(/\/$/,'');
 }
 return config;
}
export function loadModelConfig(file=process.env.WM_AGENT_CONFIG):ModelConfig|undefined {
 if(!file)return undefined;
 let text:string;
 try {text=readFileSync(file,'utf8');}catch{throw new Error('Cannot read WM_AGENT_CONFIG; no fallback model will be used');}
 return parseModelConfig(text,process.env.PROFILE??process.env.WM_AGENT_PROFILE);
}
export function modelConfigDigest(config:unknown) {
 const canonical=(value:unknown):unknown=>{
  if(Array.isArray(value))return value.map(canonical);
  if(value&&typeof value==='object')return Object.fromEntries(Object.entries(value).sort(([a],[b])=>a.localeCompare(b)).map(([key,item])=>[key,canonical(item)]));
  return value;
 };
 return createHash('sha256').update(JSON.stringify(canonical(config))).digest('hex');
}
