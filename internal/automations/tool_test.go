package automations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type toolFake struct {
	Service
	call    string
	reason  string
	limit   int
	spec    Spec
	note    string
	version int
}

func (f *toolFake) List(context.Context) ([]Automation, error) {
	f.call = "list"
	return []Automation{{ID: "a"}}, nil
}
func (f *toolFake) Get(context.Context, string) (Automation, []Version, error) {
	f.call = "get"
	return Automation{ID: "a"}, []Version{{Version: 1}}, nil
}
func (f *toolFake) Runs(_ context.Context, _ string, n int) ([]Run, error) {
	f.call = "history"
	f.limit = n
	return []Run{{ID: "r"}}, nil
}
func (f *toolFake) Create(_ context.Context, s Spec) (Automation, error) {
	f.call = "create"
	f.spec = s
	return Automation{ID: "a"}, nil
}
func (f *toolFake) Update(_ context.Context, _ string, s Spec, n string) (Automation, error) {
	f.call = "update"
	f.spec = s
	f.note = n
	return Automation{ID: "a", Version: 2}, nil
}
func (f *toolFake) RestoreVersion(_ context.Context, _ string, v int) (Automation, error) {
	f.call = "restore_version"
	f.version = v
	return Automation{ID: "a"}, nil
}
func (f *toolFake) RunNow(context.Context, string) (Run, error) {
	f.call = "run_now"
	return Run{ID: "r", Status: RunRunning}, nil
}
func (f *toolFake) Pause(_ context.Context, _, r string) (Automation, error) {
	f.call = "pause"
	f.reason = r
	return Automation{ID: "a", Status: StatusPaused}, nil
}
func (f *toolFake) Resume(context.Context, string) (Automation, error) {
	f.call = "resume"
	return Automation{ID: "a", Status: StatusActive}, nil
}
func (f *toolFake) Delete(context.Context, string) error { f.call = "delete"; return nil }

func TestToolActions(t *testing.T) {
	for _, action := range []string{"create", "update", "list", "get", "history", "run_now", "pause", "resume", "delete", "restore_version"} {
		t.Run(action, func(t *testing.T) {
			f := &toolFake{}
			reg := tools.NewMapRegistry()
			if e := WireTool(reg, f); e != nil {
				t.Fatal(e)
			}
			tool, _ := reg.Get("automations")
			if e := tool.Definition().Validate(); e != nil {
				t.Fatal(e)
			}
			args, _ := json.Marshal(map[string]any{"action": action, "id": "a", "approved": true, "version": 1, "note": "changed", "spec": Spec{Name: "Daily"}})
			r, e := tool.Invoke(context.Background(), tools.Invocation{Arguments: args})
			if e != nil || r.Status != tools.StatusOK || !json.Valid([]byte(r.Output)) || f.call != action {
				t.Fatalf("%+v %v call=%s", r, e, f.call)
			}
			if action == "pause" && f.reason != "agent" {
				t.Fatal(f.reason)
			}
			if action == "history" && f.limit != 20 {
				t.Fatal(f.limit)
			}
			if action == "update" && (f.note != "changed" || f.spec.Name != "Daily") {
				t.Fatal(f)
			}
			if action == "restore_version" && f.version != 1 {
				t.Fatal(f.version)
			}
		})
	}
}
func TestToolRejectsUnapprovedAndInvalidActions(t *testing.T) {
	for _, raw := range []string{`{"action":"create","spec":{}}`, `{"action":"update","id":"a","spec":{}}`, `{"action":"create","approved":true}`, `{"action":"get"}`, `{"action":"restore_version","id":"a","version":0}`, `{"action":"bogus","id":"a"}`, `null`, `[]`} {
		f := &toolFake{}
		if _, e := toolAction(context.Background(), f, json.RawMessage(raw)); e == nil || f.call != "" {
			t.Fatalf("accepted %s: %v", raw, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := toolAction(ctx, &toolFake{}, json.RawMessage(`{"action":"list"}`)); e != context.Canceled {
		t.Fatal(e)
	}
	if _, e := toolAction(context.Background(), nil, json.RawMessage(`{"action":"list"}`)); e == nil || !strings.Contains(e.Error(), "not available") {
		t.Fatal(e)
	}
}
func TestToolSchemaAndRewire(t *testing.T) {
	reg := tools.NewMapRegistry()
	_ = WireTool(reg, nil)
	before, _ := reg.Get("automations")
	f := &toolFake{}
	if e := WireTool(reg, f); e != nil {
		t.Fatal(e)
	}
	after, _ := reg.Get("automations")
	if before != after {
		t.Fatal("replaced catalog identity")
	}
	r, e := tools.NewExecutor(reg).Execute(context.Background(), "automations", json.RawMessage(`{"action":"list"}`), tools.ExecuteOptions{})
	if e != nil || r.Status != tools.StatusOK {
		t.Fatal(r, e)
	}
	var schema map[string]any
	if e := json.Unmarshal(after.Definition().InputSchema, &schema); e != nil {
		t.Fatal(e)
	}
}
