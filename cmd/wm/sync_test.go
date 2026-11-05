package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestThreeWayDecisions(t *testing.T) {
	for _, test := range []struct {
		name                                  string
		config, drift, ready, receipt, failed bool
		want                                  string
	}{
		{"all aligned", false, false, true, false, false, "synced"},
		{"filesystem edit", true, false, true, false, false, "plan"},
		{"filesystem edit planned", true, false, true, true, false, "apply"},
		{"remote drift", false, true, true, false, false, "plan"},
		{"unready unchanged process", false, false, false, false, false, "plan"},
		{"failed probe", false, false, false, false, true, "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := fixture()
			w.Address = "module.apps.module.grafana.wm_workload.service"
			w.Match = "yes"
			config, remote := planData{}, planData{}
			c := planChange{Address: w.Address, Type: "wm_workload"}
			c.Change.Actions = []string{"update"}
			if test.config {
				config.Changes = []planChange{c}
			}
			if test.drift {
				remote.Drift = []planChange{c}
			}
			// Exercise decoded OpenTofu JSON, including computed readiness.
			remote.Planned.Root.Resources = append(remote.Planned.Root.Resources, struct {
				Address string         `json:"address"`
				Type    string         `json:"type"`
				Values  map[string]any `json:"values"`
			}{w.Address, "wm_workload", map[string]any{"ready": test.ready}})
			var err error
			if test.failed {
				err = errors.New("unreachable")
			}
			result := applySync([]workload{w}, config, remote, nil, err, func(string) bool { return test.receipt })
			if result[0].Sync != test.want {
				t.Fatalf("got %s: %+v", result[0].Sync, result[0])
			}
		})
	}
}
func TestSourceFingerprintIncludesBindingsAndState(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "apps/grafana/bindings"), 0700)
	file := filepath.Join(root, "apps/grafana/bindings/service.json")
	os.WriteFile(file, []byte(`{"id":"first"}`), 0600)
	first, _ := sourceDigest(root)
	os.WriteFile(file, []byte(`{"id":"other"}`), 0600)
	second, _ := sourceDigest(root)
	if first == second {
		t.Fatal("binding edit invisible")
	}
	os.WriteFile(filepath.Join(root, "terraform.tfstate"), []byte(`{"serial":2}`), 0600)
	third, _ := sourceDigest(root)
	if third == second {
		t.Fatal("state edit invisible")
	}
}
func TestOperatorArgumentsAnywhere(t *testing.T) {
	for _, args := range [][]string{{"apply", "models/d3", "-v"}, {"-v", "-apply", "models/d3"}, {"--apply", "-v", "models/d3"}} {
		action, flags, err := operatorArgs(args)
		if err != nil || action != "apply" || len(flags) != 2 || flags[0] != "-v" || flags[1] != "models/d3" {
			t.Fatal(action, flags, err)
		}
	}
	if _, _, err := operatorArgs([]string{"plan", "apply"}); err == nil {
		t.Fatal("conflicting actions accepted")
	}
}

func TestRefreshOnlyJSONUsesPriorStateAndDriftAfter(t *testing.T) {
	var plan planData
	raw := []byte(`{"prior_state":{"values":{"root_module":{"resources":[{"address":"module.apps.module.grafana.wm_workload.service","type":"wm_workload","values":{"id":"captured","desired":"running","ready":true,"repair_token":"converged","recipe_digest":"digest"}}]}}},"resource_drift":[{"address":"module.apps.module.grafana.wm_workload.service","type":"wm_workload","change":{"actions":["update"],"after":{"id":"captured","desired":"running","ready":false,"repair_token":"drift-running","recipe_digest":"digest"}}}]}`)
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	values := refreshedValues(plan)
	if values["module.apps.module.grafana.wm_workload.service"]["ready"] != false {
		t.Fatal("refresh drift was hidden by prior readiness")
	}
	if refreshedDigest(plan, "apps/grafana") == remoteDigestScoped(plan.Prior.Values.Root, "apps/grafana") {
		t.Fatal("drift did not invalidate saved plan fingerprint")
	}
}

func TestOutputPropagationDoesNotMarkUnrelatedWorkloadsDirty(t *testing.T) {
	a, b := fixture(), fixture()
	a.Address = "module.apps.module.grafana.wm_workload.service"
	b.Address = "module.apps.module.hindsight.wm_workload.service"
	a.Match = "yes"
	b.Match = "yes"
	var config, remote planData
	_ = json.Unmarshal([]byte(`{"resource_changes":[{"address":"module.apps.module.grafana.wm_workload.service","type":"wm_workload","change":{"actions":["update"],"after":{"desired":"stopped"}}}],"output_changes":{"apps":{"actions":["update"]}}}`), &config)
	_ = json.Unmarshal([]byte(`{"prior_state":{"values":{"root_module":{"resources":[{"address":"module.apps.module.grafana.wm_workload.service","type":"wm_workload","values":{"ready":true}},{"address":"module.apps.module.hindsight.wm_workload.service","type":"wm_workload","values":{"ready":true}}]}}}}`), &remote)
	result := applySync([]workload{a, b}, config, remote, nil, nil, func(string) bool { return false })
	for _, w := range result {
		if w.Address == b.Address && w.Sync != "synced" {
			t.Fatal("unrelated app marked dirty")
		}
		if w.Address == a.Address && (w.Sync != "plan" || w.Configured != "stopped") {
			t.Fatal("configured edit hidden")
		}
	}
}

func TestScopeUsesModuleDirectoryNotGuessedName(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".terraform/modules"), 0700)
	os.WriteFile(filepath.Join(root, ".terraform/modules/modules.json"), []byte(`{"Modules":[{"Key":"apps.app_label","Dir":"apps/actual_folder"}]}`), 0600)
	if got := resolvedScope(root, "module.apps.module.app_label.wm_workload.service"); got != "apps/actual_folder" {
		t.Fatal(got)
	}
}
func TestPlanReceiptRejectsSourceAndPlanChanges(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.tf"), []byte("initial"), 0600)
	path := planPath(root, "apps/grafana")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("plan"), 0600)
	source, _ := sourceDigest(root)
	r := planReceipt{Scope: "apps/grafana", Source: source, PlanHash: digest([]byte("plan"))}
	raw, _ := json.Marshal(r)
	os.WriteFile(path+".json", raw, 0600)
	if _, valid := validReceipt(root, "apps/grafana"); !valid {
		t.Fatal("fresh receipt rejected")
	}
	os.WriteFile(path, []byte("changed plan"), 0600)
	if _, valid := validReceipt(root, "apps/grafana"); valid {
		t.Fatal("changed plan accepted")
	}
	os.WriteFile(path, []byte("plan"), 0600)
	os.WriteFile(filepath.Join(root, "main.tf"), []byte("changed source"), 0600)
	if _, valid := validReceipt(root, "apps/grafana"); valid {
		t.Fatal("changed source accepted")
	}
	if _, valid := validReceipt(root, "models/d3"); valid {
		t.Fatal("wrong scope accepted")
	}
}
