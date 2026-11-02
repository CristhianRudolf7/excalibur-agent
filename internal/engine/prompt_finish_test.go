package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
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

func TestPromptFinishOncePerPassingStateAndJournal(t *testing.T) {
	t.Setenv(endStateFinishNoteSwitch, "1")
	e, store := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.Artifacts = artifactOffCfg()
	r := explicitRun()
	r.Workdir = t.TempDir()
	r.Request = "Create result.txt"
	r.noteJournal = &journal.Recorder{RunID: r.ID, Store: store}
	for i := 0; i < 4; i++ {
		addFinishCheck(r, string(rune('a'+i)), "grep ok result.txt", false)
		e.promptFinishCheck(r)
	}
	count := func() int {
		rows, err := store.List(context.Background(), r.ID)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, row := range rows {
			if row.Type == "control_note" && strings.Contains(string(row.Payload), PromptFinishNote) {
				n++
			}
		}
		return n
	}
	if n := count(); n != 1 {
		t.Fatalf("unchanged passing state emitted %d notes", n)
	}
	if err := os.WriteFile(filepath.Join(r.Workdir, "result.txt"), []byte("updated"), 0600); err != nil {
		t.Fatal(err)
	}
	addFinishCheck(r, "changed", "grep updated result.txt", false)
	e.promptFinishCheck(r)
	if n := count(); n != 2 {
		t.Fatalf("changed passing state emitted %d notes", n)
	}
	t.Setenv(endStateFinishNoteSwitch, "0")
	addFinishCheck(r, "legacy", "grep updated result.txt", false)
	e.promptFinishCheck(r)
	if n := count(); n != 3 {
		t.Fatalf("disabled guard emitted %d notes", n)
	}
}

func TestPromptFinishDefersVisualJudgmentAndRetiresFailedReport(t *testing.T) {
	for _, name := range []string{"inspect_image", "call_tool", "exec"} {
		t.Run(name, func(t *testing.T) {
			e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
			e.deps.Artifacts = artifactOffCfg()
			r := explicitRun()
			r.Workdir = t.TempDir()
			r.Request = "Create result.txt and display the requested result"
			addFinishCheck(r, "check", "grep ok result.txt", false)
			r.History = append(r.History,
				ctxcompiler.ActivityEvent{ToolCalls: []providers.ToolCall{{ID: "image", Name: name, Arguments: json.RawMessage(`{"name":"inspect_image","arguments":{"path":"screen.png"}}`)}}},
				ctxcompiler.ActivityEvent{ToolResult: &providers.ToolResult{CallID: "image", Name: name, Output: "blank/uniform: false\nscreen_text:\nInitialization incomplete; user input required"}})
			e.promptFinishCheck(r)
			if strings.Contains(strings.Join(r.VolatileNotes(), " "), PromptFinishNote) {
				t.Fatal("unreviewed visual state got positive note")
			}
			history := historyTools(r.History)
			r.recordFinishVerifierReport(history, true, false)
			e.promptFinishCheck(r)
			if !strings.Contains(strings.Join(r.VolatileNotes(), " "), PromptFinishNote) {
				t.Fatal("reviewed passing visual state lost note")
			}
			r.recordFinishVerifierReport(history, true, true)
			e.promptFinishCheck(r)
			if strings.Contains(strings.Join(r.VolatileNotes(), " "), PromptFinishNote) {
				t.Fatal("failing verifier left stale positive note")
			}
			r.recordFinishVerifierReport(history, true, false)
			r.History = append(r.History, ctxcompiler.ActivityEvent{ToolResult: &providers.ToolResult{Name: "inspect_image", Output: "blank/uniform: true"}})
			e.promptFinishCheck(r)
			if strings.Contains(strings.Join(r.VolatileNotes(), " "), PromptFinishNote) {
				t.Fatal("new visual state reused prior PASS")
			}
		})
	}
}

func TestReviewIdentityIncludesImageEvidence(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	history := []tasks.HistoryTool{{Name: "inspect_image", Args: `{"path":"screen.png"}`, Output: "blank/uniform: true"}}
	a, _ := e.reviewIdentity(r, *verifyConfig(), nil, history)
	history[0].Output = "blank/uniform: false"
	b, _ := e.reviewIdentity(r, *verifyConfig(), nil, history)
	if a == b {
		t.Fatal("changed screen reused verifier identity")
	}
}

func TestPromptFinishRetiresContradictionWithoutControlTail(t *testing.T) {
	t.Setenv("EXCALIBUR_CONTROL_TAIL", "0")
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.Artifacts = artifactOffCfg()
	r := explicitRun()
	r.Workdir = t.TempDir()
	r.Request = "Create result.txt"
	addFinishCheck(r, "a", "grep ok result.txt", false)
	e.promptFinishCheck(r)
	if !strings.Contains(strings.Join(r.VolatileNotes(), " "), PromptFinishNote) {
		t.Fatal("passing state lost note")
	}
	r.recordFinishVerifierReport(historyTools(r.History), true, true)
	e.promptFinishCheck(r)
	if strings.Contains(strings.Join(r.VolatileNotes(), " "), PromptFinishNote) {
		t.Fatal("legacy control tail retained contradicted guidance")
	}
}
