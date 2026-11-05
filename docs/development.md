# Development and implementation boundaries

## Code layout

| Path | Responsibility |
|---|---|
| `cmd/wm` | Status, saved plans, scope resolution and lifecycle commands |
| `cmd/terraform-provider-wm`, `internal/provider` | Schema, intent validation, applied snapshots and lifecycle |
| `agent/src/runner.ts` | Durable agent admission and independent final verification |
| `agent/src/runtime.ts`, `protocol.ts`, `broker.ts` | Typed tools, policy and convergence |
| `agent/src/driver.ts`, `engine.ts`, `ssh.ts` | Docker CLI, model evidence and bounded SSH |
| `agent/src/operation.ts`, `lock.ts` | Checkpoints, budgets and local exclusion |
| `agent/remote/lifecycle.py` | Mutation lock, durable receipts and exact identity |
| `agent/src/auth.ts`, `model-config.ts` | Authentication and YAML model profiles |
| `agent/src/metrics.ts` | Read-only Prometheus evidence and freshness |
| `scripts/tofu_verbose.py` | Scope mapping, profile discovery and progress transport |
| `agent/testdata` | Synthetic test fixtures; no live host dependencies |

See [build and checks](../README.md#build-and-verify). Tests use fake transports,
temporary directories and simulated services. They cover failed apply/state retention,
saved-plan checks, identity drift, lost replies, durable replay, cancellation and probes.
They do not establish that all failure scenarios have been exercised on real hosts.

## Authentication

See [agent configuration](agent-model.md). The Codex bridge reads access tokens from
`$CODEX_HOME/auth.json` (default `~/.codex/auth.json`) on demand. Codex owns refresh;
wm requires 35 minutes of remaining validity and does not support keychain-only auth.
Never copy credentials into source, HCL, bindings, journals or workload hosts.
Explicit Pi session cleanup closes cached connections when a runner completes.

Without a model config, `WM_MODEL_AUTH=codex|api|auto` selects the legacy backend.
`api` requires `OPENAI_API_KEY`; `auto` prefers that key, then Codex sign-in.
Compatible endpoint profiles only receive their explicitly configured credentials.

## Recovery and durability

The loop observes actual state, chooses scoped steps, diagnoses failures and independently
verifies readiness. It runs within apply, not as a background daemon. Existing Docker
supervision and monitoring remain independent. Destroy stops an adopted container and
retains it and its data; reconstruction and image/model replacement remain unimplemented.

Admission checkpoints preserve request IDs, a 30-minute deadline, an 80-command budget
and a 40-model-request budget across retries. Output is capped at 4096 tokens per model
request; this is not a total input-token or monetary budget. Budget exhaustion terminates
outside the tool loop. SSH/waits accept cancellation; interrupted remote outcomes can
still be unknown. Observe exact identity and receipts before retrying.

A kernel lock under `~/.local/state/wm/locks`, keyed by host alias, driver and container
ID, excludes conflicting operations for one controller OS user. Remote helpers serialize
mutations and store versioned receipts. This is not cross-controller fencing.

Applied recipes, operation IDs and journal paths depend on stable checkout locations.
Keep `.local/recipes/` and `.local/operations/` in place during repository cleanup.
Controller binaries live in `.local/bin/`. Plans/backups remain under the workload root.
Private deployment notes belong in ignored `.local/docs/`, separate from public docs.

## Deterministic safeguards

Ownership checks remain available even when model/image drift makes the workload
unready; authorized stop and diagnostics must not be blocked by unrelated readiness
failure. Start/restart require the declared model/image checks. Compare desired manifest
references against RepoDigests, not Docker configuration image IDs.

Tool results are runtime-validated, bounded and sanitized. Exit status alone cannot
establish success. Model/tool output is untrusted evidence, never new authority. HTTP
probes disable redirects and proxies and bound response size/time. Application health
is distinct from full business functionality. Metrics retain freshness and coverage;
missing telemetry is not zero and never grants rights to mutate an external service.

Prometheus is an external prerequisite, not provisioned by wm. Exporter ownership
is scoped independently from the observed application and shared monitoring networks.
The [Go executor](go-docker-executor-proposal.md) remains a proposal; Docker CLI and
Python remain the production execution path.

## Progress and launcher behavior

The example launcher sets development provider overrides and `TF_VAR_wm_root`, then
calls `scripts/tofu_verbose.py`. It discovers `agent.yaml` (legacy `agent.json`
fallback), resolves `--scope` from registered module paths and forwards signals.
The operator uses a workload-root `tofu` launcher rather than a globally installed binary.

Verbose apply uses a private `WM_PROGRESS_FILE`. Allowlisted action/probe/wait summaries
stream to stderr; raw model reasoning, generated code and logs are not forwarded.
The underlying OpenTofu output, exit status and interrupts remain intact. With
`WM_HIDE_DEV_WARNING=1`, only the recognized development-override banner is filtered;
other diagnostics remain. JSON is not filtered. Progress has no authority over readiness,
receipts or cancellation.
