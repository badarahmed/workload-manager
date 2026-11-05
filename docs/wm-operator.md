# wm operator CLI

Run `./wm` from your workload configuration root. It is the primary entry point for both human
and agent operators. The old justfile is removed; the `tofu` launcher remains an
internal backend for provider setup, profile selection and progress transport.

```sh
./wm                             # compact three-way status
./wm -v                          # versions, model pins and sync explanation
./wm --json                      # machine-readable sync/action fields
./wm plan                        # plan everything
./wm plan apps/langfuse
./wm apply apps/langfuse -v
./wm -plan models/d3              # flag aliases are supported too
./wm -apply models/d3 -v
PROFILE=sol ./wm apply models/d3 -v
```

The same optional `apps/NAME` or `models/NAME` scope works for status, plan and
apply. Scopes are resolved through the registered local module inventory; plan/apply
register modules automatically. `-v`/`--verbose` replaces `apply-v`. Arguments can
appear before or after the scope. `--root` selects another local workloads root.

## STATE SYNC is the action indicator

| Display / JSON `stateSync` | Meaning | Operator action |
|---|---|---|
| ✅ synced / `synced` | Config, applied snapshot and observed readiness agree | None |
| ⛔️ PLAN → APPLY / `plan` | Config differs, remote drift exists, or readiness is unmet; no current matching plan | `wm plan [scope]`, review, then `wm apply [scope]` |
| ⛔️ APPLY / `apply` | A saved plan matches current source/state and the freshly observed remote snapshot | `wm apply [scope]` |
| ⛔️ UNKNOWN / `unknown` | A required comparison/inspection failed or inputs changed during inspection | Inspect connectivity/configuration; `wm plan` exposes diagnostics. Never assume synced. |

Three distinct sources are compared on each status invocation:

1. **Filesystem → applied state:** OpenTofu plan with refresh disabled evaluates
   actual HCL, variables, bindings and resource changes. No regex HCL parsing.
2. **Applied state → remote reality:** OpenTofu refresh-only plan invokes wm's
   exact-identity, image/model checks and configured functional readiness probes.
   This reads live local Docker/SSH workloads; it does not write state or apply.
3. **Plan currency:** saved-plan receipts check source/state fingerprints, plan
   bytes and fresh readiness/identity metadata before suggesting APPLY.

The remote leg means actual managed resources, not another Terraform state file
on the target. The current backend is the local `terraform.tfstate` in workloads;
remote Terraform backends are not implemented by this CLI. Status plans use private
throwaway directories and are never applied. Model readiness includes bounded
inference, so status can take longer than a raw Docker listing and uses model
compute. It never invokes the reconciliation LLM or performs lifecycle changes.

A refresh failure conservatively makes sync unknown, even if Docker still reports
a process running. Process status remains visible separately. New configured
resources are listed as not adopted, while deleted/moved resources are detected
through OpenTofu plan changes. Output-only configuration changes also require a
plan/apply. If inputs change during comparison, status does not claim a coherent sync.

Verbose output shows **CONFIGURED** versus **APPLIED** desired state and explains
`filesystem↔state`, `state↔remote`, and the next action per resource. JSON adds
`configuredDesired`, `configSync`, `remoteSync`, `stateSync`, and `action`. The older
`desired` and `match` fields remain the applied desired state and process-only match;
operators should use `stateSync`/`action` for decisions. JSON remains uncolored.

## Plan and apply behavior

`wm plan [scope]` saves an actual OpenTofu plan under `.local/wm/plans/`, with a
private receipt. Scope-specific files prevent accidentally applying another scope's
plan. Fingerprints cover HCL/variable files, bindings, local state and relevant
Terraform variable/workspace/CLI environment. Editing source/state, changing plan
bytes, or changed remote readiness invalidates a prior plan.

`wm apply [scope]` checks the matching saved plan and reobserves remote state. If
missing/stale it generates a fresh plan first; it never silently applies an old
plan for another scope. Successful apply consumes the receipt. Apply is automatic
without confirmation; review `wm plan` first when needed.
`-v` enables scoped action/probe/wait progress. Exit status and interrupts propagate
through the OpenTofu runner. Existing provider saved-intent checks, exact identity,
receipts and data-retention rules remain in effect.

Targeted commands include required dependencies and may omit unrelated changes;
follow them with `wm` or a full `wm plan` to see the entire root. State-sync receipts
are controller-local artifacts, not a distributed lock or fleet fencing protocol.

## Display and versions

Default: workload scope/name, version, combined process/health status, STATE SYNC.
Verbose adds engine, HF model pin for model groups, configured/applied desired
state and separate health. `--color auto|always|never`, `NO_COLOR`, `--timeout` and
`--json` remain supported. Metadata versions come from fixed application version
commands, Python distribution metadata, OCI labels or matching image tags, with
an explicit digest fallback. HF identity is the applied repository/revision pin;
JSON retains its full commit. Version inspection never invokes inference itself.

Green means aligned; red means action/inspection needed; yellow is an intentionally
stopped workload. Exit 0 means all selected resources are synced, 1 means action or
unknown comparison, and 2 means invalid invocation/state inventory.

## Verification

Isolated Go tests cover configuration edits, remote drift/unready results, unknown
comparisons, refresh-only prior-state/drift JSON, plan fingerprints, scope mapping,
interrupts and lifecycle guards. Live deployment records are private.

## Lifecycle commands

```sh
./wm status models/d3
./wm status models/d3 -v
./wm stop models/d3
./wm start models/d3 -v
./wm restart models/d3 -v
./wm stop apps/langfuse -v
```

Lifecycle commands require an explicit `apps/NAME` or `models/NAME` scope. Start
sets literal `desired = "running"`; stop sets `desired = "stopped"` in the scoped
wm_workload resource blocks, preserving surrounding HCL and comments. The command
then generates a fresh plan and applies that exact guarded plan. It does not reuse
an unrelated earlier plan or bypass OpenTofu to call Docker. Apply remains automatic
without confirmation. `-v` enables the normal plan/apply agent progress stream.

Restart is a durable **stop → start** sequence with a plan/apply per phase. Its final
intent is running, including when the workload was initially stopped. This explicit
operator action does not enable or override automatic recovery restart policy.

For a multi-container app, direct resource dependencies determine waves: dependents
stop first; dependencies start first. For Langfuse this means exporter → web → worker
→ data services on stop, and the reverse on start. Each wave edits only its own
resources before planning the module. A plan guard rejects resource changes outside
that wave, adoption, replacement or deletion. Resolve other pending module changes
with `wm plan`/`wm apply` before retrying a lifecycle command if the guard rejects it.

This editor supports native `.tf` files with direct, quoted running/stopped values.
Variable/computed desired values, count/for_each, JSON HCL, cycles, dependencies
outside the selected group, and indirect local/module references in grouped resources
are rejected before editing. Such configurations remain usable through manual HCL
intent and normal plan/apply. Nested category modules are not bulk lifecycle targets.

Edits preserve file permissions, use atomic replacement and save private source
backups under `.local/wm/intent-backups/`. Concurrent lifecycle commands are excluded
by a local lock, and intervening source/graph changes abort the command. If a plan or
apply fails, the command stops immediately. Already completed waves and pending HCL
intent are retained honestly: restart may leave desired stopped until a later
`wm start` or retry succeeds. No automatic rollback restarts a workload after failure.
Use `wm status -v` to inspect configured/applied/remote differences.

Verification used an isolated fake OpenTofu backend: stop/start/restart plan/apply
ordering, verbose propagation, failure exit codes, retained pending intent, source
concurrency checks, parser rejection and plan-boundary guards were exercised. No
live container was stopped or restarted merely to test these CLI additions.
