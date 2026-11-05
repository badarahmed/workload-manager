// wm reports live container state for workloads recorded in local OpenTofu state.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"homelab/wm/internal/version"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"
)

type target struct {
	Hostname       string `json:"hostname"`
	User           string `json:"user"`
	HostKeyAlias   string `json:"hostKeyAlias"`
	KnownHostsFile string `json:"knownHostsFile"`
}
type recipe struct {
	Name           string  `json:"name"`
	Engine         string  `json:"engine"`
	Driver         string  `json:"driver"`
	Model          string  `json:"model"`
	Revision       string  `json:"revision"`
	ImageReference string  `json:"imageDigestFromHostDocumentation"`
	Host           string  `json:"host"`
	Container      string  `json:"container"`
	ID             string  `json:"observedContainerId"`
	Image          string  `json:"observedImageId"`
	SSH            *target `json:"ssh"`
	Local          *struct {
		Hostname   string `json:"hostname"`
		DockerHost string `json:"dockerHost"`
	} `json:"local"`
}
type workload struct {
	Address       string `json:"address"`
	Sync          string `json:"stateSync"`
	Configured    string `json:"configuredDesired"`
	ConfigSync    string `json:"configSync"`
	RemoteSync    string `json:"remoteSync"`
	Action        string `json:"action"`
	Unapplied     bool   `json:"unapplied,omitempty"`
	Engine        string `json:"engine"`
	Version       string `json:"version"`
	VersionSource string `json:"versionSource"`
	HFModel       string `json:"hfModel,omitempty"`
	HFRevision    string `json:"hfRevision,omitempty"`
	ImageID       string `json:"imageId,omitempty"`
	Node          string `json:"node"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Desired       string `json:"desired"`
	Status        string `json:"status"`
	Health        string `json:"health"`
	Match         string `json:"match"`
	Error         string `json:"error,omitempty"`
	Recipe        recipe `json:"-"`
}
type inspection struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Image   string  `json:"image"`
	Status  string  `json:"status"`
	Running bool    `json:"running"`
	Health  *string `json:"health"`
}

var identity = regexp.MustCompile(`^[a-f0-9]{64}$`)
var sshIdentity = regexp.MustCompile(`^[a-zA-Z0-9._:@-]+$`)

const inspectFormat = `{"id":{{json .Id}},"name":{{json .Name}},"image":{{json .Image}},"status":{{json .State.Status}},"running":{{json .State.Running}},"health":{{with index .State "Health"}}{{json .Status}}{{else}}null{{end}}}`

func inventory(data []byte) ([]workload, error) {
	var state struct {
		Resources []struct {
			Module    string `json:"module"`
			Type      string `json:"type"`
			Name      string `json:"name"`
			Instances []struct {
				Index      json.RawMessage `json:"index_key"`
				Attributes struct {
					Desired  string `json:"desired"`
					Resolved string `json:"resolved_recipe"`
				} `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid OpenTofu state JSON")
	}
	result := make([]workload, 0)
	for _, resource := range state.Resources {
		if resource.Type != "wm_workload" {
			continue
		}
		for _, instance := range resource.Instances {
			var r recipe
			if instance.Attributes.Resolved == "" {
				return nil, fmt.Errorf("%s: saved recipe missing; refresh legacy state first", resource.Name)
			}
			if err := json.Unmarshal([]byte(instance.Attributes.Resolved), &r); err != nil {
				return nil, fmt.Errorf("%s: invalid saved recipe", resource.Name)
			}
			if !identity.MatchString(r.ID) || r.Host == "" || r.Container == "" {
				return nil, fmt.Errorf("%s: incomplete captured identity", resource.Name)
			}
			address := resource.Type + "." + resource.Name
			if resource.Module != "" {
				address = resource.Module + "." + address
			}
			if len(instance.Index) > 0 {
				address += "[" + string(instance.Index) + "]"
			}
			kind := "app"
			if strings.HasPrefix(resource.Module, "module.models.") {
				kind = "model"
			}
			result = append(result, workload{Address: address, Node: r.Host, Kind: kind, Name: r.Name, Engine: r.Engine, Version: "—", HFModel: r.Model, HFRevision: r.Revision, Desired: instance.Attributes.Desired, Status: "unknown", Health: "—", Match: "unknown", Recipe: r})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Node != result[j].Node {
			return result[i].Node < result[j].Node
		}
		return result[i].Address < result[j].Address
	})
	return result, nil
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func command(ctx context.Context, r recipe) (*exec.Cmd, error) {
	return commandFor(ctx, r, []string{"inspect", "--format", inspectFormat, r.ID})
}
func commandFor(ctx context.Context, r recipe, argv []string) (*exec.Cmd, error) {
	if r.Local != nil && r.SSH == nil {
		host, err := os.Hostname()
		if err != nil || host != r.Local.Hostname {
			return nil, fmt.Errorf("local binding belongs to another controller")
		}
		if !strings.HasPrefix(r.Local.DockerHost, "unix:///") || strings.ContainsAny(r.Local.DockerHost, "\x00\r\n") {
			return nil, fmt.Errorf("invalid local Docker socket")
		}
		cmd := exec.CommandContext(ctx, "docker", argv...)
		for _, item := range os.Environ() {
			if !strings.HasPrefix(item, "DOCKER_HOST=") && !strings.HasPrefix(item, "DOCKER_CONTEXT=") {
				cmd.Env = append(cmd.Env, item)
			}
		}
		cmd.Env = append(cmd.Env, "DOCKER_HOST="+r.Local.DockerHost)
		cmd.WaitDelay = time.Second
		return cmd, nil
	}
	if r.SSH == nil || r.Local != nil {
		return nil, fmt.Errorf("exactly one local or SSH transport is required")
	}
	t := r.SSH
	for _, value := range []string{t.Hostname, t.User, t.HostKeyAlias} {
		if !sshIdentity.MatchString(value) || strings.HasPrefix(value, "-") {
			return nil, fmt.Errorf("invalid SSH identity")
		}
	}
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=1", "-o", "HostKeyAlias=" + t.HostKeyAlias}
	if t.KnownHostsFile != "" {
		if !filepath.IsAbs(t.KnownHostsFile) || strings.ContainsAny(t.KnownHostsFile, "\r\n\x00") {
			return nil, fmt.Errorf("invalid known-hosts path")
		}
		args = append(args, "-o", "UserKnownHostsFile="+t.KnownHostsFile)
	}
	remote := []string{quote("docker")}
	for _, arg := range argv {
		remote = append(remote, quote(arg))
	}
	args = append(args, t.User+"@"+t.Hostname, strings.Join(remote, " "))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.WaitDelay = time.Second
	return cmd, nil
}

// Retain bounded output; inspection never asks for container environment or logs.
type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 65536 - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func classify(w *workload, actual inspection) error {
	if actual.ID != w.Recipe.ID || actual.Name != "/"+w.Recipe.Container || (w.Recipe.Image != "" && actual.Image != w.Recipe.Image) {
		return fmt.Errorf("captured runtime identity drift")
	}
	if actual.Status == "" {
		return fmt.Errorf("inspection returned no status")
	}
	w.Status = actual.Status
	if actual.Health != nil {
		w.Health = *actual.Health
	}
	w.Match = "no"
	if (w.Desired == "running" && actual.Running && actual.Status == "running" && (actual.Health == nil || *actual.Health == "healthy")) || (w.Desired == "stopped" && !actual.Running) {
		w.Match = "yes"
	}
	return nil
}
func check(ctx context.Context, w *workload, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := command(ctx, w.Recipe)
	if err != nil {
		w.Error = err.Error()
		return
	}
	var out, stderr limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		// Never forward daemon/SSH raw diagnostics, which may contain private paths or secrets.
		if ctx.Err() != nil {
			w.Error = "inspection timed out or cancelled"
		} else if _, ok := err.(*exec.Error); ok {
			w.Error = "docker/ssh executable unavailable"
		} else {
			w.Error = "inspection failed (host unavailable, access denied or container missing)"
		}
		return
	}
	if out.overflow {
		w.Error = "inspection response too large"
		return
	}
	var actual inspection
	if err = json.Unmarshal(out.Bytes(), &actual); err != nil {
		w.Error = "invalid inspection response"
		return
	}
	if err = classify(w, actual); err != nil {
		w.Error = err.Error()
		return
	}
	enrichVersions(ctx, w, actual)
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}
func paint(enabled bool, code, s string) string {
	if enabled {
		return "\x1b[" + code + "m" + clean(s) + "\x1b[0m"
	}
	return clean(s)
}
func render(out io.Writer, items []workload, color, verbose bool) {
	for i := 0; i < len(items); {
		end := i + 1
		for end < len(items) && items[end].Node == items[i].Node {
			end++
		}
		known := 0
		for _, w := range items[i:end] {
			if w.Error == "" {
				known++
			}
		}
		transport := "SSH"
		if items[i].Recipe.Local != nil {
			transport = "local Docker"
		}
		code := "32"
		state := "reachable"
		if known == 0 {
			code = "31"
			state = "unavailable / unknown"
		} else if known < end-i {
			code = "33"
			state = "partial"
		}
		fmt.Fprintf(out, "%s  %s · %s\n", paint(color, "1;36", items[i].Node), transport, paint(color, code, state))
		var buffer bytes.Buffer
		table := tabwriter.NewWriter(&buffer, 0, 4, 2, ' ', 0)
		showHF := false
		for _, w := range items[i:end] {
			if w.Kind == "model" {
				showHF = true
				break
			}
		}
		if verbose {
			columns := []string{"  KIND", "WORKLOAD", "ENGINE", "VERSION / IMAGE"}
			if showHF {
				columns = append(columns, "HF MODEL")
			}
			columns = append(columns, "CONFIGURED", "APPLIED", "STATUS", "HEALTH", "STATE SYNC")
			fmt.Fprintln(table, strings.Join(columns, "\t"))
		} else {
			fmt.Fprintln(table, "  WORKLOAD\tVERSION\tSTATUS\tSTATE SYNC")
		}
		for _, w := range items[i:end] {
			status, health, match := w.Status, w.Health, w.Match
			if w.Error != "" {
				status = "unknown"
				match = "unknown"
			}
			match = syncLabel(w)
			// Pad plain table first: ANSI escape bytes would otherwise break alignment.
			if verbose {
				fields := []string{"  " + clean(w.Kind), clean(w.Name), clean(displayEngine(w)), clean(w.Version)}
				if showHF {
					fields = append(fields, clean(displayModel(w)))
				}
				fields = append(fields, clean(w.Configured), clean(w.Desired), clean(status), clean(health), clean(match))
				fmt.Fprintln(table, strings.Join(fields, "\t"))
			} else {
				if w.Error == "" && w.Match == "no" {
					match += " (want " + w.Desired + ")"
				}
				fmt.Fprintf(table, "  %s\t%s\t%s\t%s\n", clean(compactName(w)), clean(compactVersion(w)), clean(compactStatus(w)), clean(match))
			}
		}
		_ = table.Flush()
		for row, line := range strings.Split(strings.TrimSuffix(buffer.String(), "\n"), "\n") {
			rowColor := "2"
			if row > 0 {
				w := items[i+row-1]
				rowColor = "32"
				if w.Error != "" || w.Match != "yes" || (w.Sync != "" && w.Sync != "synced") {
					rowColor = "31"
				} else if w.Status != "running" {
					rowColor = "33"
				}
			}
			fmt.Fprintln(out, paint(color, rowColor, line))
		}
		for _, w := range items[i:end] {
			if w.Error != "" {
				fmt.Fprintf(out, "  %s\n", paint(color, "31", w.Name+": "+w.Error))
			}
			if verbose && w.Sync != "" {
				fmt.Fprintf(out, "  %s: filesystem↔state=%s · state↔remote=%s · next=%s\n", clean(w.Name), clean(w.ConfigSync), clean(w.RemoteSync), clean(w.Action))
			}
		}
		fmt.Fprintln(out)
		i = end
	}
	if verbose {
		fmt.Fprintln(out, "STATE SYNC includes OpenTofu configuration/state comparison and remote functional readiness; unknown is never synced.")
	} else {
		fmt.Fprintln(out, "STATE SYNC compares configuration, applied state and remote readiness · use -v for details.")
	}
}
func rootDefault() string {
	cwd, _ := os.Getwd()
	for path := cwd; path != ""; path = filepath.Dir(path) {
		if _, err := os.Stat(filepath.Join(path, "terraform.tfstate")); err == nil {
			return path
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	invoked, _ := exec.LookPath(os.Args[0])
	absolute, _ := filepath.Abs(invoked)
	return filepath.Dir(absolute)
}
func run() int {
	action, args, parseErr := operatorArgs(os.Args[1:])
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, parseErr)
		return 2
	}
	flags := flag.NewFlagSet("wm", flag.ContinueOnError)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: wm [status|plan|apply|start|stop|restart] [apps/NAME|models/NAME] [-v]\nAliases: wm -plan, wm -apply. Status compares config, applied state and remote readiness.")
		flags.PrintDefaults()
	}
	root := flags.String("root", rootDefault(), "workloads directory containing local terraform.tfstate")
	colorMode := flags.String("color", "auto", "auto, always or never (NO_COLOR disables automatic colors)")
	var verbose bool
	flags.BoolVar(&verbose, "v", false, "verbose status or live reconciliation progress")
	flags.BoolVar(&verbose, "verbose", false, "alias for -v")
	showVersion := flags.Bool("version", false, "print wm release version and exit")
	asJSON := flags.Bool("json", false, "emit JSON instead of the colored table")
	timeout := flags.Duration("timeout", 10*time.Second, "maximum inspection time per workload")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Printf("wm %s\n", version.Version)
		return 0
	}
	if flags.NArg() > 1 || (*colorMode != "auto" && *colorMode != "always" && *colorMode != "never") || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "Invalid arguments; use -help")
		return 2
	}
	absoluteRoot, rootErr := filepath.Abs(*root)
	if rootErr != nil {
		fmt.Fprintln(os.Stderr, rootErr)
		return 2
	}
	*root = absoluteRoot
	scope := ""
	if flags.NArg() == 1 {
		scope = flags.Arg(0)
	}
	if action != "status" {
		if *asJSON {
			fmt.Fprintln(os.Stderr, "--json is supported for status; plan/apply use OpenTofu output")
			return 2
		}
		if action == "start" || action == "stop" || action == "restart" {
			return lifecycle(*root, action, scope, verbose)
		}
		return operate(*root, action, scope, verbose)
	}
	if err := validateScope(*root, scope); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, "terraform.tfstate"))
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "Cannot read local terraform.tfstate; use -root PATH")
		return 2
	}
	if len(data) == 0 {
		data = []byte(`{"resources":[]}`)
	}
	items, err := inventory(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for i := range items {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			check(ctx, &items[i], *timeout)
		}(i)
	}
	wg.Wait()
	syncCtx, syncCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer syncCancel()
	items, syncErr := synchronize(syncCtx, *root, items)
	if syncErr != nil {
		fmt.Fprintln(os.Stderr, syncErr)
	}
	if scope != "" {
		filtered := items[:0]
		for _, w := range items {
			if resolvedScope(*root, w.Address) == scope {
				filtered = append(filtered, w)
			}
		}
		items = filtered
	}
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(items)
	} else {
		info, _ := os.Stdout.Stat()
		_, noColor := os.LookupEnv("NO_COLOR")
		colored := *colorMode == "always" || (*colorMode == "auto" && !noColor && info != nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb")
		if len(items) == 0 {
			fmt.Println("No managed workloads in local OpenTofu state.")
		} else {
			render(os.Stdout, items, colored, verbose)
		}
	}
	if syncErr != nil {
		return 1
	}
	for _, w := range items {
		if w.Error != "" || w.Match != "yes" || (w.Sync != "" && w.Sync != "synced") {
			return 1
		}
	}
	return 0
}
func main() { os.Exit(run()) }
