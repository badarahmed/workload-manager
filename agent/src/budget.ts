import type {Models} from '@earendil-works/pi-ai';

/** Admission happens outside the tool loop, including retries and compaction. */
export function limitModelCalls(models:Models,consume:()=>void,terminate:()=>void) {
 const stream=models.streamSimple.bind(models);
 models.streamSimple=(model,context,options)=>{
  try {consume();} catch(error) {terminate();throw error;}
  return stream(model,context,{...options,maxTokens:Math.min(options?.maxTokens??4096,model.maxTokens,4096)});
 };
}
