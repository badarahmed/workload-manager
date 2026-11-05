package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func stringObject(values map[string]string) types.Object {
	attributes := map[string]attr.Type{}
	result := map[string]attr.Value{}
	for k, v := range values {
		attributes[k] = types.StringType
		result[k] = types.StringValue(v)
	}
	return types.ObjectValueMust(attributes, result)
}

func sampleIntent(t *testing.T) workloadModel {
	t.Helper()
	root := t.TempDir()
	binding := filepath.Join(root, "d3.json")
	b := map[string]any{"schemaVersion": 1, "name": "d3", "host": "gpu-01", "ssh": map[string]string{"hostname": "192.0.2.10", "user": "operator", "hostKeyAlias": "gpu-01"}, "container": "d3", "observedContainerId": strings.Repeat("a", 64), "remoteStateDirectory": "/models/d3", "endpoint": "http://127.0.0.1:8899", "discoveryPath": "/v1/systemone/models", "inferencePath": "/v1/systemone"}
	data, _ := json.Marshal(b)
	if e := os.WriteFile(binding, data, 0600); e != nil {
		t.Fatal(e)
	}
	return workloadModel{
		Host: types.StringValue("gpu-01"), Binding: types.StringValue(binding), Desired: types.StringValue("running"),
		Runtime:      stringObject(map[string]string{"driver": "docker", "engine": "vllm-sr", "image": "ghcr.io/example/engine@sha256:" + strings.Repeat("b", 64)}),
		Model:        stringObject(map[string]string{"source": "hf://org/model", "revision": strings.Repeat("c", 40)}),
		Requirements: stringObject(map[string]string{"api": "systemone"}),
		Recovery:     types.ObjectValueMust(map[string]attr.Type{"restart": types.BoolType, "startup_grace": types.StringType}, map[string]attr.Value{"restart": types.BoolValue(true), "startup_grace": types.StringValue("10m")}),
	}
}

func TestIntentUsesHCLAndNormalizesHFURI(t *testing.T) {
	m := sampleIntent(t)
	resolved, digest, e := resolveIntent(context.Background(), m)
	if e != nil {
		t.Fatal(e)
	}
	var recipe map[string]any
	if e = json.Unmarshal([]byte(resolved), &recipe); e != nil {
		t.Fatal(e)
	}
	if recipe["model"] != "org/model" || recipe["recovery"].(map[string]any)["startupGraceSeconds"] != float64(600) {
		t.Fatalf("incorrect desired recipe: %s", resolved)
	}
	if len(digest) != 64 {
		t.Fatal("missing content hash")
	}
	m.Recovery = types.ObjectValueMust(map[string]attr.Type{"restart": types.BoolType, "startup_grace": types.StringType}, map[string]attr.Value{"restart": types.BoolValue(false), "startup_grace": types.StringValue("10m")})
	changed, hash, e := resolveIntent(context.Background(), m)
	if e != nil {
		t.Fatal(e)
	}
	if hash == digest {
		t.Fatal("policy change did not change recipe digest")
	}
	if e = validateCapturedUpdate(resolved, changed); e != nil {
		t.Fatalf("policy update should be allowed: %v", e)
	}
}

func TestBindingCannotOverrideDesiredConfiguration(t *testing.T) {
	m := sampleIntent(t)
	b, _ := os.ReadFile(m.Binding.ValueString())
	var data map[string]any
	json.Unmarshal(b, &data)
	data["model"] = "hidden-model"
	b, _ = json.Marshal(data)
	os.WriteFile(m.Binding.ValueString(), b, 0600)
	if _, _, e := resolveIntent(context.Background(), m); e == nil {
		t.Fatal("binding overrode HCL")
	}
}

func TestSavedSnapshotSurvivesBindingEditsButApplyRejectsStalePlan(t *testing.T) {
	ctx := context.Background()
	m := sampleIntent(t)
	resolved, hash, e := resolveIntent(ctx, m)
	if e != nil {
		t.Fatal(e)
	}
	m.Resolved = types.StringValue(resolved)
	m.Digest = types.StringValue(hash)
	w := workload{c: &client{root: t.TempDir()}}
	os.WriteFile(m.Binding.ValueString(), []byte(`{"schemaVersion":1}`), 0600)
	file, e := w.recipeFile(m)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(file)
	if string(b) != resolved {
		t.Fatal("snapshot changed with binding")
	}
	if e = w.apply(ctx, &m); e == nil {
		t.Fatal("changed binding was accepted by saved apply")
	}
}

func TestUnsupportedRuntimeChangesFailExplicitly(t *testing.T) {
	m := sampleIntent(t)
	before, _, _ := resolveIntent(context.Background(), m)
	m.Model = stringObject(map[string]string{"source": "hf://org/another", "revision": strings.Repeat("c", 40)})
	after, _, _ := resolveIntent(context.Background(), m)
	if e := validateCapturedUpdate(before, after); e == nil {
		t.Fatal("unimplemented model switch was accepted")
	}
	m.Model = stringObject(map[string]string{"source": "https://huggingface.co/org/model", "revision": strings.Repeat("c", 40)})
	if _, _, e := resolveIntent(context.Background(), m); e == nil {
		t.Fatal("invalid source scheme accepted")
	}
}

func TestDiagnosticsPolicyIsResolvedFromHCL(t *testing.T) {
	m := sampleIntent(t)
	m.Diagnostics = types.ObjectValueMust(map[string]attr.Type{"logs": types.BoolType, "redact_patterns": types.ListType{ElemType: types.StringType}}, map[string]attr.Value{"logs": types.BoolValue(false), "redact_patterns": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("internal-label")})})
	resolved, _, err := resolveIntent(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	var recipe map[string]any
	if err = json.Unmarshal([]byte(resolved), &recipe); err != nil {
		t.Fatal(err)
	}
	policy := recipe["diagnostics"].(map[string]any)
	if policy["logs"] != false || policy["redactPatterns"].([]any)[0] != "internal-label" {
		t.Fatalf("wrong policy: %v", policy)
	}
}

func TestDiagnosticsLogsDefaultsWhenOmitted(t *testing.T) {
	for _, omittedBlock := range []bool{true, false} {
		t.Run(fmt.Sprintf("omitted_block_%t", omittedBlock), func(t *testing.T) {
			m := sampleIntent(t)
			if !omittedBlock {
				m.Diagnostics = types.ObjectValueMust(map[string]attr.Type{"logs": types.BoolType, "redact_patterns": types.ListType{ElemType: types.StringType}}, map[string]attr.Value{"logs": types.BoolNull(), "redact_patterns": types.ListNull(types.StringType)})
			}
			resolved, _, err := resolveIntent(context.Background(), m)
			if err != nil {
				t.Fatal(err)
			}
			var recipe map[string]any
			if err = json.Unmarshal([]byte(resolved), &recipe); err != nil {
				t.Fatal(err)
			}
			policy, present := recipe["diagnostics"]
			if omittedBlock && present {
				t.Fatal("omitted policy must preserve existing recipe digest")
			}
			if !omittedBlock && policy.(map[string]any)["logs"] != true {
				t.Fatal("omitted logs must default to true")
			}
		})
	}
}

func TestApplicationIntentOmitsModelAndPreservesCapturedIdentity(t *testing.T) {
	m := sampleIntent(t)
	m.Model = types.ObjectNull(map[string]attr.Type{"source": types.StringType, "revision": types.StringType})
	m.Runtime = stringObject(map[string]string{"driver": "docker", "engine": "application", "image": "example/app@sha256:" + strings.Repeat("b", 64)})
	m.Requirements = stringObject(map[string]string{"api": "docker-health"})
	raw, _ := os.ReadFile(m.Binding.ValueString())
	var b map[string]any
	json.Unmarshal(raw, &b)
	delete(b, "ssh")
	b["local"] = map[string]string{"hostname": "controller", "dockerHost": "unix:///tmp/fixture.sock"}
	b["observedImageId"] = "sha256:" + strings.Repeat("b", 64)
	data, _ := json.Marshal(b)
	os.WriteFile(m.Binding.ValueString(), data, 0600)
	resolved, _, err := resolveIntent(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	var recipe map[string]any
	json.Unmarshal([]byte(resolved), &recipe)
	if _, exists := recipe["model"]; exists {
		t.Fatal("application has model configuration")
	}
	m.Model = stringObject(map[string]string{"source": "hf://org/model", "revision": strings.Repeat("c", 40)})
	if _, _, err = resolveIntent(context.Background(), m); err == nil {
		t.Fatal("accepted application with model")
	}
}

func TestExplicitProbePolicyRequiresApplicationAndIsNotBindingMetadata(t *testing.T) {
	m := sampleIntent(t)
	attrs := map[string]attr.Type{"location": types.StringType, "format": types.StringType, "field": types.StringType, "equals": types.StringType}
	m.Probe = types.ObjectValueMust(attrs, map[string]attr.Value{"location": types.StringValue("host"), "format": types.StringValue("json"), "field": types.StringValue("status"), "equals": types.StringValue("healthy")})
	if _, _, err := resolveIntent(context.Background(), m); err == nil {
		t.Fatal("model accepted application probe")
	}
}
