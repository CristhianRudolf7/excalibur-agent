package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// The delivery summary uses `finish.summary` and never echoes the
// instruction.
func TestExplicitFinishSummaryUsesFinish(t *testing.T) {
	fake := providerfake.New("explicit-summary")
	fake.Responses = finishScript("wrote the report", "exec cat verified report.txt")
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write a very important report about quarterly earnings",
		LogsDir: logs, Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed", out.State)
	}
	if out.Summary != "wrote the report" {
		t.Fatalf("summary = %q, want finish.summary (never the instruction echo)", out.Summary)
	}
	if strings.Contains(out.Summary, "quarterly earnings") {
		t.Fatalf("summary must not echo the instruction: %q", out.Summary)
	}
	// The record stores the finish arguments and the raw final text.
	raw, err := os.ReadFile(filepath.Join(out.Dir, "explicit_finish.json"))
	if err != nil {
		t.Fatalf("explicit_finish.json missing: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["enabled"] != true {
		t.Fatalf("record must pin the switch on: %v", rec)
	}
	if rec["summary"] != "wrote the report" {
		t.Fatalf("record summary = %v", rec)
	}
}

// `finish` rides the model prefix when the switch is on.
func TestExplicitFinishToolInPrefix(t *testing.T) {
	fake := providerfake.New("explicit-prefix")
	fake.Responses = finishScript("done", "verified")
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "hi", LogsDir: logs, Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if len(fake.ChatRequests) == 0 || len(fake.ChatRequests[0].Tools) == 0 {
		t.Fatal("model calls must carry the tool prefix")
	}
	found := false
	for _, td := range fake.ChatRequests[0].Tools {
		if td.Name == "finish" {
			found = true
		}
	}
	if !found {
		names := []string{}
		for _, td := range fake.ChatRequests[0].Tools {
			names = append(names, td.Name)
		}
		t.Fatalf("finish must ride the prefix when on, got %v", names)
	}
}

// Switch OFF restores the old behaviour: a text-only turn completes,
// the summary echoes the instruction, and `finish` stays out of the
// prefix.
func TestExplicitFinishSwitchOffRestoresOld(t *testing.T) {
	t.Setenv("EXCALIBUR_EXPLICIT_FINISH", "off")
	fake := providerfake.New("explicit-off")
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "done"}},
	}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "do the thing", LogsDir: logs, Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed (old text finish)", out.State)
	}
	if out.Summary != "completed: do the thing" {
		t.Fatalf("switch off must restore the legacy echo summary, got %q", out.Summary)
	}
	for _, req := range fake.ChatRequests {
		for _, td := range req.Tools {
			if td.Name == "finish" {
				t.Fatal("switch off must hide finish from the prefix")
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(out.Dir, "explicit_finish.json"))
	if err != nil {
		t.Fatalf("explicit_finish.json missing: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["enabled"] != false {
		t.Fatalf("record must pin the switch off: %v", rec)
	}
}

// The failed-check-then-completed pattern end to end: the agent's
// last command errors, `finish` is refused, it recovers and completes
// with the finish summary.
func TestExplicitFinishFailedCheckThenCompleted(t *testing.T) {
	fake := providerfake.New("explicit-failed-check")
	// An empty exec call fails input validation (tool error); `true`
	// succeeds. Both are deterministic through the real pipeline.
	failArgs := json.RawMessage(`{}`)
	okArgs, _ := json.Marshal(map[string]string{"command": "true"})
	finishBad, _ := json.Marshal(map[string]string{"summary": "fixed", "evidence": "ran check"})
	finishGood, _ := json.Marshal(map[string]string{"summary": "fixed check", "evidence": "true exit 0"})
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "exec", Arguments: failArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "f1", Name: "finish", Arguments: finishBad}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c2", Name: "exec", Arguments: okArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "f2", Name: "finish", Arguments: finishGood}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "f3", Name: "finish", Arguments: finishGood}}}},
	}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "fix the check", LogsDir: logs, Profile: ProfileEval,
		ProjectDir: t.TempDir(), Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed after recovery", out.State)
	}
	if out.Summary != "fixed check" {
		t.Fatalf("summary = %q, want the accepted finish.summary", out.Summary)
	}
}
