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
		r.InjectNote(fmt.Sprintf("refusal: attempt %d %s", i, strings.Repeat("x", 300)))
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
	if err != nil || len(rows) != 60 {
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
