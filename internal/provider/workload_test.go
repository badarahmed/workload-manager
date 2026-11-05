package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDecodeObservationRequiresRuntimeEvidence(t *testing.T) {
	for _, value := range []string{"", "{}", `{"converged":true}`, `{"converged":true,"runtime":{"id":"d3","status":"running"}}`, `{"runtime":{"id":"` + strings.Repeat("a", 64) + `"}}`} {
		if _, err := decodeObservation([]byte(value)); err == nil {
			t.Fatalf("accepted incomplete observation: %s", value)
		}
	}
	value := `{"converged":true,"runtime":{"id":"` + strings.Repeat("a", 64) + `","status":"running"}}`
	actual, err := decodeObservation([]byte(value))
	if err != nil || !actual.Converged {
		t.Fatalf("valid observation rejected: %v", err)
	}
}

func TestFailedApplyRetainsCapturedIdentityWithoutClaimingReadiness(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("b", 64)
	script := "#!/bin/sh\nif [ \"$4\" = observe ]; then\n" +
		"printf '%s\\n' '{\"converged\":false,\"runtime\":{\"id\":\"" + id + "\",\"status\":\"exited\"}}'\n" +
		"else\necho 'model unavailable' >&2\nexit 1\nfi\n"
	if err := os.WriteFile(filepath.Join(root, "node"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	w := workload{c: &client{root: root}}
	m := workloadModel{ID: types.StringUnknown(), Recipe: types.StringValue("/capture.json"), Digest: types.StringValue("digest"), Desired: types.StringValue("running"), Ready: types.BoolUnknown()}
	if err := w.apply(context.Background(), &m); err == nil {
		t.Fatal("expected runner failure")
	}
	if m.ID.ValueString() != id {
		t.Fatalf("lost adopted identity: %v", m.ID)
	}
	if !m.Ready.IsNull() || m.Repair.ValueString() != "incomplete" {
		t.Fatalf("unexpected failed-apply state: %+v", m)
	}
}

func TestDestroyUsesSnapshotAfterBindingRemoval(t *testing.T) {
	ctx := context.Background()
	m := sampleIntent(t)
	resolved, hash, err := resolveIntent(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	m.Resolved = types.StringValue(resolved)
	m.Digest = types.StringValue(hash)
	if err = os.Remove(m.Binding.ValueString()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	script := "#!/bin/sh\nif [ \"$7\" != stopped ]; then exit 8; fi\nprintf '%s\\n' '{\"converged\":true,\"runtime\":{\"id\":\"" + id + "\",\"status\":\"exited\"}}'\n"
	if err = os.WriteFile(filepath.Join(root, "node"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	w := workload{c: &client{root: root}}
	if err = w.stopSnapshot(ctx, &m); err != nil {
		t.Fatal(err)
	}
	if !m.Ready.ValueBool() || m.ID.ValueString() != id {
		t.Fatalf("unexpected stop state: %+v", m)
	}
	// An ordinary saved apply must still reject missing authoring inputs.
	if err = w.apply(ctx, &m); err == nil {
		t.Fatal("apply bypassed saved-plan validation")
	}
}
