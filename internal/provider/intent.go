package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type runtimeConfig struct {
	Driver types.String `tfsdk:"driver"`
	Engine types.String `tfsdk:"engine"`
	Image  types.String `tfsdk:"image"`
}
type modelConfig struct {
	Source   types.String `tfsdk:"source"`
	Revision types.String `tfsdk:"revision"`
}
type requirementsConfig struct {
	API types.String `tfsdk:"api"`
}
type recoveryConfig struct {
	Restart      types.Bool   `tfsdk:"restart"`
	StartupGrace types.String `tfsdk:"startup_grace"`
}

func intentBlocks() map[string]schema.Block {
	return map[string]schema.Block{
		"probe": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{
			"location": schema.StringAttribute{Optional: true}, "format": schema.StringAttribute{Optional: true}, "field": schema.StringAttribute{Optional: true}, "equals": schema.StringAttribute{Optional: true},
		}},
		"diagnostics": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{
			"logs":            schema.BoolAttribute{Optional: true, Description: "Allow sanitized raw logs into model context and durable history. Defaults to true when omitted. Redaction is best effort."},
			"redact_patterns": schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Additional literal strings removed before model submission; do not put credentials here."},
		}},
		"runtime": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{
			"driver": schema.StringAttribute{Required: true}, "engine": schema.StringAttribute{Required: true}, "image": schema.StringAttribute{Required: true, Description: "Engine Docker image pinned by sha256 digest."},
		}},
		"model": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{
			"source": schema.StringAttribute{Optional: true, Description: "Hugging Face source URI: hf://organization/repository."}, "revision": schema.StringAttribute{Optional: true},
		}},
		"requirements": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{"api": schema.StringAttribute{Required: true}}},
		"recovery": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{
			"restart": schema.BoolAttribute{Required: true}, "startup_grace": schema.StringAttribute{Required: true, Description: "Time to allow loading before corrective restart, e.g. 10m. Readiness may succeed sooner."},
		}},
	}
}

var hfSource = regexp.MustCompile(`^hf://[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
var imageReference = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[a-f0-9]{64}$`)
var modelRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)

func validateCapturedUpdate(previous, proposed string) error {
	if previous == "" {
		return nil
	}
	var before, after map[string]any
	if e := json.Unmarshal([]byte(previous), &before); e != nil {
		return e
	}
	if e := json.Unmarshal([]byte(proposed), &after); e != nil {
		return e
	}
	for _, key := range []string{"host", "driver", "engine", "imageDigestFromHostDocumentation", "model", "revision", "observedContainerId"} {
		if !reflect.DeepEqual(before[key], after[key]) {
			return fmt.Errorf("changing %s requires deployment replacement, which the captured-container adapter does not yet implement; current workload is unchanged", key)
		}
	}
	return nil
}

// HCL is desired intent. The binding holds only the captured runtime identity
// and host/probe addresses. Never allow its contents to override HCL.
func resolveIntent(ctx context.Context, m workloadModel) (string, string, error) {
	if !m.Recipe.IsNull() && m.Recipe.ValueString() != "" {
		if !m.Binding.IsNull() || !m.Host.IsNull() || !m.Runtime.IsNull() || !m.Model.IsNull() || !m.Requirements.IsNull() || !m.Recovery.IsNull() || !m.Diagnostics.IsNull() || !m.Probe.IsNull() {
			return "", "", fmt.Errorf("use either legacy recipe or host/binding with intent blocks, not both")
		}
		b, e := os.ReadFile(m.Recipe.ValueString())
		if e != nil {
			return "", "", e
		}
		if !filepath.IsAbs(m.Recipe.ValueString()) {
			return "", "", fmt.Errorf("recipe path must be absolute")
		}
		h := sha256.Sum256(b)
		return string(b), hex.EncodeToString(h[:]), nil
	}
	if m.Host.IsNull() || m.Binding.IsNull() || m.Runtime.IsNull() || m.Requirements.IsNull() || m.Recovery.IsNull() {
		return "", "", fmt.Errorf("host, binding, runtime, model, requirements and recovery are required")
	}
	if m.Host.ValueString() == "" || !filepath.IsAbs(m.Binding.ValueString()) {
		return "", "", fmt.Errorf("host must be nonempty and binding must be an absolute path")
	}
	var runtime runtimeConfig
	var model modelConfig
	var requirements requirementsConfig
	var recovery recoveryConfig
	for _, d := range []struct {
		object types.Object
		target any
	}{{m.Runtime, &runtime}, {m.Requirements, &requirements}, {m.Recovery, &recovery}} {
		if diagnostics := d.object.As(ctx, d.target, basetypes.ObjectAsOptions{}); diagnostics.HasError() {
			return "", "", fmt.Errorf("intent blocks must be fully known: %s", diagnostics.Errors()[0].Detail())
		}
	}
	if !m.Model.IsNull() {
		if diagnostics := m.Model.As(ctx, &model, basetypes.ObjectAsOptions{}); diagnostics.HasError() {
			return "", "", fmt.Errorf("model must be known")
		}
	}
	application := runtime.Engine.ValueString() == "application"
	if runtime.Driver.ValueString() != "docker" || (!application && runtime.Engine.ValueString() != "vllm-sr") {
		return "", "", fmt.Errorf("supported runtime: docker with vllm-sr or application")
	}
	if !imageReference.MatchString(runtime.Image.ValueString()) {
		return "", "", fmt.Errorf("runtime.image must be pinned by repository manifest digest")
	}
	if application {
		if !m.Model.IsNull() {
			return "", "", fmt.Errorf("application workloads must omit model")
		}
		if requirements.API.ValueString() != "docker-health" && requirements.API.ValueString() != "http-health" {
			return "", "", fmt.Errorf("application requires docker-health or http-health")
		}
	} else {
		if m.Model.IsNull() || !hfSource.MatchString(model.Source.ValueString()) || !modelRevision.MatchString(model.Revision.ValueString()) {
			return "", "", fmt.Errorf("model requires hf://organization/repository and pinned revision")
		}
		for _, part := range strings.Split(strings.TrimPrefix(model.Source.ValueString(), "hf://"), "/") {
			if part == "." || part == ".." {
				return "", "", fmt.Errorf("invalid model path")
			}
		}
		if requirements.API.ValueString() != "systemone" {
			return "", "", fmt.Errorf("vllm-sr requires systemone")
		}
	}
	grace, e := time.ParseDuration(recovery.StartupGrace.ValueString())
	if e != nil || grace < time.Minute || grace > 30*time.Minute || grace%time.Second != 0 {
		return "", "", fmt.Errorf("recovery.startup_grace must be whole seconds between 1m and 30m")
	}
	if recovery.Restart.IsNull() || recovery.Restart.IsUnknown() {
		return "", "", fmt.Errorf("recovery.restart must be known")
	}
	b, e := os.ReadFile(m.Binding.ValueString())
	if e != nil {
		return "", "", e
	}
	var binding map[string]any
	if e = json.Unmarshal(b, &binding); e != nil {
		return "", "", fmt.Errorf("invalid binding JSON: %w", e)
	}
	allowed := map[string]bool{"schemaVersion": true, "name": true, "host": true, "ssh": true, "local": true, "container": true, "observedContainerId": true, "observedImageId": true, "remoteStateDirectory": true, "endpoint": true, "discoveryPath": true, "inferencePath": true, "monitoring": true}
	for key := range binding {
		if !allowed[key] {
			return "", "", fmt.Errorf("binding field %q is not runtime identity metadata; configure desired values in HCL", key)
		}
	}
	if binding["schemaVersion"] != float64(1) {
		return "", "", fmt.Errorf("binding schemaVersion must be 1")
	}
	if binding["host"] != m.Host.ValueString() {
		return "", "", fmt.Errorf("host does not match captured binding; adopt a binding for the intended host")
	}
	id, _ := binding["observedContainerId"].(string)
	if !containerIdentity.MatchString(id) {
		return "", "", fmt.Errorf("binding must identify an exact container")
	}
	required := []string{"name", "container"}
	if !application {
		required = append(required, "remoteStateDirectory", "endpoint", "discoveryPath", "inferencePath")
	} else {
		if binding["local"] == nil || binding["observedImageId"] == nil || binding["ssh"] != nil {
			return "", "", fmt.Errorf("application adoption requires local transport and captured image ID")
		}
		if requirements.API.ValueString() == "http-health" {
			required = append(required, "endpoint", "inferencePath")
		}
	}
	for _, key := range required {
		if s, ok := binding[key].(string); !ok || s == "" {
			return "", "", fmt.Errorf("binding missing %s", key)
		}
	}
	if value, ok := binding["observedImageId"]; ok {
		id, valid := value.(string)
		if !valid || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(id) {
			return "", "", fmt.Errorf("invalid observedImageId")
		}
	}
	if !m.Probe.IsNull() {
		if !application || requirements.API.ValueString() != "http-health" {
			return "", "", fmt.Errorf("probe requires an http-health application")
		}
		var probe struct {
			Location types.String `tfsdk:"location"`
			Format   types.String `tfsdk:"format"`
			Field    types.String `tfsdk:"field"`
			Equals   types.String `tfsdk:"equals"`
		}
		if d := m.Probe.As(ctx, &probe, basetypes.ObjectAsOptions{}); d.HasError() {
			return "", "", fmt.Errorf("probe must be known")
		}
		for _, value := range []types.String{probe.Location, probe.Format, probe.Field, probe.Equals} {
			if value.IsNull() || value.IsUnknown() {
				return "", "", fmt.Errorf("probe requires location, format, field and equals")
			}
		}
		if probe.Location.ValueString() != "host" && probe.Location.ValueString() != "container" {
			return "", "", fmt.Errorf("probe location must be host or container")
		}
		format := probe.Format.ValueString()
		if format != "json" && format != "text" && format != "prometheus" {
			return "", "", fmt.Errorf("unsupported probe format")
		}
		if !regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`).MatchString(probe.Field.ValueString()) || len(probe.Field.ValueString()) > 128 || len(probe.Equals.ValueString()) > 128 {
			return "", "", fmt.Errorf("invalid probe comparison")
		}
		binding["probe"] = map[string]any{"location": probe.Location.ValueString(), "format": format, "field": probe.Field.ValueString(), "equals": probe.Equals.ValueString()}
	}
	if !m.Diagnostics.IsNull() {
		var policy struct {
			Logs     types.Bool `tfsdk:"logs"`
			Patterns types.List `tfsdk:"redact_patterns"`
		}
		if d := m.Diagnostics.As(ctx, &policy, basetypes.ObjectAsOptions{}); d.HasError() {
			return "", "", fmt.Errorf("diagnostics must be known")
		}
		if policy.Logs.IsUnknown() || policy.Patterns.IsUnknown() {
			return "", "", fmt.Errorf("diagnostics must be known")
		}
		patterns := []string{}
		if !policy.Patterns.IsNull() {
			if d := policy.Patterns.ElementsAs(ctx, &patterns, false); d.HasError() {
				return "", "", fmt.Errorf("invalid redaction patterns")
			}
		}
		if len(patterns) > 20 {
			return "", "", fmt.Errorf("at most 20 redaction patterns allowed")
		}
		for _, pattern := range patterns {
			if len(pattern) == 0 || len(pattern) > 256 {
				return "", "", fmt.Errorf("redaction patterns must be 1..256 bytes")
			}
		}
		binding["diagnostics"] = map[string]any{"logs": policy.Logs.IsNull() || policy.Logs.ValueBool(), "redactPatterns": patterns}
	}
	delete(binding, "schemaVersion")
	binding["driver"] = runtime.Driver.ValueString()
	binding["engine"] = runtime.Engine.ValueString()
	binding["mode"] = "engine"
	if application {
		binding["mode"] = "application"
	}
	binding["imageDigestFromHostDocumentation"] = runtime.Image.ValueString()
	if !application {
		binding["model"] = strings.TrimPrefix(model.Source.ValueString(), "hf://")
		binding["revision"] = model.Revision.ValueString()
	}
	binding["recovery"] = map[string]any{"allowRestart": recovery.Restart.ValueBool(), "startupGraceSeconds": int(grace / time.Second)}
	binding["requirements"] = map[string]any{"api": requirements.API.ValueString()}
	resolved, e := json.Marshal(binding)
	if e != nil {
		return "", "", e
	}
	h := sha256.Sum256(resolved)
	return string(resolved), hex.EncodeToString(h[:]), nil
}

func (w *workload) recipeFile(m workloadModel) (string, error) {
	if m.Resolved.IsNull() || m.Resolved.IsUnknown() || m.Resolved.ValueString() == "" {
		return m.Recipe.ValueString(), nil
	}
	content := []byte(m.Resolved.ValueString())
	h := sha256.Sum256(content)
	if hex.EncodeToString(h[:]) != m.Digest.ValueString() {
		return "", fmt.Errorf("resolved recipe digest mismatch")
	}
	directory := filepath.Join(w.c.root, ".local", "recipes")
	if e := os.MkdirAll(directory, 0700); e != nil {
		return "", e
	}
	file := filepath.Join(directory, m.Digest.ValueString()+".json")
	if b, e := os.ReadFile(file); e == nil {
		if string(b) != string(content) {
			return "", fmt.Errorf("cached recipe corrupted")
		}
		return file, nil
	}
	// Atomic rename prevents observation of partial JSON by another runner.
	temp, e := os.CreateTemp(directory, "recipe-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(temp.Name())
	if _, e = temp.Write(content); e != nil {
		temp.Close()
		return "", e
	}
	if e = temp.Close(); e != nil {
		return "", e
	}
	if e = os.Rename(temp.Name(), file); e != nil {
		return "", e
	}
	return file, nil
}
