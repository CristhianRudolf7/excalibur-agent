package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
)

func TestControlTailRetainsLatestAndJournalsAll(t *testing.T) {
	r := testRun(Profile{})
	store := journal.NewMemStore()
	r.noteJournal = &journal.Recorder{RunID: r.ID, Store: store}
	for i := 0; i < 30; i++ {
		r.InjectNote(fmt.Sprintf("finish refused (attempt %d): %s", i, strings.Repeat("x", 300)))
	}
	notes := r.VolatileNotes()
	if len(notes) != 1 || !strings.Contains(notes[0], "attempt 29") {
		t.Fatal(notes)
	}
	for i := 0; i < 30; i++ {
		r.InjectNote(fmt.Sprintf("issue-%d: %s", i, strings.Repeat("z", 300)))
	}
	if len(strings.Join(r.VolatileNotes(), "\n")) > 2048 {
		t.Fatal("tail exceeds cap")
	}
	rows, err := store.List(context.Background(), r.ID)
	noteCount := 0
	for _, row := range rows {
		if row.Type == journal.EntryType("control_note") {
			noteCount++
		}
	}
	if err != nil || noteCount != 60 {
		t.Fatalf("audit rows=%d err=%v", len(rows), err)
	}
	b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: testSnapshotFunc()}
	_, c, err := b.BuildRequest(context.Background(), r, false)
	if err != nil || len(c.LayerText(ctxcompiler.LayerVolatile)) > 2048 {
		t.Fatalf("compiled tail over cap: %v", err)
	}
}

func TestControlTailSwitchAndCustomCap(t *testing.T) {
	t.Setenv("EXCALIBUR_CONTROL_TAIL_BYTES", "100")
	r := testRun(Profile{})
	r.InjectNote("refusal: " + strings.Repeat("x", 1000))
	if len(strings.Join(r.VolatileNotes(), "\n")) > 100 {
		t.Fatal("custom cap ignored")
	}
	t.Setenv("EXCALIBUR_CONTROL_TAIL", "0")
	r.InjectNote("refusal: second")
	if len(r.VolatileNotes()) != 2 {
		t.Fatal("legacy switch ignored")
	}
}

func TestExplicitControlKeyRetirement(t *testing.T) {
	r := testRun(Profile{})
	r.InjectControlNote("verification", "a missing check")
	r.InjectControlNote("verification", "a different current check")
	if notes := r.VolatileNotes(); len(notes) != 1 || notes[0] != "a different current check" {
		t.Fatal(notes)
	}
	r.InjectControlNote("verification", "")
	if notes := r.VolatileNotes(); len(notes) != 0 {
		t.Fatal(notes)
	}
}

func TestLongUserDirectiveSurvivesControlCap(t *testing.T) {
	text := "user instruction " + strings.Repeat("literal content ", 400)
	r := testRun(Profile{})
	b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{Volatile: ctxcompiler.Volatile{Directives: []string{text}}}, nil
	}}
	req, c, err := b.BuildRequest(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, text) {
			found = true
		}
	}
	if !found {
		t.Fatal("control cap dropped user instruction")
	}
	if len(c.LayerText(ctxcompiler.LayerVolatile)) > 2048 {
		t.Fatal("control cap exceeded")
	}
	n := len(r.historyCopy())
	if _, _, err := b.BuildRequest(context.Background(), r, false); err != nil {
		t.Fatal(err)
	}
	if len(r.historyCopy()) != n {
		t.Fatal("unchanged directive duplicated")
	}
}

func TestUserDirectiveRemainsLiteralAfterCompaction(t *testing.T) {
	r := testRun(Profile{})
	instruction := "Keep the public adapter signature exactly as provided."
	r.mu.Lock()
	r.appendDirectiveLocked(instruction, nil)
	for i := 0; i < 40; i++ {
		r.History = append(r.History, ctxcompiler.ActivityEvent{Seq: r.nextHistorySeqLocked(), Role: "assistant", Content: strings.Repeat("x", 1000)})
	}
	r.mu.Unlock()
	cfg := ctxcompiler.DefaultConfig()
	cfg.ModelLimits = map[string]ctxcompiler.ModelLimits{"directive-small": {ContextWindow: 8000, MaxInput: 7000, MaxOutput: 500}}
	cfg.CompactionReserveTokens = 500
	cfg.PostCompactionLiteralTokens = 500
	off := false
	b := &DefaultBuilder{Config: cfg, Counter: ctxcompiler.NewEstimator(4), ModelID: "directive-small", Snapshot: testSnapshotFunc(), EnableMask: &off}
	for i := 0; i < 2; i++ {
		_, compiled, err := b.BuildRequest(context.Background(), r, false)
		if err != nil {
			t.Fatal(err)
		}
		if r.Compactions == 0 {
			t.Fatal("fixture did not compact")
		}
		if !strings.Contains(compiled.LayerText(ctxcompiler.LayerTask), instruction) {
			t.Fatal("compaction lost the literal user instruction")
		}
	}
}
