import {hostname} from 'node:os';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import {sshArgs} from './ssh.js';
import {Type} from 'typebox';
import {Value} from 'typebox/value';
import type { SshTarget } from './ssh.js';
import type { Monitoring } from './metrics.js';
export interface Recipe {
 name: string; host: string; ssh?: SshTarget; local?:{hostname:string;dockerHost:string}; driver: string; engine: string; model?: string;
 revision?: string; remoteStateDirectory?: string; container: string; observedContainerId: string;
 imageDigestFromHostDocumentation: string; endpoint?: string; discoveryPath?: string; inferencePath?: string;
 requirements?:{api:string};
 probe?:{location:"host"|"container";format:"json"|"text"|"prometheus";field:string;equals:string};
 observedImageId?:string;
 diagnostics?:{logs:boolean;redactPatterns?:string[]};
 monitoring?: Monitoring;
 recovery?: {allowRestart:boolean;startupGraceSeconds:number};
}
const nonempty=Type.String({minLength:1,maxLength:4096});
const sshSchema=Type.Object({hostname:nonempty,user:nonempty,hostKeyAlias:nonempty,knownHostsFile:Type.Optional(nonempty)});
const modelRecipeSchema=Type.Object({
 name:nonempty,host:nonempty,ssh:sshSchema,driver:Type.Literal('docker'),engine:Type.Literal('vllm-sr'),
 model:Type.String({pattern:'^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$'}),revision:Type.String({pattern:'^[a-f0-9]{40}$'}),
 remoteStateDirectory:Type.String({pattern:'^/[^\\u0000]*$'}),container:Type.String({pattern:'^[A-Za-z0-9][A-Za-z0-9_.-]*$'}),
 observedContainerId:Type.String({pattern:'^[a-f0-9]{64}$'}),observedImageId:Type.Optional(Type.String({pattern:'^sha256:[a-f0-9]{64}$'})),
 imageDigestFromHostDocumentation:Type.String({pattern:'^[A-Za-z0-9._:/-]+@sha256:[a-f0-9]{64}$'}),
 endpoint:nonempty,discoveryPath:nonempty,inferencePath:nonempty,
 recovery:Type.Optional(Type.Object({allowRestart:Type.Boolean(),startupGraceSeconds:Type.Integer({minimum:60,maximum:1800})})),
 diagnostics:Type.Optional(Type.Object({logs:Type.Boolean(),redactPatterns:Type.Optional(Type.Array(Type.String({minLength:1,maxLength:256}),{maxItems:20}))})),
 monitoring:Type.Optional(Type.Object({ssh:sshSchema,jobs:Type.Array(Type.String({pattern:'^[-a-zA-Z0-9_]+$'}),{minItems:1,maxItems:10}),scrapeIntervalSeconds:Type.Number({minimum:1,maximum:120})})),
});
const applicationSchema=Type.Object({
 name:nonempty,host:nonempty,driver:Type.Literal('docker'),engine:Type.Literal('application'),mode:Type.Optional(Type.Literal('application')),
 local:Type.Object({hostname:nonempty,dockerHost:Type.String({pattern:'^unix:///[^\\u0000]+$'})}),
 container:Type.String({pattern:'^[A-Za-z0-9][A-Za-z0-9_.-]*$'}),
 observedContainerId:Type.String({pattern:'^[a-f0-9]{64}$'}),observedImageId:Type.String({pattern:'^sha256:[a-f0-9]{64}$'}),
 imageDigestFromHostDocumentation:modelRecipeSchema.properties.imageDigestFromHostDocumentation,
 requirements:Type.Object({api:Type.Union([Type.Literal('docker-health'),Type.Literal('http-health')])}),
 endpoint:Type.Optional(nonempty),inferencePath:Type.Optional(nonempty),
 probe:Type.Optional(Type.Object({location:Type.Union([Type.Literal('host'),Type.Literal('container')]),format:Type.Union([Type.Literal('json'),Type.Literal('text'),Type.Literal('prometheus')]),field:Type.String({pattern:'^[a-zA-Z_:][a-zA-Z0-9_:]*$',maxLength:128}),equals:Type.String({maxLength:128})},{additionalProperties:false})),
 recovery:modelRecipeSchema.properties.recovery,diagnostics:modelRecipeSchema.properties.diagnostics,
});
const recipeSchema=Type.Union([modelRecipeSchema,applicationSchema]);
export function loadRecipe(path:string):{recipe:Recipe;digest:string} {
 const bytes=readFileSync(path);const raw:unknown=JSON.parse(bytes.toString());
 if(!Value.Check(recipeSchema,raw)) throw new Error('Invalid recipe: captured identity, pinned sources and typed transport/policy fields required');
 const recipe=raw as Recipe;
 if(recipe.model?.split('/').some(part=>part==='.'||part==='..')) throw new Error('Invalid model path');
 if(recipe.ssh)sshArgs(recipe.ssh,['true']);if(recipe.monitoring) sshArgs(recipe.monitoring.ssh,['true']);
 if(recipe.engine==='application'&&(recipe.model!==undefined||recipe.revision!==undefined))throw new Error('Application must omit model metadata');
 if(recipe.engine==='application'&&recipe.local?.hostname!==hostname())throw new Error('Local controller hostname differs from captured binding');
 if(recipe.probe&&(recipe.engine!=='application'||recipe.requirements?.api!=='http-health'))throw new Error('Explicit probe requires http-health');
 if(recipe.engine==='application'&&recipe.requirements?.api==='docker-health')return {recipe,digest:createHash('sha256').update(bytes).digest('hex')};
 if(!recipe.endpoint||!recipe.inferencePath)throw new Error('HTTP probe endpoint and path required');
 const endpoint=new URL(recipe.endpoint);
 if(endpoint.hostname!=='127.0.0.1'||endpoint.protocol!=='http:'||endpoint.username||endpoint.password||endpoint.search||endpoint.hash||endpoint.pathname!=='/') throw new Error('Target probe must use a remote loopback origin');
 for(const path of [recipe.discoveryPath,recipe.inferencePath].filter((p):p is string=>p!==undefined)) {
  if(!/^\/[A-Za-z0-9/_-]+$/.test(path)||path.startsWith('//')) throw new Error('Probe path must be an absolute HTTP path');
 }
 return {recipe,digest:createHash('sha256').update(bytes).digest('hex')};
}
