package codeintel

import (
	"path/filepath"
	"testing"
)

func testGraph(t *testing.T) *Graph {
	t.Helper()
	g, err := OpenGraph(filepath.Join(t.TempDir(), "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

func mustParse(t *testing.T, rel, src string) FileUpdate {
	t.Helper()
	upd, err := ParseFile(rel, []byte(src))
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	return upd
}

func TestGraphUpsertAndResolve(t *testing.T) {
	g := testGraph(t)
	lib := mustParse(t, "pkg/lib.go", `package pkg

func Add(a, b int) int { return a + b }

func Double(n int) int { return Add(n, n) }
`)
	if err := g.UpsertFile(lib); err != nil {
		t.Fatal(err)
	}
	if got := g.FileHash("pkg/lib.go"); got == "" {
		t.Fatal("file hash not stored")
	}
	syms := g.FileSymbols("pkg/lib.go")
	if len(syms) != 2 || syms[0].Name != "Add" || syms[1].Name != "Double" {
		t.Fatalf("symbols: %+v", syms)
	}
	// Double calls Add: resolved live from the name index even
	// though BuildUpdate never pinned cross-file targets.
	var doubleID string
	for _, s := range syms {
		if s.Name == "Double" {
			doubleID = s.ID
		}
	}
	found := false
	for _, e := range g.OutEdges(doubleID) {
		if e.Kind == EdgeCalls {
			if n, ok := g.Node(e.Dst); ok && n.Name == "Add" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("Double->Add call missing: %+v", g.OutEdges(doubleID))
	}
	// Reverse direction resolves too.
	var addID string
	for _, s := range syms {
		if s.Name == "Add" {
			addID = s.ID
		}
	}
	callers := g.InEdges(addID)
	callerKinds := map[string]bool{}
	for _, e := range callers {
		callerKinds[e.Kind] = true
	}
	if !callerKinds[EdgeCalls] {
		t.Fatalf("Add has no callers: %+v", callers)
	}
}

func TestGraphIncrementalNoStaleEdges(t *testing.T) {
	g := testGraph(t)
	v1 := mustParse(t, "a.go", "package p\n\nfunc Callee() {}\nfunc Caller() { Callee() }\n")
	if err := g.UpsertFile(v1); err != nil {
		t.Fatal(err)
	}
	var callerID string
	for _, s := range g.FileSymbols("a.go") {
		if s.Name == "Caller" {
			callerID = s.ID
		}
	}
	if len(g.OutEdges(callerID)) == 0 {
		t.Fatal("expected Caller->Callee edge")
	}
	// Edit removes the call: the incremental update must drop it.
	v2 := mustParse(t, "a.go", "package p\n\nfunc Callee() {}\nfunc Caller() {}\n")
	if v1.Hash == v2.Hash {
		t.Fatal("hash did not change")
	}
	if err := g.UpsertFile(v2); err != nil {
		t.Fatal(err)
	}
	for _, e := range g.OutEdges(callerID) {
		if e.Kind == EdgeCalls {
			t.Fatalf("stale call edge after edit: %+v", e)
		}
	}
}

func TestGraphRemoveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codegraph.db")
	g, err := OpenGraph(path)
	if err != nil {
		t.Fatal(err)
	}
	upd := mustParse(t, "x.py", "def foo():\n    bar()\n\ndef bar():\n    pass\n")
	if err := g.UpsertFile(upd); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	// Reload from disk: nodes, static edges and uses survive.
	g2, err := OpenGraph(path)
	if err != nil {
		t.Fatal(err)
	}
	defer g2.Close()
	if got := g2.FileHash("x.py"); got == "" {
		t.Fatal("hash lost across reload")
	}
	syms := g2.FileSymbols("x.py")
	if len(syms) != 2 {
		t.Fatalf("symbols lost: %+v", syms)
	}
	// Dynamic edges resolve live after reload as well.
	calls := 0
	for _, s := range syms {
		for _, e := range g2.OutEdges(s.ID) {
			if e.Kind == EdgeCalls {
				calls++
			}
		}
	}
	if calls == 0 {
		t.Fatal("call edge lost across reload")
	}
	if err := g2.RemoveFile("x.py"); err != nil {
		t.Fatal(err)
	}
	if len(g2.FileSymbols("x.py")) != 0 {
		t.Fatal("symbols survive removal")
	}
	if got := g2.FileHash("x.py"); got != "" {
		t.Fatal("hash survives removal")
	}
}

func TestGraphRequirements(t *testing.T) {
	g := testGraph(t)
	upd := mustParse(t, "impl.go", "package p\n\nfunc Wanted() {}\n")
	if err := g.UpsertFile(upd); err != nil {
		t.Fatal(err)
	}
	syms := g.FileSymbols("impl.go")
	if err := g.AttachRequirement("R1", syms[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := g.AttachRequirement("R1", FileID("impl.go")); err != nil {
		t.Fatal(err)
	}
	impls := g.Implementers("R1")
	if len(impls) != 2 {
		t.Fatalf("implementers: %+v", impls)
	}
	if len(g.Implementers("R-none")) != 0 {
		t.Fatal("unknown requirement should be empty")
	}
	if err := g.AttachRequirement("R1", "sym:nope:0"); err == nil {
		t.Fatal("expected error for unknown node")
	}
}

func TestGraphStats(t *testing.T) {
	g := testGraph(t)
	for rel, src := range map[string]string{
		"a.go":      "package p\n\nfunc A() {}\n",
		"a_test.go": "package p\n\nfunc TestA() { A() }\n",
		"doc.md":    "# Title\n\nUses `A` here.\n",
	} {
		if err := g.UpsertFile(mustParse(t, rel, src)); err != nil {
			t.Fatal(err)
		}
	}
	st := g.Stats()
	if st.Files != 3 || st.Symbols != 2 || st.TestSymbols != 1 || st.Sections != 1 {
		t.Fatalf("stats: %+v", st)
	}
	// Doc cites resolve against the project name index.
	found := false
	for _, e := range g.OutEdges(SectionID("doc.md", "title", 1)) {
		if e.Kind == EdgeCites {
			found = true
		}
	}
	if !found {
		t.Fatal("doc cites edge missing")
	}
}
