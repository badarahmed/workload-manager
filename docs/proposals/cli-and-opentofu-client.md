# Proposal: CLI framework and OpenTofu execution client

Status: **proposed, not implemented**. Recorded 2026-11-02 from the library
comparison discussed on 2026-10-31.

## Recommendation

Adopt Cobra for command parsing. Evaluate HashiCorp's `terraform-exec` behind a
small `internal/tofu` package before replacing the current execution code. Keep
the existing subprocess runner if the library cannot preserve wm's behavior
without compatibility patches or substantial additional plumbing.

OpenTofu remains a separate executable in either implementation. Do not embed
OpenTofu or Terraform internal command packages.

## CLI framework

wm now exposes status, plan, apply, start, stop and restart, with shared flags and
app/model scope arguments. The current stdlib implementation includes custom
argument reordering and command dispatch. [Cobra](https://cobra.dev/docs/)
provides subcommands, inherited flags, argument validation, generated help and
shell completion, making it a reasonable dependency at this size.

Keep command definitions thin: parse options and call operator services. Retain
the existing YAML configuration loader; adding Viper is unnecessary. Preserve
default status behavior, `-v`, scope syntax, JSON restrictions and exit codes.
Handle existing legacy aliases explicitly during migration rather than silently
changing their meaning.

## OpenTofu client candidates

| Candidate | Assessment |
|---|---|
| [hashicorp/terraform-exec](https://github.com/hashicorp/terraform-exec) | Strongest library candidate. Typed CLI options, saved-plan operations, structured plan/state JSON, streamed output and context cancellation cover much of wm's execution plumbing. Its documented compatibility policy targets Terraform, so OpenTofu compatibility must be verified. |
| [opentofu/tofu-exec](https://github.com/opentofu/tofu-exec#readme) | At the time of review, its README describes it as a work in progress, currently unusable and not recommended outside tofu-ls. Do not adopt on that basis; reassess if its support policy changes. |
| [Terratest](https://terratest.gruntwork.io/docs/getting-started/quick-start/) | Explicit OpenTofu support, but designed for infrastructure testing. Suitable for test tooling rather than wm's runtime orchestration layer. |
| [Terramate run/tofu](https://pkg.go.dev/github.com/terramate-io/terramate/run/tofu) | The reviewed API mainly provides version detection and construction of show commands. It does not replace wm's plan/apply execution layer. |
| Existing os/exec runner | Straightforward fallback with direct control over signals, streams and OpenTofu flags. Consolidate it even if no external client is adopted. |

`terraform-exec` accepts an executable path through `NewTerraform`, making the
installed tofu binary a candidate for compatibility testing. This is not a
guarantee of OpenTofu support: the projects' version numbers and feature sets can
diverge. Pin the library version and verify the installed OpenTofu version.

Relevant upstream references:

- [Compatibility policy](https://github.com/hashicorp/terraform-exec#terraform-compatibility)
- [Execution API](https://pkg.go.dev/github.com/hashicorp/terraform-exec/tfexec),
  including Plan, Apply, ShowPlanFile, RefreshOnly, PlanJSON and ApplyJSON
- [Subprocess and cancellation implementation](https://github.com/hashicorp/terraform-exec/blob/main/tfexec/cmd.go)

## Package boundary

`internal/tofu` should own typed plan, apply-saved-plan, show-plan and module-update
operations, working directories, environment construction, output streaming,
cancellation and subprocess error translation. Keep this boundary small and
driven by existing callers rather than mirroring every OpenTofu command.

The operator layer continues to own scope resolution, source/state fingerprints,
plan receipts, lifecycle dependency waves, exact-plan validation, backups and
three-way synchronization. These policies are not responsibilities of an execution
library.

Gradually consolidate the current shell/Python launcher responsibilities into Go:
provider development overrides, TF_VAR_wm_root, agent configuration/profile
selection, development-warning filtering and verbose agent progress. OpenTofu JSON
progress and Pi/codemode progress are separate streams; adopting a client library
does not replace the latter.

## Evaluation and acceptance

First add the package boundary around the existing runner. Then evaluate a pinned
`terraform-exec` version against an isolated fixture and compare behavior before
switching implementations.

Required checks:

- Scoped plans resolve the same module targets and apply the exact saved plan.
- Refresh-disabled and refresh-only comparisons preserve the plan JSON fields
  used by three-way synchronization, including prior state and resource drift.
- Empty and changed plans retain the correct command behavior and exit status.
- Provider overrides and agent YAML/profile environment settings reach the child.
- Normal output, JSON, diagnostic errors, warning filtering and `-v` agent progress
  remain available without exposing sensitive plan/state contents.
- Interrupts and deadlines allow graceful OpenTofu cancellation and state-lock
  cleanup; child processes do not continue accidentally after wm exits.
- Stale-plan rejection and lifecycle plan guards continue to run before apply.

Use disposable fixtures for mutation tests. Read-only checks can inspect the live
workloads root, but this refactor does not require stopping or restarting live
workloads. If evaluation reveals unsupported flags, incompatible version gates
or insufficient signal control, retain the os/exec implementation behind the same
package boundary rather than maintaining a client-library fork.
