package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"text/template"
)

func fixture() workload {
	return workload{Node: "studio", Kind: "app", Name: "web", Desired: "running", Status: "unknown", Health: "—", Match: "unknown", Recipe: recipe{ID: strings.Repeat("a", 64), Container: "web", Image: "sha256:expected"}}
}
func TestIdentityAndProcessStatus(t *testing.T) {
	for _, test := range []struct {
		name                           string
		running                        bool
		status, health, desired, match string
	}{
		{"healthy", true, "running", "healthy", "running", "yes"},
		{"unhealthy", true, "running", "unhealthy", "running", "no"},
		{"stopped as requested", false, "exited", "", "stopped", "yes"},
		{"crashed", false, "exited", "", "running", "no"},
		{"paused", true, "paused", "healthy", "running", "no"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := fixture()
			w.Desired = test.desired
			actual := inspection{ID: w.Recipe.ID, Name: "/web", Image: w.Recipe.Image, Running: test.running, Status: test.status}
			if test.health != "" {
				actual.Health = &test.health
			}
			if err := classify(&w, actual); err != nil {
				t.Fatal(err)
			}
			if w.Match != test.match {
				t.Fatal(w)
			}
			actual.ID = strings.Repeat("b", 64)
			if classify(&w, actual) == nil {
				t.Fatal("accepted another container")
			}
		})
	}
}
func TestInventoryDoesNotExposeRecipeSecrets(t *testing.T) {
	r := fixture().Recipe
	r.Name = "web"
	r.Host = "studio"
	data, _ := json.Marshal(r)
	state := map[string]any{"resources": []any{map[string]any{"module": "module.apps.module.langfuse", "type": "wm_workload", "name": "web", "instances": []any{map[string]any{"attributes": map[string]any{"desired": "running", "resolved_recipe": string(data)}}}}}}
	raw, _ := json.Marshal(state)
	items, err := inventory(raw)
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %v", items, err)
	}
	output, _ := json.Marshal(items)
	if strings.Contains(string(output), "observedContainerId") {
		t.Fatal("internal recipe escaped into JSON output")
	}
	if _, err = inventory([]byte(`{"resources":[{"type":"wm_workload","instances":[{"attributes":{}}]}]}`)); err == nil {
		t.Fatal("accepted missing snapshot")
	}
}
func TestTransportValidationAndSocketPin(t *testing.T) {
	w := fixture()
	w.Recipe.SSH = &target{Hostname: "-oProxyCommand=bad", User: "test", HostKeyAlias: "host"}
	if _, err := command(context.Background(), w.Recipe); err == nil {
		t.Fatal("SSH injection allowed")
	}
	w.Recipe.SSH = &target{Hostname: "10.0.0.1", User: "test", HostKeyAlias: "host"}
	cmd, err := command(context.Background(), w.Recipe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "StrictHostKeyChecking=yes") {
		t.Fatal("host verification disabled")
	}
	t.Setenv("DOCKER_CONTEXT", "wrong-daemon")
	t.Setenv("DOCKER_HOST", "unix:///wrong.sock")
	host, _ := os.Hostname()
	w.Recipe.SSH = nil
	w.Recipe.Local = &struct {
		Hostname   string `json:"hostname"`
		DockerHost string `json:"dockerHost"`
	}{host, "unix:///tmp/expected.sock"}
	cmd, err = command(context.Background(), w.Recipe)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range cmd.Env {
		if strings.HasPrefix(value, "DOCKER_CONTEXT=") || value == "DOCKER_HOST=unix:///wrong.sock" {
			t.Fatal("ambient Docker endpoint retained")
		}
	}
	w.Recipe.Local.Hostname = "another-controller"
	if _, err = command(context.Background(), w.Recipe); err == nil {
		t.Fatal("local host drift accepted")
	}
}
func TestColorAndUnknownStatus(t *testing.T) {
	w := fixture()
	w.Error = "inspection failed"
	var plain, colored bytes.Buffer
	render(&plain, []workload{w}, false, true)
	render(&colored, []workload{w}, true, true)
	if strings.Contains(plain.String(), "\x1b[") || !strings.Contains(colored.String(), "\x1b[31m") {
		t.Fatal("color mode incorrect")
	}
	if !strings.Contains(plain.String(), "STATE SYNC") || !strings.Contains(plain.String(), "⛔️ unknown") || !strings.Contains(plain.String(), "unknown") || !strings.Contains(plain.String(), "remote functional readiness") {
		t.Fatal("unknown or readiness limitation missing")
	}
}

func TestDockerTemplateSupportsContainersWithoutHealthchecks(t *testing.T) {
	tmpl, err := template.New("inspect").Funcs(template.FuncMap{"json": func(value any) string { raw, _ := json.Marshal(value); return string(raw) }}).Parse(inspectFormat)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	data := map[string]any{"Id": strings.Repeat("a", 64), "Name": "/fixture", "Image": "sha256:fixture", "State": map[string]any{"Status": "exited", "Running": false}}
	if err = tmpl.Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	var actual inspection
	if err = json.Unmarshal(out.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Health != nil || actual.Status != "exited" {
		t.Fatalf("unexpected inspection: %s", out.String())
	}
}
