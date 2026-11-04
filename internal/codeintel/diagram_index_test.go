package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func writeDiagramFile(t *testing.T, root, rel, source string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestDiagramIndexBudgetsAndPriority(t *testing.T) {
	root := t.TempDir()
	writeDiagramFile(t, root, "a/other.go", "package other\nfunc Other() {}")
	writeDiagramFile(t, root, "z/focus.go", "package focus\nfunc Focus() {}")
	limits := defaultDiagramIndexLimits
	limits.Files = 1
	g, rep, err := indexDiagram(context.Background(), root, root, "z", false, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if !rep.Partial || rep.Files != 1 || rep.Prioritized != filepath.Join(root, "z") || len(g.SymbolsNamed("Focus")) != 1 || len(g.SymbolsNamed("Other")) != 0 {
		t.Fatalf("priority/budget: %+v %+v", rep, g.IndexedFiles())
	}
	for _, tc := range []struct {
		name   string
		update func(*diagramIndexLimits)
	}{
		{"bytes", func(l *diagramIndexLimits) { l.TotalBytes = 1 }},
		{"file size", func(l *diagramIndexLimits) { l.FileBytes = 1 }},
		{"entries", func(l *diagramIndexLimits) { l.Entries = 1 }},
		{"timeout", func(l *diagramIndexLimits) { l.Timeout = time.Nanosecond }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := defaultDiagramIndexLimits
			tc.update(&l)
			graph, r, e := indexDiagram(context.Background(), root, root, "", false, l)
			if e != nil {
				t.Fatal(e)
			}
			defer graph.Close()
			if !r.Partial || r.Files != 0 {
				t.Fatalf("limit ignored: %+v", r)
			}
		})
	}
}
func TestDiagramIndexRefreshAndExclusions(t *testing.T) {
	root := t.TempDir()
	writeDiagramFile(t, root, "go.mod", "module example.test/diagram\n")
	writeDiagramFile(t, root, "app/main.go", "package app\nimport \"example.test/diagram/lib\"\nfunc Main() { lib.Start() }")
	writeDiagramFile(t, root, "lib/start.go", "package lib\nfunc Start() {}")
	writeDiagramFile(t, root, "lib/start_test.go", "package lib\nfunc TestStart() {}")
	writeDiagramFile(t, root, "node_modules/skip.go", "package skip\n")
	writeDiagramFile(t, root, "ignored/skip.go", "package skip\n")
	writeDiagramFile(t, root, ".gitignore", "ignored/\n")
	outside := t.TempDir()
	writeDiagramFile(t, outside, "secret.go", "package secret\n")
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	g, r, err := indexDiagram(context.Background(), root, root, "", false, defaultDiagramIndexLimits)
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 2 {
		t.Fatalf("exclusions: %+v %+v", r, g.IndexedFiles())
	}
	p := fixtureProjection(t, g, "overview", "")
	if p.Edges[diagramEdge{Src: "app", Dst: "lib", Kind: EdgeImports}] != 1 {
		t.Fatalf("module prefix lost: %+v", p)
	}
	_ = g.Close()
	if err := os.Remove(filepath.Join(root, "lib/start.go")); err != nil {
		t.Fatal(err)
	}
	writeDiagramFile(t, root, "app/main.go", "package app\nfunc Changed() {}")
	g, _, err = indexDiagram(context.Background(), root, root, "", true, defaultDiagramIndexLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if len(g.SymbolsNamed("Start")) != 0 || len(g.SymbolsNamed("Main")) != 0 || len(g.SymbolsNamed("Changed")) != 1 || len(g.SymbolsNamed("TestStart")) != 1 {
		t.Fatal("fresh snapshot retained stale code or omitted tests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if graph, _, err := indexDiagram(ctx, root, root, "", false, defaultDiagramIndexLimits); err == nil {
		graph.Close()
		t.Fatal("ignored cancellation")
	}
}
func TestDiagramPathConfinement(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeDiagramFile(t, root, "sub/main.go", "package main\n")
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	tool := &CodeDiagramTool{Media: &tools.MediaDeps{Workdir: root}}
	for _, p := range []string{outside, "../", "escape", "sub/main.go", "missing"} {
		if _, _, err := tool.diagramPaths(p); err == nil {
			t.Errorf("accepted %s", p)
		}
	}
	got, target, err := tool.diagramPaths("sub")
	wantRoot, resolveErr := filepath.EvalSymlinks(root)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || got != wantRoot || target != filepath.Join(wantRoot, "sub") {
		t.Fatalf("valid subtree: %q %q %v", got, target, err)
	}
	g, rep, err := indexDiagram(context.Background(), got, target, "", false, defaultDiagramIndexLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if rep.Files != 1 || g.IndexedFiles()[0] != "sub/main.go" {
		t.Fatalf("subtree: %+v %+v", rep, g.IndexedFiles())
	}
}
