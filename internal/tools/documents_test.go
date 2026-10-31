package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mediaTestSetup() *MediaDeps {
	return &MediaDeps{
		Documents: &FileDocumentBackend{},
		Vision:    &MemoryVisionBackend{},
		PDF:       &FilePDFBackend{},
		Convert:   &FileConvertBackend{},
		Diagram:   &SVGDiagramBackend{},
		Chart:     &SVGChartBackend{},
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDocumentReadAnchors(t *testing.T) {
	md := "# Title\n\nIntro paragraph.\n\n## Section One\n\nBody text here.\n"
	path := writeTemp(t, "note.md", md)
	tool := &DocumentReadTool{Deps: mediaTestSetup()}
	raw, _ := json.Marshal(map[string]string{"path": path})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("document_read: %+v", res)
	}
	if !strings.Contains(res.Output, "Title") || !strings.Contains(res.Output, "Section One") {
		t.Fatalf("sections missing: %q", res.Output)
	}
	if !strings.Contains(res.Output, "\"page\"") {
		t.Fatalf("pages missing: %q", res.Output)
	}
	// Page selection.
	raw2, _ := json.Marshal(map[string]any{"path": path, "page": 1})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusOK {
		t.Fatalf("document_read page: %+v", res2)
	}
	// Unsupported format.
	bin := writeTemp(t, "a.exe", "MZ binary")
	raw3, _ := json.Marshal(map[string]string{"path": bin})
	res3, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusInputError {
		t.Fatalf("unsupported format must be input_error, got %+v", res3)
	}
}

func TestPDFOpsPreserveContent(t *testing.T) {
	deps := mediaTestSetup()
	tool := &PDFOpsTool{Deps: deps}
	a := writeTemp(t, "a.pdf", "page one content\fpage two content")
	b := writeTemp(t, "b.pdf", "page three content")
	merged := filepath.Join(t.TempDir(), "merged.pdf")
	raw, _ := json.Marshal(map[string]any{"action": "merge", "inputs": []string{a, b}, "output": merged})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("merge: %+v", res)
	}
	data, _ := os.ReadFile(merged)
	if !strings.Contains(string(data), "page one") || !strings.Contains(string(data), "page three") {
		t.Fatalf("merge lost content: %q", string(data))
	}
	// Extract pages.
	extracted := filepath.Join(t.TempDir(), "p1.pdf")
	raw2, _ := json.Marshal(map[string]any{"action": "extract_pages", "inputs": []string{merged}, "output": extracted, "pages": []int{1}})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusOK {
		t.Fatalf("extract: %+v", res2)
	}
	// Rotate with bad angle.
	raw3, _ := json.Marshal(map[string]any{"action": "rotate", "inputs": []string{a}, "output": filepath.Join(t.TempDir(), "r.pdf"), "angle": 45})
	res3, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusInputError {
		t.Fatalf("bad angle must be input_error, got %+v", res3)
	}
	// Annotate/redact path.
	annotated := filepath.Join(t.TempDir(), "ann.pdf")
	raw4, _ := json.Marshal(map[string]any{"action": "annotate", "inputs": []string{a}, "output": annotated, "notes": []string{"review"}})
	res4, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw4})
	if res4.Status != StatusOK {
		t.Fatalf("annotate: %+v", res4)
	}
	annData, _ := os.ReadFile(annotated)
	if !strings.Contains(string(annData), "review") {
		t.Fatalf("annotation lost: %q", string(annData))
	}
}

func TestFileConvertPairs(t *testing.T) {
	tool := &FileConvertTool{Deps: mediaTestSetup()}
	raw, _ := json.Marshal(map[string]any{"input": "", "output": "", "list": true})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("list pairs: %+v", res)
	}
	var listed struct {
		Pairs []string `json:"pairs"`
	}
	if err := json.Unmarshal([]byte(res.Output), &listed); err != nil || len(listed.Pairs) == 0 {
		t.Fatalf("list pairs JSON: %v %q", err, res.Output)
	}
	found := false
	for _, pr := range listed.Pairs {
		if pr == "md->html" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pairs missing md->html: %v", listed.Pairs)
	}
	md := writeTemp(t, "doc.md", "# Hello\n\nWorld\n")
	html := filepath.Join(t.TempDir(), "doc.html")
	raw2, _ := json.Marshal(map[string]string{"input": md, "output": html})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusOK {
		t.Fatalf("md->html: %+v", res2)
	}
	out, _ := os.ReadFile(html)
	if !strings.Contains(string(out), "<h1>Hello</h1>") {
		t.Fatalf("html headings: %q", string(out))
	}
	// Unsupported pair without a converter is an input error, not a silent pass.
	png := filepath.Join(t.TempDir(), "doc.png")
	raw3, _ := json.Marshal(map[string]string{"input": md, "output": png, "from": "md", "to": "png"})
	res3, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusInputError {
		t.Fatalf("unsupported pair must be input_error, got %+v", res3)
	}
	if !strings.Contains(res3.Output, "supported") {
		t.Fatalf("unsupported pair must list supported pairs: %q", res3.Output)
	}
}

func TestDiagramRenderSyntax(t *testing.T) {
	tool := &DiagramRenderTool{Deps: mediaTestSetup()}
	raw, _ := json.Marshal(map[string]any{"source": "graph TD\nA-->B", "dialect": "mermaid", "output_format": "svg"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK || !strings.Contains(res.Output, "<svg") {
		t.Fatalf("mermaid svg: %+v", res)
	}
	raw2, _ := json.Marshal(map[string]any{"source": "not a diagram", "dialect": "mermaid"})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusInputError || !strings.Contains(strings.ToLower(res2.Output), "mermaid") {
		t.Fatalf("bad mermaid must name the dialect: %+v", res2)
	}
	raw3, _ := json.Marshal(map[string]any{"source": "digraph { a -> b }", "dialect": "graphviz"})
	res3, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusOK {
		t.Fatalf("graphviz: %+v", res3)
	}
	// PNG without a rasterizer is an honest input error.
	raw4, _ := json.Marshal(map[string]any{"source": "graph TD\nA-->B", "dialect": "mermaid", "output_format": "png"})
	res4, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw4})
	if res4.Status != StatusInputError {
		t.Fatalf("png without rasterizer must be input_error, got %+v", res4)
	}
}

func TestChartRenderSVG(t *testing.T) {
	tool := &ChartRenderTool{Deps: mediaTestSetup()}
	raw, _ := json.Marshal(map[string]any{
		"kind": "bar", "title": "T",
		"series": []any{map[string]any{"name": "s", "values": []any{1, 2, 3}}},
	})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK || !strings.Contains(res.Output, "<svg") {
		t.Fatalf("bar chart: %+v", res)
	}
	raw2, _ := json.Marshal(map[string]any{"kind": "pie", "series": []any{}})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusInputError {
		t.Fatalf("bad chart must be input_error, got %+v", res2)
	}
}

func TestVisionExtractContract(t *testing.T) {
	img := writeTemp(t, "shot.png", "fake-png-bytes")
	tool := &VisionExtractTool{Deps: mediaTestSetup()}
	raw, _ := json.Marshal(map[string]any{"input": img, "mode": "text", "pages": []int{1}})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("vision text: %+v", res)
	}
	if !strings.Contains(res.Output, "vision-fake-1") && !strings.Contains(res.Output, "model") {
		t.Fatalf("vision must report the model used: %q", res.Output)
	}
	raw2, _ := json.Marshal(map[string]any{"input": img, "mode": "layout"})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusOK || !strings.Contains(res2.Output, "box") {
		t.Fatalf("vision layout boxes: %+v", res2)
	}
	raw3, _ := json.Marshal(map[string]any{"input": img, "mode": "bogus"})
	res3, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusInputError {
		t.Fatalf("bad mode must be input_error, got %+v", res3)
	}
}
