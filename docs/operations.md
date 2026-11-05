# Operations

Run `./wm` from your workload root. It is the entry point for human and agent
operators; the root's `tofu` launcher is an internal backend for provider setup,
profile selection and progress.

```sh
./wm                         # compact status for every workload
./wm -v                      # versions, model pins and sync explanation
./wm --json                  # machine-readable fields
./wm plan                    # plan everything
./wm plan apps/web
./wm apply models/d3 -v
./wm stop apps/web
./wm start models/d3 -v
./wm restart apps/web -v
PROFILE=sol ./wm apply models/d3 -v
```

An optional `apps/NAME` or `models/NAME` scope works for status, plan and apply;
lifecycle commands require one. Scopes resolve through the registered module
inventory. Arguments can come before or after the scope, flag aliases such as
`-plan` work, and `--root` selects another workload root. `--color
auto|always|never`, `NO_COLOR` and `--timeout` are supported.

## Status and STATE SYNC

The default table shows `WORKLOAD`, `VERSION`, `STATUS` and `STATE SYNC`. Names
use scope labels such as `apps/web` or `models/d3`. STATE SYNC is the action
indicator:

| Display / JSON `stateSync` | Meaning | Action |
|---|---|---|
| ✅ synced / `synced` | Config, applied snapshot and observed readiness agree | None |
| ⛔️ PLAN → APPLY / `plan` | Config differs, remote drift exists or readiness is unmet, with no matching plan | `wm plan [scope]`, review, then `wm apply [scope]` |
| ⛔️ APPLY / `apply` | A saved plan matches current source, state and fresh remote observation | `wm apply [scope]` |
| ⛔️ UNKNOWN / `unknown` | A comparison or inspection failed, or inputs changed during it | Check connectivity and configuration; `wm plan` shows diagnostics. Never assume synced. |

Each status run compares three things:

1. **Filesystem → applied state:** an OpenTofu plan with refresh disabled
   evaluates the actual HCL, variables and bindings.
2. **Applied state → remote reality:** a refresh-only plan runs wm's identity,
   image/model and functional readiness checks against live workloads. It never
   writes state or applies.
3. **Plan currency:** saved-plan receipts check source and state fingerprints,
   plan bytes and fresh readiness and identity before suggesting APPLY.

Status plans run in private throwaway directories. Model readiness includes
bounded inference, so status takes longer than `docker ps` and uses model compute,
but it never calls the reconciliation agent or changes a workload. A refresh
failure makes sync unknown even if Docker reports the process running. New
resources show as not adopted. The CLI uses the root's local `terraform.tfstate`;
remote backends are not supported.

Green means aligned, red means action or inspection is needed, and yellow is an
intentionally stopped workload. Exit 0 means all selected resources are synced, 1
means action needed or unknown, and 2 means invalid invocation or inventory.

### Verbose and JSON output

`-v` adds `ENGINE`, `VERSION / IMAGE`, `HF MODEL` (model groups only), configured
versus applied desired state, separate health, and an explanation of
`filesystem↔state`, `state↔remote` and the next action. Verbose changes the table
only; JSON always has every field.

JSON adds `configuredDesired`, `configSync`, `remoteSync`, `stateSync`, `action`,
`engine`, `version`, `versionSource`, `imageId`, `hfModel` and `hfRevision`. The
older `desired` and `match` fields are the applied desired state and a
process-only match; use `stateSync` and `action` for decisions.

Versions come from, in order of preference:

- `application-command`: fixed `--version` queries for running ClickHouse,
  PostgreSQL, Redis and MinIO, avoiding misleading base-image labels.
- `python-package`: installed distribution metadata in running Python model
  runtimes. The CLI never infers a vLLM version from an engine name.
- `oci-label`, then `image-tag` (shown as `tag:`) from the same repository.
- `image-digest` when nothing better exists.

The HF model is the applied recipe's pin, shown as `hf://org/repo@<12-char commit>`,
not a fresh content check. Metadata lookups never exec into stopped containers,
never trigger inference or lifecycle actions, and fall back to image metadata on
failure.

## Plan and apply

`wm plan [scope]` saves a real OpenTofu plan under `.local/wm/plans/` with a
private receipt. Plans are scope-specific. Fingerprints cover HCL and variable
files, bindings, local state and relevant Terraform environment; editing any of
them, changing plan bytes or a change in remote readiness invalidates the plan.

`wm apply [scope]` checks the matching saved plan and re-observes remote state. If
the plan is missing or stale it makes a fresh one; it never applies another
scope's plan. A successful apply consumes the receipt. **Apply does not ask for
confirmation**, so review `wm plan` first. `-v` streams scoped action, probe and
wait progress.

Targeted commands include required dependencies and may omit unrelated changes;
follow up with `wm` or a full `wm plan`. Receipts are controller-local, not a
distributed lock.

## Lifecycle commands

`start` and `stop` set literal `desired = "running"` or `"stopped"` in the scoped
resource blocks, preserving surrounding HCL and comments, then make a fresh plan
and apply exactly that plan. They never call Docker directly. `restart` is a
durable stop → start sequence with a plan and apply per phase, ending at
`running` even if the workload started stopped. It does not change the automatic
recovery policy.

For multi-container apps, resource dependencies set the order: dependents stop
first and dependencies start first. Each wave edits only its own resources, and a
plan guard rejects changes outside the wave, adoption, replacement or deletion.
Resolve other pending changes with `wm plan` / `wm apply` if the guard rejects a
command.

The editor supports native `.tf` files with direct, quoted `running`/`stopped`
values. Variables, `count`/`for_each`, JSON HCL, cycles, dependencies outside the
group and indirect references are rejected before editing; manage those through
HCL and plan/apply. Nested category modules are not bulk lifecycle targets.

Edits keep file permissions, replace atomically and save private backups under
`.local/wm/intent-backups/`. A local lock excludes concurrent lifecycle commands,
and intervening source or graph changes abort the command. If a plan or apply
fails, the command stops and keeps completed waves and pending HCL intent as they
are: a failed restart may leave `desired = "stopped"` until a later `wm start`.
There is no automatic rollback. Use `wm status -v` to inspect the differences.
