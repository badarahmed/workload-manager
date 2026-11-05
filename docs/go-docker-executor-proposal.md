# Proposal: Go executor with Docker driver

Status: **proposed, not implemented**. The current agent retains Docker CLI calls
and the Python remote lifecycle helper. This proposal records the next extraction;
it does not authorize rollout, workload replacement or a new state owner.

## Purpose

Keep Pi Durable, codemode and Sol on the controller for reasoning. Move deterministic
remote operations into one versioned Go executable. The executable owns runtime
identity validation, bounded driver operations, remote locking and durable receipts.
The model chooses from scoped operations; it never supplies arbitrary shell commands.

The Go provider continues to own OpenTofu intent, saved-plan validation and applied
snapshots. Engine-specific model and readiness checks remain distinct from driver
lifecycle and ownership. External Prometheus supplies supporting observations and
is never provisioned by wm. Generic applications need a driver and optional probes,
not an AI engine profile.

## Target matrix

| Host | Binary | Docker endpoint |
|---|---|---|
| Apple Silicon Mac / Mac mini | darwin/arm64 | Configured local socket for the chosen Docker VM/runtime |
| DGX Spark | linux/arm64 | Local Docker Engine socket |
| gpu-01 | linux/amd64 | Local Docker Engine socket |

DGX Spark has an Arm CPU ([NVIDIA specifications](https://www.nvidia.com/en-us/products/workstations/dgx-spark/)).
Go supports these OS/architecture combinations ([Go documentation](https://go.dev/doc/install/source#environment)).
Keep dependencies compatible with `CGO_ENABLED=0` and cross-build all three artifacts.
Cross-compilation is not runtime verification: smoke-test each real target before rollout.

Executor platform and container platform are different facts. Docker Desktop on Mac
runs Linux containers in a VM ([Docker documentation](https://docs.docker.com/desktop/troubleshoot-and-support/faqs/macfaqs/)).
Detect the daemon platform, configure its socket explicitly and account for mount and
network behavior. ARM64 and AMD64 workload images, CUDA versions and GPU capabilities
need separate compatibility validation. A portable executor does not make a workload
image portable. Native Apple GPU workloads use a future launchd driver.

## Execution and deployment

1. Controller detects the target OS/architecture over existing strict SSH transport.
2. Select a pinned release artifact and verify its checksum against a trusted manifest.
3. Upload atomically to `~/.local/lib/wm/<version>/wm-executor` with restricted permissions.
4. Verify executor version/protocol and capabilities before admitting any mutation.
5. Invoke the executable over SSH, sending a bounded JSON request through stdin.
   For workloads on the controller, invoke the same binary locally.
6. Keep locks and receipts under a stable state directory independent of binary versions.

No resident executor daemon, TCP listener, remote LLM or remote model credentials are
required. Binary distribution, compatibility checks and retaining old versions for
in-flight operations are the principal additional operational costs. Pin the executor
version in the operation and eventually the saved plan; do not silently upgrade a
resumed operation. Define receipt schema migrations before removing old binaries.

## Protocol and ownership

Use versioned request/result schemas. Requests identify the authorized workload,
exact runtime, desired transition, operation generation, deadline and policy.
Results distinguish `verified`, `rejected`, `failed` and `unknown`; include safe
structured diagnostics and a receipt reference. Process exit status is insufficient.

Capabilities report executor/protocol version, host and daemon platforms, supported
drivers and relevant GPU/runtime availability. They do not establish readiness.

Keep workload-scoped ownership distinct from per-action receipts. The current code
supports one controller OS user and stable host aliases; remote mutations are serialized.
Before supporting multiple controllers, implement a remote lease/generation protocol
with fencing checked under the mutation lock. A stale controller must be unable to
mutate after a newer generation takes ownership. Test expiration, clock assumptions,
process death, lost replies and delayed old requests. Do not describe a local lock as
fleet-wide fencing.

Cancellation stops local work but cannot undo a Docker action already accepted by
the daemon. Unknown outcomes require exact inspection and receipt recovery before retry.
A restarted executor must never repeat a completed corrective restart merely because
its client lost the reply.

## Driver choices

Use the [Moby Go client](https://github.com/moby/moby/tree/master/client) against the
local daemon. Pin a compatible module version. It provides typed Docker API operations
and request contexts; wm still implements ownership, idempotency, budgets and convergence.
Keep desired repository/manifest references separate from the resolved configuration
image ID and exact container ID.

A controller-side SDK over SSH is possible using Docker's
[connection helper](https://github.com/docker/cli/blob/master/cli/connhelper/connhelper.go)
(`docker system dial-stdio`). Prefer the remote helper here because durable receipts
and mutation ownership already need target-side execution. No exposed Docker TCP port
is needed for either approach.

For systemd, start with `systemctl show` and bounded lifecycle commands if sufficient.
Once native job tracking is useful, use [go-systemd](https://github.com/coreos/go-systemd)
inside the Linux helper against local D-Bus. Avoid remote D-Bus tunneling from macOS.
Use build constraints for Linux-only code; keep journal reading through `journalctl`
if that avoids introducing cgo. launchd remains a separate macOS CLI adapter.

## Migration and acceptance

First harden the existing CLI contract and tests. Then replace the Docker adapter and
Python lifecycle helper behind that contract; keep Pi and provider semantics stable.
Adopt systemd/launchd only with real workload requirements.

Acceptance includes three successful builds, native smoke tests on each platform,
exact identity rejection, correct manifest/image-ID validation, conflicting-intent
exclusion, stopped and running convergence, interrupted operations, lost replies,
restart replay, version mismatch, redaction, output bounds and cancellation. Run all
failure scenarios against fixtures before any explicitly scoped live rollout.
