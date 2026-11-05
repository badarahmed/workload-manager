package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type planChange struct {
	Address  string `json:"address"`
	Previous string `json:"previous_address"`
	Type     string `json:"type"`
	Change   struct {
		Actions []string       `json:"actions"`
		Before  map[string]any `json:"before"`
		After   map[string]any `json:"after"`
	} `json:"change"`
}
type valueModule struct {
	Resources []struct {
		Address string         `json:"address"`
		Type    string         `json:"type"`
		Values  map[string]any `json:"values"`
	} `json:"resources"`
	Children []valueModule `json:"child_modules"`
}
type planData struct {
	Outputs map[string]struct {
		Actions []string `json:"actions"`
	} `json:"output_changes"`
	Changes []planChange `json:"resource_changes"`
	Drift   []planChange `json:"resource_drift"`
	Planned struct {
		Root valueModule `json:"root_module"`
	} `json:"planned_values"`
	Prior struct {
		Values struct {
			Root valueModule `json:"root_module"`
		} `json:"values"`
	} `json:"prior_state"`
}
type planReceipt struct {
	Scope    string `json:"scope"`
	Source   string `json:"source"`
	Remote   string `json:"remote"`
	PlanHash string `json:"planHash"`
	Created  string `json:"created"`
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func sourceDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".local" || d.Name() == ".terraform" || d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasSuffix(path, ".tf") || strings.HasSuffix(path, ".tf.json") || strings.HasSuffix(path, ".tfvars") || strings.HasSuffix(path, ".tfvars.json") || (strings.HasSuffix(path, ".json") && (strings.HasPrefix(rel, "apps/") || strings.HasPrefix(rel, "models/"))) || rel == "terraform.tfstate" {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "%s\x00%s\x00", rel, data)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	var environment []string
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "TF_VAR_") || strings.HasPrefix(value, "TF_CLI_ARGS") || strings.HasPrefix(value, "TF_DATA_DIR=") || strings.HasPrefix(value, "TF_WORKSPACE=") || strings.HasPrefix(value, "WM_ROOT=") {
			environment = append(environment, value)
		}
	}
	sort.Strings(environment)
	for _, value := range environment {
		fmt.Fprintln(hash, value)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func remoteDigest(root valueModule) string { return remoteDigestScoped(root, "") }
func remoteDigestScoped(root valueModule, scope string) string {
	values := map[string]map[string]any{}
	var visit func(valueModule)
	visit = func(m valueModule) {
		for _, r := range m.Resources {
			if r.Type == "wm_workload" && (scope == "" || scopeOf(r.Address) == scope) {
				fields := map[string]any{}
				for _, key := range []string{"id", "desired", "ready", "repair_token", "recipe_digest"} {
					fields[key] = r.Values[key]
				}
				values[r.Address] = fields
			}
		}
		for _, child := range m.Children {
			visit(child)
		}
	}
	visit(root)
	raw, _ := json.Marshal(values)
	return digest(raw)
}
func wrapper(root string, args ...string) *exec.Cmd {
	cmd := exec.Command(filepath.Join(root, "tofu"), args...)
	cmd.Dir = root
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "WM_HIDE_DEV_WARNING=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "WM_HIDE_DEV_WARNING=1")
	return cmd
}
func waitInteractive(cmd *exec.Cmd) int {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// The foreground terminal sends SIGINT to child and parent. Keep the CLI alive
	// until OpenTofu has finished its own graceful cancellation/state handling.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-signals:
				if s, ok := sig.(syscall.Signal); ok {
					_ = syscall.Kill(-cmd.Process.Pid, s)
				}
			case <-done:
				return
			}
		}
	}()
	defer func() { signal.Stop(signals); close(done) }()
	if err := cmd.Wait(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return e.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
func planPath(root, scope string) string {
	name := "all"
	if scope != "" {
		name = strings.ReplaceAll(scope, "/", "--")
	}
	return filepath.Join(root, ".local/wm/plans", name+".tfplan")
}
func validateScope(root, scope string) error {
	if scope == "" {
		return nil
	}
	parts := strings.Split(scope, "/")
	if len(parts) != 2 || (parts[0] != "apps" && parts[0] != "models") || !scopeName.MatchString(parts[1]) {
		return fmt.Errorf("scope must be apps/NAME or models/NAME")
	}
	info, err := os.Stat(filepath.Join(root, scope))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("workload folder does not exist")
	}
	return nil
}
func inspectPlan(root, path string) (planData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base := wrapper(root, "show", "-json", path)
	cmd := exec.CommandContext(ctx, base.Path, base.Args[1:]...)
	cmd.Dir = base.Dir
	cmd.Env = base.Env
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	// Plan JSON can include credentials from other providers. Keep it in memory only.
	raw, err := cmd.Output()
	if err != nil {
		return planData{}, fmt.Errorf("cannot inspect OpenTofu plan")
	}
	var result planData
	err = json.Unmarshal(raw, &result)
	return result, err
}
func savePlan(root, scope string, verbose bool) int {
	path := planPath(root, scope)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	before, err := sourceDigest(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.Remove(path + ".json")
	args := []string{"plan", "-out=" + path, "--scope", scope}
	if verbose {
		args = append(args, "--verbose")
	}
	if code := waitInteractive(wrapper(root, args...)); code != 0 {
		return code
	}
	after, err := sourceDigest(root)
	if err != nil || before != after {
		fmt.Fprintln(os.Stderr, "Configuration/state changed during planning; run wm plan again")
		return 1
	}
	data, err := inspectPlan(root, path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	receipt := planReceipt{scope, after, operatorRemoteDigest(root, valuesByAddress(data.Prior.Values.Root), scope), digest(raw), time.Now().UTC().Format(time.RFC3339)}
	encoded, _ := json.Marshal(receipt)
	if err = os.WriteFile(path+".json", encoded, 0600); err != nil {
		return 1
	}
	fmt.Fprintln(os.Stdout, "\nPlan saved. Apply with: wm apply "+scope)
	return 0
}
func validReceipt(root, scope string) (planReceipt, bool) {
	path := planPath(root, scope)
	raw, err := os.ReadFile(path + ".json")
	if err != nil {
		return planReceipt{}, false
	}
	var r planReceipt
	if json.Unmarshal(raw, &r) != nil || r.Scope != scope {
		return r, false
	}
	current, err := sourceDigest(root)
	if err != nil || current != r.Source {
		return r, false
	}
	plan, err := os.ReadFile(path)
	return r, err == nil && digest(plan) == r.PlanHash
}
func operate(root, action, scope string, verbose bool) int {
	if err := validateScope(root, scope); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if code := waitInteractive(wrapper(root, "get")); code != 0 {
		return code
	}
	if action == "plan" {
		return savePlan(root, scope, verbose)
	}
	r, valid := validReceipt(root, scope)
	if valid {
		dir, err := os.MkdirTemp(filepath.Join(root, ".local"), "wm-apply-check-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		remote, err := readOnlyPlan(ctx, root, filepath.Join(dir, "remote.tfplan"), "-refresh-only")
		cancel()
		os.RemoveAll(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		_, stillValid := validReceipt(root, scope)
		valid = stillValid && r.Remote == operatorRemoteDigest(root, refreshedValues(remote), scope)
	}
	if !valid {
		fmt.Fprintln(os.Stdout, "No current plan for this scope; generating one.")
		if code := savePlan(root, scope, verbose); code != 0 {
			return code
		}
	}
	args := []string{"apply", planPath(root, scope)}
	if verbose {
		args = append(args, "--verbose")
	}
	code := waitInteractive(wrapper(root, args...))
	if code == 0 {
		_ = os.Remove(planPath(root, scope) + ".json")
	}
	return code
}
func readOnlyPlan(ctx context.Context, root, path, mode string) (planData, error) {
	cmd := wrapper(root, "plan", "-input=false", "-lock-timeout=5s", mode, "-out="+path, "-no-color")
	cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	cmd.Dir = root
	cmd.Env = wrapper(root).Env
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return planData{}, fmt.Errorf("OpenTofu comparison failed; run wm plan for diagnostics")
	}
	return inspectPlan(root, path)
}

// Accept flags before/after subcommands/scopes; support both wm plan and wm -plan.
func operatorArgs(args []string) (string, []string, error) {
	action := "status"
	selected := false
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		value := args[i]
		if value == "plan" || value == "apply" || value == "status" || value == "start" || value == "stop" || value == "restart" || value == "-plan" || value == "--plan" || value == "-apply" || value == "--apply" {
			chosen := strings.TrimLeft(value, "-")
			if selected && chosen != action {
				return "", nil, fmt.Errorf("choose one operator command")
			}
			action = chosen
			selected = true
			continue
		}
		if strings.HasPrefix(value, "-") {
			flags = append(flags, value)
			if value == "-root" || value == "--root" || value == "-color" || value == "--color" || value == "-timeout" || value == "--timeout" {
				i++
				if i >= len(args) {
					return "", nil, fmt.Errorf("missing flag value")
				}
				flags = append(flags, args[i])
			}
		} else {
			positionals = append(positionals, value)
		}
	}
	return action, append(flags, positionals...), nil
}
