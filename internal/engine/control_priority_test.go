package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func TestControlLifecycleDeduplicatesByIssue(t *testing.T) {
	r := testRun(Profile{})
	for i := 0; i < 25; i++ {
		r.InjectNote(tasks.BackgroundHint(fmt.Sprintf("compile-module-%d --verbose", i)))
	}
	if notes := r.VolatileNotes(); len(notes) != 1 || !strings.Contains(notes[0], "compile-module-24") {
		t.Fatalf("lifecycle tips are not issue keyed: %v", notes)
	}
	t.Setenv("EXCALIBUR_CONTROL_ISSUE_PRIORITY", "0")
	legacy := testRun(Profile{})
	legacy.InjectNote(tasks.BackgroundHint("first command"))
	legacy.InjectNote(tasks.BackgroundHint("second command"))
	if len(legacy.VolatileNotes()) != 2 {
		t.Fatal("legacy issue key switch ignored")
	}
}

func TestControlPriorityReservesCurrentStateAndAuditsLoss(t *testing.T) {
	t.Setenv("EXCALIBUR_CONTROL_TAIL_BYTES", "512")
	r := testRun(Profile{})
	store := journal.NewMemStore()
	r.noteJournal = &journal.Recorder{RunID: r.ID, Store: store}
	r.InjectControlNote("finish.refusal", "finish refused: missing assertion; "+strings.Repeat("details ", 100))
	for i := 0; i < 15; i++ {
		r.InjectControlNote(fmt.Sprintf("tip.%d", i), strings.Repeat("background advice ", 15))
	}
	if !strings.Contains(strings.Join(r.VolatileNotes(), "\n"), "missing assertion") {
		t.Fatal("current refusal evicted by tips")
	}
	b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{Volatile: ctxcompiler.Volatile{ContinuationNote: strings.Repeat("older continuation ", 100), BudgetState: "17 seconds remain", GateReports: []string{"current gate requires output validation"}}}, nil
	}}
	_, compiled, err := b.BuildRequest(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	layer := compiled.LayerText(ctxcompiler.LayerVolatile)
	for _, text := range []string{"17 seconds remain", "current gate requires output validation", "missing assertion"} {
		if !strings.Contains(layer, text) {
			t.Fatalf("current state lost %q in %q", text, layer)
		}
	}
	if len(layer) > 512 {
		t.Fatal("volatile cap exceeded")
	}
	rows, err := store.List(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	layers := map[string]bool{}
	for _, row := range rows {
		if row.Type != journal.EntryType("control_layer_loss") {
			continue
		}
		var payload struct {
			Layer  string        `json:"layer"`
			Issues []controlLoss `json:"issues"`
		}
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		layers[payload.Layer] = true
		for _, issue := range payload.Issues {
			if issue.Key == "" {
				t.Fatal("loss lacks issue identity")
			}
			actions[issue.Action] = true
		}
	}
	if !actions["clipped"] || !actions["evicted"] || !layers["notes"] || !layers["volatile"] {
		t.Fatalf("missing loss audit actions=%v layers=%v", actions, layers)
	}
}
