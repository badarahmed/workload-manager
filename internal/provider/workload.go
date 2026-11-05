package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

type workload struct{ c *client }
type workloadModel struct {
	ID           types.String `tfsdk:"id"`
	Host         types.String `tfsdk:"host"`
	Binding      types.String `tfsdk:"binding"`
	Runtime      types.Object `tfsdk:"runtime"`
	Model        types.Object `tfsdk:"model"`
	Requirements types.Object `tfsdk:"requirements"`
	Recovery     types.Object `tfsdk:"recovery"`
	Diagnostics  types.Object `tfsdk:"diagnostics"`
	Probe        types.Object `tfsdk:"probe"`
	Resolved     types.String `tfsdk:"resolved_recipe"`
	Recipe       types.String `tfsdk:"recipe"`
	Digest       types.String `tfsdk:"recipe_digest"`
	Desired      types.String `tfsdk:"desired"`
	Ready        types.Bool   `tfsdk:"ready"`
	Repair       types.String `tfsdk:"repair_token"`
}
type observation struct {
	Converged bool `json:"converged"`
	Runtime   struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"runtime"`
}

var containerIdentity = regexp.MustCompile(`^[a-f0-9]{64}$`)

// A successful process exit alone is not evidence of an observed workload.
func decodeObservation(data []byte) (observation, error) {
	var actual observation
	if err := json.Unmarshal(bytes.TrimSpace(data), &actual); err != nil {
		return actual, fmt.Errorf("invalid runner result: %w", err)
	}
	if !containerIdentity.MatchString(actual.Runtime.ID) || actual.Runtime.Status == "" {
		return observation{}, fmt.Errorf("runner result missing verified runtime identity/status")
	}
	return actual, nil
}

func (w *workload) Metadata(_ context.Context, q resource.MetadataRequest, r *resource.MetadataResponse) {
	r.TypeName = q.ProviderTypeName + "_workload"
}
func (w *workload) Schema(_ context.Context, _ resource.SchemaRequest, r *resource.SchemaResponse) {
	r.Schema = schema.Schema{Description: "Captured docker/vllm-sr workload reconciled by Pi Durable. Current prototype adopts an exact existing container; it does not delete containers or volumes.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"recipe":          schema.StringAttribute{Optional: true, Description: "Legacy captured recipe path. Use intent blocks and binding for new configurations."},
		"host":            schema.StringAttribute{Optional: true, Description: "Inventory identity of the target host."},
		"binding":         schema.StringAttribute{Optional: true, Description: "Companion JSON containing captured runtime identity and transport metadata; no desired settings."},
		"resolved_recipe": schema.StringAttribute{Computed: true, Sensitive: true, Description: "Internal applied recipe snapshot, generated from HCL and binding metadata."},
		"recipe_digest":   schema.StringAttribute{Computed: true},
		"desired":         schema.StringAttribute{Required: true, Description: "running or stopped"},
		"ready":           schema.BoolAttribute{Computed: true},
		"repair_token":    schema.StringAttribute{Computed: true},
	}, Blocks: intentBlocks()}
}
func (w *workload) Configure(_ context.Context, q resource.ConfigureRequest, r *resource.ConfigureResponse) {
	if q.ProviderData == nil {
		return
	}
	var ok bool
	w.c, ok = q.ProviderData.(*client)
	if !ok {
		r.Diagnostics.AddError("Invalid provider client", "Unexpected provider configuration")
	}
}
func digest(file string) (string, error) {
	if !filepath.IsAbs(file) {
		return "", fmt.Errorf("recipe must be an absolute path")
	}
	b, e := os.ReadFile(file)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func (w *workload) ModifyPlan(ctx context.Context, q resource.ModifyPlanRequest, r *resource.ModifyPlanResponse) {
	if q.Plan.Raw.IsNull() {
		return
	}
	var m workloadModel
	r.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if m.Desired.IsUnknown() || m.Recipe.IsUnknown() {
		return
	}
	if m.Desired.ValueString() != "running" && m.Desired.ValueString() != "stopped" {
		r.Diagnostics.AddError("Invalid desired state", "Use running or stopped")
		return
	}
	resolved, d, e := resolveIntent(ctx, m)
	if e != nil {
		r.Diagnostics.AddError("Recipe unavailable", e.Error())
		return
	}
	r.Diagnostics.Append(r.Plan.SetAttribute(ctx, path.Root("recipe_digest"), d)...)
	r.Diagnostics.Append(r.Plan.SetAttribute(ctx, path.Root("resolved_recipe"), resolved)...)
	r.Diagnostics.Append(r.Plan.SetAttribute(ctx, path.Root("repair_token"), "converged")...)
	ready := types.BoolUnknown()
	if !q.State.Raw.IsNull() {
		var prior workloadModel
		r.Diagnostics.Append(q.State.Get(ctx, &prior)...)
		previous := prior.Resolved.ValueString()
		if previous == "" && prior.Recipe.ValueString() != "" {
			if b, err := os.ReadFile(prior.Recipe.ValueString()); err == nil {
				previous = string(b)
			}
		}
		if err := validateCapturedUpdate(previous, resolved); err != nil {
			r.Diagnostics.AddError("Deployment change not supported yet", err.Error())
			return
		}
		if prior.Recipe.Equal(m.Recipe) && prior.Desired.Equal(m.Desired) && prior.Digest.ValueString() == d && prior.Repair.ValueString() == "converged" {
			ready = prior.Ready
		}
	}
	r.Diagnostics.Append(r.Plan.SetAttribute(ctx, path.Root("ready"), ready)...)
}
func (w *workload) run(ctx context.Context, m workloadModel, mode string) (observation, error) {
	var result observation
	if w.c == nil {
		return result, fmt.Errorf("provider not configured")
	}
	// Same desired recipe reattaches to the same durable operation directory after interruption.
	recipePath, recipeErr := w.recipeFile(m)
	if recipeErr != nil {
		return result, recipeErr
	}
	key := sha256.Sum256([]byte(recipePath + "\x00" + m.Digest.ValueString() + "\x00" + m.Desired.ValueString()))
	ctx, cancel := context.WithTimeout(ctx, 35*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--import", "tsx", "agent/src/runner.ts", mode, recipePath, m.Digest.ValueString(), m.Desired.ValueString(), hex.EncodeToString(key[:16]))
	cmd.Dir = w.c.root
	// Let the runner abort the durable task and close SQLite before forcing exit.
	// SSH disconnect still leaves a potentially unknown remote outcome.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 15 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if stdout.Len() > 0 {
		var decodeErr error
		result, decodeErr = decodeObservation(stdout.Bytes())
		if decodeErr != nil {
			return result, decodeErr
		}
	}
	if err != nil {
		return result, fmt.Errorf("runner: %w: %s", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return result, fmt.Errorf("runner exited without an observation")
	}
	return result, nil
}
func (w *workload) Read(ctx context.Context, q resource.ReadRequest, r *resource.ReadResponse) {
	var m workloadModel
	r.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if m.Resolved.IsNull() || m.Resolved.ValueString() == "" {
		currentDigest, digestErr := digest(m.Recipe.ValueString())
		if digestErr != nil {
			r.Diagnostics.AddError("Recipe unavailable; state retained", digestErr.Error())
			return
		}
		if currentDigest != m.Digest.ValueString() {
			// The working recipe is proposed configuration, not the previously
			// applied runtime manifest. Do not use it to refresh the old identity.
			m.Ready = types.BoolNull()
			m.Repair = types.StringValue("unverified-recipe-change")
			r.Diagnostics.Append(r.State.Set(ctx, &m)...)
			r.Diagnostics.AddWarning("Recipe changed; runtime refresh deferred", "The captured recipe differs from the applied digest. Readiness is unverified; the next plan will include the recipe change and apply will validate the proposed exact runtime before acting.")
			return
		}
	}
	actual, e := w.run(ctx, m, "observe")
	if e != nil {
		r.Diagnostics.AddError("Observation failed; state retained", e.Error())
		return
	}
	m.Ready = types.BoolValue(actual.Converged)
	token := "converged"
	if !actual.Converged {
		token = "drift-" + actual.Runtime.Status
	}
	m.Repair = types.StringValue(token)
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (w *workload) apply(ctx context.Context, m *workloadModel) error {
	return w.reconcile(ctx, m, true)
}

// Destroy consumes the saved snapshot; it must not depend on authoring files.
func (w *workload) stopSnapshot(ctx context.Context, m *workloadModel) error {
	if m.Resolved.IsNull() || m.Resolved.IsUnknown() || m.Resolved.ValueString() == "" {
		return fmt.Errorf("saved recipe snapshot unavailable; restore the legacy recipe and refresh state before destroy")
	}
	m.Desired = types.StringValue("stopped")
	return w.reconcile(ctx, m, false)
}

func (w *workload) reconcile(ctx context.Context, m *workloadModel, validatePlan bool) error {
	m.Ready = types.BoolNull()
	m.Repair = types.StringValue("incomplete")
	if validatePlan && !m.Resolved.IsNull() && m.Resolved.ValueString() != "" {
		_, current, e := resolveIntent(ctx, *m)
		if e != nil {
			return e
		}
		if current != m.Digest.ValueString() {
			return fmt.Errorf("binding or recipe changed after plan; run tofu plan again")
		}
	}
	// This prototype adopts an existing exact container. Capture its identity
	// before the agent can mutate it, including on Create with no prior state.
	before, observeErr := w.run(ctx, *m, "observe")
	if observeErr != nil {
		return observeErr
	}
	m.ID = types.StringValue(before.Runtime.ID)
	actual, e := w.run(ctx, *m, "apply")
	if actual.Runtime.ID != "" {
		m.ID = types.StringValue(actual.Runtime.ID)
	}
	if actual.Runtime.ID != "" {
		m.Ready = types.BoolValue(actual.Converged)
	}
	if actual.Converged {
		m.Repair = types.StringValue("converged")
	} else {
		m.Repair = types.StringValue("incomplete")
	}
	if e != nil {
		return e
	}
	if !actual.Converged {
		return fmt.Errorf("intent not reached")
	}
	return nil
}
func (w *workload) Create(ctx context.Context, q resource.CreateRequest, r *resource.CreateResponse) {
	var m workloadModel
	r.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	e := w.apply(ctx, &m)
	if !m.ID.IsUnknown() && !m.ID.IsNull() {
		r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	}
	if e != nil {
		r.Diagnostics.AddError("Reconciliation incomplete", e.Error())
	}
}
func (w *workload) Update(ctx context.Context, q resource.UpdateRequest, r *resource.UpdateResponse) {
	var m workloadModel
	r.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	e := w.apply(ctx, &m)
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	if e != nil {
		r.Diagnostics.AddError("Reconciliation incomplete", e.Error())
	}
}
func (w *workload) Delete(ctx context.Context, q resource.DeleteRequest, r *resource.DeleteResponse) {
	var m workloadModel
	r.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if e := w.stopSnapshot(ctx, &m); e != nil {
		r.Diagnostics.AddError("Stop failed; state retained", e.Error())
	}
}

var _ resource.ResourceWithModifyPlan = (*workload)(nil)
var _ resource.ResourceWithConfigure = (*workload)(nil)

// Import adopts only the exact captured identity after a successful read-only observation.
func (w *workload) ImportState(ctx context.Context, q resource.ImportStateRequest, r *resource.ImportStateResponse) {
	d, e := digest(q.ID)
	if e != nil {
		r.Diagnostics.AddError("Invalid import recipe", e.Error())
		return
	}
	m := workloadModel{Recipe: types.StringValue(q.ID), Digest: types.StringValue(d), Desired: types.StringValue("running")}
	m.Runtime = types.ObjectNull(map[string]attr.Type{"driver": types.StringType, "engine": types.StringType, "image": types.StringType})
	m.Model = types.ObjectNull(map[string]attr.Type{"source": types.StringType, "revision": types.StringType})
	m.Requirements = types.ObjectNull(map[string]attr.Type{"api": types.StringType})
	m.Recovery = types.ObjectNull(map[string]attr.Type{"restart": types.BoolType, "startup_grace": types.StringType})
	actual, e := w.run(ctx, m, "observe")
	if e != nil {
		r.Diagnostics.AddError("Import observation failed", e.Error())
		return
	}
	m.ID = types.StringValue(actual.Runtime.ID)
	m.Ready = types.BoolValue(actual.Converged)
	token := "converged"
	if !actual.Converged {
		token = "drift-" + actual.Runtime.Status
	}
	m.Repair = types.StringValue(token)
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}

var _ resource.ResourceWithImportState = (*workload)(nil)
