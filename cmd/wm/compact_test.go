package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompactViewKeepsOnlyDistinctSignal(t *testing.T) {
	w := fixture()
	w.Address = "module.apps.module.langfuse.wm_workload.langfuse_web"
	w.Version = "4.71.0"
	w.Status = "running"
	w.Health = "healthy"
	w.Match = "yes"
	var compact, verbose bytes.Buffer
	render(&compact, []workload{w}, false, false)
	render(&verbose, []workload{w}, false, true)
	if !strings.Contains(compact.String(), "apps/langfuse:web") || !strings.Contains(compact.String(), "healthy") || !strings.Contains(compact.String(), "✅") {
		t.Fatal(compact.String())
	}
	if strings.Contains(compact.String(), "APPLIED") || strings.Contains(compact.String(), "HF MODEL") {
		t.Fatal("detailed columns leaked into default")
	}
	if !strings.Contains(verbose.String(), "APPLIED") || strings.Contains(verbose.String(), "HF MODEL") {
		t.Fatal("verbose columns missing")
	}
	w.Match = "no"
	compact.Reset()
	render(&compact, []workload{w}, false, false)
	if !strings.Contains(compact.String(), "(want running)") {
		t.Fatal("mismatch lost desired state")
	}
}
func TestCompactModelIncludesRevisionAndRuntime(t *testing.T) {
	w := fixture()
	w.Kind = "model"
	w.Address = "module.models.module.d3.wm_workload.d3"
	w.Version = "vllm-srun 0.2.0"
	w.HFModel = "example-org/example-model"
	w.HFRevision = "bbbbbbbbbbbb"
	if compactName(w) != "models/d3" || compactVersion(w) != "vllm-srun 0.2.0 · example-model@bbbbbbbb" {
		t.Fatal(compactName(w), compactVersion(w))
	}
	w.Error = "unreachable"
	if compactStatus(w) != "unknown" {
		t.Fatal("error masked")
	}
}

func TestHFColumnOnlyInModelGroups(t *testing.T) {
	app := fixture()
	app.Node = "app-node"
	model := fixture()
	model.Node = "model-node"
	model.Kind = "model"
	var output bytes.Buffer
	render(&output, []workload{app, model}, false, true)
	sections := strings.Split(output.String(), "model-node")
	if len(sections) != 2 || strings.Contains(sections[0], "HF MODEL") || !strings.Contains(sections[1], "HF MODEL") {
		t.Fatal(output.String())
	}
}
