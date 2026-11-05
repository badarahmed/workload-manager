package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type imageMetadata struct {
	Version string   `json:"version"`
	Tags    []string `json:"tags"`
}

const imageFormat = `{"version":{{with .Config.Labels}}{{json (index . "org.opencontainers.image.version")}}{{else}}""{{end}},"tags":{{json .RepoTags}}}`
const packageScript = `import json,importlib.metadata
result={}
for name in ['vllm','vllm-sr','vllm-srun','sglang','omlx']:
 try: result[name]=importlib.metadata.version(name)
 except importlib.metadata.PackageNotFoundError: pass
print(json.dumps(result))`

var imageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var releaseTag = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)*(?:[-+._][a-zA-Z0-9._+-]+)?$`)

func short(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
func repository(ref string) string {
	ref = strings.SplitN(ref, "@", 2)[0]
	if colon := strings.LastIndex(ref, ":"); colon > strings.LastIndex(ref, "/") {
		ref = ref[:colon]
	}
	return ref
}
func chooseVersion(meta imageMetadata, reference, id string) (string, string) {
	if version := strings.TrimSpace(meta.Version); version != "" {
		return version, "oci-label"
	}
	repo := repository(reference)
	var tags []string
	for _, tag := range meta.Tags {
		if repository(tag) != repo {
			continue
		}
		colon := strings.LastIndex(tag, ":")
		if colon <= strings.LastIndex(tag, "/") {
			continue
		}
		value := tag[colon+1:]
		if releaseTag.MatchString(value) {
			tags = append(tags, value)
		}
	}
	// More specific release tags win over floating major/minor aliases.
	sort.Slice(tags, func(i, j int) bool {
		if len(tags[i]) != len(tags[j]) {
			return len(tags[i]) > len(tags[j])
		}
		return tags[i] < tags[j]
	})
	if len(tags) > 0 {
		return "tag:" + tags[0], "image-tag"
	}
	if imageID.MatchString(id) {
		return "sha256:" + short(strings.TrimPrefix(id, "sha256:")), "image-digest"
	}
	return "—", "unknown"
}
func metadataCommand(ctx context.Context, r recipe, args []string, value any) bool {
	cmd, err := commandFor(ctx, r, args)
	if err != nil {
		return false
	}
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	return cmd.Run() == nil && !stdout.overflow && json.Unmarshal(stdout.Bytes(), value) == nil
}
func enrichVersions(ctx context.Context, w *workload, actual inspection) {
	w.ImageID = actual.Image
	if !imageID.MatchString(actual.Image) {
		return
	}
	var meta imageMetadata
	_ = metadataCommand(ctx, w.Recipe, []string{"image", "inspect", actual.Image, "--format", imageFormat}, &meta)
	w.Version, w.VersionSource = chooseVersion(meta, w.Recipe.ImageReference, actual.Image)
	if actual.Running && w.Kind == "app" {
		if args, pattern := applicationVersionCommand(w.Recipe.ImageReference); args != nil {
			cmd, err := commandFor(ctx, w.Recipe, append([]string{"exec", w.Recipe.ID}, args...))
			if err == nil {
				var out, stderr limitedBuffer
				cmd.Stdout = &out
				cmd.Stderr = &stderr
				if cmd.Run() == nil && !out.overflow {
					if version := extractVersion(out.String(), pattern); version != "" {
						w.Version = version
						w.VersionSource = "application-command"
					}
				}
			}
		}
	}
	if w.Kind != "model" || !actual.Running {
		return
	}
	// Fixed metadata-only query, not arbitrary exec or model inference.
	packages := map[string]string{}
	if !metadataCommand(ctx, w.Recipe, []string{"exec", w.Recipe.ID, "python3", "-c", packageScript}, &packages) {
		return
	}
	if version := packages[w.Recipe.Engine]; version != "" {
		w.Version = version
		w.VersionSource = "python-package"
	}
	if w.Recipe.Engine == "vllm-sr" && packages["vllm-sr"] == "" && packages["vllm-srun"] != "" {
		w.Version = "vllm-srun " + packages["vllm-srun"]
		w.VersionSource = "python-package"
	}
	if w.Recipe.Engine == "vllm-sr" && packages["vllm"] != "" {
		if packages["vllm-sr"] != "" {
			w.Version = fmt.Sprintf("%s (vllm %s)", packages["vllm-sr"], packages["vllm"])
		} else {
			w.Version = "vllm " + packages["vllm"]
		}
		w.VersionSource = "python-package"
	}
}
func displayEngine(w workload) string {
	if w.Kind == "model" && w.Engine != "" {
		return w.Engine
	}
	return "—"
}
func displayModel(w workload) string {
	if w.HFModel == "" {
		return "—"
	}
	model := w.HFModel
	if !strings.HasPrefix(model, "hf://") {
		model = "hf://" + model
	}
	if w.HFRevision != "" {
		model += "@" + short(w.HFRevision)
	}
	return model
}

func applicationVersionCommand(reference string) ([]string, string) {
	repo := strings.TrimPrefix(repository(reference), "docker.io/library/")
	switch repo {
	case "clickhouse/clickhouse-server":
		return []string{"clickhouse", "--version"}, `\bversion\s+([0-9][a-zA-Z0-9.+_-]*)`
	case "postgres":
		return []string{"postgres", "--version"}, `PostgreSQL\)\s+([0-9.]+)`
	case "redis":
		return []string{"redis-server", "--version"}, `\bv=([0-9][a-zA-Z0-9.+_-]*)`
	case "cgr.dev/chainguard/minio":
		return []string{"minio", "--version"}, `\bversion\s+(RELEASE\.[0-9TZ:-]+)`
	}
	return nil, ""
}
func extractVersion(text, pattern string) string {
	match := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}
