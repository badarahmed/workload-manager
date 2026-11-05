# Development

See [design](design.md) for the safeguards every change must preserve and
[AGENTS.md](../AGENTS.md) for contributor rules.

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

Docker CLI calls and the Python remote helper are the production execution path.
A Go executor is only a [proposal](proposals/implementation-proposals.md).

## Build

```sh
npm ci
make build BIN_DIR=.local/release-check
.local/release-check/wm --version
```

- `make build`: CLI and provider for the current platform in `.local/bin/`. This
  replaces the binaries workload launchers use, so run it only to install a
  reviewed build.
- `make build-wm` or `make build-provider`: build one binary.
- `make build-mac`: Apple Silicon binaries in `.local/bin/darwin-arm64/`.
- `make build BIN_DIR=/path/to/bin`: select another output directory.

`GO`, `GOCACHE`, `CGO_ENABLED`, `GOOS` and `GOARCH` can be overridden; cgo is
disabled by default. The CLI uses Unix process and locking APIs, so Windows is not
supported. The provider is not published to a registry, and the runner needs this
checkout and its Node dependencies at runtime.

## Tests

```sh
npm run typecheck
npm test
make test-go
python3 -m unittest discover -s agent/remote -p 'test_*.py'
python3 -m unittest discover -s scripts -p 'test_*.py'
```

Tests use fake transports, temporary directories, simulated services and an
isolated fake OpenTofu backend. They cover failed apply and state retention,
saved-plan checks and fingerprints, identity drift, remote drift and unknown
comparisons, lost replies, durable replay, cancellation, probes, scope mapping,
interrupts, and lifecycle ordering, failure handling and plan-boundary guards.
They do not show that every failure scenario has been exercised on real hosts. The
model [smoke test](configuration.md#smoke-test) checks a real LLM against
simulated tools. Never stop or restart a live workload just to test a change.

## Launcher and progress

The example `tofu` launcher sets development provider overrides and
`TF_VAR_wm_root`, then calls `scripts/tofu_verbose.py`. That script discovers
`agent.yaml`, resolves `--scope` from registered module paths and forwards signals.

Verbose apply uses a private `WM_PROGRESS_FILE`. Allowlisted action, probe and wait
summaries stream to stderr; raw model reasoning, generated code and logs are not
forwarded. OpenTofu output, exit status and interrupts are unchanged. With
`WM_HIDE_DEV_WARNING=1` only the development-override banner is filtered. JSON is
never filtered. Progress has no authority over readiness, receipts or
cancellation.

## Runtime paths

Applied recipes, operation IDs and journal paths depend on stable checkout
locations. Keep `.local/recipes/` and `.local/operations/` in place during cleanup;
controller binaries live in `.local/bin/`. Saved plans, intent backups and state
live under the workload root. Private deployment notes belong in ignored
`.local/docs/`. None of these are committed.

## Versioning and releases

wm uses Semantic Versioning, starting at **0.1.0** (unreleased). The CLI, provider
and agent ship together from one source tree. Git tags use `v0.1.0`; package
metadata uses `0.1.0`. Before 1.0, patch releases are compatible fixes and minor
releases may break compatibility. Record HCL, state, journal, binding or CLI
compatibility changes in the [changelog](../CHANGELOG.md). A version bump is not a
state or journal migration.

To change the version, update together:

1. The Go constant in `internal/version/version.go`.
2. `version` in `package.json` and `package-lock.json` (including the lockfile
   root package).
3. `CHANGELOG.md`, with compatibility notes and the release date.

`make test-go` checks that Go and package metadata agree. Builds embed the version
without linker flags or a Git checkout; `wm --version` prints it without
inspecting workloads, and the provider reports it in protocol metadata.

To release, build and test in an alternate `BIN_DIR`, run the Python tests for
executor or launcher changes, review the public file set and changelog, then commit
before tagging. Never include `.local/`, state, credentials or deployment captures
in release assets. Setting the version does not tag, publish or install anything.
