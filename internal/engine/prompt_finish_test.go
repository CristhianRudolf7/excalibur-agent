package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func addFinishCheck(r *Run, id, cmd string, failed bool) {
	r.History = append(r.History,
		ctxcompiler.ActivityEvent{ToolCalls: []providers.ToolCall{{ID: id, Name: "exec", Arguments: json.RawMessage(`{"command":` + strconvQuote(cmd) + `}`)}}},
		ctxcompiler.ActivityEvent{ToolResult: &providers.ToolResult{CallID: id, Name: "exec", Output: "exit: 0  duration: 0.1s  session: " + id + "  alive: false\nok", IsError: failed}})
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestPromptFinishEvidenceAndAblation(t *testing.T) {
	t.Setenv("EXCALIBUR_PROMPT_FINISH", "on")
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.Artifacts = artifactOffCfg()
	r := explicitRun()
	r.Workdir = t.TempDir()
	r.Request = "Create answer.txt"
	r.drafts = []tasks.DraftCriterion{{ID: "D1", Key: "answer.txt", Statement: "answer.txt exists"}, {ID: "D2", Key: "other.txt", Statement: "other.txt exists"}}
	r.SetPromptFinishRecordDir(t.TempDir())
	addFinishCheck(r, "a", "grep ok answer.txt", false)
	e.promptFinishCheck(r)
	if !r.firstAllChecksPassAt.IsZero() {
		t.Fatal("missing criterion counted as passing")
	}
	addFinishCheck(r, "b", "grep ok other.txt", true)
	e.promptFinishCheck(r)
	if !r.firstAllChecksPassAt.IsZero() {
		t.Fatal("failed check counted as passing")
	}
	addFinishCheck(r, "c", "grep ok other.txt", false)
	e.promptFinishCheck(r)
	if r.firstAllChecksPassAt.IsZero() {
		t.Fatal("no first-pass timestamp")
	}
	first := r.firstAllChecksPassAt
	if notes := strings.Join(r.VolatileNotes(), " "); !strings.Contains(notes, PromptFinishNote) {
		t.Fatal(notes)
	}
	addFinishCheck(r, "alternate", "grep ok answer.txt", false)
	e.promptFinishCheck(r)
	if r.repeatedVerification != 1 {
		t.Fatal("alternating check was not recognized as repeated")
	}
	addFinishCheck(r, "d", "grep ok other.txt", false)
	e.promptFinishCheck(r)
	if r.repeatedVerification != 2 || !r.firstAllChecksPassAt.Equal(first) {
		t.Fatal("repeat/timestamp changed incorrectly")
	}
	raw, err := os.ReadFile(filepath.Join(r.promptFinishRecordDir, "prompt_finish.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec promptFinishRecord
	if json.Unmarshal(raw, &rec) != nil || rec.FirstAllChecksPassAt == nil || rec.RepeatedVerification != 2 {
		t.Fatal(string(raw))
	}
	// A changed artifact invalidates repeat reuse even if the same check passes.
	if err := os.WriteFile(filepath.Join(r.Workdir, "other.txt"), []byte("new artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	addFinishCheck(r, "changed", "grep ok other.txt", false)
	e.promptFinishCheck(r)
	if r.repeatedVerification != 2 {
		t.Fatal("changed artifact counted as repeated verification")
	}
	t.Setenv("EXCALIBUR_PROMPT_FINISH", "off")
	addFinishCheck(r, "e", "grep ok other.txt", false)
	e.promptFinishCheck(r)
	if r.repeatedVerification != 2 {
		t.Fatal("disabled switch still active")
	}
}

// Both W12 mechanisms are enabled in production. Artifact checks must be able
// to consult withdrawn requirements without recursively locking the run.
func TestPromptFinishWithActionableArtifactGate(t *testing.T) {
	t.Setenv("EXCALIBUR_PROMPT_FINISH", "on")
	t.Setenv("EXCALIBUR_ACTIONABLE_REFUSALS", "on")
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	r.Workdir = t.TempDir()
	r.Request = "Create answer.txt"
	if err := os.WriteFile(filepath.Join(r.Workdir, "answer.txt"), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	addFinishCheck(r, "verified", "grep ok answer.txt", false)
	e.promptFinishCheck(r)
	if r.firstAllChecksPassAt.IsZero() {
		t.Fatal("passing artifact gate did not produce finish guidance")
	}
}
