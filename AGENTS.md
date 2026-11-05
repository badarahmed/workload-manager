# Working on wm

## Scope and source of truth

- This repository implements the Go provider, operator CLI and TypeScript Pi Durable
  runner. Live HCL/state belong in a separate workload configuration directory.
- Read [README.md](README.md), [design](docs/design.md), [configuration](docs/configuration.md)
  and [development notes](docs/development.md) for context.
- If `.local/docs/README.md` exists, read it before working on this checkout's
  live deployment. Private operating records belong in ignored `.local/docs/`.
- Public examples and test fixtures use synthetic identities. Never adopt the same
  real container into a second state root or test against it by accident.
- Specs describe a broader target than the implementation. Distinguish proposed,
  implemented and verified behavior. Keep public detailed docs in `docs/`.
- Version CLI/provider/agent releases together; follow [versioning](docs/development.md#versioning-and-releases)
  and keep Go/package metadata and the changelog consistent.

## Implementation boundaries

- Desired policy belongs in HCL. Binding JSON contains captured runtime identity,
  SSH/local transport and probe addresses; it must not override desired policy.
- Preserve saved-plan checks, applied snapshots, exact identity, durable receipts
  and independent functional verification. SSH failure means unknown outcome.
- Keep ownership checks separate from engine drift: logs and an authorized stop
  must remain available for an exact owned container with broken model config.
- Preserve existing Compose sources, mounts, volumes and explicit daemon/controller
  bindings. Do not assume reverse update ordering supplies coordinated shutdown.
- Support one controller OS user with stable SSH host-key aliases. A local lock
  is not cross-controller fencing.
- Keep the agent behind scoped codemode tools. Do not add ambient shell access,
  extension discovery or tools that bypass ownership checks.
- Do not stop/restart live workloads or apply plans to test unrelated changes.
- Prometheus and shared monitoring networks remain externally owned. Managing an
  exporter or application grants no rights over shared monitoring infrastructure.

## Credentials and artifacts

- Codex owns credential refresh. Never print credentials, copy them into source,
  HCL, bindings or journals, or transmit model credentials to workload hosts.
- Keep state, backups, saved plans, caches and journals ignored. These are runtime
  artifacts, not documentation. Preserve runtime paths during documentation cleanup.
- Never delete data volumes, model caches or unrelated containers as cleanup.
- Keep `.local/` out of commits. It may contain private docs, credentials in logs,
  operation history, backups and machine-specific configuration.

## Validation

Run checks appropriate to the change:

```sh
npm run typecheck
npm test
make test-go
python3 -m unittest discover -s agent/remote -p 'test_*.py'
python3 -m unittest discover -s scripts -p 'test_*.py'
```

Use `make build` to build both Go binaries, or `make build-provider` after provider
changes before a live wrapper uses it. Build to an alternate `BIN_DIR` when validating
without replacing installed binaries. For documentation-only edits, verify links and
paths; do not run live apply or recovery tests. Report checks and remaining gaps.
