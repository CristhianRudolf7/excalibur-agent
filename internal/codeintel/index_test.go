package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, src := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndexAllAndIncremental(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":       "module example.com/proj\n\ngo 1.27\n",
		"calc/calc.go": "package calc\n\nfunc Add(a, b int) int { return a + b }\n",
		"app.py":       "from . import util\n\ndef run():\n    return util.help()\n",
		"app/util.py":  "def help():\n    return 1\n",
		"docs/read.md": "# Read\n\nSee `Add`.\n",
		".gitignore":   "ignored/\n",
		"ignored/x.go": "package ignored\n",
		"notes.bin":    "\x00\x01\x02",
	})
	g := testGraph(t)
	x := NewIndexer(g, root)
	ctx := context.Background()
	rep, err := x.IndexAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned == 0 || rep.Updated == 0 {
		t.Fatalf("rep: %+v", rep)
	}
	if rep.Removed != 0 {
		t.Fatalf("unexpected removals: %+v", rep)
	}
	for _, rel := range []string{"calc/calc.go", "app.py", "app/util.py", "docs/read.md", "go.mod"} {
		_ = rel
	}
	if g.FileHash("calc/calc.go") == "" {
		t.Error("calc.go not indexed")
	}
	if g.FileHash("ignored/x.go") != "" {
		t.Error("gitignored file indexed")
	}
	if g.FileHash("notes.bin") != "" {
		t.Error("binary file indexed")
	}
	// Deterministic no-op: a second full scan updates nothing.
	rep2, err := x.IndexAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Updated != 0 || rep2.Removed != 0 {
		t.Fatalf("second scan not a no-op: %+v", rep2)
	}
	// Edit one file: only it updates.
	writeTree(t, root, map[string]string{
		"calc/calc.go": "package calc\n\nfunc Add(a, b int) int { return a + b }\n\nfunc Sub(a, b int) int { return a - b }\n",
	})
	rep3, err := x.NotifyChanged(ctx, []string{"calc/calc.go"})
	if err != nil {
		t.Fatal(err)
	}
	if rep3.Updated != 1 {
		t.Fatalf("rep3: %+v", rep3)
	}
	found := false
	for _, s := range g.FileSymbols("calc/calc.go") {
		if s.Name == "Sub" {
			found = true
		}
	}
	if !found {
		t.Error("Sub missing after edit")
	}
	// Delete a file: it drops out of the graph.
	if err := os.Remove(filepath.Join(root, "app/util.py")); err != nil {
		t.Fatal(err)
	}
	rep4, err := x.NotifyChanged(ctx, []string{"app/util.py"})
	if err != nil {
		t.Fatal(err)
	}
	if rep4.Removed != 1 {
		t.Fatalf("rep4: %+v", rep4)
	}
	if len(g.FileSymbols("app/util.py")) != 0 {
		t.Error("removed file symbols survive")
	}
}

func TestIndexGoModuleRewrite(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":               "module example.com/proj\n\ngo 1.27\n",
		"pkg/calc/calc.go":     "package calc\n\nfunc Add(a, b int) int { return a + b }\n",
		"cmd/app/main.go":      "package main\n\nimport \"example.com/proj/pkg/calc\"\n\nfunc main() { println(calc.Add(1, 2)) }\n",
		"cmd/app/main_test.go": "package main\n\nimport \"example.com/proj/pkg/calc\"\n\nfunc TestMain() { println(calc.Add(1, 1)) }\n",
	})
	g := testGraph(t)
	x := NewIndexer(g, root)
	if _, err := x.IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	// First-party import resolves to the provider directory module.
	mods := map[string]bool{}
	for _, e := range g.OutEdges(FileID("cmd/app/main.go")) {
		if e.Kind == EdgeImports {
			mods[e.Dst] = true
		}
	}
	if !mods["mod:dir:go:pkg/calc"] {
		t.Errorf("first-party import not rewritten: %v", mods)
	}
	// Impacted selection reaches the test through the call edge.
	res := g.ImpactedTests([]string{"pkg/calc/calc.go"})
	files := res.Files()
	if len(files) != 1 || files[0] != "cmd/app/main_test.go" {
		t.Errorf("impacted: %+v", res)
	}
}
