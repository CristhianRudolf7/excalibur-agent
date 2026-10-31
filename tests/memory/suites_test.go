package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
)

// Suite mem-scope (Sections 9.5, 23.5): memories planted in project
// A queried from B through every retrieval path never appear.

func TestMemScopeSuite(t *testing.T) {
	db := openSuiteDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newSuiteService(t, db, now)

	const secret = "meridian harbor ledger"
	suiteAdmit(t, svc, memory.Candidate{Scope: "project:A", Kind: memory.KindFact,
		Statement: "Project A archives the " + secret,
		Entities:  []string{"ent-ledger"}})
	ent, err := svc.UpsertEntity(context.Background(), "project:A", "concept", "Ledger", []string{"ledger"})
	if err != nil {
		t.Fatal(err)
	}
	mems, err := svc.ListMemories(context.Background(), []string{"project:A"}, memory.MemoryFilter{}, 10)
	if err != nil || len(mems) == 0 {
		t.Fatalf("plant: %v %d", err, len(mems))
	}
	if err := svc.AddEdge(context.Background(), memory.Edge{FromType: "memory", FromID: mems[0].ID, Rel: "mentions", ToType: "entity", ToID: ent.ID, Scope: "project:A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ingest(context.Background(), []string{"project:A"}, "a.txt", "text/plain",
		[]byte("Handbook of the "+secret+" for project A."), memory.IngestOptions{Title: "Handbook A"}); err != nil {
		t.Fatal(err)
	}

	// Lexical, semantic, temporal and relational paths from B.
	queries := []memory.Query{
		{Text: secret, Scopes: []string{"project:B"}},
		{Text: "meridian harbor", Scopes: []string{"project:B"}},
		{Text: "ledger", Scopes: []string{"project:B"}},
		{Text: "project archives", Scopes: []string{"project:B"},
			From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for i, q := range queries {
		ret, err := svc.Retrieve(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range ret.Items {
			if it.Scope == "project:A" {
				t.Fatalf("query %d leaked project A memory: %+v", i, it)
			}
		}
	}
	hits, _, err := svc.LibrarySearch(context.Background(), []string{"project:B"}, "", secret+" handbook", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("library leaked %d project A chunks", len(hits))
	}
	hist, err := svc.History(context.Background(), []string{"project:B"}, memory.HistoryQuery{Text: secret})
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 0 {
		t.Fatalf("history leaked %d turns", len(hist))
	}
}

// Suite mem-time (Sections 10.3, 10.7, 23.5): changing and expiring
// facts answer "now" versus "before" correctly.
func TestMemTimeSuite(t *testing.T) {
	db := openSuiteDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newSuiteService(t, db, now)

	suiteAdmit(t, svc, memory.Candidate{Scope: "general", Kind: memory.KindFact,
		Statement: "Office is on Baker Street",
		ValidFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		ValidTo:   time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)})
	suiteAdmit(t, svc, memory.Candidate{Scope: "general", Kind: memory.KindFact,
		Statement: "Office is on Camden High Street",
		ValidFrom: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)})
	suiteAdmit(t, svc, memory.Candidate{Scope: "general", Kind: memory.KindEvent,
		Statement: "Visitor pass for Friday", ExpiresAt: now.Add(-time.Hour)})

	ask := func(text string, from, to time.Time, hist bool) []string {
		t.Helper()
		ret, err := svc.Retrieve(context.Background(), memory.Query{
			Text: text, Scopes: []string{"general"}, From: from, To: to, Historical: hist})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range ret.Items {
			out = append(out, it.Statement)
		}
		return out
	}
	mk := func(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }

	got := ask("where is the office", mk(2026, 1, 1), mk(2026, 2, 1), false)
	if !contains(got, "Office is on Camden High Street") || contains(got, "Office is on Baker Street") {
		t.Fatalf("now-answer wrong: %v", got)
	}
	got = ask("where was the office before", mk(2022, 1, 1), mk(2023, 1, 1), false)
	if !contains(got, "Office is on Baker Street") || contains(got, "Office is on Camden High Street") {
		t.Fatalf("before-answer wrong: %v", got)
	}
	got = ask("visitor pass", mk(2026, 10, 1), mk(2026, 11, 1), false)
	if contains(got, "Visitor pass for Friday") {
		t.Fatalf("expired event reads as active: %v", got)
	}
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// Suite mem-conflict (Sections 10.5, 10.7, 23.5): contradictory
// claims surface as disputes with notices; explicit temporal change
// replaces. Both run through the default deterministic decider.
func TestMemConflictSuite(t *testing.T) {
	db := openSuiteDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newSuiteService(t, db, now)

	suiteAdmit(t, svc, memory.Candidate{Scope: "general", Kind: memory.KindFact,
		Statement: "The deploy runs on fridays", Entities: []string{"ent-deploy"}})
	outcome, err := svc.Admit(context.Background(), memory.Candidate{Scope: "general", Kind: memory.KindFact,
		Statement: "The deploy runs on fridays never", Entities: []string{"ent-deploy"}})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Action != memory.ActionDisputed || !outcome.NeedsUser {
		t.Fatalf("action = %+v, want disputed with a user question", outcome)
	}
	ret, err := svc.Retrieve(context.Background(), memory.Query{Text: "deploy schedule", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) == 0 {
		t.Fatal("conflict suite: no items retrieved")
	}
	disputed := 0
	for _, it := range ret.Items {
		if it.Status == memory.StatusDisputed {
			disputed++
		}
	}
	if disputed == 0 {
		t.Fatal("expected disputed rows in the results")
	}
	found := false
	for _, n := range ret.Notices {
		if n.Code == "contradiction" {
			found = true
			t.Logf("contradiction detail: %s", n.Detail)
		}
	}
	if !found {
		t.Fatal("disputed rows without a contradiction notice")
	}
	// Explicit temporal change replaces deterministically.
	suiteAdmit(t, svc, memory.Candidate{Scope: "general", Kind: memory.KindFact,
		Statement: "The team standup is at nine sharp daily", Entities: []string{"ent-standup"}})
	o2, err := svc.Admit(context.Background(), memory.Candidate{Scope: "general", Kind: memory.KindFact,
		Statement: "The team standup is now at nine sharp daily never", Entities: []string{"ent-standup"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = o2
	if o2.Action != memory.ActionReplaced {
		t.Fatalf("temporal change action = %q, want replaced", o2.Action)
	}
}

// Suite mem-cite (Sections 10.9, 23.5): paginated and unpaginated
// questions cite exact anchors; invalid refs drop.
func TestMemCiteSuite(t *testing.T) {
	db := openSuiteDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newSuiteService(t, db, now)

	pages := [][]string{
		{"The harbor tariff schedule opens this page."},
		{"Berth fees rise by five percent next quarter."},
	}
	doc, err := svc.Ingest(context.Background(), []string{"general"}, "tariff.pdf", "application/pdf", []byte("x"),
		memory.IngestOptions{Title: "Tariff", Pages: pages, Paginated: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ingest(context.Background(), []string{"general"}, "note.txt", "text/plain",
		[]byte("# Ops\n\nThe night checklist lives here."), memory.IngestOptions{Title: "Ops"}); err != nil {
		t.Fatal(err)
	}
	hits, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "run_cite", "berth fees percent", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("cite search: %v", err)
	}
	cit := hits[0].Citation
	if cit.Page != 2 || cit.DocID != doc.ID {
		t.Fatalf("paginated citation wrong: %+v", cit)
	}
	if cit.Verbatim != "Tariff, p. 2, ¶ 1" {
		t.Fatalf("verbatim = %q", cit.Verbatim)
	}
	uhits, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "run_cite", "night checklist", 5)
	if err != nil || len(uhits) == 0 {
		t.Fatalf("unpag search: %v", err)
	}
	if uhits[0].Citation.Page != 0 || uhits[0].Citation.Section != "Ops" {
		t.Fatalf("unpaginated citation invented a page: %+v", uhits[0].Citation)
	}
	good := "[[doc:" + doc.ID + "#" + hits[0].Chunk.ID + "]]"
	valid, dropped := svc.ValidateCitations(context.Background(),
		[]string{good, "[[doc:" + doc.ID + "#chn_nope]]", "[[doc:nope#chn_nope]]"},
		[]string{"run_cite"})
	if len(valid) != 1 || len(dropped) != 2 {
		t.Fatalf("cite validation: valid=%d dropped=%d", len(valid), len(dropped))
	}
}
