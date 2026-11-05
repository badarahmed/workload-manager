# wm status and operator reference

See [wm operator CLI](wm-operator.md) for the current commands and three-way
STATE SYNC contract. The process-only state-match behavior described by earlier
versions has been replaced.

## Application, engine and model versions

The default table has four columns: `WORKLOAD`, `VERSION`, `STATUS` and
`STATE SYNC`. Workload names use scope labels such as `apps/langfuse:web` and
`models/d3`. Status combines running/health information without redundant columns;
mismatches show the requested state next to ⛔️. Model versions include the runtime,
model name and eight-character HF commit.

`-v` (or `--verbose`) shows the detailed table, including `ENGINE`,
`VERSION / IMAGE`, `HF MODEL`, applied desired state and separate health:

- Model engine comes from the applied profile (`vllm-sr`, vLLM, etc.). For running
  Python model runtimes, a fixed metadata-only query reads installed distribution
  versions. An engine may expose `vllm-srun` metadata while having no installed
  `vllm` distribution. The CLI does not invent a vLLM version from its engine name.
- HF model identity and full commit revision come from the applied recipe's pinned
  Hugging Face metadata. The table shows `hf://org/repo@<12-character commit>`;
  JSON retains full `hfModel` and `hfRevision`. This is the configured pin, not a
  new artifact-content verification or Hugging Face API lookup.
- Running ClickHouse, PostgreSQL, Redis and MinIO use fixed `--version` queries,
  avoiding misleading inherited base-image labels. Other images prefer the OCI
  version label, then a version-looking tag on the same repository. `tag:` marks
  that provenance. If no useful release metadata exists, the image digest is shown.
  Exporter's Python image tag describes its runtime image, not a separately
  versioned exporter source release.

JSON includes `engine`, `version`, `versionSource` and live `imageId` as well as HF
fields. Version sources distinguish `application-command`, `python-package`,
`oci-label`, `image-tag` and `image-digest`. Metadata lookup failures do not alter
process status or trigger lifecycle operations; available image metadata/digest is
used instead. No exec is attempted for stopped containers. The existing deadline,
output bounds, SSH verification and captured-identity checks apply to metadata calls.

The verbose `HF MODEL` column is omitted for app-only node groups.

Verbose affects table presentation only; JSON retains all metadata in either mode.
