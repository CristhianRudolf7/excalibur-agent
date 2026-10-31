package memory

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// seedRun plants a run row so journal entries join a scope.
func seedRun(t *testing.T, db *store.DB, id, scope string, at time.Time) {
	t.Helper()
	err := db.Write(context.Background(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO runs(id, scope_id, status, created_at) VALUES(?,'general','done',?)`, id, at.UnixMilli())
		_ = scope
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func seedJournal(t *testing.T, db *store.DB, run string, seq int64, typ, payload, status string, at time.Time) {
	t.Helper()
	err := db.Write(context.Background(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO journal(run_id, seq, type, payload, status, created_at) VALUES(?,?,?,?,?,?)`,
			run, seq, typ, payload, status, at.UnixMilli())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestHistoryFilters covers history_search over messages and
// recorded actions with date, tool and state filters.
func TestHistoryFilters(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	writeMessage(t, db, "msg_h1", "cnv_h", "general", "user", "deploy the blue service now", now.Add(-2*time.Hour))
	writeMessage(t, db, "msg_h2", "cnv_h", "general", "assistant", "deployed with warnings", now.Add(-time.Hour))
	seedRun(t, db, "run_h1", "general", now.Add(-2*time.Hour))
	seedJournal(t, db, "run_h1", 1, "tool_call", `{"tool":"exec","cmd":"deploy blue"}`, "ok", now.Add(-90*time.Minute))
	seedJournal(t, db, "run_h1", 2, "tool_call", `{"tool":"read_file","path":"x"}`, "failed", now.Add(-80*time.Minute))

	items, err := svc.History(context.Background(), []string{"general"}, HistoryQuery{Text: "deploy blue"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("history found nothing")
	}
	foundAction := false
	for _, it := range items {
		if it.Kind == "action:tool_call" && it.Tool == "exec" {
			foundAction = true
		}
	}
	if !foundAction {
		t.Fatalf("recorded action missing: %+v", items)
	}
	// State filter narrows to the failed call.
	failed, err := svc.History(context.Background(), []string{"general"}, HistoryQuery{Tool: "read_file", State: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].State != "failed" {
		t.Fatalf("state filter = %+v", failed)
	}
	// Date filter excludes everything before the window.
	empty, err := svc.History(context.Background(), []string{"general"},
		HistoryQuery{Text: "deploy", From: now.Add(-30 * time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range empty {
		if it.Kind == "message:user" {
			t.Fatalf("stale turn leaked: %+v", it)
		}
	}
}

// TestHistoryEnrichedKeys proves turns surface through extracted
// facts even when the words differ (Section 10.7 enriched keys).
func TestHistoryEnrichedKeys(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	writeMessage(t, db, "msg_e1", "cnv_e", "general", "user", "the parrot is called Captain Beak", now.Add(-3*time.Hour))
	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact,
		Statement: "The parrot is called Captain Beak",
		Sources:   []Source{{Kind: "message", Ref: "msg_e1"}}})

	items, err := svc.History(context.Background(), []string{"general"}, HistoryQuery{Text: "parrot name captain"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range items {
		if it.ID == "msg_e1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("source turn not pulled by its fact: %+v", items)
	}
}
