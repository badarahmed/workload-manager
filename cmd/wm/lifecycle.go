package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

type intentResource struct {
	Name, File        string
	Start, End        int
	Dependencies      []string
	ComplexReferences bool
}
type lifecycleSource struct {
	Files     map[string][]byte
	Modes     map[string]os.FileMode
	Resources map[string]intentResource
}

func loadLifecycle(root, scope string) (lifecycleSource, error) {
	source := lifecycleSource{Files: map[string][]byte{}, Modes: map[string]os.FileMode{}, Resources: map[string]intentResource{}}
	if scope == "" {
		return source, fmt.Errorf("start/stop/restart require a workload scope, e.g. models/d3")
	}
	if err := validateScope(root, scope); err != nil {
		return source, err
	}
	directory, err := filepath.EvalSymlinks(filepath.Join(root, scope))
	if err != nil {
		return source, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return source, err
	}
	if !strings.HasPrefix(directory, realRoot+string(os.PathSeparator)) {
		return source, fmt.Errorf("scope escapes workloads root")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return source, err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tf.json") {
			return source, fmt.Errorf("lifecycle editing supports native .tf files only")
		}
		if !strings.HasSuffix(entry.Name(), ".tf") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return source, err
		}
		if !info.Mode().IsRegular() {
			return source, fmt.Errorf("HCL source must be a regular file")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return source, err
		}
		file, diags := hclsyntax.ParseConfig(data, path, hcl.InitialPos)
		if diags.HasErrors() {
			return source, fmt.Errorf("invalid HCL in %s", entry.Name())
		}
		source.Files[path] = data
		source.Modes[path] = info.Mode().Perm()
		for _, block := range file.Body.(*hclsyntax.Body).Blocks {
			if block.Type != "resource" || len(block.Labels) != 2 || block.Labels[0] != "wm_workload" {
				continue
			}
			name := block.Labels[1]
			if _, exists := source.Resources[name]; exists {
				return source, fmt.Errorf("duplicate workload resource %s", name)
			}
			if block.Body.Attributes["count"] != nil || block.Body.Attributes["for_each"] != nil {
				return source, fmt.Errorf("lifecycle editing does not support count/for_each; edit desired intent manually")
			}
			desired := block.Body.Attributes["desired"]
			if desired == nil {
				return source, fmt.Errorf("%s must declare a literal desired attribute", name)
			}
			value, diags := desired.Expr.Value(nil)
			if diags.HasErrors() || value.IsNull() || !value.IsKnown() || value.Type() != cty.String || (value.AsString() != "running" && value.AsString() != "stopped") {
				return source, fmt.Errorf("%s desired must be a literal running/stopped string", name)
			}
			// An expression that evaluates to a string is not necessarily a literal; only
			// quoted string tokens without interpolation are safe to replace automatically.
			r := desired.Expr.Range()
			literal := string(data[r.Start.Byte:r.End.Byte])
			parsed, err := strconv.Unquote(literal)
			if err != nil || parsed != value.AsString() {
				return source, fmt.Errorf("%s desired must use a quoted literal", name)
			}
			item := intentResource{Name: name, File: path, Start: r.Start.Byte, End: r.End.Byte}
			deps := map[string]bool{}
			hclsyntax.VisitAll(block.Body, func(node hclsyntax.Node) hcl.Diagnostics {
				if expr, ok := node.(*hclsyntax.ScopeTraversalExpr); ok && (expr.Traversal.RootName() == "local" || expr.Traversal.RootName() == "module") {
					item.ComplexReferences = true
				}
				if expr, ok := node.(*hclsyntax.ScopeTraversalExpr); ok && len(expr.Traversal) >= 2 && expr.Traversal.RootName() == "wm_workload" {
					if dep, ok := expr.Traversal[1].(hcl.TraverseAttr); ok {
						deps[dep.Name] = true
					}
				}
				return nil
			})
			for dep := range deps {
				item.Dependencies = append(item.Dependencies, dep)
			}
			sort.Strings(item.Dependencies)
			source.Resources[name] = item
		}
	}
	if len(source.Resources) == 0 {
		return source, fmt.Errorf("scope has no direct wm_workload resources")
	}
	return source, nil
}
func lifecycleWaves(resources map[string]intentResource, stop bool) ([][]string, error) {
	remaining := map[string]bool{}
	for name, r := range resources {
		if len(resources) > 1 && r.ComplexReferences {
			return nil, fmt.Errorf("group lifecycle needs direct resource dependencies; indirect local/module references require manual intent and plan/apply")
		}
		remaining[name] = true
		for _, dep := range r.Dependencies {
			if _, ok := resources[dep]; !ok {
				return nil, fmt.Errorf("dependency %s is outside the selected workload scope", dep)
			}
		}
	}
	var waves [][]string
	for len(remaining) > 0 {
		var wave []string
		for name := range remaining {
			ready := true
			for _, dep := range resources[name].Dependencies {
				if remaining[dep] {
					ready = false
				}
			}
			if ready {
				wave = append(wave, name)
			}
		}
		if len(wave) == 0 {
			return nil, fmt.Errorf("cyclic workload dependencies")
		}
		sort.Strings(wave)
		waves = append(waves, wave)
		for _, name := range wave {
			delete(remaining, name)
		}
	}
	if stop {
		for i, j := 0, len(waves)-1; i < j; i, j = i+1, j-1 {
			waves[i], waves[j] = waves[j], waves[i]
		}
	}
	return waves, nil
}
func editWave(source lifecycleSource, names []string, desired string) (map[string][]byte, error) {
	if desired != "running" && desired != "stopped" {
		return nil, fmt.Errorf("invalid desired state")
	}
	byFile := map[string][]intentResource{}
	for _, name := range names {
		r, ok := source.Resources[name]
		if !ok {
			return nil, fmt.Errorf("unknown resource")
		}
		byFile[r.File] = append(byFile[r.File], r)
	}
	result := map[string][]byte{}
	for path, resources := range byFile {
		data := append([]byte(nil), source.Files[path]...)
		sort.Slice(resources, func(i, j int) bool { return resources[i].Start > resources[j].Start })
		for _, r := range resources {
			replacement := []byte(strconv.Quote(desired))
			data = append(append(append([]byte(nil), data[:r.Start]...), replacement...), data[r.End:]...)
		}
		if _, diags := hclsyntax.ParseConfig(data, path, hcl.InitialPos); diags.HasErrors() {
			return nil, fmt.Errorf("updated HCL is invalid")
		}
		result[path] = data
	}
	return result, nil
}
func atomicSource(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".wm-edit-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func writeIntent(root string, source lifecycleSource, edits map[string][]byte) error {
	for path, before := range source.Files {
		now, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(now, before) {
			return fmt.Errorf("HCL changed concurrently; command aborted before editing")
		}
	}
	backup := filepath.Join(root, ".local/wm/intent-backups", fmt.Sprint(time.Now().UnixNano()))
	if err := os.MkdirAll(backup, 0700); err != nil {
		return err
	}
	var paths []string
	for path := range edits {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if bytes.Equal(source.Files[path], edits[path]) {
			continue
		}
		if err := os.WriteFile(filepath.Join(backup, filepath.Base(path)), source.Files[path], 0600); err != nil {
			return err
		}
	}
	var written []string
	for _, path := range paths {
		if bytes.Equal(source.Files[path], edits[path]) {
			continue
		}
		if err := atomicSource(path, edits[path], source.Modes[path]); err != nil {
			for _, prior := range written {
				_ = atomicSource(prior, source.Files[prior], source.Modes[prior])
			}
			return err
		}
		written = append(written, path)
	}
	return nil
}
func lifecyclePlanGuard(plan planData, root, scope string, names []string) error {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	for _, c := range plan.Changes {
		if len(c.Change.Actions) == 1 && c.Change.Actions[0] == "no-op" {
			continue
		}
		parts := strings.Split(c.Address, ".")
		if c.Type != "wm_workload" || len(parts) < 2 || resolvedScope(root, c.Address) != scope || !allowed[parts[len(parts)-1]] || len(c.Change.Actions) != 1 || c.Change.Actions[0] != "update" {
			return fmt.Errorf("plan includes changes outside this lifecycle wave or requires adoption/replacement; review wm plan %s before continuing", scope)
		}
	}
	return nil
}
func lifecycle(root, action, scope string, verbose bool) int {
	source, err := loadLifecycle(root, scope)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if _, err = lifecycleWaves(source.Resources, false); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	lockDir := filepath.Join(root, ".local/wm")
	if err = os.MkdirAll(lockDir, 0700); err != nil {
		return 1
	}
	lock, err := os.OpenFile(filepath.Join(lockDir, "lifecycle.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return 1
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, "Another lifecycle command is active")
		return 1
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	phases := []string{"running"}
	if action == "stop" {
		phases = []string{"stopped"}
	}
	if action == "restart" {
		phases = []string{"stopped", "running"}
	}
	expectedFiles := source.Files
	for _, desired := range phases {
		waves, _ := lifecycleWaves(source.Resources, desired == "stopped")
		for _, names := range waves {
			current, err := loadLifecycle(root, scope)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if len(current.Files) != len(expectedFiles) {
				fmt.Fprintln(os.Stderr, "HCL changed during command")
				return 1
			}
			for path, expected := range expectedFiles {
				if !bytes.Equal(current.Files[path], expected) {
					fmt.Fprintln(os.Stderr, "HCL changed during command; resolve the pending intent before retrying")
					return 1
				}
			}
			if len(current.Resources) != len(source.Resources) {
				fmt.Fprintln(os.Stderr, "Workload graph changed during command")
				return 1
			}
			for name, resource := range source.Resources {
				now, ok := current.Resources[name]
				if !ok || strings.Join(now.Dependencies, "\x00") != strings.Join(resource.Dependencies, "\x00") {
					fmt.Fprintln(os.Stderr, "Dependency graph changed during command")
					return 1
				}
			}
			edits, err := editWave(current, names, desired)
			if err == nil {
				err = writeIntent(root, current, edits)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			for path, data := range edits {
				expectedFiles[path] = data
			}
			fmt.Fprintf(os.Stdout, "%s: desired=%s for %s; plan → apply\n", scope, desired, strings.Join(names, ", "))
			if code := operate(root, "plan", scope, verbose); code != 0 {
				return code
			}
			plan, err := inspectPlan(root, planPath(root, scope))
			if err == nil {
				err = lifecyclePlanGuard(plan, root, scope, names)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if _, valid := validReceipt(root, scope); !valid {
				fmt.Fprintln(os.Stderr, "Lifecycle plan became stale; command aborted")
				return 1
			}
			args := []string{"apply", planPath(root, scope)}
			if verbose {
				args = append(args, "--verbose")
			}
			if code := waitInteractive(wrapper(root, args...)); code != 0 {
				return code
			}
			_ = os.Remove(planPath(root, scope) + ".json")
		}
	}
	return 0
}
