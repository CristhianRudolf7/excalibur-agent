package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagramArtifacts(t *testing.T) {
	for _, tc := range []struct{ name, output, dialect, format, extension string }{
		{"generated", "", "svg", "svg", "svg"},
		{"explicit", "nested/diagram.svg", "svg", "svg", "svg"},
		{"graphviz", "diagram.svg", "graphviz", "svg", "dot"},
		{"alias", "alias.svg", "dot", "svg", "dot"},
		{"png", "diagram.png", "graphviz", "", "dot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workdir := t.TempDir()
			source := testDiagramSVG
			if tc.dialect != "svg" {
				source = "digraph {a->b}"
			}
			dot := diagramFake(t, "dot", `if [ "$1" = '-Tpng' ]; then printf '\211PNG\r\n\032\nrendered' > "$4"; else printf '%s' '`+testDiagramSVG+`' > "$4"; fi`)
			tool := &DiagramRenderTool{Deps: &MediaDeps{Workdir: workdir, Diagram: &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"dot": dot})}}}
			args, _ := json.Marshal(map[string]string{"source": source, "dialect": tc.dialect, "output_format": tc.format, "output": tc.output, "title": "Service overview"})
			keys, err := tool.ResourceKeys(args)
			if err != nil {
				t.Fatal(err)
			}
			res, err := tool.Invoke(context.Background(), Invocation{Arguments: args})
			if err != nil || res.Status != StatusOK || len(res.Artifacts) != 1 {
				t.Fatalf("%+v, %v", res, err)
			}
			a := res.Artifacts[0]
			format := tc.format
			if format == "" {
				format = "png"
			}
			if a.Kind != "diagram" || a.Title != "Service overview" || a.Dialect != canonicalDiagramDialect(tc.dialect) || a.Format != format {
				t.Fatalf("%+v", a)
			}
			if !filepath.IsAbs(a.Path) || !filepath.IsAbs(a.SourcePath) || a.Path == a.SourcePath {
				t.Fatalf("invalid paths: %+v", a)
			}
			if !strings.HasSuffix(a.SourcePath, "."+tc.extension) {
				t.Fatal(a.SourcePath)
			}
			if len(keys) != 2 || keys[0] != "file:"+a.Path || keys[1] != "file:"+a.SourcePath {
				t.Fatalf("resource keys %v, artifact %+v", keys, a)
			}
			data, err := os.ReadFile(a.Path)
			if err != nil || !strings.Contains(string(data), "rendered") {
				t.Fatalf("render %q: %v", data, err)
			}
			data, err = os.ReadFile(a.SourcePath)
			if err != nil || string(data) != source {
				t.Fatalf("source %q: %v", data, err)
			}
			if !strings.Contains(res.Output, "using ") {
				t.Fatalf("renderer not reported: %s", res.Output)
			}
			// Repeated calls without an output remain idempotent and keep the same path.
			again, _ := tool.Invoke(context.Background(), Invocation{Arguments: args})
			if len(again.Artifacts) != 1 || again.Artifacts[0].Path != a.Path {
				t.Fatalf("non-idempotent path: %+v", again)
			}
		})
	}
}

func TestChartArtifactsPreserveRendering(t *testing.T) {
	for _, output := range []string{"", "nested/chart.svg"} {
		t.Run(output, func(t *testing.T) {
			tool := &ChartRenderTool{Deps: &MediaDeps{Workdir: t.TempDir()}}
			spec := ChartSpec{Kind: "bar", Title: "Counts", X: []string{"A", "B"}, Series: []ChartSeries{{Name: "Requests", Values: []float64{1, 2}}}}
			raw, _ := json.Marshal(map[string]any{"kind": spec.Kind, "title": spec.Title, "x": spec.X, "series": spec.Series, "output": output})
			res, err := tool.Invoke(context.Background(), Invocation{Arguments: raw})
			if err != nil || res.Status != StatusOK || len(res.Artifacts) != 1 {
				t.Fatalf("%+v, %v", res, err)
			}
			a := res.Artifacts[0]
			if a.Kind != "chart" || a.Title != spec.Title || a.Format != "svg" || a.Dialect != "" {
				t.Fatalf("%+v", a)
			}
			want, err := (&SVGChartBackend{}).Render(context.Background(), spec, "svg")
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(a.Path)
			if err != nil || string(got) != want {
				t.Fatalf("chart changed: %v", err)
			}
			source, err := os.ReadFile(a.SourcePath)
			if err != nil {
				t.Fatal(err)
			}
			var parsed ChartSpec
			if err := json.Unmarshal(source, &parsed); err != nil || parsed.Title != spec.Title || len(parsed.Series) != 1 {
				t.Fatalf("invalid source: %s, %v", source, err)
			}
			keys, err := tool.ResourceKeys(raw)
			if err != nil || len(keys) != 2 || keys[0] != "file:"+a.Path || keys[1] != "file:"+a.SourcePath {
				t.Fatalf("resource keys: %v, %v", keys, err)
			}
		})
	}
}

func TestDiagramArtifactRefusals(t *testing.T) {
	root := t.TempDir()
	tool := &DiagramRenderTool{Deps: &MediaDeps{Workdir: root}}
	for _, args := range []map[string]string{
		{"source": testDiagramSVG, "dialect": "svg", "output": "../escape.svg"},
		{"source": testDiagramSVG, "dialect": "svg", "output": "diagram.png", "output_format": "svg"},
		{"source": "bad syntax", "dialect": "mermaid", "output": "bad.svg"},
	} {
		raw, _ := json.Marshal(args)
		res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
		if res.Status == StatusOK || len(res.Artifacts) != 0 {
			t.Fatalf("invalid request accepted: %+v", res)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("failed request wrote files: %v", entries)
	}
	// Both output and source destinations are checked against workspace symlinks.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	raw, _ := json.Marshal(map[string]string{"source": testDiagramSVG, "dialect": "svg", "output": "link/diagram.svg"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusInputError || len(res.Artifacts) != 0 {
		t.Fatalf("symlink escaped workspace: %+v", res)
	}
	// Writing the source must not follow a neighboring file symlink outside.
	if err := os.WriteFile(filepath.Join(outside, "diagram.dot"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "diagram.dot"), filepath.Join(root, "diagram.dot")); err != nil {
		t.Fatal(err)
	}
	// An existing source symlink is resolved and rejected before rendering.
	raw, _ = json.Marshal(map[string]string{"source": "digraph {a->b}", "dialect": "graphviz", "output": "diagram.svg"})
	if _, err := tool.ResourceKeys(raw); err == nil {
		t.Fatal("source symlink outside workspace accepted")
	}
}
