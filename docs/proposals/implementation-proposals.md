# Implementation proposals

> **Status: proposed, not implemented.** These are independent refactoring
> proposals for the current prototype. Neither authorizes a rollout, workload
> replacement or new state owner. Current behaviour is described in
> [design](../design.md), [operations](../operations.md) and
> [development](../development.md); the broader architecture is in the
> [target design](target-design.md).

- [Go executor with Docker driver](#go-executor-with-docker-driver)
- [CLI framework and OpenTofu execution client](#cli-framework-and-opentofu-execution-client)

## Go executor with Docker driver

The current agent retains Docker CLI calls and the Python remote lifecycle helper.
This proposal records the next extraction.

### Purpose

Keep Pi Durable, codemode and Sol on the controller for reasoning. Move deterministic
remote operations into one versioned Go executable. The executable owns runtime
identity validation, bounded driver operations, remote locking and durable receipts.
The model chooses from scoped operations; it never supplies arbitrary shell commands.

The Go provider continues to own OpenTofu intent, saved-plan validation and applied
snapshots. Engine-specific model and readiness checks remain distinct from driver
lifecycle and ownership. External Prometheus supplies supporting observations and
is never provisioned by wm. Generic applications need a driver and optional probes,
not an AI engine profile.

### Target matrix

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

### Execution and deployment

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

### Protocol and ownership

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

### Driver choices

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

### Migration and acceptance

First harden the existing CLI contract and tests. Then replace the Docker adapter and
Python lifecycle helper behind that contract; keep Pi and provider semantics stable.
Adopt systemd/launchd only with real workload requirements.

Acceptance includes three successful builds, native smoke tests on each platform,
exact identity rejection, correct manifest/image-ID validation, conflicting-intent
exclusion, stopped and running convergence, interrupted operations, lost replies,
restart replay, version mismatch, redaction, output bounds and cancellation. Run all
failure scenarios against fixtures before any explicitly scoped live rollout.

## CLI framework and OpenTofu execution client

Recorded 2026-11-02 from the library comparison discussed on 2026-10-31.

### Recommendation

Adopt Cobra for command parsing. Evaluate HashiCorp's `terraform-exec` behind a
small `internal/tofu` package before replacing the current execution code. Keep
the existing subprocess runner if the library cannot preserve wm's behavior
without compatibility patches or substantial additional plumbing.

OpenTofu remains a separate executable in either implementation. Do not embed
OpenTofu or Terraform internal command packages.

### CLI framework

wm now exposes status, plan, apply, start, stop and restart, with shared flags and
app/model scope arguments. The current stdlib implementation includes custom
argument reordering and command dispatch. [Cobra](https://cobra.dev/docs/)
provides subcommands, inherited flags, argument validation, generated help and
shell completion, making it a reasonable dependency at this size.

Keep command definitions thin: parse options and call operator services. Retain
the existing YAML configuration loader; adding Viper is unnecessary. Preserve
default status behavior, `-v`, scope syntax, JSON restrictions and exit codes.
Handle existing legacy aliases explicitly during migration rather than silently
changing their meaning.

### OpenTofu client candidates

| Candidate | Assessment |
|---|---|
| [hashicorp/terraform-exec](https://github.com/hashicorp/terraform-exec) | Strongest library candidate. Typed CLI options, saved-plan operations, structured plan/state JSON, streamed output and context cancellation cover much of wm's execution plumbing. Its documented compatibility policy targets Terraform, so OpenTofu compatibility must be verified. |
| [opentofu/tofu-exec](https://github.com/opentofu/tofu-exec#readme) | At the time of review, its README describes it as a work in progress, currently unusable and not recommended outside tofu-ls. Do not adopt on that basis; reassess if its support policy changes. |
| [Terratest](https://terratest.gruntwork.io/docs/getting-started/quick-start/) | Explicit OpenTofu support, but designed for infrastructure testing. Suitable for test tooling rather than wm's runtime orchestration layer. |
| [Terramate run/tofu](https://pkg.go.dev/github.com/terramate-io/terramate/run/tofu) | The reviewed API mainly provides version detection and construction of show commands. It does not replace wm's plan/apply execution layer. |
| Existing os/exec runner | Straightforward fallback with direct control over signals, streams and OpenTofu flags. Consolidate it even if no external client is adopted. |

`terraform-exec` accepts an executable path through `NewTerraform`, making the
installed tofu binary a candidate for compatibility testing. This is not a
guarantee of OpenTofu support: the projects' version numbers and feature sets can
diverge. Pin the library version and verify the installed OpenTofu version.

Relevant upstream references:

- [Compatibility policy](https://github.com/hashicorp/terraform-exec#terraform-compatibility)
- [Execution API](https://pkg.go.dev/github.com/hashicorp/terraform-exec/tfexec),
  including Plan, Apply, ShowPlanFile, RefreshOnly, PlanJSON and ApplyJSON
- [Subprocess and cancellation implementation](https://github.com/hashicorp/terraform-exec/blob/main/tfexec/cmd.go)

### Package boundary

`internal/tofu` should own typed plan, apply-saved-plan, show-plan and module-update
operations, working directories, environment construction, output streaming,
cancellation and subprocess error translation. Keep this boundary small and
driven by existing callers rather than mirroring every OpenTofu command.

The operator layer continues to own scope resolution, source/state fingerprints,
plan receipts, lifecycle dependency waves, exact-plan validation, backups and
three-way synchronization. These policies are not responsibilities of an execution
library.

Gradually consolidate the current shell/Python launcher responsibilities into Go:
provider development overrides, TF_VAR_wm_root, agent configuration/profile
selection, development-warning filtering and verbose agent progress. OpenTofu JSON
progress and Pi/codemode progress are separate streams; adopting a client library
does not replace the latter.

### Evaluation and acceptance

First add the package boundary around the existing runner. Then evaluate a pinned
`terraform-exec` version against an isolated fixture and compare behavior before
switching implementations.

Required checks:

- Scoped plans resolve the same module targets and apply the exact saved plan.
- Refresh-disabled and refresh-only comparisons preserve the plan JSON fields
  used by three-way synchronization, including prior state and resource drift.
- Empty and changed plans retain the correct command behavior and exit status.
- Provider overrides and agent YAML/profile environment settings reach the child.
- Normal output, JSON, diagnostic errors, warning filtering and `-v` agent progress
  remain available without exposing sensitive plan/state contents.
- Interrupts and deadlines allow graceful OpenTofu cancellation and state-lock
  cleanup; child processes do not continue accidentally after wm exits.
- Stale-plan rejection and lifecycle plan guards continue to run before apply.

Use disposable fixtures for mutation tests. Read-only checks can inspect the live
workloads root, but this refactor does not require stopping or restarting live
workloads. If evaluation reveals unsupported flags, incompatible version gates
or insufficient signal control, retain the os/exec implementation behind the same
package boundary rather than maintaining a client-library fork.
