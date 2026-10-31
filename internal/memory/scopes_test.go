package memory

import (
	"context"
	"testing"
	"time"
)

// TestScopeIsolationFourPaths plants memories, documents and an
// entity in project A and queries from B through all four retrieval
// paths: any appearance is a leak (Section 9.5, suite mem-scope).
func TestScopeIsolationFourPaths(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svcA := newTestService(t, db, now, "project:A")
	svcB := newTestService(t, db, now, "project:B")

	const marker = "quetzal vault kaleidoscope"
	mA, _ := mustAdmitScoped(t, svcA, "project:A", Candidate{Scope: "project:A", Kind: KindFact, Statement: "Project A stores " + marker + " designs"})
	_ = mA
	ent, err := svcA.UpsertEntity(context.Background(), "project:A", "concept", "Kaleidoscope", []string{"kaleidoscope"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svcA.AddEdge(context.Background(), Edge{FromType: "memory", FromID: mA.ID, Rel: "mentions", ToType: "entity", ToID: ent.ID, Scope: "project:A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svcA.Ingest(context.Background(), []string{"project:A"}, "a.txt", "text/plain", []byte("Project A "+marker+" handbook text."),
		IngestOptions{Title: "Handbook A"}); err != nil {
		t.Fatal(err)
	}

	ret, err := svcB.Retrieve(context.Background(), Query{Text: marker + " designs", Scopes: []string{"project:B"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range ret.Items {
		if it.Scope == "project:A" {
			t.Fatalf("scope leak in retrieval: %+v", it)
		}
	}
	hits, _, err := svcB.LibrarySearch(context.Background(), []string{"project:B"}, "", marker+" handbook", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Doc.Scope == "project:A" {
			t.Fatalf("scope leak in library: %+v", h.Doc)
		}
	}
	hist, err := svcB.History(context.Background(), []string{"project:B"}, HistoryQuery{Text: marker})
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 0 {
		t.Fatalf("scope leak in history: %+v", hist)
	}
	// A scoped service rejects reads outside its set outright.
	if _, err := svcB.GetMemory(context.Background(), []string{"project:B"}, mA.ID); err == nil {
		t.Fatal("cross-scope GetMemory should fail")
	}
	if _, err := svcB.Retrieve(context.Background(), Query{Text: "x", Scopes: []string{"project:A"}}); err == nil {
		t.Fatal("fixed scope set must reject foreign scopes")
	}
	if _, err := svcB.Admit(context.Background(), Candidate{Scope: "project:A", Kind: KindFact, Statement: "write outside"}); err == nil {
		t.Fatal("fixed scope set must reject foreign writes")
	}
}

func mustAdmitScoped(t *testing.T, svc *Service, scope string, c Candidate) (Memory, AdmitOutcome) {
	t.Helper()
	outcome, err := svc.Admit(context.Background(), c)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	mem, err := svc.GetMemory(context.Background(), []string{scope}, outcome.MemoryID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	return mem, outcome
}

// TestScopeLabelsTwoProjects asks across two projects: results stay
// labeled per scope, never mixed (Section 9.4).
func TestScopeLabelsTwoProjects(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now)

	mustAdmit(t, svc, Candidate{Scope: "project:A", Kind: KindFact, Statement: "Build uses the oak toolchain"})
	mustAdmit(t, svc, Candidate{Scope: "project:B", Kind: KindFact, Statement: "Build uses the pine toolchain"})

	ret, err := svc.Retrieve(context.Background(), Query{Text: "build toolchain", Scopes: []string{"project:A", "project:B"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) != 2 {
		t.Fatalf("items = %d, want one per project", len(ret.Items))
	}
	byScope := map[string]string{}
	for _, it := range ret.Items {
		byScope[it.Scope] = it.Statement
	}
	if byScope["project:A"] != "Build uses the oak toolchain" || byScope["project:B"] != "Build uses the pine toolchain" {
		t.Fatalf("unlabeled mix: %+v", byScope)
	}
}
