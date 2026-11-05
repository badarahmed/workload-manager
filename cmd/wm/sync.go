package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var scopeName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func scopeOf(address string) string {
	parts := strings.Split(address, ".")
	if len(parts) >= 4 && parts[0] == "module" && parts[2] == "module" {
		return parts[1] + "/" + strings.ReplaceAll(parts[3], "_", "-")
	}
	return ""
}
func changed(c planChange) bool {
	return len(c.Change.Actions) != 1 || c.Change.Actions[0] != "no-op" || c.Previous != ""
}
func valuesByAddress(root valueModule) map[string]map[string]any {
	result := map[string]map[string]any{}
	var visit func(valueModule)
	visit = func(m valueModule) {
		for _, r := range m.Resources {
			if r.Type == "wm_workload" {
				result[r.Address] = r.Values
			}
		}
		for _, child := range m.Children {
			visit(child)
		}
	}
	visit(root)
	return result
}
func addPlanned(items []workload, plan planData) []workload {
	seen := map[string]bool{}
	for _, w := range items {
		seen[w.Address] = true
	}
	for _, c := range plan.Changes {
		if c.Type != "wm_workload" || seen[c.Address] || c.Previous != "" || c.Change.After == nil {
			continue
		}
		var r recipe
		raw, _ := c.Change.After["resolved_recipe"].(string)
		_ = json.Unmarshal([]byte(raw), &r)
		node := r.Host
		if node == "" {
			node = "unassigned"
		}
		name := r.Name
		if name == "" {
			name = c.Address
		}
		kind := "app"
		if strings.HasPrefix(c.Address, "module.models.") {
			kind = "model"
		}
		desired, _ := c.Change.After["desired"].(string)
		items = append(items, workload{Address: c.Address, Node: node, Name: name, Kind: kind, Engine: r.Engine, HFModel: r.Model, HFRevision: r.Revision, Desired: desired, Version: "—", Status: "not adopted", Health: "—", Match: "unknown", Recipe: r, Unapplied: true})
	}
	return items
}
func applySync(items []workload, config, remote planData, configErr, remoteErr error, receipt func(string) bool) []workload {
	items = addPlanned(items, config)
	configChanged := map[string]bool{}
	remoteChanged := map[string]bool{}
	for _, c := range config.Changes {
		if c.Type == "wm_workload" && changed(c) {
			configChanged[c.Address] = true
			if c.Previous != "" {
				configChanged[c.Previous] = true
			}
		}
	}
	for _, c := range remote.Drift {
		if c.Type == "wm_workload" && changed(c) {
			remoteChanged[c.Address] = true
			if c.Previous != "" {
				remoteChanged[c.Previous] = true
			}
		}
	}
	outputOnlyChange := outputChanges(config) && len(configChanged) == 0
	observed := refreshedValues(remote)
	configured := valuesByAddress(config.Planned.Root)
	for i := range items {
		w := &items[i]
		w.Configured = w.Desired
		if value, ok := configured[w.Address]["desired"].(string); ok {
			w.Configured = value
		}
		for _, c := range config.Changes {
			if c.Address == w.Address {
				if c.Change.After == nil {
					w.Configured = "removed"
				} else if value, ok := c.Change.After["desired"].(string); ok {
					w.Configured = value
				}
			}
		}
		w.ConfigSync = "synced"
		w.RemoteSync = "synced"
		w.Sync = "synced"
		w.Action = "none"
		if configChanged[w.Address] || outputOnlyChange {
			w.ConfigSync = "changed"
		}
		if remoteChanged[w.Address] {
			w.RemoteSync = "drift"
		}
		if ready, ok := observed[w.Address]["ready"].(bool); ok && !ready {
			w.RemoteSync = "not-ready"
		}
		if w.Unapplied {
			w.RemoteSync = "not-adopted"
		} else if observed[w.Address] == nil {
			w.RemoteSync = "unknown"
		} else if _, ok := observed[w.Address]["ready"].(bool); !ok {
			w.RemoteSync = "unknown"
		}
		if configErr != nil {
			w.ConfigSync = "unknown"
			w.Configured = "unknown"
		}
		if remoteErr != nil || w.Error != "" {
			w.RemoteSync = "unknown"
		}
		if w.ConfigSync == "unknown" || w.RemoteSync == "unknown" {
			w.Sync = "unknown"
			w.Action = "inspect / plan"
			continue
		}
		if w.ConfigSync != "synced" || w.RemoteSync != "synced" || w.Match != "yes" {
			w.Sync = "plan"
			w.Action = "plan then apply"
			if receipt(w.Address) {
				w.Sync = "apply"
				w.Action = "apply"
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Node != items[j].Node {
			return items[i].Node < items[j].Node
		}
		return items[i].Address < items[j].Address
	})
	return items
}
func synchronize(ctx context.Context, root string, items []workload) ([]workload, error) {
	before, beforeErr := sourceDigest(root)
	_ = os.MkdirAll(filepath.Join(root, ".local"), 0700)
	dir, err := os.MkdirTemp(filepath.Join(root, ".local"), "wm-sync-")
	if err != nil {
		return applySync(items, planData{}, planData{}, err, err, func(string) bool { return false }), err
	}
	defer os.RemoveAll(dir)
	config, configErr := readOnlyPlan(ctx, root, filepath.Join(dir, "config.tfplan"), "-refresh=false")
	remote, remoteErr := readOnlyPlan(ctx, root, filepath.Join(dir, "remote.tfplan"), "-refresh-only")
	after, afterErr := sourceDigest(root)
	if beforeErr != nil || afterErr != nil || before != after {
		configErr = fmt.Errorf("configuration/state changed during comparison")
	}

	usable := func(address string) bool {
		for _, scope := range []string{"", resolvedScope(root, address)} {
			r, valid := validReceipt(root, scope)
			if !valid || r.Remote != operatorRemoteDigest(root, refreshedValues(remote), scope) {
				continue
			}
			data, err := inspectPlan(root, planPath(root, scope))
			if err != nil {
				continue
			}
			if scope == "" && outputChanges(data) {
				return true
			}
			for _, c := range data.Changes {
				if (c.Address == address || c.Previous == address) && changed(c) {
					return true
				}
			}
			for _, c := range data.Drift {
				if (c.Address == address || c.Previous == address) && changed(c) {
					return true
				}
			}
		}
		return false
	}
	var errSummary error
	if configErr != nil || remoteErr != nil {
		errSummary = fmt.Errorf("STATE SYNC is unknown: configuration or remote comparison failed; run wm plan for diagnostics")
	}
	return applySync(items, config, remote, configErr, remoteErr, usable), errSummary
}
func syncLabel(w workload) string {
	switch w.Sync {
	case "synced":
		return "✅ synced"
	case "plan":
		return "⛔️ PLAN → APPLY"
	case "apply":
		return "⛔️ APPLY"
	case "unknown":
		return "⛔️ UNKNOWN · wm plan"
	}
	switch w.Match {
	case "yes":
		return "✅"
	case "no":
		return "⛔️"
	}
	return "⛔️ unknown"
}

func outputChanges(plan planData) bool {
	for _, change := range plan.Outputs {
		if len(change.Actions) != 1 || change.Actions[0] != "no-op" {
			return true
		}
	}
	return false
}

func refreshedValues(plan planData) map[string]map[string]any {
	values := valuesByAddress(plan.Prior.Values.Root)
	if len(values) == 0 {
		values = valuesByAddress(plan.Planned.Root)
	}
	for _, change := range plan.Drift {
		if change.Type == "wm_workload" && change.Change.After != nil {
			values[change.Address] = change.Change.After
		}
	}
	return values
}
func refreshedDigest(plan planData, scope string) string {
	values := refreshedValues(plan)
	selected := map[string]map[string]any{}
	for address, attrs := range values {
		if scope != "" && scopeOf(address) != scope {
			continue
		}
		fields := map[string]any{}
		for _, key := range []string{"id", "desired", "ready", "repair_token", "recipe_digest"} {
			fields[key] = attrs[key]
		}
		selected[address] = fields
	}
	raw, _ := json.Marshal(selected)
	return digest(raw)
}

// Module labels need not equal folder names (e.g. app_name vs app-name).
func resolvedScope(root, address string) string {
	raw, err := os.ReadFile(filepath.Join(root, ".terraform/modules/modules.json"))
	if err != nil {
		return scopeOf(address)
	}
	var inventory struct {
		Modules []struct {
			Key string `json:"Key"`
			Dir string `json:"Dir"`
		} `json:"Modules"`
	}
	if json.Unmarshal(raw, &inventory) != nil {
		return scopeOf(address)
	}
	selected := ""
	length := 0
	for _, module := range inventory.Modules {
		prefix := "module." + strings.ReplaceAll(module.Key, ".", ".module.") + "."
		if module.Key == "" || !strings.HasPrefix(address, prefix) || len(prefix) <= length {
			continue
		}
		directory := module.Dir
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(root, directory)
		}
		relative, err := filepath.Rel(root, directory)
		if err != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) == 2 && (parts[0] == "apps" || parts[0] == "models") {
			selected = strings.Join(parts, "/")
			length = len(prefix)
		}
	}
	if selected != "" {
		return selected
	}
	return scopeOf(address)
}
func operatorRemoteDigest(root string, values map[string]map[string]any, scope string) string {
	selected := map[string]map[string]any{}
	for address, attrs := range values {
		if scope != "" && resolvedScope(root, address) != scope {
			continue
		}
		fields := map[string]any{}
		for _, key := range []string{"id", "desired", "ready", "repair_token", "recipe_digest"} {
			fields[key] = attrs[key]
		}
		selected[address] = fields
	}
	raw, _ := json.Marshal(selected)
	return digest(raw)
}
