# Changelog

## 0.1.0 (unreleased)

Initial MIT-licensed prototype:

- OpenTofu provider for captured Docker application and SSH model workloads.
- Pi Durable/codemode reconciliation with configurable model profiles.
- Operator status, scoped plan/apply and start/stop/restart commands.
- Applied snapshots, saved-plan guards, exact identity checks and durable receipts.
- Synthetic app/model configuration examples and Makefile build targets.

This version does not provide container provisioning, image/model replacement,
systemd/launchd drivers, remote state support in the operator, or cross-controller
fencing. See the implementation documentation for supported behavior.
