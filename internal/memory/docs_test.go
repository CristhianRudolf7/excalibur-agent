package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func testDoc(t *testing.T, svc *Service, scope, kind string) string {
	t.Helper()
	id, err := svc.ResolveDoc(context.Background(), scope, kind)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedFragmentDirect(t *testing.T, svc *Service, docID, heading, text string) Fragment {
	t.Helper()
	frag, err := svc.AppendFragment(context.Background(), svc.Scopes(), docID, heading, text, "")
	if err != nil {
		t.Fatal(err)
	}
	return frag
}

// TestDocEditGuarded covers the hash-guarded edit cycle: a good
// edit applies and versions, a stale hash returns the current
// fragment, and two consecutive mismatches escalate (Section 10.2).
func TestDocEditGuarded(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	docID := testDoc(t, svc, "general", DocNotebook)
	frag := seedFragmentDirect(t, svc, docID, "notes", "First note")
	edited, err := svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: frag.Hash, Op: OpReplace, Text: "First note, revised",
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Version != frag.Version+1 || edited.Text != "First note, revised" {
		t.Fatalf("bad edit result: %+v", edited)
	}
	// Stale hash: nothing applies, current returned.
	_, err = svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: frag.Hash, Op: OpReplace, Text: "Lost update",
	})
	var hm *HashMismatchError
	if !errors.As(err, &hm) {
		t.Fatalf("err = %v, want HashMismatchError", err)
	}
	if hm.Current.Text != "First note, revised" {
		t.Fatalf("current text = %q", hm.Current.Text)
	}
	// Second consecutive mismatch escalates instead of insisting.
	_, err = svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: frag.Hash, Op: OpReplace, Text: "Lost update again",
	})
	if !errors.Is(err, ErrEscalated) {
		t.Fatalf("err = %v, want ErrEscalated", err)
	}
	// History kept both versions.
	hist, err := svc.FragmentHistory(context.Background(), []string{"general"}, docID, frag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) < 2 {
		t.Fatalf("history versions = %d, want >= 2", len(hist))
	}
	// A good edit resets the counter: one more stale hash is a
	// plain mismatch again, not an escalation.
	cur, err := svc.ReadDoc(context.Background(), []string{"general"}, docID, "section:notes")
	if err != nil || len(cur.Frags) == 0 {
		t.Fatal(err)
	}
	if _, err := svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: cur.Frags[0].Hash, Op: OpReplace, Text: "Recovery edit",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: "stale", Op: OpReplace, Text: "x",
	})
	if !errors.As(err, &hm) || errors.Is(err, ErrEscalated) {
		t.Fatalf("err = %v, want plain mismatch after reset", err)
	}
}

// TestDocProfileProvenance requires a motivating user message for
// identity changes and marks unrequested ones as proposals
// (Section 10.2).
func TestDocProfileProvenance(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	docID := testDoc(t, svc, "general", DocUser)
	frag := seedFragmentDirect(t, svc, docID, "preferences", "Likes concise answers")
	_, err := svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: frag.Hash, Op: OpReplace, Text: "Likes verbose answers",
	})
	if !errors.Is(err, ErrNoSource) {
		t.Fatalf("err = %v, want ErrNoSource", err)
	}
	prop, err := svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: frag.Hash, Op: OpReplace,
		Text: "Likes verbose answers", Provenance: "msg_user_9",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !prop.Proposal {
		t.Fatal("unrequested profile change should land as a visible proposal")
	}
	// Explicit user requests apply without the proposal flag.
	expl, err := svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: prop.Hash, Op: OpReplace,
		Text: "Likes verbose answers", Provenance: "msg_user_9", Explicit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if expl.Proposal {
		t.Fatal("explicit change should not be a proposal")
	}
}

// TestDocRevert restores an older version as a new guarded version.
func TestDocRevert(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	docID := testDoc(t, svc, "general", DocNotebook)
	frag := seedFragmentDirect(t, svc, docID, "log", "v1 text")
	v2, err := svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
		Doc: docID, FragmentID: frag.ID, ExpectedHash: frag.Hash, Op: OpReplace, Text: "v2 text",
	})
	if err != nil {
		t.Fatal(err)
	}
	back, err := svc.RevertFragment(context.Background(), []string{"general"}, docID, frag.ID, 1, v2.Hash, "msg_user_1")
	if err != nil {
		t.Fatal(err)
	}
	if back.Text != "v1 text" || back.Version != v2.Version+1 {
		t.Fatalf("revert = %+v", back)
	}
}
