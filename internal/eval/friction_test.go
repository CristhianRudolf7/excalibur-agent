package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFrictionRecords(t *testing.T) {
	r, err := NewTrialRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ tool, out string }{
		{"call_tool", `tool "exec_io" is not deferred; call it directly`},
		{"call_tool", "name is required"}, {"subagent", "action is required"},
		{"subagent", "run is required for status"}, {"get_tool_output", `unknown blob ref "sess-6"`},
		{"search_tools", "query is required"}, {"exec", "command failed"},
	} {
		if err := r.RecordToolCall(ToolCall{Tool: c.tool, IsError: true}, c.out); err != nil {
			t.Fatal(err)
		}
	}
	// Reopening proves the summary survives process restarts.
	r, err = NewTrialRecorder(r.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WriteFinal("completed", "", 0); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(r.Dir(), "final.json"))
	if err != nil {
		t.Fatal(err)
	}
	var final FinalState
	if err := json.Unmarshal(raw, &final); err != nil {
		t.Fatal(err)
	}
	if final.FrictionErrorCount != 6 || len(final.FrictionErrors) != 6 {
		t.Fatalf("%s", raw)
	}
}

func TestSessionToolCoreSwitch(t *testing.T) {
	has := func() bool {
		for _, n := range DefaultEvalProfile().EnabledTools {
			if n == "exec_io" {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Fatal("exec_io absent")
	}
	t.Setenv("EXCALIBUR_TOOL_FRICTION_REPAIR", "off")
	if has() {
		t.Fatal("switch did not restore catalog")
	}
}

func TestSchemaFrictionClassification(t *testing.T) {
	for _, tool := range []string{"call_tool", "search_tools"} {
		field := "name"
		if tool == "search_tools" {
			field = "query"
		}
		got := classifyFriction(ToolCall{Tool: tool, IsError: true}, `invalid arguments: $: missing required property "`+field+`"`)
		if got == "" {
			t.Fatalf("schema error not counted: %s", tool)
		}
	}
	if got := classifyFriction(ToolCall{Tool: "exec", IsError: true}, `missing required property "command"`); got != "" {
		t.Fatal(got)
	}
}
