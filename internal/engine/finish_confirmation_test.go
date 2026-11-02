package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestConfirmationProgressFooterAndReminder(t *testing.T) {
	r := explicitRun()
	r.Workdir = t.TempDir()
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.Clock = func() time.Time { return time.Unix(100, 0) }
	if strings.Contains(e.timeFooterFor(r), confirmationReminder) {
		t.Fatal("premature footer")
	}
	r.armConfirm(FinishArgs{Summary: "done", Evidence: "checked"})
	if !strings.Contains(e.timeFooterFor(r), confirmationReminder) {
		t.Fatal("missing footer")
	}
	for i := 0; i < 12; i++ {
		r.observeConfirmation(true)
	}
	if countContaining(r.VolatileNotes(), "Twelve consecutive") != 1 {
		t.Fatal(r.VolatileNotes())
	}
	t.Setenv("EXCALIBUR_FINISH_CONFIRM_PROGRESS", "off")
	if r.confirmationFooter() != "" {
		t.Fatal("switch ignored")
	}
}

func TestPendingConfirmationDeadline(t *testing.T) {
	t.Setenv("EXCALIBUR_FINISH_TIME_RESERVE", "0")
	for _, change := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[change], func(t *testing.T) {
			e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
			r := explicitRun()
			r.Workdir = t.TempDir()
			r.Tracker.Limit.MaxSeconds = 100
			startRun(t, e, r, js)
			r.armConfirm(FinishArgs{Summary: "done", Evidence: "checked"})
			e.deps.Clock = func() time.Time { return r.StartedAt.Add(94 * time.Second) }
			if done, err := e.completePendingConfirmation(finishTestContext(t), r, e.deps.Clock()); done || err != nil {
				t.Fatalf("early: %v %v", done, err)
			}
			if change {
				if err := os.WriteFile(filepath.Join(r.Workdir, "new"), []byte("edit"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			e.deps.Clock = func() time.Time { return r.StartedAt.Add(95 * time.Second) }
			done, err := e.completePendingConfirmation(finishTestContext(t), r, e.deps.Clock())
			if err != nil || done == change {
				t.Fatalf("done=%v err=%v", done, err)
			}
			if done {
				mustState(t, r, types.RunCompleted)
				entries, err := js.List(context.Background(), r.ID)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, entry := range entries {
					if entry.Type == journal.TypeStateChange && strings.Contains(string(entry.Payload), "confirmation pending at deadline") {
						found = true
					}
				}
				if !found {
					t.Fatal("missing completion reason")
				}
			}
		})
	}
}

func TestConfirmationDeadlineFallbackAndToolResults(t *testing.T) {
	t.Setenv("EXCALIBUR_FINISH_TIME_RESERVE", "0")
	runner := scriptedRunner()
	runner.add("read_file", types.EffectReadOnly, types.IdemPure, tools.Result{Status: tools.StatusOK, Output: "contents"})
	e, js := testEngine(&fakeEndpoint{}, runner, passingGate())
	r := explicitRun()
	r.Workdir = t.TempDir()
	r.Tracker.Limit.MaxSeconds = 100
	startRun(t, e, r, js)
	r.armConfirm(FinishArgs{Summary: "done", Evidence: "checked"})
	e.deps.Clock = func() time.Time { return r.StartedAt.Add(94 * time.Second) }
	tc := toolCall("probe", "read_file", `{"path":"data"}`)
	if stop, err := e.execTools(context.Background(), r, []providers.ToolCall{tc}); stop || err != nil {
		t.Fatalf("%v %v", stop, err)
	}
	for _, event := range r.historyCopy() {
		if event.ToolResult != nil && !strings.Contains(event.ToolResult.Output, confirmationReminder) {
			t.Fatal(event.ToolResult.Output)
		}
	}
	e.deps.Clock = func() time.Time { return r.StartedAt.Add(100 * time.Second) }
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	if err := e.Run(ctx, r); err != nil {
		t.Fatal(err)
	}
	mustState(t, r, types.RunCompleted)
}
