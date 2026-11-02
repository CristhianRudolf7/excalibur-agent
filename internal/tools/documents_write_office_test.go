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
	if e != nil || len(keys) != 1 || keys[0] != "file:"+filepath.Join(root, "report.docx") {
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
