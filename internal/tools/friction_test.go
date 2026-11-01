package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCallCoreToolCompatibility(t *testing.T) {
	_, cat, ex := deferredTestSetup()
	call := &CallToolTool{Catalog: cat, Executor: ex}
	for _, args := range []string{`{"name":"exec_io","arguments":{"action":"list"}}`, `{"arguments":{"name":"exec_io","action":"list"}}`, `{"arguments":{"name":"exec_io","arguments":{"action":"list"}}}`} {
		got, err := call.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(args)})
		if err != nil || got.Status != StatusOK || !strings.Contains(got.Output, "session(s)") {
			t.Fatalf("%s: %+v %v", args, got, err)
		}
	}
	got, err := ex.Execute(context.Background(), "call_tool", json.RawMessage(`{"arguments":{"name":"exec_io","arguments":{"action":"list"}}}`), ExecuteOptions{})
	if err != nil || got.Status != StatusOK {
		t.Fatalf("pipeline nested call: %+v %v", got, err)
	}
	for _, name := range []string{"search_tools", "call_tool"} {
		raw, _ := json.Marshal(map[string]any{"name": name, "arguments": map[string]any{"query": ""}})
		res, err := call.Invoke(context.Background(), Invocation{Arguments: raw})
		if err != nil || (name == "search_tools" && res.Status != StatusOK) || (name == "call_tool" && res.Status != StatusInputError) {
			t.Fatalf("%s: %+v %v", name, res, err)
		}
	}
	t.Setenv("EXCALIBUR_TOOL_FRICTION_REPAIR", "off")
	got, _ = call.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"name":"exec_io","arguments":{"action":"list"}}`)})
	if got.Status != StatusInputError || !strings.Contains(got.Output, "not deferred") {
		t.Fatal(got)
	}
}

func TestSessionOutputCompatibility(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	sess, _, alive := m.Start("test", "printf friction-output; sleep 30", "", false, 300, nil)
	defer m.Close(sess.ID)
	if !alive {
		t.Fatal("expected a live terminal session")
	}
	if _, _, _, err := m.WaitFor(context.Background(), sess.ID, "friction-output", 3*time.Second, 0); err != nil {
		t.Fatal(err)
	}
	get := &GetToolOutputTool{Sessions: m}
	raw, _ := json.Marshal(map[string]any{"ref": sess.ID, "offset": 9, "limit": 6})
	got, err := get.Invoke(context.Background(), Invocation{Arguments: raw})
	if err != nil || got.Status != StatusOK || !strings.Contains(got.Output, "output") {
		t.Fatalf("%+v %v", got, err)
	}
	t.Setenv("EXCALIBUR_TOOL_FRICTION_REPAIR", "off")
	got, _ = get.Invoke(context.Background(), Invocation{Arguments: raw})
	if got.Status != StatusInputError {
		t.Fatal(got)
	}
}
