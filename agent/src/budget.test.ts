import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createModels} from '@earendil-works/pi-ai/models';
import {fauxProvider,fauxAssistantMessage} from '@earendil-works/pi-ai/providers/faux';
import {limitModelCalls} from './budget.js';

test('model budget prevents another provider request and invokes terminal cancellation',async()=>{
 const models=createModels();const faux=fauxProvider();models.setProvider(faux.provider);
 faux.setResponses([fauxAssistantMessage('done')]);
 let calls=0,terminated=false;
 limitModelCalls(models,()=>{if(++calls>1)throw new Error('budget exhausted');},()=>{terminated=true;});
 await models.streamSimple(faux.getModel(),{messages:[]}).result();
 assert.throws(()=>models.streamSimple(faux.getModel(),{messages:[]}),/budget exhausted/);
 assert.equal(terminated,true);assert.equal(faux.state.callCount,1);
});
