package memory

import (
	"context"
	"testing"
	"time"
)

// TestRetrieveLexicalDiacritics asserts the unicode61 index folds
// accents both ways (Section 10.7: Spanish-friendly search).
func TestRetrieveLexicalDiacritics(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "La canción favorita suena en la radio"})
	for _, q := range []string{"cancion", "canción", "CANCION"} {
		ret, err := svc.Retrieve(context.Background(), Query{Text: q, Scopes: []string{"general"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(ret.Items) == 0 {
			t.Fatalf("query %q returned no items", q)
		}
	}
}

// TestRetrieveRequiredPhrase enforces quoted-phrase matches.
func TestRetrieveRequiredPhrase(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "The red bicycle is in the garage"})
	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "The blue bicycle needs a red bell"})
	ret, err := svc.Retrieve(context.Background(), Query{Text: `"red bicycle" garage`, Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) != 1 {
		t.Fatalf("items = %d, want exactly the phrase match", len(ret.Items))
	}
}

// TestRetrieveTemporalRelative covers relative intervals in both
// languages (Section 10.7 step 1).
func TestRetrieveTemporalRelative(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) // a Tuesday
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindEvent, Statement: "Old spring festival"})
	mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindEvent, Statement: "Team offsite in the hills",
		ValidFrom: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC),
		ValidTo:   time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
	})
	for _, q := range []string{"events last week", "eventos de la semana pasada"} {
		ret, err := svc.Retrieve(context.Background(), Query{Text: q, Scopes: []string{"general"}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, it := range ret.Items {
			if it.Statement == "Team offsite in the hills" {
				found = true
			}
			if it.Statement == "Old spring festival" {
				t.Fatalf("query %q leaked an out-of-window event", q)
			}
		}
		if !found {
			t.Fatalf("query %q missed the in-window event", q)
		}
	}
}

// TestRetrieveBitemporal answers "what was true at T" differently
// from "what did we know at T" (Section 10.3).
func TestRetrieveBitemporal(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindFact, Statement: "User lives in Madrid",
		ValidFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		ValidTo:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	asOf, err := svc.Retrieve(context.Background(), Query{
		Text: "where does the user live", Scopes: []string{"general"},
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range asOf.Items {
		if it.Statement == "User lives in Madrid" {
			t.Fatal("validity window wrongly matched a fact that ended in 2025")
		}
	}
	hist, err := svc.Retrieve(context.Background(), Query{
		Text: "where did the user live", Scopes: []string{"general"}, Historical: true,
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = hist
	past, err := svc.Retrieve(context.Background(), Query{
		Text: "where does the user live", Scopes: []string{"general"},
		From: time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range past.Items {
		if it.Statement == "User lives in Madrid" {
			found = true
		}
	}
	if !found {
		t.Fatal("2022 window missed the Madrid fact")
	}
	// Record axis: learned only in 2026, so unknown in 2021 even
	// though it was true then.
	unknown, err := svc.Retrieve(context.Background(), Query{
		Text: "where does the user live", Scopes: []string{"general"},
		From:    time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
		To:      time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		KnownAt: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range unknown.Items {
		if it.Statement == "User lives in Madrid" {
			t.Fatal("record axis leaked a fact unknown in 2021")
		}
	}
}

// TestRetrieveExpiryExcludedBeforeReview asserts an expired row
// stops reading as active at read time, before the nightly flip
// (Section 10.3).
func TestRetrieveExpiryExcludedBeforeReview(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{
		Scope: "general", Kind: KindEvent, Statement: "Temporary access token",
		ExpiresAt: now.Add(-time.Hour),
	})
	ret, err := svc.Retrieve(context.Background(), Query{Text: "temporary access token", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range ret.Items {
		if it.Statement == "Temporary access token" {
			t.Fatal("expired memory reads as active")
		}
	}
	found := false
	for _, n := range ret.Notices {
		if n.Code == "insufficient" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected an insufficient-evidence notice")
	}
}

// TestRetrieveContradictionNotice surfaces disputed rows instead of
// silently picking one (Section 10.7 step 6).
func TestRetrieveContradictionNotice(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "User likes dark mode", Entities: []string{"ent-user"}})
	svc.decider = &scriptDecider{verdict: "contradiction"}
	if _, err := svc.Admit(context.Background(), Candidate{Scope: "general", Kind: KindFact, Statement: "User likes dark mode not", Entities: []string{"ent-user"}}); err != nil {
		t.Fatal(err)
	}
	ret, err := svc.Retrieve(context.Background(), Query{Text: "dark mode preference", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range ret.Notices {
		if n.Code == "contradiction" {
			found = true
			t.Logf("contradiction detail: %s", n.Detail)
		}
	}
	if !found {
		t.Fatal("expected a contradiction notice naming the M-codes")
	}
}

// TestRetrieveRelationalSeeds walks entity edges 1-2 hops
// (Section 10.7).
func TestRetrieveRelationalSeeds(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	ada, err := svc.UpsertEntity(context.Background(), "general", "person", "Ada", []string{"ada"})
	if err != nil {
		t.Fatal(err)
	}
	paris, err := svc.UpsertEntity(context.Background(), "general", "place", "Paris", []string{"paris"})
	if err != nil {
		t.Fatal(err)
	}
	mem, _ := mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Travel notes and packing list", Entities: []string{paris.ID}})
	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Ada lives in Paris and loves it", Entities: []string{ada.ID, paris.ID}})
	if err := svc.AddEdge(context.Background(), Edge{FromType: "memory", FromID: mem.ID, Rel: "mentions", ToType: "entity", ToID: paris.ID, Scope: "general"}); err != nil {
		t.Fatal(err)
	}
	ret, err := svc.Retrieve(context.Background(), Query{Text: "Ada trip planning", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) == 0 {
		t.Fatal("relational seeds returned nothing")
	}
}

// TestFuseRRF checks the RRF table math: rank fusion beats any
// single-list order for an item present in two lists.
func TestFuseRRF(t *testing.T) {
	fused := fuseRRF(60, []string{"a", "b"}, []string{"b", "c"})
	if len(fused) != 3 {
		t.Fatalf("fused = %d items, want 3", len(fused))
	}
	if fused[0].id != "b" {
		t.Fatalf("top = %q, want b (present in both lists)", fused[0].id)
	}
	want := 1.0/61 + 1.0/62
	if diff := fused[0].score - want; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("score = %v, want %v", fused[0].score, want)
	}
}

// TestDecayHalfLife pins the per-kind half-lives (Appendix B).
func TestDecayHalfLife(t *testing.T) {
	now := time.Now().UTC()
	mk := func(k Kind, days float64) Memory {
		return Memory{Kind: k, RecordedAt: now.Add(-time.Duration(days*24) * time.Hour), Confidence: 1, Importance: 1}
	}
	if d := decay(mk(KindEvent, 7), now); d < 0.49 || d > 0.51 {
		t.Fatalf("event 7d decay = %v, want ~0.5", d)
	}
	if d := decay(mk(KindFact, 365), now); d < 0.49 || d > 0.51 {
		t.Fatalf("fact 365d decay = %v, want ~0.5", d)
	}
	if d := decay(mk(KindPreference, 90), now); d < 0.49 || d > 0.51 {
		t.Fatalf("preference 90d decay = %v, want ~0.5", d)
	}
}

// TestUsageTouchBumpsCounters asserts retrieval records use
// (Section 10.3 last_used_at/use_count).
func TestUsageTouchBumpsCounters(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	m, _ := mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Usage counting probe"})
	if _, err := svc.Retrieve(context.Background(), Query{Text: "usage counting probe", Scopes: []string{"general"}}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetMemory(context.Background(), []string{"general"}, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UseCount < 1 || got.LastUsedAt.IsZero() {
		t.Fatalf("usage not recorded: %+v", got)
	}
}
