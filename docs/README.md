# Documentation

## Implementation and use

- [Reconciliation design](reconciliation.md): declarative intent, agentic reconciliation, tradeoffs and alternatives.
- [Operator CLI](wm-operator.md): status, plan/apply, lifecycle and STATE SYNC.
- [Status details](status-cli.md): application/engine versions and model pins.
- [Configuration](workloads-dx.md): HCL, companion bindings and ownership.
- [Agent configuration](agent-model.md): model profiles and authentication.
- [Development](development.md): code layout, recovery boundaries and testing.
- [Versioning](versioning.md) and [changelog](../CHANGELOG.md): release metadata and compatibility policy.
- [Example workload root](../examples/workloads/README.md): app and model templates.

## Design proposals

These documents include future capabilities beyond the current prototype. Host
names and configurations in public specs are illustrative, not a deployment inventory.

- [CLI framework and OpenTofu client](proposals/cli-and-opentofu-client.md)
- [Go Docker executor](go-docker-executor-proposal.md)
- [OpenTofu agent specification](workload-manager-opentofu-agent-spec.md)
- [Pi Durable/codemode design](workload-manager-pi-codemode-design.md)
- [Flowchart PNG](workload-manager-opentofu-agent-flowchart.png) and
  [Mermaid source](workload-manager-opentofu-agent-flowchart.mmd): conceptual design
- [Original specification](workload-manager-spec.md): historical architectural context

Repository entry points: [README](../README.md) and [AGENTS.md](../AGENTS.md).
