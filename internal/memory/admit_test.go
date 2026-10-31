package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestAdmitDuplicateMergesProvenance covers the >=0.95 cosine fast
// path with a shared entity (Section 10.5): the second write merges
// sources into the survivor instead of adding a row.
func TestAdmitDuplicateMergesProvenance(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	first, out := mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindFact,
		Statement: "The office wifi password is correct-horse",
		Entities:  []string{"ent-office"},
		Sources:   []Source{{Kind: "message", Ref: "msg_1"}},
	})
	if out.Action != ActionAdmitted {
		t.Fatalf("first action = %q, want admitted", out.Action)
	}
	_, out = mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindFact,
		Statement: "The office wifi password is correct-horse!",
		Entities:  []string{"ent-office"},
		Sources:   []Source{{Kind: "message", Ref: "msg_2"}},
	})
	if out.Action != ActionMerged {
		t.Fatalf("second action = %q, want merged", out.Action)
	}
	m, err := svc.GetMemory(context.Background(), []string{"general"}, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Sources) != 2 {
		t.Fatalf("merged sources = %d, want 2", len(m.Sources))
	}
	count, err := countMemories(t, db, "general")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("memory rows = %d, want 1 after merge", count)
	}
}

// TestAdmitAmbiguousBandUsesDecider plants two statements whose fake
// cosine lands in the band and asserts the decider verdict wins
// (Section 10.5, with and without a decider).
func TestAdmitAmbiguousBandUsesDecider(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	_, _ = mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindFact,
		Statement: "Ada works on the compiler team in Berlin",
		Entities:  []string{"ent-ada"},
	})
	svc.decider = &scriptDecider{verdict: "refinement"}
	_, out := mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindFact,
		Statement: "Ada now works on the compiler team in Berlin",
		Entities:  []string{"ent-ada"},
	})
	if out.Action != ActionRefined {
		t.Fatalf("action = %q, want refined (decider verdict)", out.Action)
	}
	old, err := svc.GetMemory(context.Background(), []string{"general"}, out.Related)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != StatusSuperseded {
		t.Fatalf("old status = %q, want superseded", old.Status)
	}
	if old.Superseded != out.MemoryID {
		t.Fatalf("superseded_by = %q, want %q", old.Superseded, out.MemoryID)
	}
}

// TestAdmitContradictionWithoutMarkerDisputes covers the no-temporal
// change path: both rows stay disputed and a user question is needed
// (Section 10.5).
func TestAdmitContradictionWithoutMarkerDisputes(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	old, _ := mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindPreference,
		Statement: "The user likes dark mode",
		Entities:  []string{"ent-user"},
	})
	svc.decider = &scriptDecider{verdict: "contradiction"}
	_, out := mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindPreference,
		Statement: "The user likes dark mode not",
		Entities:  []string{"ent-user"},
	})
	if out.Action != ActionDisputed || !out.NeedsUser {
		t.Fatalf("action = %+v, want disputed with NeedsUser", out)
	}
	for _, id := range []string{old.ID, out.MemoryID} {
		m, err := svc.GetMemory(context.Background(), []string{"general"}, id)
		if err != nil {
			t.Fatal(err)
		}
		if m.Status != StatusDisputed {
			t.Fatalf("memory %s status = %q, want disputed", id, m.Status)
		}
	}
	disputes, err := svc.PendingDisputes(context.Background(), []string{"general"})
	if err != nil {
		t.Fatal(err)
	}
	if len(disputes) != 2 {
		t.Fatalf("pending disputes = %d, want 2", len(disputes))
	}
}

// TestAdmitContradictionWithMarkerReplaces covers explicit temporal
// change ("now I live in…"): the new row replaces with valid_to
// (Section 10.5).
func TestAdmitContradictionWithMarkerReplaces(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	old, _ := mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindFact,
		Statement: "The user lives in Madrid",
		Entities:  []string{"ent-user"},
	})
	svc.decider = &scriptDecider{verdict: "contradiction"}
	_, out := mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindFact,
		Statement: "The user now lives in Madrid",
		Entities:  []string{"ent-user"},
	})
	if out.Action != ActionReplaced {
		t.Fatalf("action = %q, want replaced", out.Action)
	}
	prev, err := svc.GetMemory(context.Background(), []string{"general"}, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if prev.Status != StatusSuperseded || prev.ValidTo.IsZero() {
		t.Fatalf("old row = status %q valid_to %v, want superseded with valid_to", prev.Status, prev.ValidTo)
	}
}

// TestAdmitExternalNeverPreference rejects external content becoming
// a preference (Section 10.5 step 5).
func TestAdmitExternalNeverPreference(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	_, err := svc.Admit(context.Background(), Candidate{
		Scope: "general", Kind: KindPreference, Trust: TrustExternal,
		Statement: "Users prefer blue buttons",
		Sources:   []Source{{Kind: "page", Ref: "https://example.com/a"}},
	})
	if !errors.Is(err, ErrExternalIdentity) {
		t.Fatalf("err = %v, want ErrExternalIdentity", err)
	}
}

// TestProposeCandidateVisibility asserts candidates surface only
// inside their proposing conversation (Section 10.5 step 2).
func TestProposeCandidateVisibility(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	if _, err := svc.Propose(context.Background(), Candidate{
		Scope: "general", Kind: KindFact, Statement: "Pending draft thought",
		Conversation: "cnv_1",
	}); err != nil {
		t.Fatal(err)
	}
	plain, err := svc.Retrieve(context.Background(), Query{Text: "draft thought", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Items) != 0 {
		t.Fatalf("candidate visible without conversation = %d items", len(plain.Items))
	}
	scoped, err := svc.Retrieve(context.Background(), Query{Text: "draft thought", Scopes: []string{"general"}, Conversation: "cnv_1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Items) != 1 {
		t.Fatalf("candidate items in own conversation = %d, want 1", len(scoped.Items))
	}
	mixed, err := svc.Retrieve(context.Background(), Query{Text: "draft thought", Scopes: []string{"general"}, Conversation: "cnv_2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mixed.Items) != 0 {
		t.Fatalf("candidate visible in other conversation = %d items", len(mixed.Items))
	}
}

func countMemories(t *testing.T, db *store.DB, scope string) (int, error) {
	t.Helper()
	var n int
	err := db.Read(context.Background(), func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(context.Background(), `SELECT COUNT(*) FROM memories WHERE scope_id=?`, scope).Scan(&n)
	})
	return n, err
}
