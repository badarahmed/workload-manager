# Versioning

wm uses Semantic Versioning, starting at **0.1.0**. The CLI, OpenTofu provider and
TypeScript agent ship as one versioned source tree. Git release tags use the form
`v0.1.0`; runtime/package metadata uses `0.1.0` without the prefix.

The project remains a prototype before 1.0. Patch releases contain compatible fixes;
minor releases introduce features and may include breaking changes. Document HCL,
state, journal, binding or CLI compatibility changes explicitly in the changelog.
A version bump alone is not a state or durable-journal migration mechanism.

## Maintaining the version

Update these together:

1. The shared Go constant in `internal/version/version.go`.
2. `version` in `package.json`, `package-lock.json`, and the lockfile root package.
3. `CHANGELOG.md`, including compatibility notes and the actual release date when released.

`make test-go` checks that Go and package metadata agree. Both normal Go builds and
Makefile builds embed the shared Go version; no linker flags or Git checkout are
required. `wm --version` prints the CLI release without inspecting workloads.
The provider reports the same value in its protocol metadata.

Validate in an alternate output directory before replacing an installed build:

```sh
make test-go
make build BIN_DIR=.local/release-check
.local/release-check/wm --version
npm run typecheck
npm test
```

Run relevant Python tests for executor/launcher changes. Review the public file set,
update the changelog, and commit reviewed source before creating a release tag.
Do not include `.local/`, state, credentials or deployment captures in release assets.

Version `0.1.0` is currently unreleased. Setting metadata does not create a Git tag,
publish a GitHub release or install new binaries into an existing deployment.
