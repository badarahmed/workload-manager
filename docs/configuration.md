# Configuration

Keep live configuration in its own OpenTofu root, separate from this source
checkout. Start from the [example](../examples/workloads/README.md).

## Workload root

```text
workloads/
  tofu                    # development-provider and progress launcher
  wm                      # local symlink to the built operator CLI
  provider.tf
  main.tf                 # loads apps and models
  agent.yaml              # agent model profiles
  apps/web/main.tf
  apps/web/bindings/service.json
  models/d3/main.tf
  models/d3/bindings/d3.json
```

The root loads category modules, and each category registers its children. Run
`./tofu get` after adding or moving local modules. Use `moved` blocks when
changing addresses of existing resources. Never import a container already
managed by another state root.

## Workload HCL

HCL holds desired policy:

- `desired`: `running` or `stopped`.
- `runtime`: driver, engine and a manifest-pinned image.
- `model` (models only): an `hf://` source and exact revision.
- `requirements`: which readiness check defines "ready".
- `recovery`: whether corrective restarts are allowed, and `startup_grace` (1–30
  minutes) before one is considered. A large model can use `"10m"` so normal
  loading does not trigger recovery.
- `diagnostics` (optional): log access and redaction, below.

Applications use `engine = "application"`, omit model metadata and declare Docker
health or an explicit HTTP check. The application adapter supports
controller-local Docker only. The model adapter supports SSH and vLLM-SR Engine
mode.

### Diagnostics

```hcl
diagnostics {
  logs            = false
  redact_patterns = ["internal-project-label"]
}
```

Omitting the block permits sanitized log access. Logs are untrusted evidence and
may be persisted in the durable journal; filtering cannot recognize every secret.
Redaction patterns are literal strings, not regexes, and are stored in HCL and
state, so never put secrets in them. Existing journals are not retroactively
scrubbed.

## Companion bindings

Each workload has a binding JSON file holding captured runtime identity and
transport, not policy. The provider rejects image, model or recovery overrides in
bindings. Never copy container environments or credentials into them.

- **Local app:** controller hostname (Python's `socket.gethostname()`), explicit
  Unix Docker socket, exact container name and ID, and configuration image ID.
- **SSH model:** SSH user, host and known-host alias, exact container identity,
  engine state directory and loopback probe addresses on the target.

The optional `observedImageId` is Docker's configuration image ID. It differs from
the manifest digest in HCL `runtime.image`, which must appear in the captured
image's RepoDigests. Capture both on the correct daemon or SSH target without
dumping secret environment:

```sh
docker inspect --format '{{.Id}} {{.Name}} {{.Image}}' CONTAINER
docker image inspect --format '{{json .RepoDigests}}' IMAGE
```

## Applied snapshots and ownership

At plan time wm combines HCL and binding metadata into a canonical recipe and
digest. State keeps this applied snapshot, and the runner materializes it under
the source checkout's ignored `.local/recipes/`. Refresh uses the applied
snapshot. Apply checks the source again and rejects bindings changed after the
saved plan.

Start, stop and supported recovery-policy edits work against the captured
container. Image, model, host or driver replacement needs deployment support that
is not implemented, so such changes fail during planning. Destroy stops the
captured container and keeps its data.

## Agent model

Agent model profiles live in `agent.yaml` at the workload root, separate from HCL.
The example `./tofu` launcher discovers it for every command. Change `active` to
switch the default:

```yaml
version: 1
active: glm # Change to sol for Codex.

profiles:
  glm:
    backend: openai-compatible
    model: GLM-5.3-Flash-EXL3
    baseUrl: http://127.0.0.1:8888/v1
    contextWindow: 1000000
    maxOutputTokens: 4096
    reasoning: true
    compat:
      thinkingFormat: chat-template
      chatTemplateKwargs:
        enable_thinking: false
      requiresReasoningContentOnAssistantMessages: true

  sol:
    backend: codex
    model: gpt-6.1-sol
    thinkingLevel: medium
```

Override the profile for one command with `PROFILE=sol ./wm apply -v`. No rebuild
is needed. The [example YAML](../examples/agent/agent.yaml) contains both profiles.

### Backends and endpoints

- **`openai-compatible`** uses chat completions at `baseUrl`, including `/v1`.
  Check the model ID and context length against the endpoint's `/v1/models`.
  `reasoning` declares capability; the template setting above disables thinking
  for agent requests. By default the adapter disables OpenAI-specific grammar
  tools, strict mode, developer-role messages, store and reasoning-effort
  parameters. `maxOutputTokens` must be 256–4096 and smaller than the context
  window. Local model cost metadata is zero, not an estimate of hardware cost.
- **`codex`** uses the controller's existing Codex sign-in.
- **`openai`** uses `OPENAI_API_KEY`; model IDs must exist in Pi's installed
  catalog.

`thinkingLevel` accepts `off`, `minimal`, `low`, `medium`, `high`, `xhigh` and
`max`, checked against the selected model's catalog entry; it defaults to `off`.

The LLM endpoint is an external prerequisite. Select another backend before
managing or stopping the agent's own serving workload.

### Credentials

Codex owns credential refresh. The Codex bridge reads access tokens on demand from
`$CODEX_HOME/auth.json` (default `~/.codex/auth.json`), requires 35 minutes of
remaining validity and does not support keychain-only auth.

A compatible endpoint without `apiKeyEnv` receives the non-secret placeholder
`wm-local-no-auth`; it never inherits Codex credentials or ambient
`OPENAI_API_KEY`. For an authenticated server, add `apiKeyEnv: WM_AGENT_API_KEY`
to its profile and set that variable. Missing configured keys fail before
admission. Never store keys or credential-bearing URLs in YAML, HCL, bindings or
journals, and never send model credentials to workload hosts.

### Selection order

1. `WM_AGENT_CONFIG` selects a file; otherwise the launcher discovers
   `agent.yaml`, falling back to legacy `agent.json` only if YAML is absent.
2. `PROFILE` selects a profile, then the legacy `WM_AGENT_PROFILE`, then YAML's
   `active`.
3. With no configuration file, legacy `WM_MODEL_AUTH=auto|codex|api` selects
   `gpt-6.1-sol`. `api` requires `OPENAI_API_KEY`; `auto` prefers that key, then
   Codex sign-in.

A missing or malformed explicit file, unknown profile, duplicate YAML keys or
custom tags fail without fallback. All profiles are validated, including inactive
ones. Single-model JSON stays readable but cannot take a `PROFILE` override.

An active operation pins the selected model settings. Switching profiles
mid-operation is rejected; restore the original selection to resume. Editing
inactive profiles or reformatting YAML does not change the pin. Legacy active
journals without a pin are rejected. Model selection is controller
configuration, not a workload change in a saved plan.

### Smoke test

From the source checkout, an opt-in test runs the real configured model through
Pi Durable and codemode against simulated workload tools:

```sh
WM_AGENT_CONFIG="/path/to/workloads/agent.yaml" PROFILE=glm \
  node --import tsx agent/src/model-check.ts
```

It exercises a rejected action, observe, start, an initially unready probe, wait
and convergence, with a 90-second deadline and 12-request budget. It uses a
temporary journal and never calls SSH or Docker, so it checks model and tool
compatibility, not live workload operations.
