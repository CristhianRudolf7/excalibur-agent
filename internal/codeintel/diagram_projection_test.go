package codeintel

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func diagramFixture(t *testing.T, lang string) *Graph {
	t.Helper()
	g, err := OpenGraph(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	if _, err := NewIndexer(g, filepath.Join("testdata", "diagrams", lang)).IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	return g
}
func fixtureProjection(t *testing.T, g *Graph, level, focus string) diagramProjection {
	t.Helper()
	p, err := projectDiagram(context.Background(), g, diagramArgs{Level: level, Focus: focus, Depth: 1}, "example.test/diagram")
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestDiagramFixtureProjections(t *testing.T) {
	for _, lang := range []string{"go", "python", "typescript"} {
		t.Run(lang, func(t *testing.T) {
			g := diagramFixture(t, lang)
			overview := fixtureProjection(t, g, "overview", "")
			entry := "app"
			if lang == "go" {
				entry = "cmd"
			}
			if _, ok := overview.Nodes[entry]; !ok {
				t.Fatalf("missing entry package: %+v", overview.Nodes)
			}
			if overview.Edges[diagramEdge{Src: entry, Dst: "lib", Kind: EdgeImports}] != 1 {
				t.Fatalf("missing package import: %+v", overview.Edges)
			}
			module := fixtureProjection(t, g, "module", "lib")
			ext, library := "go", "fmt"
			if lang == "python" {
				ext, library = "py", "json"
			} else if lang == "typescript" {
				ext, library = "ts", "lodash"
			}
			file := "lib/service." + ext
			if len(module.Nodes) != 2 || len(module.Edges) != 1 || module.Nodes[file].Label != file || module.Edges[diagramEdge{Src: file, Dst: "ext:" + library, Kind: EdgeImports}] != 1 {
				t.Fatalf("module file/library projection: %+v", module)
			}
			if lang == "go" && fixtureProjection(t, g, "module", "example.test/diagram/lib").source("mermaid", "LR") != module.source("mermaid", "LR") {
				t.Fatal("Go import-path focus differs from directory focus")
			}
			for id := range module.Nodes {
				if strings.Contains(id, "test") {
					t.Fatalf("included tests: %s", id)
				}
			}
			symbols := fixtureProjection(t, g, "symbols", "lib")
			found := map[string]bool{}
			for _, n := range symbols.Nodes {
				for _, name := range []string{"Service", "Start", "Finish"} {
					if strings.HasPrefix(n.Label, name+" (") {
						found[name] = true
					}
				}
			}
			if len(found) != 3 {
				t.Fatalf("symbols: %+v", symbols.Nodes)
			}
			calls, defines := 0, 0
			for e := range symbols.Edges {
				if e.Kind == EdgeCalls {
					calls++
				}
				if e.Kind == EdgeDefines {
					defines++
				}
			}
			if calls < 1 || defines < 3 {
				t.Fatalf("symbols edges: %+v", symbols.Edges)
			}
			flow := fixtureProjection(t, g, "flow", "main")
			if len(flow.Nodes) != 2 || len(flow.Edges) != 1 {
				t.Fatalf("depth 1 flow: %+v", flow)
			}
			a := diagramArgs{Level: "flow", Focus: "main", Depth: 2}
			deep, err := projectDiagram(context.Background(), g, a, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(deep.Nodes) != 3 || len(deep.Edges) != 2 {
				t.Fatalf("depth 2 flow: %+v", deep)
			}
			for _, format := range []string{"mermaid", "graphviz"} {
				want := symbols.source(format, "LR")
				for i := 0; i < 20; i++ {
					if got := fixtureProjection(t, g, "symbols", "lib").source(format, "LR"); got != want {
						t.Fatalf("unstable %s output", format)
					}
				}
				if !strings.Contains(want, "lib") || !strings.Contains(want, "defines") {
					t.Fatal(want)
				}
			}
		})
	}
}
func TestDiagramBounding(t *testing.T) {
	p := newDiagramProjection()
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("node%02d", i)
		p.node(id, id, fmt.Sprintf("dir%d", i%3))
		p.edge("node00", id, EdgeCalls, 1)
	}
	// Duplicate edges are aggregated, and self loops never survive.
	p.edge("node00", "node00", EdgeCalls, 1)
	p.edge("node00", "node01", EdgeCalls, 1)
	q := p.bounded(8)
	if len(q.Nodes) != 8 || q.Collapsed != 43 {
		t.Fatalf("bound: %+v", q)
	}
	if _, ok := q.Nodes["node00"]; !ok {
		t.Fatal("highest degree node lost")
	}
	if _, ok := q.Nodes["node01"]; !ok {
		t.Fatal("weighted degree ignored")
	}
	for e := range q.Edges {
		if e.Src == e.Dst {
			t.Fatal("self loop")
		}
	}
	for _, format := range []string{"mermaid", "graphviz"} {
		source := q.source(format, "TB")
		if !strings.Contains(source, "+43 more") || !strings.Contains(source, "dir0") {
			t.Fatal(source)
		}
		if format == "graphviz" && !strings.Contains(source, "subgraph cluster_") {
			t.Fatal(source)
		}
		for i := 0; i < 20; i++ {
			if p.bounded(8).source(format, "TB") != source {
				t.Fatal("unstable collapse")
			}
		}
	}
	p.Entry = "node49"
	q = p.bounded(2)
	if _, ok := q.Nodes[p.Entry]; !ok {
		t.Fatal("flow entry lost")
	}
}
func TestDiagramTestsAndFocusErrors(t *testing.T) {
	g := diagramFixture(t, "go")
	p, err := projectDiagram(context.Background(), g, diagramArgs{Level: "symbols", Focus: "lib", IncludeTests: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	tests := 0
	for e := range p.Edges {
		if e.Kind == EdgeTests {
			tests++
		}
	}
	if tests == 0 {
		t.Fatalf("missing test edges: %+v", p.Edges)
	}
	for _, a := range []diagramArgs{{Level: "flow", Focus: "missing"}, {Level: "flow", Focus: "Service"}, {Level: "symbols", Focus: "missing"}} {
		if _, err := projectDiagram(context.Background(), g, a, ""); err == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := projectDiagram(ctx, g, diagramArgs{Level: "overview"}, ""); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestDiagramSymbolFocusAndImplementationEdges(t *testing.T) {
	g := diagramFixture(t, "go")
	p := fixtureProjection(t, g, "symbols", "Start")
	calls := 0
	for e := range p.Edges {
		if e.Kind == EdgeCalls {
			calls++
		}
	}
	if calls != 2 {
		t.Fatalf("symbol neighborhood: %+v", p.Edges)
	}
	upd, err := ParseFile("lib/types.go", []byte("package lib\ntype Interface interface{}\ntype Implementation struct{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var src, dst string
	for _, n := range upd.Nodes {
		if n.Name == "Implementation" {
			src = n.ID
		}
		if n.Name == "Interface" {
			dst = n.ID
		}
	}
	upd.Edges = append(upd.Edges, Edge{Src: src, Dst: dst, Kind: EdgeImplements})
	if err := g.UpsertFile(upd); err != nil {
		t.Fatal(err)
	}
	p = fixtureProjection(t, g, "symbols", "Implementation")
	if p.Edges[diagramEdge{Src: src, Dst: dst, Kind: EdgeImplements}] != 1 {
		t.Fatalf("implementation relationship lost: %+v", p)
	}
}
func TestDiagramWeightedImportsAndExternalLibraries(t *testing.T) {
	g, err := OpenGraph(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for _, rel := range []string{"app/one.go", "app/two.go"} {
		upd, err := ParseFile(rel, []byte("package app\nimport (\"github.com/acme/library/core\"; \"github.com/acme/library/extra\")\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := g.UpsertFile(upd); err != nil {
			t.Fatal(err)
		}
	}
	p := fixtureProjection(t, g, "overview", "")
	if len(p.Nodes) != 2 || len(p.Edges) != 1 || p.Edges[diagramEdge{Src: "app", Dst: "ext:github.com/acme/library", Kind: EdgeImports}] != 2 {
		t.Fatalf("weighted imports/external collapse: %+v", p)
	}
}
func TestDiagramFlowAmbiguityCyclesAndEscaping(t *testing.T) {
	g, err := OpenGraph(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for _, rel := range []string{"one/a.go", "two/b.go"} {
		upd, err := ParseFile(rel, []byte("package example\nfunc Entry() { Next() }\nfunc Next() { Entry() }\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := g.UpsertFile(upd); err != nil {
			t.Fatal(err)
		}
	}
	a := diagramArgs{Level: "flow", Focus: "Entry", Depth: 10}
	if _, err := projectDiagram(context.Background(), g, a, ""); err == nil {
		t.Fatal("ambiguous entry accepted")
	}
	a.Focus = g.SymbolsNamed("Entry")[0]
	p, err := projectDiagram(context.Background(), g, a, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Nodes) != 4 || len(p.Edges) != 8 {
		t.Fatalf("cycle traversal: %+v", p)
	}
	p.node("label", "quoted \"<label>&", "group")
	if !strings.Contains(p.source("mermaid", "LR"), "&quot;&lt;label&gt;&amp;") {
		t.Fatal("Mermaid label not escaped")
	}
	if !strings.Contains(p.source("graphviz", "LR"), `\"<label>&`) {
		t.Fatal("DOT label not escaped")
	}
}

func TestDiagramRootPackageImport(t *testing.T) {
	root := t.TempDir()
	writeDiagramFile(t, root, "go.mod", "module example.test/diagram\n")
	writeDiagramFile(t, root, "root.go", "package library\nfunc Start() {}")
	writeDiagramFile(t, root, "cmd/main.go", "package main\nimport \"example.test/diagram\"\nfunc main() { library.Start() }")
	g, _, err := indexDiagram(context.Background(), root, root, "", false, defaultDiagramIndexLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	p := fixtureProjection(t, g, "overview", "")
	if p.Edges[diagramEdge{Src: "cmd", Dst: ".", Kind: EdgeImports}] != 1 {
		t.Fatalf("missing root package: %+v", p)
	}
	module := fixtureProjection(t, g, "module", "cmd")
	if module.Nodes["pkg:"].Label != "." {
		t.Fatalf("blank root dependency: %+v", module)
	}
}
