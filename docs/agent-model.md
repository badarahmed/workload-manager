# Configuring the reconciliation agent model

Store agent model profiles in `agent.yaml` at your workload configuration root,
separate from HCL. The example `./tofu` launcher discovers it for all commands.
Change one line to switch the default:

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

For one apply, override the selected profile without editing the file:

```sh
PROFILE=sol ./wm apply -v
PROFILE=glm ./wm apply -v
```

The [example YAML](../examples/agent/agent.yaml) contains both profiles. No rebuild
is needed. Plans and already-converged applies still do not call the LLM; verbose
apply shows the model when the agent is needed.

The Sol profile exposes `thinkingLevel` (set to `medium`). Pi accepts `off`,
`minimal`, `low`, `medium`, `high`, `xhigh` and `max`; wm also checks support in the
selected model catalog. When omitted, Pi defaults to `off`. This setting is passed
to the durable agent and included in its active-operation configuration pin.

## Endpoint and compatibility

The compatible backend uses chat completions at `baseUrl`, including `/v1`. Check the
model ID and context length against your endpoint's `/v1/models` response.
`reasoning` declares the model's capability; the template setting above disables
thinking for agent requests. Compatibility settings can be edited in YAML if the
serving template changes. By default, the adapter disables OpenAI-specific grammar
tools, strict mode, developer-role messages, store and reasoning-effort parameters.
Codemode uses ordinary JSON function tool calls with this vLLM server.

Output is capped per request by `maxOutputTokens` (256–4096, smaller than the context
window). The existing 40-model-request, 80-command and 30-minute operation budgets
remain in effect. Context is capacity, not promised consumption. Local model cost
metadata is zero/unmetered, not an estimate of hardware cost.

## Authentication and precedence

Without `apiKeyEnv`, a compatible endpoint profile receives the
non-secret placeholder `wm-local-no-auth`; it never inherits Codex credentials or
ambient `OPENAI_API_KEY` for that endpoint. For an authenticated compatible server,
add `apiKeyEnv: WM_AGENT_API_KEY` under its profile and set that environment variable.
Missing configured keys fail before admission. Never store keys or credential-bearing
URLs in YAML.

The `sol` profile uses the controller's existing Codex sign-in. A profile with
`backend: openai` uses `OPENAI_API_KEY`; cloud model IDs must exist in Pi's installed
catalog. Codex remains responsible for credential refresh.

Selection order:

1. `WM_AGENT_CONFIG` selects an explicit configuration file; otherwise the wrapper
   discovers `agent.yaml`, with legacy `agent.json` as a fallback only if YAML is absent.
2. `PROFILE` selects a named profile; otherwise the legacy `WM_AGENT_PROFILE`
   alias is checked, then YAML's `active`. `PROFILE` wins when both are set.
3. Without any configuration file, legacy `WM_MODEL_AUTH=auto|codex|api` behavior
   remains, selecting `gpt-6.1-sol`.

An explicit missing/malformed file, unknown profile, duplicate YAML keys or custom
tags fail without model fallback. All profiles are validated, including inactive ones.
Single-model JSON remains readable for compatibility, but cannot accept a named-profile
override. New configurations should use one YAML file.

Active operations pin the selected model settings. Switching profiles mid-operation
is rejected; restore the original selection to resume. Editing inactive profiles or
reformatting YAML does not change the selected model's pin. The JSON-to-YAML migration
preserves that pin. Legacy active journals without a model pin are rejected rather
than silently switched. Completed operations can use another model on the next apply.
Model selection is controller configuration, not a workload change in a saved plan.

LLM requests go directly to the selected endpoint; workload operations remain scoped
tools. The configured LLM endpoint remains an external prerequisite. Select another backend
before managing or stopping the agent's own serving workload.

## Verification

From the wm checkout, run the opt-in inference smoke test:

```sh
WM_AGENT_CONFIG="/path/to/workloads/agent.yaml" PROFILE=glm \
  node --import tsx agent/src/model-check.ts
```

It uses the real configured LLM through Pi Durable/codemode with simulated workload
tools. It exercises a rejected action, observe, start, an initially unready probe,
simulated wait and convergence. It has a 90-second deadline and 12-request budget,
uses a temporary journal, and never calls SSH or Docker.

The smoke test checks model/tool compatibility and simulated recovery. It does not
verify live workload operations. YAML selection, validation and digest equivalence
are also covered by isolated tests without inference.
