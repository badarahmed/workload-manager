# Workload Manager

Version **0.1.0** (unreleased prototype). See [versioning](docs/versioning.md) and
the [changelog](CHANGELOG.md).

wm uses OpenTofu to declare desired workload state and a Pi Durable/codemode agent
to reconcile it through scoped tools. It manages existing Docker containers and
checks functional readiness, with configurable cloud or OpenAI-compatible models.

This is an early prototype. It currently adopts exact existing containers rather
than provisioning a complete deployment. Docker applications run on the controller's
explicitly pinned daemon; the implemented model adapter uses SSH and vLLM-SR Engine
mode. Other drivers, engine profiles and multi-host orchestration are design work.

## Build and verify

Requires Go (see [go.mod](go.mod)), Node >=22.19, npm, Python 3, OpenTofu, and SSH.
Docker CLI access is required on the controller for local applications and on the
remote host for model workloads. Install dependencies and validate both Go binaries
in a separate output directory:

```sh
npm ci
make build BIN_DIR=.local/release-check
.local/release-check/wm --version
npm run typecheck
npm test
make test-go
python3 -m unittest discover -s agent/remote -p 'test_*.py'
python3 -m unittest discover -s scripts -p 'test_*.py'
```

Build targets:

- `make build`: CLI and provider for the current platform in `.local/bin/`.
- `make build-wm` or `make build-provider`: build one binary.
- `make build-mac`: Apple Silicon binaries in `.local/bin/darwin-arm64/`.
- `make build BIN_DIR=/path/to/bin`: select another output directory.

Plain `make build` replaces the binaries in `.local/bin/` that workload launchers
use. Run it when intentionally installing a reviewed build.

`GO`, `GOCACHE`, `CGO_ENABLED`, `GOOS`, and `GOARCH` can be overridden.
The default build disables cgo. The macOS/Linux CLI uses Unix process and locking
APIs; Windows is not a supported target.

## Try the configuration example

[examples/workloads](examples/workloads/README.md) includes a local Docker app,
an SSH model workload, companion bindings and a development-provider launcher.
Copy it to your own configuration directory and fill in the exact identities of
containers you intend to adopt. The shipped identities are synthetic placeholders.

Keep workload configuration/state separate from this source checkout. The provider
is not published to a registry; the example launcher configures a development
override pointing at your build. The runner currently needs this checkout and its
Node dependencies at runtime. It is not a standalone Go-only installation.

From your configured workload root:

```sh
./wm
./wm plan models/d3
./wm apply models/d3 -v
```

Apply runs without an interactive confirmation. Review the saved plan first.
Recovery runs during an apply, not as a continuous background service. Start/stop,
scoped restart and bounded recovery are implemented; image/model replacement and
container reconstruction are rejected or remain unimplemented. Destroy stops an
adopted container and retains the container and its data.

## Documentation

- [Documentation index](docs/README.md)
- [Operator CLI](docs/wm-operator.md): status, lifecycle, saved plans and STATE SYNC
- [Workload configuration](docs/workloads-dx.md): HCL and companion bindings
- [Agent configuration](docs/agent-model.md): YAML profiles and authentication
- [Development](docs/development.md): implementation boundaries and validation
- [Contributor instructions](AGENTS.md)

## License

[MIT](LICENSE). Dependencies retain their own licenses.
