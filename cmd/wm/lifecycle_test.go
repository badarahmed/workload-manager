package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T, body string) (string, string) {
	t.Helper()
	root := t.TempDir()
	scope := "apps/test"
	os.MkdirAll(filepath.Join(root, scope), 0700)
	if err := os.WriteFile(filepath.Join(root, scope, "main.tf"), []byte(body), 0640); err != nil {
		t.Fatal(err)
	}
	return root, scope
}

const graphFixture = `# desired = "running" in a comment must remain untouched.
resource "wm_workload" "database" {
  desired = "running" # database intent
}
resource "wm_workload" "web" {
  desired = "running"
  depends_on = [wm_workload.database]
}
`

func TestLifecycleEditsLiteralOnlyAndOrdersDependencies(t *testing.T) {
	root, scope := sourceFixture(t, graphFixture)
	source, err := loadLifecycle(root, scope)
	if err != nil {
		t.Fatal(err)
	}
	start, err := lifecycleWaves(source.Resources, false)
	if err != nil {
		t.Fatal(err)
	}
	stop, _ := lifecycleWaves(source.Resources, true)
	if !reflect.DeepEqual(start, [][]string{{"database"}, {"web"}}) || !reflect.DeepEqual(stop, [][]string{{"web"}, {"database"}}) {
		t.Fatal(start, stop)
	}
	edits, err := editWave(source, []string{"web"}, "stopped")
	if err != nil {
		t.Fatal(err)
	}
	data := edits[source.Resources["web"].File]
	if !bytes.Contains(data, []byte(`desired = "running" # database intent`)) || !bytes.Contains(data, []byte(`# desired = "running" in a comment`)) || !bytes.Contains(data, []byte(`desired = "stopped"`)) {
		t.Fatal(string(data))
	}
	if err = writeIntent(root, source, edits); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(root, scope, "main.tf"))
	if info.Mode().Perm() != 0640 {
		t.Fatal("permissions changed")
	}
	if err = writeIntent(root, source, edits); err == nil {
		t.Fatal("concurrent source edit not detected")
	}
}
func TestLifecycleRejectsUnsupportedInputsBeforeEditing(t *testing.T) {
	for _, body := range []string{
		`resource "wm_workload" "x" { desired = var.desired }`,
		`resource "wm_workload" "x" { desired = "unknown" }`,
		"resource \"wm_workload\" \"x\" {\n desired = \"running\"\n count = 2\n}",
	} {
		root, scope := sourceFixture(t, body)
		if _, err := loadLifecycle(root, scope); err == nil {
			t.Fatal(body)
		}
	}
	root, _ := sourceFixture(t, graphFixture)
	if _, err := loadLifecycle(root, ""); err == nil {
		t.Fatal("global lifecycle allowed")
	}
	if _, err := lifecycleWaves(map[string]intentResource{"a": {Dependencies: []string{"b"}}, "b": {Dependencies: []string{"a"}}}, false); err == nil {
		t.Fatal("cycle allowed")
	}
}
func TestLifecyclePlanGuardRejectsOtherChanges(t *testing.T) {
	root, scope := sourceFixture(t, graphFixture)
	allowed := planChange{Address: "module.apps.module.test.wm_workload.web", Type: "wm_workload"}
	allowed.Change.Actions = []string{"update"}
	plan := planData{Changes: []planChange{allowed}}
	if err := lifecyclePlanGuard(plan, root, scope, []string{"web"}); err != nil {
		t.Fatal(err)
	}
	if err := lifecyclePlanGuard(plan, root, scope, []string{"database"}); err == nil {
		t.Fatal("unrelated update allowed")
	}
	plan.Changes[0].Change.Actions = []string{"delete", "create"}
	if err := lifecyclePlanGuard(plan, root, scope, []string{"web"}); err == nil {
		t.Fatal("replacement allowed")
	}
}
func TestRestartRunsPlanApplyInSafeOrderWithVerbose(t *testing.T) {
	root, scope := sourceFixture(t, graphFixture)
	os.MkdirAll(filepath.Join(root, ".local"), 0700)
	script := `#!/usr/bin/env python3
import sys,json,pathlib,re,os
root=pathlib.Path(__file__).parent
state=root/'.local/fake-state.json'
current=json.loads(state.read_text()) if state.exists() else {'database':'running','web':'running'}
args=sys.argv[1:]
with (root/'.local/commands.jsonl').open('a') as f:f.write(json.dumps(args)+'\n')
if args[0]=='get':sys.exit(0)
if args[0]=='show':print(pathlib.Path(args[-1]).read_text());sys.exit(0)
if args[0]=='plan':
 text=(root/'apps/test/main.tf').read_text()
 desired=dict(re.findall(r'resource "wm_workload" "([^"]+)"\s*{\s*desired\s*=\s*"([^"]+)"',text))
 changes=[]
 for name,value in desired.items():
  changes.append({'address':'module.apps.module.test.wm_workload.'+name,'type':'wm_workload','change':{'actions':['no-op'] if current[name]==value else ['update'],'before':{'desired':current[name]},'after':{'desired':value}}})
 out=next(a.split('=',1)[1] for a in args if a.startswith('-out='))
 pathlib.Path(out).write_text(json.dumps({'resource_changes':changes}));sys.exit(0)
if args[0]=='apply':
 if os.environ.get('WM_TEST_FAIL_APPLY'):sys.exit(7)
 plan=json.loads(pathlib.Path(args[1]).read_text())
 for c in plan['resource_changes']:
  name=c['address'].split('.')[-1];value=c['change']['after']['desired']
  if c['change']['actions']!=['no-op']:
   with (root/'.local/actions.txt').open('a') as f:f.write(name+':'+value+'\n')
   current[name]=value
 state.write_text(json.dumps(current));sys.exit(0)
sys.exit(2)
`
	if err := os.WriteFile(filepath.Join(root, "tofu"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if code := lifecycle(root, "restart", scope, true); code != 0 {
		t.Fatalf("exit %d", code)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".local/actions.txt"))
	if string(raw) != "web:stopped\ndatabase:stopped\ndatabase:running\nweb:running\n" {
		t.Fatal(string(raw))
	}
	raw, _ = os.ReadFile(filepath.Join(root, ".local/commands.jsonl"))
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var args []string
		if err := json.Unmarshal([]byte(line), &args); err != nil {
			t.Fatal(err)
		}
		if args[0] == "plan" || args[0] == "apply" {
			found := false
			for _, arg := range args {
				if arg == "--verbose" {
					found = true
				}
			}
			if !found {
				t.Fatal("verbose flag lost", args)
			}
		}
	}
	source, err := loadLifecycle(root, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range source.Files {
		if strings.Contains(string(data), `desired = "stopped"`) {
			t.Fatal("restart did not leave running intent")
		}
	}
	rawActions, _ := os.ReadFile(filepath.Join(root, ".local/actions.txt"))
	t.Setenv("WM_TEST_FAIL_APPLY", "1")
	if code := lifecycle(root, "restart", scope, false); code != 7 {
		t.Fatalf("failed apply not propagated: %d", code)
	}
	pending, err := loadLifecycle(root, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range pending.Files {
		if !strings.Contains(string(data), `desired = "stopped"`) {
			t.Fatal("failed apply hid pending stop intent")
		}
	}
	actions, _ := os.ReadFile(filepath.Join(root, ".local/actions.txt"))
	if string(actions) != string(rawActions) {
		t.Fatal("restart continued after failure")
	}

}

func TestExistingLifecycleSourcesReadonly(t *testing.T) {
	root := os.Getenv("WM_TEST_LIFECYCLE_ROOT")
	if root == "" {
		t.Skip("opt-in read-only validation of deployed HCL")
	}
	for _, scope := range []string{"models/d3", "apps/langfuse", "apps/grafana", "apps/hindsight"} {
		t.Run(scope, func(t *testing.T) {
			source, err := loadLifecycle(root, scope)
			if err != nil {
				t.Fatal(err)
			}
			waves, err := lifecycleWaves(source.Resources, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, names := range waves {
				if _, err = editWave(source, names, "stopped"); err != nil {
					t.Fatal(err)
				}
			}
			for path, before := range source.Files {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("read-only check changed source")
				}
			}
			t.Log("verified stop waves (not executed):", waves)
		})
	}
}
