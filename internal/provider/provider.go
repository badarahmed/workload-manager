package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"homelab/wm/internal/version"
	"os"
	"path/filepath"
)

type wmProvider struct{}
type providerConfig struct {
	Root types.String `tfsdk:"root"`
}
type client struct{ root string }

func New() provider.Provider { return &wmProvider{} }
func (p *wmProvider) Metadata(_ context.Context, _ provider.MetadataRequest, r *provider.MetadataResponse) {
	r.TypeName = "wm"
	r.Version = version.Version
}
func (p *wmProvider) Schema(_ context.Context, _ provider.SchemaRequest, r *provider.SchemaResponse) {
	r.Schema = schema.Schema{Attributes: map[string]schema.Attribute{"root": schema.StringAttribute{Required: true, Description: "Absolute wm checkout path containing agent dependencies."}}}
}
func (p *wmProvider) Configure(ctx context.Context, q provider.ConfigureRequest, r *provider.ConfigureResponse) {
	var c providerConfig
	r.Diagnostics.Append(q.Config.Get(ctx, &c)...)
	if r.Diagnostics.HasError() {
		return
	}
	if c.Root.IsUnknown() {
		r.Diagnostics.AddError("Unknown root", "wm root must be known before configuration")
		return
	}
	root := c.Root.ValueString()
	if !filepath.IsAbs(root) {
		r.Diagnostics.AddError("Invalid root", "root must be absolute")
		return
	}
	if _, err := os.Stat(filepath.Join(root, "agent/src/runner.ts")); err != nil {
		r.Diagnostics.AddError("Missing runner", err.Error())
		return
	}
	r.ResourceData = &client{root: root}
}
func (p *wmProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource { return &workload{} }}
}
func (p *wmProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }
