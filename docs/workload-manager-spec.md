# Workload Manager (`wm`): homelab model recipes and switching

> Historical design: retained for context. The current direction is the [OpenTofu agent specification](workload-manager-opentofu-agent-spec.md); see [implementation status](development.md) for what exists today.

**Version:** 0.2 historical design draft. Public examples are illustrative; deployment audit records are local.

## 1. Purpose

Make it easy to run, switch, and experiment with AI models across existing Linux GPU hosts and Apple Silicon Macs. Preserve working serving recipes, stage weights before a switch, show what is actually serving, and make failures visible. Brief planned downtime is acceptable. High availability is not a goal.

The first implementation is a Go CLI plus one local manager on the Mac Studio. It uses SSH for Linux operations and the existing Docker installations. A separate, optional gateway provides stable model aliases. No per-node daemon, scheduler, distributed assignment protocol, generic job framework, or new monitoring stack is required initially.

This is a design, not installed software. Commands below describe the proposed CLI.

## 2. Illustrative target environment

The architecture targets heterogeneous existing machines: Arm Linux accelerator
hosts, an AMD64 Linux GPU host and Apple Silicon macOS. Inventory must capture
verified connection settings, OS/architecture, memory model, driver capabilities,
runtime identities and functional probes before adoption. Public examples do not
represent an inspected fleet or assert that a particular model fits on a host.

Capture startup scripts, patches, templates, immutable model revisions and image
digests. Required assets under temporary paths must be made durable before claiming
reproducible deployment. Distributed topology comes from the real launch contract,
not an image name. Preserve strict SSH host-key checking and explicit socket pins.

Prometheus is independently managed. Discover actual scrape labels, intervals and
metric semantics from its configuration; failed collection is not proof that a
workload should be started. Keep inventory and monitoring destinations in private
configuration, outside the source repository.

## 3. Scope and operating model

The unit of reuse is a **recipe**: model artifact plus engine build, hardware compatibility, launch files, flags, patches, template, ports, probes, and expected memory envelope. A **deployment** binds an immutable recipe revision to explicit hosts. A **route** binds a client alias to one verified deployment.

Start with these workflows:

1. Inventory running and stopped servers without taking ownership.
2. Capture a known recipe; clone it and change one or more parameters for an experiment.
3. Prepare weights, images, and supporting files while the current model still serves.
4. Plan and execute a switch with a visible outage estimate, bounded drain, readiness checks, and recovery.
5. Move a logical model to another host using a compatible target recipe/artifact.
6. Compare runs and promote a tested recipe to the preferred alias.

Keep explicit host placement and fixed ports. Default to one managed serving deployment per accelerator; the Spark pair is one resource group. Co-location requires an explicit memory budget and validation. Unmanaged workloads and monitoring containers are never stopped to make room.

Defer automatic placement, request-triggered loading, generic non-AI jobs, automatic weight eviction, multi-replica balancing, protocol translation, and HA. Artifact staging is in the first release because moving and experimenting with models is a core use case.

## 4. Minimal architecture

`wm` CLI → local `wm manager` → SSH/Docker on Linux; optional local service adapter on macOS.

The manager runs under launchd, owns one SQLite operation journal, and accepts local commands through a Unix socket. One writer serializes changes to overlapping host groups. Read-only inventory and preparation may run concurrently. A failed SSH observation means **Unknown**, never stopped or absent.

Use the Docker CLI/Compose already installed on each host. Compose operates separately on each host; it does not orchestrate a two-host deployment by itself. Versioned recipe scripts may implement engine-specific launch steps. Pass structured argv or materialized files rather than space-joining arbitrary arguments into shell strings.

A small remote invocation of the same binary may hold a host lock, validate an expected operation ID, and execute a bounded step over SSH; it is not a permanent agent. Keep durable remote operation receipts and runtime IDs. If a connection drops during a start/stop, inspect those receipts and actual Docker state before issuing another mutation. A stale lock is not proof that an operation ended.

Manager failure stops new management actions; existing engines continue. On restart, reconcile the journal with runtime identity and probes before resuming an operation. Do not replay an old desired-state snapshot destructively. A sleeping/offline Mac prevents management and any gateway hosted there; direct engine URLs remain available. This is an accepted homelab tradeoff.

## 5. Recipe contract

Each recipe bundle contains:

| Field | Requirement |
|---|---|
| Identity | Stable recipe ID, immutable content hash, description, parent experiment |
| Compatibility | OS/architecture, accelerator, engine build and required runtime/driver checks |
| Artifacts | Repository and exact revision or local manifest; format; per-host source path and container path; expected files/sizes/checksums |
| Runtime | Image digest, command argv, mounts, necessary environment, network/IPC settings, explicit host ports |
| Support files | Startup scripts, patches, templates and their hashes; no required `/tmp` files |
| Topology | Single host or explicit rank map, rendezvous address/port, network interfaces and startup strategy |
| Capacity | Estimated weights/runtime/KV memory, context length, concurrency, reserve and disk staging requirement |
| Validation | Model discovery and inference probes, timeout, required API surfaces and optional tool/vision checks |
| Recovery | Stop order, restart owner, retry budget, startup timeout and prior known-good revision |

Secrets remain in local protected files and are referenced by name; capture and logs redact credentials. Resolve mutable image tags during preparation and retain the actual image digest or local image ID where a registry digest is unavailable. A local-only image must be exportable or reproducibly buildable before calling its recipe portable.

An import is initially observational. Capture actual mounts, script contents, allowlisted environment, images, model snapshot, ports and runtime identity. Mark missing pieces explicitly. Import never restarts, relabels, or deletes an existing container. Taking management ownership is a distinct operator action after reviewing the captured bundle and a recreation plan.

Recipe hashes include executable files, patches, templates, image identity, relevant configuration and model revision; exclude timestamps and unrelated catalog edits. Editing an inactive recipe cannot restart a serving deployment.

## 6. Artifacts and moving models

Separate logical model identity from weight format and serving recipe. EXL3, NVFP4, GGUF and MLX variants are not interchangeable. Moving to the Mac means selecting or preparing an MLX-compatible artifact and tested oMLX recipe, not copying a CUDA container. Reject a move when no compatible target exists; show what is missing.

`wm prepare` supports an explicit download source or resumable SSH/rsync copy, with progress. Check destination free space including partial downloads and any retained rollback copy. Stage to a temporary directory, verify a manifest, then atomically publish the completed artifact. Preserve Hugging Face snapshot/blob links or copy a complete standalone snapshot; a copied symlink-only snapshot is insufficient. Never overwrite weights in use or delete cache data automatically.

For cross-host moves: prepare target → start and validate it → switch the alias atomically → drain source → stop source if requested. If resources overlap, use the stop/start switch below. Preserve the original endpoint until target validation succeeds whenever capacity permits. Changing quantization or template may change behavior; record that in the plan and run the recipe's compatibility tests before promotion.

## 7. Switch transaction and recovery

Every change creates an operation ID, immutable source/target recipe hashes, affected-host set, and journaled stages:

`Planned → Preparing → Prepared → Draining → Stopping → Starting → Validating → Ready`

Failures become `Failed`, `Unknown`, or `RecoveryRequired`, with the failed stage, observations, logs and suggested next action. Desired state, observed state, and route state are separate fields.

1. Lock all affected hosts as a group; reject conflicting operations.
2. Prepare every target host before touching the source. Verify files, architecture, image identity, ports, topology and capacity. A missing artifact fails here.
3. Withdraw the old route from new requests; drain existing requests to a configurable deadline. Show when the plan will interrupt remaining streams. Direct clients cannot be drained by wm and must be called out.
4. Stop only recorded owned runtime IDs; verify all ranks stopped and memory/ports released. A timeout is a failure, not permission to start a competing workload.
5. Start the target using its explicit topology. Launch distributed ranks in the recipe's rendezvous order or concurrently when required. Do not wait for a worker to join a leader that has not started.
6. Require model identity plus a bounded real inference probe; run additional streaming/tool checks declared by the recipe. Publish the route only after success.
7. On failure after stopping the source, clean up only the failed target and attempt the preselected known-good source recipe once if all affected hosts are reachable and safely stopped. Otherwise leave `RecoveryRequired`. Rollback is another measured startup and can fail; never promise zero downtime.

If a distributed rank fails, withdraw the deployment route and record the group failure. Recovery restarts the complete group after all ranks are reachable and known stopped. No partial-rank replacement or restart while a peer's state is unknown.

Existing Spark containers have restart policy `no`; retain this during observation. For managed recipes choose exactly one restart owner. Single-host Docker restart policies may handle process exits; the manager detects repeated failures and surfaces them. Distributed recipes use manager-coordinated group recovery, with a bounded retry budget (initial default: one automatic group retry, then operator action). Never stack Docker restart loops and independent manager restart loops.

## 8. Health, capacity and oMLX

Use three distinct observations: runtime alive, API reachable, model inference verified. Store timestamps and error details. `/v1/models` alone is insufficient. Probe on startup/promotion, then run a low-frequency bounded canary for deployments intended to remain available; report timeouts separately from crashes. Busy queues can delay probes, so a single missed canary does not trigger destructive recovery.

For the 5090, budget dedicated VRAM separately from system RAM. For Sparks and the Studio, budget unified RAM once, including OS and other applications. Compare declared envelope with observed available resources before starting; warn about swap/memory pressure. A memory estimate is not proof that a context/concurrency setting fits. Record measured peaks after each successful run. Do not reject an already-running deployment by adding its full footprint to observed usage again.

The first oMLX milestone is a manual local installation and one tested model. Record installed version, supported model format, startup mechanism, endpoint, discovery behavior, streaming, and memory pressure. Do not invent load/unload/admin endpoints or assume Anthropic compatibility. Begin with observe-only integration and let oMLX own its model loading/eviction. Distinguish available-on-demand from loaded-and-probed; do not repeatedly reload an intentionally evicted model. Add lifecycle control only after verifying the installed version's contract.

## 9. Optional stable gateway

A separate process routes stable aliases to one selected deployment each. Start with verified OpenAI chat-completions and streaming; enable other API paths only when the recipe has demonstrated them. No claim of universal OpenAI/Anthropic compatibility or automatic format translation.

Persist routes atomically. Continue using the last valid routes during manager failure, with independent endpoint health checks and visible cache age. Cached routes do not establish readiness. A planned drain must be acknowledged by the gateway before stopping the source. If the gateway cannot acknowledge, fail the switch before disruption unless the operator explicitly selects a direct-endpoint workflow.

Unknown alias returns 404; known alias with no usable endpoint returns 503 and a clear reason. Propagate cancellations and stream incrementally. Do not automatically replay generation requests after an upstream failure: the backend may already have started generation even if no response byte arrived. Keep inference credentials separate from management access. Bind management locally; expose gateway/engine endpoints only on explicitly chosen trusted interfaces.

## 10. Monitoring and notification

Extend the existing ops stack. Keep its dashboards, exporter metrics and label conventions. Add wm scrape configuration and a compact dashboard showing desired versus observed deployment, last successful probe, last error, switch duration, operation stage and route target.

Minimum new metrics: `wm_deployment_desired`, `wm_deployment_ready`, `wm_last_observation_timestamp_seconds`, `wm_last_successful_probe_timestamp_seconds`, `wm_operation_duration_seconds`, `wm_operation_failures_total`, and `wm_manager_last_reconcile_timestamp_seconds`. Use bounded deployment/node labels, not operation IDs or exception messages. Put detailed operation IDs and redacted log tails in the journal.

Add alerts for intended-active models failing probes, stale observations, failed/stuck operations, and missing manager metrics. Combine expected inventory with `absent()`/staleness checks so disappearing series do not look healthy. Include existing `gpu_exporter_collect_success`, `nvidia_smi_last_collect_success`, and node collector health; scrape success alone does not prove collection success.

Planned inactive deployments do not raise model-down alerts. A maintenance operation suppresses only affected availability alerts, expires at its deadline, and emits a failure if unfinished. The currently stopped SGLang server is an inventory discrepancy until operator intent is known, not an automatic restart instruction.

Choose and test one notification destination during implementation. An in-terminal error or red Grafana panel does not meet the unattended notification requirement. Use the existing Prometheus/Grafana alerting path rather than a bespoke alert daemon. Because monitoring and management share this Mac, Mac power/sleep failure cannot reliably notify from the same host; an external heartbeat is optional, not an HA requirement.

## 11. Proposed CLI

```text
wm inventory
wm status
wm inspect <deployment>
wm recipe capture <host> <container> --output <directory>
wm recipe clone <recipe> --as <experiment>
wm prepare <recipe> --on <host-or-group>
wm plan switch <deployment> --to <recipe>
wm switch <deployment> --to <recipe> --wait
wm plan move <deployment> --to <host> --recipe <compatible-recipe>
wm move <deployment> --to <host> --recipe <compatible-recipe> --wait
wm stop <deployment>
wm recover <operation>
wm logs <deployment>
wm runs <recipe>
```

Plans list exact hosts, old/new identities, staging requirements, route changes, drain deadline, expected downtime and recovery recipe. `--wait` returns nonzero on failure or unknown outcome and prints the operation ID. Capture run metadata: artifact/image/recipe hashes, topology, context/concurrency, startup and downtime, probe results, measured memory, and optional benchmark results. Do not rank experiments by throughput alone; preserve quality and tool-use test results.

## 12. Delivery sequence and acceptance

1. **Read-only inventory and capture.** Reproduce this fleet inventory, including stopped 5090 experiments and the unavailable SGLang endpoint. Capture the Spark recipe and identify every temporary dependency without changing running containers. Preserve unrelated exporters and services.
2. **One-host lifecycle and artifact preparation.** On the idle 5090, select a captured, complete recipe; prepare and validate it. Demonstrate a switch between two explicit recipes, missing-artifact failure before source stop, bounded timeout, and one recovery attempt. Do not silently revive an old container merely because it exists.
3. **Spark group support.** Bundle durable startup/patch assets, capture actual topology parameters, and schedule a deliberate recreation test. Verify both ranks start without circular readiness gating, leader inference works, and rank failure produces a visible group failure. This is future implementation validation, not authorization to interrupt today's model during spec editing.
4. **Stable alias and movement.** Prepare a compatible model on a second host, validate, switch the alias, drain source, and record downtime. Test unavailable target and manager/gateway restart during a pending operation. No duplicated start after an ambiguous SSH result.
5. **Mac/oMLX trial.** Verify one model and its installed API behavior, then demonstrate a compatible logical-model move to/from the Studio. Keep unsupported combinations explicit.
6. **Failure visibility.** Deliver and recover a test notification for a failed canary and a missing manager scrape. Verify intentional stops stay quiet, maintenance suppression expires, and idle charts do not fabricate zeros.

Across these milestones: restarting wm must not restart healthy engines; editing an inactive recipe must not change serving; no operation may kill an unmanaged process or delete model caches. Retain prior recipe bundles and weights until explicit cleanup. Unit tests should target transaction recovery, identity/ownership checks, and route/drain transitions; integration tests should exercise actual Docker and SSH failure boundaries.

## 13. Decisions still requiring implementation evidence

- Full current Spark environment/topology and image identity, plus successful inference and recreation from the captured bundle.
- Which stopped 5090 recipe is the preferred baseline and whether its artifacts remain complete.
- oMLX installed version and supported API/lifecycle semantics.
- Notification destination and tested delivery.
- Measured memory envelopes, load times and drain/startup deadlines per recipe.

These are explicit gates, not guessed defaults or claims that the current environment has already passed them. The original distributed control-plane/nodelet framework, Ray example, imaginary always-on Mac mini, and active oMLX deployment have been removed to keep the design grounded in this fleet.
