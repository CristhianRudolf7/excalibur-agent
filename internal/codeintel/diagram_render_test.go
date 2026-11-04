package codeintel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type diagramBackendFake struct {
	Sources, Dialects, Formats []string
	Err                        error
	Cancel                     context.CancelFunc
}

func (f *diagramBackendFake) Render(_ context.Context, source, dialect, format string) (string, error) {
	f.Sources = append(f.Sources, source)
	f.Dialects = append(f.Dialects, dialect)
	f.Formats = append(f.Formats, format)
	if f.Cancel != nil {
		f.Cancel()
	}
	return "rendered " + format, f.Err
}
func invokeDiagram(t *testing.T, tool *CodeDiagramTool, args map[string]any) tools.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestCodeDiagramRenderedArtifacts(t *testing.T) {
	for _, lang := range []string{"go", "python", "typescript"} {
		t.Run(lang, func(t *testing.T) {
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", "diagrams", lang))); err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"mermaid", "graphviz"} {
				for _, output := range []string{"svg", "png", "pdf"} {
					backend := &diagramBackendFake{}
					tool := &CodeDiagramTool{Media: &tools.MediaDeps{Workdir: root, Diagram: backend}, RunID: "run-test"}
					res := invokeDiagram(t, tool, map[string]any{"path": ".", "level": "overview", "format": format, "output": output})
					if res.Status != tools.StatusOK || len(res.Artifacts) != 1 {
						t.Fatalf("%s/%s: %+v", format, output, res)
					}
					ref := res.Artifacts[0]
					if ref.Kind != "diagram" || ref.Dialect != format || ref.Format != output || ref.Group == "" || !filepath.IsAbs(ref.Path) || !filepath.IsAbs(ref.SourcePath) || !strings.Contains(filepath.ToSlash(ref.Path), "/artifacts/run-test/") {
						t.Fatalf("artifact metadata: %+v", ref)
					}
					src, err := os.ReadFile(ref.SourcePath)
					if err != nil {
						t.Fatal(err)
					}
					rendered, err := os.ReadFile(ref.Path)
					if err != nil {
						t.Fatal(err)
					}
					if string(src) != backend.Sources[0] || string(rendered) != "rendered "+output || backend.Dialects[0] != format || backend.Formats[0] != output {
						t.Fatal("backend contract not respected")
					}
					if strings.Contains(res.Output, "flowchart") || strings.Contains(res.Output, "digraph") || !strings.Contains(res.Output, ref.Path) || !strings.Contains(res.Output, "nodes") {
						t.Fatalf("not a compact artifact summary: %s", res.Output)
					}
				}
			}
		})
	}
}
func TestCodeDiagramGroupAndFailure(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		writeDiagramFile(t, root, fmt.Sprintf("module%02d/file.go", i), "package example\nfunc Main() {}")
	}
	backend := &diagramBackendFake{}
	tool := &CodeDiagramTool{Media: &tools.MediaDeps{Workdir: root, Diagram: backend}}
	res := invokeDiagram(t, tool, map[string]any{"path": ".", "level": "overview", "max_nodes": 3})
	if res.Status != tools.StatusOK || len(res.Artifacts) != 7 {
		t.Fatalf("split: %+v", res)
	}
	for _, ref := range res.Artifacts {
		if ref.Group != res.Artifacts[0].Group || !strings.Contains(res.Output, ref.Path) {
			t.Fatalf("unlinked artifact: %+v", ref)
		}
	}
	if !strings.Contains(res.Output, "10 collapsed") {
		t.Fatal(res.Output)
	}
	backend.Err = errors.New("renderer unavailable")
	res = invokeDiagram(t, tool, map[string]any{"path": ".", "level": "overview"})
	if res.Status != tools.StatusToolError || !strings.Contains(res.Output, "renderer unavailable") || len(res.Artifacts) != 0 {
		t.Fatalf("renderer error: %+v", res)
	}
	dirs, err := filepath.Glob(filepath.Join(root, ".excalibur", "artifacts", "standalone", "code-diagram-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("failed render leaked sources: %v %v", dirs, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	backend.Err = nil
	backend.Cancel = cancel
	raw := json.RawMessage(`{"path":".","level":"overview"}`)
	res, err = tool.Invoke(ctx, tools.Invocation{Arguments: raw})
	if err != nil || res.Status != tools.StatusCanceled || len(res.Artifacts) != 0 {
		t.Fatalf("renderer cancellation: %+v %v", res, err)
	}
}
func TestCodeDiagramRejectsArtifactEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeDiagramFile(t, root, "main.go", "package main\nfunc Main() {}")
	if err := os.Symlink(outside, filepath.Join(root, ".excalibur")); err != nil {
		t.Fatal(err)
	}
	tool := &CodeDiagramTool{Media: &tools.MediaDeps{Workdir: root, Diagram: &diagramBackendFake{}}}
	res := invokeDiagram(t, tool, map[string]any{"path": ".", "level": "overview"})
	if res.Status != tools.StatusToolError || !strings.Contains(res.Output, "artifact directory") {
		t.Fatalf("accepted artifact escape: %+v", res)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside workspace: %+v %v", entries, err)
	}
}

func TestCodeDiagramSubtreeOverviewAndPartialNotice(t *testing.T) {
	root := t.TempDir()
	writeDiagramFile(t, root, "sub/one/main.go", "package one\nfunc One() {}")
	writeDiagramFile(t, root, "sub/two/main.go", "package two\nfunc Two() {}")
	writeDiagramFile(t, root, "sub/oversized.go", strings.Repeat(" ", int(defaultDiagramIndexLimits.FileBytes+1)))
	backend := &diagramBackendFake{}
	tool := &CodeDiagramTool{Media: &tools.MediaDeps{Workdir: root, Diagram: backend}}
	res := invokeDiagram(t, tool, map[string]any{"path": "sub", "level": "overview"})
	if res.Status != tools.StatusOK || !strings.Contains(res.Output, "Partial index") {
		t.Fatalf("missing partial notice: %+v", res)
	}
	if len(backend.Sources) != 1 || !strings.Contains(backend.Sources[0], "sub/one") || !strings.Contains(backend.Sources[0], "sub/two") {
		t.Fatalf("subtree collapsed to workspace ancestor: %+v", backend.Sources)
	}
}
