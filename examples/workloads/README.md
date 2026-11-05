# Example workload root

This example uses the current provider schema and folder layout. It includes a
controller-local Docker application and an SSH vLLM-SR Engine-mode model. All host,
container, image and revision identities are synthetic placeholders. wm adopts
existing containers; this example does not create a web server or download a model.

## Set up a separate configuration root

First run `npm ci` and `make build` in the wm source checkout. Then copy this
directory somewhere outside the source checkout:

```sh
cp -R examples/workloads /path/to/my-workloads
cd /path/to/my-workloads
export WM_ROOT=/absolute/path/to/wm
ln -s "$WM_ROOT/.local/bin/wm" wm
cp "$WM_ROOT/examples/agent/agent.yaml" agent.yaml
```

Set `WM_ROOT` whenever using this root, or put the source checkout at the sibling
path `../wm`. The `tofu` launcher generates a private provider override, sets the
provider root and delegates scope/progress handling to the checked-out wm scripts.
The provider is not installed from a registry. Go and npm dependency versions are
pinned by the source checkout's module/lock files.

The copied agent profile defaults to the illustrative compatible endpoint at
`127.0.0.1:8888/v1`. Configure an actual endpoint/model, or set `active: sol` to use
the controller's Codex file sign-in. `PROFILE=sol` overrides it for one command.
Never put tokens or credential-bearing URLs in YAML.

## Replace captured identity and desired policy

- `apps/web/main.tf`: image manifest reference, running/stopped intent, readiness
  and recovery. This example requires an existing Docker health check.
- `apps/web/bindings/service.json`: controller hostname (as returned by Python's
  `socket.gethostname()`), explicit Unix Docker socket, exact container name/ID
  and configuration image ID. This adapter only supports controller-local apps.
- `models/d3/main.tf`: manifest-pinned engine image, `hf://` model source/revision,
  desired state and ten-minute startup grace. It requires a System One endpoint.
- `models/d3/bindings/d3.json`: SSH user/host/known-host alias, exact container
  identity, engine state directory and loopback probe addresses on the target.

Use `docker inspect --format '{{.Id}} {{.Name}} {{.Image}}' CONTAINER` to inspect
identity without dumping secret environment. Use `docker image inspect --format
'{{json .RepoDigests}}' IMAGE` to get manifest references. Run these on the correct
daemon/SSH target. A configuration image ID is not a registry manifest digest.
Keep existing Compose sources, mounts, credentials and volumes intact.

Remove an unwanted example module declaration before first adoption. The root loads
`apps` and `models`; each category explicitly registers its children. `get` refreshes
this local module inventory. Never import or apply this example against a container
already owned by another OpenTofu state.

## Validate and review

```sh
./tofu get
./tofu fmt -check -recursive
./tofu validate
./wm plan
./wm plan apps/web
./wm plan models/d3
```

Schema validation does not prove the captured identities or image/model pins are
real. Planning new resources can succeed with placeholders; replace and independently
verify them before adoption. Plans may inspect/probe existing managed containers.
Only after reviewing a plan against your own containers, use `./wm apply [scope]`.
Apply has no confirmation prompt and may start/stop/recover owned workloads.

Routine commands include `./wm status`, `./wm -v`, `./wm stop apps/web`,
`./wm start models/d3 -v` and `./wm restart apps/web -v`. The lifecycle commands
edit HCL and perform guarded plan/apply. The CLI currently uses local state only.

This directory does not contain state, credentials or deployment captures. Keep
the copied root private and back up its state. `.local/`, `.terraform/`, plans,
state/backups, agent configuration and variable files are ignored by its gitignore.

See the source checkout's [configuration](../../docs/configuration.md) and
[operations](../../docs/operations.md) docs for the full behavior and current limits.
