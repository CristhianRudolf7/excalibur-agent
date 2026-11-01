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
	"github.com/CristhianRudolf7/excalibur-agent/internal/repoexplore"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// extractBriefBlock returns the Repo brief note from the system text.
func extractBrief(sys string) string {
	start := strings.Index(sys, "Repo brief")
	if start < 0 {
		return ""
	}
	// The brief rides the L3 working-notes block up to the memories
	// section trailer.
	end := strings.Index(sys[start:], "\nmemories:")
	if end < 0 {
		return sys[start:]
	}
	return sys[start : start+end]
}

// Default off: no brief, no explore.json, L3 unchanged.
func TestExecExploreDefaultOff(t *testing.T) {
	if err := os.Unsetenv(repoexplore.ExploreSwitch); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := t.TempDir()
	fake := providerfake.New("explore-off")
	fake.Responses = doneImmediately()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "say hi", ProjectDir: dir, LogsDir: logs,
		Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if got := systemText(fake.ChatRequests[0]); strings.Contains(got, "Repo brief") {
		t.Fatalf("disabled explorer must not inject:\n%s", got)
	}
	// Disabled: no explore.json in the run dir.
	if _, err := os.Stat(filepath.Join(logs, "explore.json")); !os.IsNotExist(err) {
		// Eval writes events/run files under logsDir (out.Dir == logs).
		t.Fatalf("disabled explorer must not write explore.json (err=%v)", err)
	}
	// No explore event either.
	if hasEvent(t, filepath.Join(logs, "events.jsonl"), "explore_brief") {
		t.Fatal("disabled explorer must not emit explore_brief")
	}
}

// Switch on: the brief rides L3 working notes, byte-identical across
// steps, capped at 60 lines, with explore.json and an event.
func TestExecExploreInjectedOnce(t *testing.T) {
	t.Setenv(repoexplore.ExploreSwitch, "on")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := t.TempDir()
	fake := providerfake.New("explore-on")
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "clock", Arguments: []byte(`{}`)}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "done"}},
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "say hi", ProjectDir: dir, LogsDir: logs,
		Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if len(fake.ChatRequests) != 2 {
		t.Fatalf("model calls = %d, want 2", len(fake.ChatRequests))
	}
	first, second := systemText(fake.ChatRequests[0]), systemText(fake.ChatRequests[1])
	for _, sys := range []string{first, second} {
		if !strings.Contains(sys, "Repo brief") {
			t.Fatalf("L3 must carry the brief:\n%s", sys)
		}
		if !strings.Contains(sys, "working notes:") {
			t.Fatalf("brief must ride working notes:\n%s", sys)
		}
		if !strings.Contains(sys, "main.go") {
			t.Fatalf("brief must name the entry point:\n%s", sys)
		}
	}
	if b1, b2 := extractBrief(first), extractBrief(second); b1 != b2 {
		t.Fatalf("brief bytes must be identical across steps (stable L3 prefix):\n%s\n---\n%s", b1, b2)
	}
	// Line cap: the brief note itself holds at most 60 lines.
	briefLines := strings.Count(extractBrief(first), "\n") + 1
	if briefLines > 80 { // note wrapper + 60 brief lines with margin
		t.Fatalf("brief too long: %d lines", briefLines)
	}
	// explore.json record (same dir as skills.json: out.Dir).
	raw, err := os.ReadFile(filepath.Join(out.Dir, "explore.json"))
	if err != nil {
		t.Fatalf("explore.json missing: %v", err)
	}
	var rec struct {
		Enabled bool     `json:"enabled"`
		Workdir string   `json:"workdir"`
		Lines   int      `json:"lines"`
		Brief   []string `json:"brief"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if !rec.Enabled || rec.Workdir != dir || rec.Lines == 0 || len(rec.Brief) == 0 {
		t.Fatalf("bad explore.json: %+v", rec)
	}
	if len(rec.Brief) > 60 {
		t.Fatalf("explore.json brief = %d lines, want <=60", len(rec.Brief))
	}
	if !hasEvent(t, filepath.Join(logs, "events.jsonl"), "explore_brief") {
		t.Fatal("missing explore_brief event")
	}
}

func hasEvent(t *testing.T, path, name string) bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.Contains(ln, `"`+name+`"`) {
			return true
		}
	}
	return false
}

// Empty project dir with the switch on degrades to a short brief
// (no crash, still stable).
func TestExecExploreEmptyWorkdir(t *testing.T) {
	t.Setenv(repoexplore.ExploreSwitch, "on")
	logs := t.TempDir()
	fake := providerfake.New("explore-empty")
	fake.Responses = doneImmediately()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "hi", LogsDir: logs, Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	// No workdir means no exploration: no crash, no brief.
	if got := systemText(fake.ChatRequests[0]); strings.Contains(got, "Repo brief") {
		t.Fatalf("empty workdir must not inject a brief:\n%s", got)
	}
}
