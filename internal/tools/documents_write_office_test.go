package tools

import (
	"context"
	"encoding/json"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/edit"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficeToolsDeferredPipeline(t *testing.T) {
	root := t.TempDir()
	reg := NewMapRegistry()
	cat := NewDeferredCatalog(reg)
	if e := RegisterDeferredMedia(reg, cat, &MediaDeps{Workdir: root}); e != nil {
		t.Fatal(e)
	}
	exec := NewExecutor(reg)
	search := SearchToolsTool{Catalog: cat}
	res, e := search.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"query":"create Office document"}`)})
	if e != nil || !strings.Contains(res.Output, "document_create") {
		t.Fatal(res, e)
	}
	call := CallToolTool{Catalog: cat, Executor: exec}
	res, e = call.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"name":"document_create","arguments":{"path":"report.docx","format":"docx","spec":{"title":"Report","blocks":[{"kind":"paragraph","text":"Before"}]}}}`)})
	if e != nil || res.Status != StatusOK {
		t.Fatal(res, e)
	}
	var result edit.Result
	if e = json.Unmarshal([]byte(res.Output), &result); e != nil || result.SHA256 == "" || len(result.Anchors) != 2 {
		t.Fatal(res, e)
	}
	args, _ := json.Marshal(map[string]any{"path": "report.docx", "expected_sha256": result.SHA256, "ops": []any{map[string]any{"action": "replace_text", "anchor": result.Anchors[1], "match": "Before", "text": "After"}}})
	res, e = exec.Execute(context.Background(), "document_edit", args, ExecuteOptions{})
	if e != nil || res.Status != StatusOK {
		t.Fatal(res, e)
	}
	data, _ := os.ReadFile(filepath.Join(root, "report.docx"))
	p, e := opc.Open(data)
	if e != nil || !strings.Contains(string(p.Parts["word/document.xml"]), "After") {
		t.Fatal(e)
	}
	ct, _ := reg.Get("document_create")
	keys, e := ct.ResourceKeys(json.RawMessage(`{"path":"report.docx"}`))
	wantPath, resolveErr := filepath.EvalSymlinks(filepath.Join(root, "report.docx"))
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if e != nil || len(keys) != 1 || keys[0] != "file:"+wantPath {
		t.Fatal(keys, e)
	}
	for _, name := range []string{"document_create", "document_edit"} {
		tool, _ := reg.Get(name)
		if !cat.IsDeferred(name) {
			t.Fatal(name, "not deferred")
		}
		for _, profile := range tool.Definition().Profiles {
			if profile == "eval" {
				t.Fatal("eval admitted")
			}
		}
	}
}
func TestOfficeToolConfinementOverwriteSwitchAndCancel(t *testing.T) {
	root := t.TempDir()
	tool := DocumentCreateTool{Deps: &MediaDeps{Workdir: root}}
	invoke := func(path string, overwrite bool) Result {
		a, _ := json.Marshal(map[string]any{"path": path, "format": "docx", "spec": map[string]any{"title": "test"}, "overwrite": overwrite})
		r, _ := tool.Invoke(context.Background(), Invocation{Arguments: a})
		return r
	}
	if r := invoke("../escape.docx", false); r.Status != StatusInputError {
		t.Fatal(r)
	}
	outside := t.TempDir()
	if e := os.Symlink(outside, filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	if r := invoke("link/escape.docx", false); r.Status != StatusInputError {
		t.Fatal(r)
	}
	if r := invoke("test.docx", false); r.Status != StatusOK {
		t.Fatal(r)
	}
	if r := invoke("test.docx", false); r.Status != StatusInputError {
		t.Fatal(r)
	}
	if r := invoke("test.docx", true); r.Status != StatusOK {
		t.Fatal(r)
	}
	t.Setenv("EXCALIBUR_OFFICE_WRITE", "off")
	if r := invoke("disabled.docx", false); r.Status != StatusDenied {
		t.Fatal(r)
	}
	if _, e := os.Stat(filepath.Join(root, "disabled.docx")); !os.IsNotExist(e) {
		t.Fatal("disabled write occurred")
	}
	t.Setenv("EXCALIBUR_OFFICE_WRITE", "on")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, _ := tool.Invoke(ctx, Invocation{Arguments: json.RawMessage(`{"path":"cancel.docx","format":"docx","spec":{"title":"test"}}`)})
	if r.Status != StatusCanceled {
		t.Fatal(r)
	}
	r, _ = tool.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":"img.docx","format":"docx","spec":{"blocks":[{"kind":"image","image":"../outside.png"}]}}`)})
	if r.Status != StatusInputError {
		t.Fatal(r)
	}
}

func TestStyledPresentationThroughDeferredTools(t *testing.T) {
	root := t.TempDir()
	reg := NewMapRegistry()
	cat := NewDeferredCatalog(reg)
	if err := RegisterDeferredMedia(reg, cat, &MediaDeps{Workdir: root}); err != nil {
		t.Fatal(err)
	}
	call := CallToolTool{Catalog: cat, Executor: NewExecutor(reg)}
	res, err := call.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"name":"document_create","arguments":{"path":"brief.pptx","format":"pptx","spec":{"theme":{"accent":"#14B8A6","style":"dark"},"slides":[{"kind":"title","title":"Project brief"},{"kind":"section","title":"Evidence","subtitle":"The next chapter"},{"kind":"cards","title":"Results","cards":[{"title":"Tests","text":"All passing","value":"38"},{"title":"Platforms","text":"A consistent experience","value":"3"}],"notes":"Supporting detail"},{"kind":"quote","title":"Feedback","quote":"A clearer workflow","attribution":"Customer"}]}}}`)})
	if err != nil || res.Status != StatusOK {
		t.Fatal(res, err)
	}
	var result edit.Result
	if err := json.Unmarshal([]byte(res.Output), &result); err != nil || result.SHA256 == "" || len(result.Issues) != 0 {
		t.Fatal(res, err)
	}
	if !strings.Contains(strings.Join(result.Anchors, " "), "slide:3/shape:3") {
		t.Fatal("main body anchor missing", result.Anchors)
	}
	args, err := json.Marshal(map[string]any{"name": "document_edit", "arguments": map[string]any{
		"path": "brief.pptx", "expected_sha256": result.SHA256,
		"ops": []any{map[string]any{"action": "replace_text", "anchor": "slide:3/shape:3", "match": "38", "text": "39"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err = call.Invoke(context.Background(), Invocation{Arguments: args})
	if err != nil || res.Status != StatusOK {
		t.Fatal(res, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "brief.pptx"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := opc.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if issues := p.Validate(); len(issues) != 0 {
		t.Fatal(issues)
	}
	for part, want := range map[string]string{
		"ppt/theme/theme1.xml":            `val="14B8A6"`,
		"ppt/slides/slide3.xml":           `<a:t>39</a:t>`,
		"ppt/slides/slide4.xml":           "A clearer workflow",
		"ppt/notesSlides/notesSlide3.xml": "Supporting detail",
	} {
		if !strings.Contains(string(p.Parts[part]), want) {
			t.Fatal("styled presentation lost content", part, want)
		}
	}
}

func TestPresentationRuntimeValidationKeepsDiscoverySchemaPinned(t *testing.T) {
	tool := &DocumentCreateTool{}
	before := string(tool.Definition().InputSchema)
	valid := json.RawMessage(`{"path":"brief.pptx","format":"pptx","spec":{"theme":{"accent":"#6366F1"},"slides":[{"kind":"quote","quote":"Simple","attribution":"Customer"}]}}`)
	if ValidateAgainstSchema(valid, tool.Definition().InputSchema) == nil {
		t.Fatal("new fields leaked into the pinned discovery schema")
	}
	if err := ValidateAgainstSchema(valid, tool.runtimeInputSchema(valid)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"path":"brief.pptx","format":"pptx","spec":{"theme":{"unexpected":true}}}`,
		`{"path":"brief.pptx","format":"pptx","spec":{"theme":{"style":"invalid"}}}`,
		`{"path":"brief.pptx","format":"pptx","spec":{"slides":[{"kind":"quote","quote":42}]}}`,
		`{"path":"brief.pptx","format":"pptx","spec":{"slides":[{"kind":"quote","unknown":"value"}]}}`,
		`{"path":"brief.pptx","format":"pptx","spec":{"slides":[{"kind":"cards","cards":[{"title":"Fact","text":"Detail","unknown":true},{"title":"Fact","text":"Detail"}]}]}}`,
		`{"path":"brief.pptx","format":"pptx","spec":{"slides":[{"kind":"cards","cards":[{"title":"Fact"},{"title":"Fact","text":"Detail"}]}]}}`,
		`{"path":"brief.docx","format":"docx","spec":{"theme":{"style":"dark"}}}`,
	} {
		args := json.RawMessage(raw)
		if err := ValidateAgainstSchema(args, tool.runtimeInputSchema(args)); err == nil {
			t.Fatal("runtime schema accepted invalid extension", raw)
		}
	}
	if string(tool.Definition().InputSchema) != before {
		t.Fatal("runtime validation mutated discovery schema")
	}
}
