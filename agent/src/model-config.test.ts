import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {parseModelConfig,loadModelConfig,modelConfigDigest} from './model-config.js';
import {hasApi} from '@earendil-works/pi-ai/models';
import {workloadModels} from './auth.js';

const example=parseModelConfig(readFileSync('examples/agent/agent.yaml','utf8'));
test('external agent config validates and does not silently fall back',()=>{
 const config=parseModelConfig(JSON.stringify(example));assert.equal(config.backend,'openai-compatible');
 for(const change of [{backend:'unknown'},{model:''},{apiKey:'synthetic-secret'},
  {baseUrl:'http://user:password@localhost:8888/v1'},{baseUrl:'http://localhost/v1?token=secret'},
  {contextWindow:1024},{maxOutputTokens:100000},{apiKeyEnv:'not a variable'}]) {
  assert.throws(()=>parseModelConfig(JSON.stringify({...example,...change})));
 }
 assert.throws(()=>loadModelConfig('/nonexistent-wm-agent-config'),/no fallback/);
 assert.equal(modelConfigDigest({a:1,b:2}),modelConfigDigest({b:2,a:1}));
 const dir=mkdtempSync(join(tmpdir(),'wm-config-'));try {
  const file=join(dir,'agent.json');writeFileSync(file,JSON.stringify(example));
  assert.deepEqual(loadModelConfig(file),config);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
test('compatible endpoint uses explicit local auth, never ambient cloud credentials',async()=>{
 const previous=process.env.OPENAI_API_KEY;process.env.OPENAI_API_KEY='synthetic-cloud-secret';
 try {
  const {models,model}=await workloadModels(parseModelConfig(JSON.stringify(example)));
  const auth=await models.getAuth(model.provider);
  assert.equal(auth?.auth.apiKey,'wm-local-no-auth');
  const selected=models.getModel(model.provider,model.modelId)!;
  assert.ok('baseUrl' in example);
  assert.equal(selected.api,'openai-completions');assert.equal(selected.baseUrl,example.baseUrl);
  assert.ok(hasApi(selected,'openai-completions'));
  assert.equal(selected.compat?.supportsOpenAIGrammarTools,false);
  const missing=parseModelConfig(JSON.stringify({...example,apiKeyEnv:'WM_TEST_ABSENT_AGENT_KEY'}));
  delete process.env.WM_TEST_ABSENT_AGENT_KEY;
  await assert.rejects(workloadModels(missing),/unset or empty/);
  process.env.WM_TEST_AGENT_KEY='synthetic-local-key';
  const keyed=await workloadModels(parseModelConfig(JSON.stringify({...example,apiKeyEnv:'WM_TEST_AGENT_KEY'})));
  assert.equal((await keyed.models.getAuth(keyed.model.provider))?.auth.apiKey,'synthetic-local-key');
 }finally{
  if(previous===undefined)delete process.env.OPENAI_API_KEY;else process.env.OPENAI_API_KEY=previous;
  delete process.env.WM_TEST_AGENT_KEY;
 }
});

test('YAML profiles select GLM or Sol and preserve the selected model digest',()=>{
 const yaml=readFileSync('examples/agent/agent.yaml','utf8');
 const glm=parseModelConfig(yaml);const sol=parseModelConfig(yaml,'sol');
 assert.equal(glm.backend,'openai-compatible');assert.equal(sol.backend,'codex');
 assert.equal(sol.model,'gpt-6.1-sol');
 assert.equal(modelConfigDigest(glm),modelConfigDigest(parseModelConfig(JSON.stringify(glm))));
 assert.equal(modelConfigDigest(glm),modelConfigDigest(parseModelConfig(yaml.replace('gpt-6.1-sol','another-model'))));
 assert.throws(()=>parseModelConfig(yaml,'missing'),/does not exist/);
 assert.throws(()=>parseModelConfig(yaml.replace('active: glm','active: missing')),/does not exist/);
 assert.throws(()=>parseModelConfig(yaml.replace('active: glm','active: glm\nactive: sol')),/valid YAML/);
 assert.throws(()=>parseModelConfig(yaml.replace('active: glm','active: !custom glm')),/valid YAML/);
 assert.throws(()=>parseModelConfig(JSON.stringify(glm),'sol'),/named profiles/);
 assert.throws(()=>parseModelConfig(yaml.replace('backend: codex','backend: misspelled')));
});

test('PROFILE selects the model and takes precedence over the legacy alias',()=>{
 const previous=process.env.PROFILE, legacy=process.env.WM_AGENT_PROFILE;
 const file='examples/agent/agent.yaml';
 try {
  process.env.WM_AGENT_PROFILE='glm';process.env.PROFILE='sol';
  assert.equal(loadModelConfig(file)?.model,'gpt-6.1-sol');
  process.env.PROFILE='glm';assert.equal(loadModelConfig(file)?.model,'GLM-5.3-Flash-EXL3');
  process.env.PROFILE='missing';assert.throws(()=>loadModelConfig(file),/does not exist/);
  delete process.env.PROFILE;assert.equal(loadModelConfig(file)?.model,'GLM-5.3-Flash-EXL3');
 }finally{
  if(previous===undefined)delete process.env.PROFILE;else process.env.PROFILE=previous;
  if(legacy===undefined)delete process.env.WM_AGENT_PROFILE;else process.env.WM_AGENT_PROFILE=legacy;
 }
});

test('cloud thinking level is validated, passed to Pi settings and pinned',async()=>{
 const previous=process.env.OPENAI_API_KEY;process.env.OPENAI_API_KEY='synthetic-test-key';
 try {
  const config=parseModelConfig('version: 1\nbackend: openai\nmodel: gpt-6.1-sol\nthinkingLevel: medium');
  const selected=await workloadModels(config);
  assert.equal(selected.thinkingLevel,'medium');
  const low=await workloadModels(parseModelConfig('version: 1\nbackend: openai\nmodel: gpt-6.1-sol\nthinkingLevel: low'));
  assert.notEqual(selected.configDigest,low.configDigest);
  assert.throws(()=>parseModelConfig('version: 1\nbackend: codex\nmodel: gpt-6.1-sol\nthinkingLevel: turbo'));
 }finally{if(previous===undefined)delete process.env.OPENAI_API_KEY;else process.env.OPENAI_API_KEY=previous;}
});
