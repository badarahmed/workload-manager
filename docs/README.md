# Documentation

Current behavior:

- [Design](design.md): declarative intent, agentic reconciliation, safeguards,
  budgets, tradeoffs and alternatives considered.
- [Configuration](configuration.md): workload root, HCL, companion bindings,
  ownership and agent model profiles.
- [Operations](operations.md): status and STATE SYNC, plan/apply and lifecycle
  commands.
- [Development](development.md): code layout, build, tests and versioning.
- [Example workload root](../examples/workloads/README.md) and
  [changelog](../CHANGELOG.md).

Proposals (future capabilities beyond the prototype; host names are illustrative):

- [Target design](proposals/target-design.md): the broader OpenTofu + Pi Durable
  architecture, drivers, engines, applications and monitoring.
- [Implementation proposals](proposals/implementation-proposals.md): Go executor,
  CLI framework and OpenTofu client.
