# Workload configuration

Use a separate OpenTofu root for your live configuration. The
[example](../examples/workloads/README.md) shows this layout:

```text
workloads/
  tofu                    # development-provider and progress launcher
  wm                      # local symlink to the built operator CLI
  provider.tf
  main.tf                 # loads apps and models
  agent.yaml              # local model profiles
  apps/web/main.tf
  apps/web/bindings/service.json
  models/d3/main.tf
  models/d3/bindings/d3.json
```

Nested directories must be registered as child modules. The category modules load
individual apps/models. Run `./tofu get` after adding or moving local modules.
Use moved declarations when changing addresses of existing resources; do not re-import
an already managed container into another root.

## HCL is the authoring surface

`runtime` combines driver, engine and a manifest-pinned image. Models add an explicit
`hf://` source and exact revision. `desired` accepts `running` or `stopped`.
`requirements` selects readiness; `recovery` controls corrective restarts and startup
grace. A large model can use `startup_grace = "10m"` so normal loading does not
immediately trigger recovery. The wait tool pauses 60 seconds before probing again;
operation deadlines and command budgets still apply.

Applications use `engine = "application"`, omit model metadata and declare Docker
health or an explicit HTTP check. Local application bindings pin the controller
hostname, Docker socket, exact container ID and configuration image ID. The current
application adapter supports controller-local Docker; SSH application adoption is
not implemented. The model adapter supports SSH and vLLM-SR Engine mode.

The companion binding holds captured identity and transport, not desired policy.
The provider rejects image/model/recovery overrides in binding JSON. Never copy
resolved container environments or credentials into it.

## Applied snapshots and ownership

At plan time wm combines HCL and binding metadata into a canonical recipe and digest.
State retains the applied snapshot; the runner materializes it under the implementation
checkout's ignored `.local/recipes/`. Refresh uses the applied snapshot. Apply checks
source again and rejects bindings changed after the saved plan was created.

Start/stop and supported recovery-policy edits work against the captured container.
Image/model/host/driver replacements require deployment support that is not implemented;
unsupported changes fail during planning. Destroy stops the captured container and
retains its data. Never create a second state owner for a live container.

The optional binding `observedImageId` is Docker's configuration image ID. It differs
from the manifest digest in HCL `runtime.image`, which must appear in the captured
image's RepoDigests. Inspect those independently during adoption.

## Diagnostics

```hcl
diagnostics {
  logs            = false
  redact_patterns = ["internal-project-label"]
}
```

Omitting this block permits sanitized log access. Logs are untrusted evidence and
may be persisted by the durable runtime; filtering cannot recognize every secret.
Redaction patterns are optional literal strings, not regexes. Do not put secrets
in the patterns: HCL and state retain them. Existing journals are not retroactively scrubbed.

## Routine operation

```sh
./wm
./wm plan
./wm plan models/d3
./wm apply models/d3 -v
./wm stop apps/web
./wm start apps/web
./wm restart apps/web -v
```

Apply validates a saved plan for the selected scope or generates one first, without
an interactive confirmation. Review a plan before applying. Lifecycle commands edit
literal desired values in HCL and use guarded plan/apply steps. See the
[operator guide](wm-operator.md) for limits, dependency waves and failure behavior.
