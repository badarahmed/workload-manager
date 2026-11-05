package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// Exercise the protocol validation used by tofu validate, not just intent resolution.
func TestOptionalDiagnosticsPassesProtocolValidation(t *testing.T) {
	for name, diagnostics := range map[string]any{
		"omitted_block":    nil,
		"empty_block":      map[string]any{"logs": nil, "redact_patterns": nil},
		"explicit_opt_out": map[string]any{"logs": false, "redact_patterns": nil},
	} {
		t.Run(name, func(t *testing.T) {
			config := map[string]any{
				"id": nil, "host": "gpu-01", "binding": "/binding.json", "recipe": nil,
				"resolved_recipe": nil, "recipe_digest": nil, "desired": "running", "ready": nil, "repair_token": nil,
				"runtime":      map[string]any{"driver": "docker", "engine": "vllm-sr", "image": "pinned-image"},
				"model":        map[string]any{"source": "hf://org/model", "revision": "pinned-revision"},
				"requirements": map[string]any{"api": "systemone"},
				"recovery":     map[string]any{"restart": true, "startup_grace": "10m"},
				"diagnostics":  diagnostics,
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			server := providerserver.NewProtocol6(New())()
			response, err := server.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
				TypeName: "wm_workload", Config: &tfprotov6.DynamicValue{JSON: raw},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range response.Diagnostics {
				if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
					t.Fatalf("%s: %s", diagnostic.Summary, diagnostic.Detail)
				}
			}
		})
	}
}
