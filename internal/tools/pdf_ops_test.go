package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

type pdfReadFake struct {
	pages     []int
	page, dpi int
}

func (*pdfReadFake) Formats() []string { return []string{"pdf"} }
func (f *pdfReadFake) Extract(ctx context.Context, r io.ReaderAt, size int64, o docextract.Options) (docextract.Document, error) {
	f.pages = o.Pages
	return docextract.Document{Format: "pdf", PageCount: 3, Paginated: true}, ctx.Err()
}
func (f *pdfReadFake) RenderPNG(ctx context.Context, r io.ReaderAt, size int64, page, dpi int) ([]byte, error) {
	f.page, f.dpi = page, dpi
	return []byte("fake PNG from renderer"), ctx.Err()
}

func invokePDF(t *testing.T, tool *PDFOpsTool, a PDFRequest) Result {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	r, err := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPDFOpsDefaultAndSecretHook(t *testing.T) {
	dir := t.TempDir()
	input := makePDF(t, dir, "input.pdf", false)
	called := false
	tool := &PDFOpsTool{Deps: &MediaDeps{Workdir: dir, PDFSecret: func(ctx context.Context, ref string) (string, error) {
		called = true
		if ref != "secret://pdf-test" {
			t.Fatal("wrong ref")
		}
		return "private-test-password", nil
	}}}
	req := PDFRequest{Action: "encrypt", Inputs: []string{"input.pdf"}, Output: "encrypted.pdf", Password: "secret://pdf-test"}
	r := invokePDF(t, tool, req)
	if r.Status != StatusOK || !called {
		t.Fatalf("%+v", r)
	}
	var out PDFOutput
	if err := json.Unmarshal([]byte(r.Output), &out); err != nil {
		t.Fatal(err)
	}
	if out.Path != filepath.Join(dir, "encrypted.pdf") || out.PageCount != 3 || len(out.SHA256) != 64 {
		t.Fatal(out)
	}
	if strings.Contains(r.Output, "private-test-password") {
		t.Fatal("password leaked")
	}
	req.Action = "info"
	req.Inputs = []string{out.Path}
	req.Password = "wrong-password"
	r = invokePDF(t, tool, req)
	if r.Status != StatusToolError || strings.Contains(r.Output, "wrong-password") {
		t.Fatal(r)
	}
	tool.Deps.PDFSecret = func(context.Context, string) (string, error) { return "", errors.New("private-resolver-error") }
	req.Password = "secret://pdf-test"
	r = invokePDF(t, tool, req)
	if strings.Contains(r.Output, "private-resolver-error") || r.Status != StatusToolError {
		t.Fatal(r)
	}
	tool.Deps.PDFSecret = nil
	r = invokePDF(t, tool, req)
	if !strings.Contains(r.Output, "not configured") {
		t.Fatal(r)
	}
	req = PDFRequest{Action: "optimize", Inputs: []string{input}, Output: input}
	r = invokePDF(t, tool, req)
	if r.Status == StatusOK {
		t.Fatal("overwrote input")
	}
	req.Overwrite = true
	r = invokePDF(t, tool, req)
	if r.Status != StatusOK {
		t.Fatal(r)
	}
	t.Setenv("EXCALIBUR_PDF_OPS", "off")
	r = invokePDF(t, tool, PDFRequest{Action: "info", Inputs: []string{input}})
	if r.Status == StatusOK || !strings.Contains(r.Output, "disabled") {
		t.Fatal(r)
	}
}

func TestPDFOpsConfinementAndResourceKeys(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	makePDF(t, root, "input.pdf", false)
	makePDF(t, outside, "outside.pdf", false)
	tool := &PDFOpsTool{Deps: &MediaDeps{Workdir: root}}
	for _, path := range []string{filepath.Join(outside, "outside.pdf"), "../escape.pdf"} {
		r := invokePDF(t, tool, PDFRequest{Action: "info", Inputs: []string{path}})
		if r.Status != StatusInputError {
			t.Fatal(r)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, req := range []PDFRequest{{Action: "info", Inputs: []string{"link/outside.pdf"}}, {Action: "optimize", Inputs: []string{"input.pdf"}, Output: "link/new.pdf"}, {Action: "split", Inputs: []string{"input.pdf"}, OutputDir: "link"}} {
		r := invokePDF(t, tool, req)
		if r.Status != StatusInputError {
			t.Fatal(r)
		}
	}
	raw := json.RawMessage(`{"action":"split","inputs":["input.pdf"],"output_dir":"parts"}`)
	keys, err := tool.ResourceKeys(raw)
	if err != nil || len(keys) != 2 || keys[0] != "file:"+filepath.Join(root, "input.pdf") || keys[1] != "file:"+filepath.Join(root, "parts") {
		t.Fatalf("%v %v", keys, err)
	}
}

func TestPDFOpsReadingHooks(t *testing.T) {
	dir := t.TempDir()
	input := makePDF(t, dir, "input.pdf", false)
	tool := &PDFOpsTool{}
	for _, action := range []string{"text", "render"} {
		r := invokePDF(t, tool, PDFRequest{Action: action, Inputs: []string{input}})
		if r.Status != StatusToolError || !strings.Contains(r.Output, "not configured") {
			t.Fatal(r)
		}
	}
	fake := &pdfReadFake{}
	tool.Deps = &MediaDeps{PDFText: fake, PDFRender: fake, Workdir: dir}
	r := invokePDF(t, tool, PDFRequest{Action: "text", Inputs: []string{input}, Ranges: []string{"2-3"}})
	if r.Status != StatusOK || len(fake.pages) != 2 || fake.pages[0] != 2 {
		t.Fatalf("%+v %v", r, fake.pages)
	}
	req := PDFRequest{Action: "render", Inputs: []string{input}, Output: "page.png", Page: 2, DPI: 96}
	r = invokePDF(t, tool, req)
	if r.Status != StatusOK || fake.page != 2 || fake.dpi != 96 {
		t.Fatal(r)
	}
	r = invokePDF(t, tool, req)
	if r.Status == StatusOK {
		t.Fatal("render overwrote output")
	}
	req.Page = 4
	r = invokePDF(t, tool, req)
	if r.Status != StatusInputError {
		t.Fatal(r)
	}
}

func TestPDFOpsThroughDeferredExecutor(t *testing.T) {
	dir := t.TempDir()
	input := makePDF(t, dir, "input.pdf", false)
	reg, cat, executor := deferredTestSetup()
	reg.Unregister("pdf_ops")
	if err := cat.RegisterDeferred(&PDFOpsTool{Deps: &MediaDeps{Workdir: dir}}); err != nil {
		t.Fatal(err)
	}
	if !cat.IsDeferred("pdf_ops") {
		t.Fatal("pdf_ops must remain deferred")
	}
	args, _ := json.Marshal(map[string]any{"name": "pdf_ops", "arguments": PDFRequest{Action: "extract_pages", Inputs: []string{input}, Output: "one.pdf", Pages: []int{2}}})
	result, err := executor.Execute(context.Background(), "call_tool", args, ExecuteOptions{})
	if err != nil || result.Status != StatusOK {
		t.Fatalf("%+v %v", result, err)
	}
	info, err := readPDFInfo(filepath.Join(dir, "one.pdf"), "")
	if err != nil || info.PageCount != 1 {
		t.Fatalf("%+v %v", info, err)
	}
}
