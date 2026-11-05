package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"text/template"
)

func TestVersionSourcesAndRegistryPorts(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	for _, test := range []struct {
		name                 string
		meta                 imageMetadata
		ref, version, source string
	}{
		{"OCI preferred", imageMetadata{Version: "4.71.0", Tags: []string{"registry:5000/app:latest"}}, "registry:5000/app@" + id, "4.71.0", "oci-label"},
		{"release tag", imageMetadata{Tags: []string{"registry:5000/app:latest", "registry:5000/app:4", "registry:5000/app:4.71.0", "other/app:99.0.0"}}, "registry:5000/app@" + id, "tag:4.71.0", "image-tag"},
		{"floating tag fallback", imageMetadata{Tags: []string{"example/app:latest"}}, "example/app@" + id, "sha256:aaaaaaaaaaaa", "image-digest"},
		{"no metadata", imageMetadata{}, "example/app@" + id, "sha256:aaaaaaaaaaaa", "image-digest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			v, source := chooseVersion(test.meta, test.ref, id)
			if v != test.version || source != test.source {
				t.Fatalf("%s (%s)", v, source)
			}
		})
	}
}
func TestHFModelRevisionAndEngineDisplay(t *testing.T) {
	w := fixture()
	w.Kind = "model"
	w.Engine = "vllm-sr"
	w.HFModel = "org/model"
	w.HFRevision = strings.Repeat("b", 40)
	if displayEngine(w) != "vllm-sr" || displayModel(w) != "hf://org/model@bbbbbbbbbbbb" {
		t.Fatal("incorrect model metadata")
	}
	raw, _ := json.Marshal(w)
	if !strings.Contains(string(raw), strings.Repeat("b", 40)) {
		t.Fatal("JSON lost full HF revision")
	}
	w.Kind = "app"
	if displayEngine(w) != "—" {
		t.Fatal("app shown as model engine")
	}
}
func TestImageMetadataTemplateWithNoLabels(t *testing.T) {
	tmpl, err := template.New("image").Funcs(template.FuncMap{"json": func(value any) string { raw, _ := json.Marshal(value); return string(raw) }}).Parse(imageFormat)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = tmpl.Execute(&out, map[string]any{"Config": map[string]any{"Labels": nil}, "RepoTags": []string{"app:1.2.3"}}); err != nil {
		t.Fatal(err)
	}
	var meta imageMetadata
	if err = json.Unmarshal(out.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.Tags) != 1 || meta.Version != "" {
		t.Fatal(meta)
	}
}

func TestApplicationVersionCommandsDoNotMistakeBaseImageForProduct(t *testing.T) {
	for _, test := range []struct{ image, output, want string }{
		{"clickhouse/clickhouse-server@sha256:abc", "ClickHouse local version 25.12.11.4 (official build).", "25.12.11.4"},
		{"postgres@sha256:abc", "postgres (PostgreSQL) 17.11 (Debian build)", "17.11"},
		{"redis@sha256:abc", "Redis server v=7.4.12 sha=00000000", "7.4.12"},
		{"cgr.dev/chainguard/minio@sha256:abc", "minio version RELEASE.2026-10-23T20-06-51Z (commit-id=abc)", "RELEASE.2026-10-23T20-06-51Z"},
	} {
		args, pattern := applicationVersionCommand(test.image)
		if len(args) != 2 || args[1] != "--version" || extractVersion(test.output, pattern) != test.want {
			t.Fatal(test.image)
		}
	}
	if args, _ := applicationVersionCommand("arbitrary/image@sha256:abc"); args != nil {
		t.Fatal("arbitrary execution enabled")
	}
}
