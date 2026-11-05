import {Type, type Static} from 'typebox';
import {Value} from 'typebox/value';

export const Actions=Type.Union([
 Type.Literal('observe'),Type.Literal('start'),Type.Literal('stop'),Type.Literal('logs'),
 Type.Literal('probe'),Type.Literal('wait'),Type.Literal('metrics'),Type.Literal('restart'),
]);
export type Action=Static<typeof Actions>;
export const RuntimeSchema=Type.Object({id:Type.String({pattern:'^[a-f0-9]{64}$'}),status:Type.String(),
 running:Type.Boolean(),exitCode:Type.Integer(),oom:Type.Boolean(),startedAt:Type.String(),
 health:Type.Union([Type.String(),Type.Null()]),image:Type.String({pattern:'^sha256:[a-f0-9]{64}$'})});
export const ProbeSchema=Type.Object({ready:Type.Boolean(),error:Type.Optional(Type.String())});
const Observation=Type.Object({runtime:RuntimeSchema,probe:Type.Union([ProbeSchema,Type.Null()]),converged:Type.Boolean()});
const Receipt=Type.Object({version:Type.Literal(1),phase:Type.Literal('verified'),container_id:Type.String({pattern:'^[a-f0-9]{64}$'})});
const Mutation=Type.Union([
 Type.Intersect([RuntimeSchema,Type.Object({changed:Type.Boolean()})]),
 Type.Object({changed:Type.Literal(false),reason:Type.String()}),
 Type.Object({receipt:Receipt,runtime:RuntimeSchema,requiresProbe:Type.Literal(true)}),
]);
const Metrics=Type.Union([
 Type.Object({status:Type.Literal('unsupported'),reason:Type.String()}),
 Type.Object({status:Type.Literal('error'),error:Type.String()}),
 Type.Object({status:Type.Union([Type.Literal('observed'),Type.Literal('missing')]),evaluatedAt:Type.Number(),
  observations:Type.Array(Type.Object({labels:Type.Record(Type.String(),Type.String()),
   status:Type.Union(['missing','error','stale','fresh'].map(s=>Type.Literal(s))),
   value:Type.Union([Type.Number(),Type.Null()]),ageSeconds:Type.Union([Type.Number(),Type.Null()]),
   sourceTimestamp:Type.Union([Type.Number(),Type.Null()]),samples:Type.Array(Type.Tuple([Type.Number(),Type.String()]))})),
  missingJobs:Type.Array(Type.String()),readinessEvidence:Type.Literal(false)}),
]);
export const Results={observe:Observation,wait:Observation,start:Mutation,stop:Mutation,restart:Mutation,
 probe:ProbeSchema,logs:Type.Object({text:Type.String(),untrusted:Type.Literal(true),exitCode:Type.Union([Type.Integer(),Type.Null()])}),metrics:Metrics};
export const ResultSchema=Type.Union(Object.values(Results));
export function validateResult(action:Action,result:unknown) {
 if(!Value.Check(Results[action],result)) throw new Error(`Invalid ${action} result from executor`);
 return result;
}
