package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf/testpdf"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "internal" && os.Args[2] == "pdf-worker" {
		if mode := os.Getenv("PDF_TEST_WORKER"); mode != "" {
			simulateWorker(mode)
			os.Exit(0)
		}
		if err := ServeWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func simulateWorker(mode string) {
	if path := os.Getenv("PDF_TEST_STARTS"); path != "" {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		_, _ = fmt.Fprintln(f, os.Getpid())
		_ = f.Close()
	}
	dec, enc := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var q request
		if err := dec.Decode(&q); err != nil {
			return
		}
		r := response{}
		if q.Operation == "metadata" {
			r.Document = docextract.Document{Format: "pdf", Paginated: true, PageCount: 3}
		} else {
			if q.Page == 2 {
				if mode == "crash" {
					os.Exit(7)
				}
				time.Sleep(5 * time.Second)
			}
			r.Page = docextract.Page{Number: q.Page, Width: 612, Height: 792, Method: docextract.MethodNative, Quality: 1}
		}
		if err := enc.Encode(r); err != nil {
			return
		}
	}
}
func extractFixture(t *testing.T, pages []testpdf.Page, o testpdf.Options) (docextract.Document, error) {
	t.Helper()
	b := testpdf.Build(pages, o)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return New(Options{InProcess: true}).Extract(ctx, bytes.NewReader(b), int64(len(b)), docextract.Options{})
}
func body(text string, y float64) testpdf.Line { return testpdf.Line{Text: text, X: 50, Y: y} }
func pageText(p docextract.Page) string {
	var s []string
	for _, b := range p.Blocks {
		s = append(s, b.Text)
	}
	return strings.Join(s, "\n")
}
func TestNativeMetadataLabelsOutlineAndBoxes(t *testing.T) {
	pages := []testpdf.Page{{Lines: []testpdf.Line{body("Clean document text with exact positions.", 700)}}, {Lines: []testpdf.Line{body("Second physical page", 700)}}, {Lines: []testpdf.Line{body("Third physical page", 700)}}}
	d, err := extractFixture(t, pages, testpdf.Options{Title: "Fixture", Author: "Writer", Labels: true, Outline: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Fixture" || d.Author != "Writer" || d.PageCount != 3 || !d.Paginated || d.Format != "pdf" {
		t.Fatalf("metadata: %+v", d)
	}
	if len(d.Outline) != 1 || d.Outline[0].Page != 1 || d.Outline[0].Title != "First chapter" {
		t.Fatalf("outline: %+v", d.Outline)
	}
	for i, label := range []string{"i", "ii", "A-1"} {
		p := d.Pages[i]
		if p.Label != label || p.Number != i+1 || p.Width != 612 || p.Height != 792 || PageNeedsOCR(p) {
			t.Fatalf("page: %+v", p)
		}
	}
	b := d.Pages[0].Blocks[0]
	box := b.Boxes[0]
	if b.Text != "Clean document text with exact positions." || b.Anchor != "p1/b1" || box.Page != 1 || box.X0 < 49 || box.X0 > 55 || box.Y0 < 75 || box.Y0 > 95 || box.X1 <= box.X0 || box.Y1 <= box.Y0 {
		t.Fatalf("block: %+v", b)
	}
}
func TestColumnsParagraphsHyphensAndRunningMatter(t *testing.T) {
	var pages []testpdf.Page
	for i := 0; i < 3; i++ {
		pages = append(pages, testpdf.Page{Lines: []testpdf.Line{body("Running header", 770), body(fmt.Sprintf("Page %d", i+1), 20), body(fmt.Sprintf("Left %s para-", []string{"Alpha", "Beta", "Gamma"}[i]), 700), body(fmt.Sprintf("graph continues %s", []string{"Alpha", "Beta", "Gamma"}[i]), 685), {Text: fmt.Sprintf("Right column %s", []string{"Alpha", "Beta", "Gamma"}[i]), X: 330, Y: 700}, {Text: fmt.Sprintf("comes second %s", []string{"Alpha", "Beta", "Gamma"}[i]), X: 330, Y: 685}, body(fmt.Sprintf("Separated paragraph %s", []string{"Alpha", "Beta", "Gamma"}[i]), 630)}})
	}
	d, err := extractFixture(t, pages, testpdf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range d.Pages {
		text := pageText(p)
		if strings.Contains(text, "Running") || strings.Contains(text, "Page ") {
			t.Fatalf("running matter: %q", text)
		}
		if text != fmt.Sprintf("Left %s paragraph continues %s\nSeparated paragraph %s\nRight column %s comes second %s", []string{"Alpha", "Beta", "Gamma"}[i], []string{"Alpha", "Beta", "Gamma"}[i], []string{"Alpha", "Beta", "Gamma"}[i], []string{"Alpha", "Beta", "Gamma"}[i], []string{"Alpha", "Beta", "Gamma"}[i]) {
			t.Fatalf("reading order/paragraphs: %q; %+v", text, p.Blocks)
		}
	}
}
func TestQualityImageOnlyGarbledAndInvisibleOCR(t *testing.T) {
	text := "This is a clean existing OCR layer that should be preserved as native text."
	d, err := extractFixture(t, []testpdf.Page{{Image: true}, {Garbled: true, Lines: []testpdf.Line{body("Garbled text should request OCR", 700)}}, {Image: true, Lines: []testpdf.Line{{Text: text, X: 50, Y: 700, Invisible: true}}}}, testpdf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Pages[0].Quality != 0 || !PageNeedsOCR(d.Pages[0]) {
		t.Fatalf("image: %+v", d.Pages[0])
	}
	if d.Pages[1].Quality >= .6 || !PageNeedsOCR(d.Pages[1]) {
		t.Fatalf("garbled: %+v", d.Pages[1])
	}
	if d.Pages[2].Method != docextract.MethodNative || PageNeedsOCR(d.Pages[2]) || pageText(d.Pages[2]) != text {
		t.Fatalf("OCR layer: %+v", d.Pages[2])
	}
}
func TestPrintedTOCDropped(t *testing.T) {
	d, err := extractFixture(t, []testpdf.Page{{Lines: []testpdf.Line{body("Contents", 700), body("Introduction .... 1", 670), body("Chapter one .... 5", 650), body("Chapter two .... 17", 630), body("Appendix .... 30", 610)}}}, testpdf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := d.Pages[0]
	if len(p.Blocks) != 0 || len(p.Warnings) != 1 || p.Warnings[0] != "toc_page" {
		t.Fatalf("TOC: %+v", p)
	}
}
func TestRenderBoundsAndPNG(t *testing.T) {
	b := testpdf.Build([]testpdf.Page{{Width: 144, Height: 144, Lines: []testpdf.Line{body("Render me", 100)}}}, testpdf.Options{})
	e := New(Options{InProcess: true})
	for _, dpi := range []int{0, 72, 300} {
		p, err := e.RenderPNG(context.Background(), bytes.NewReader(b), int64(len(b)), 1, dpi)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(p))
		if err != nil {
			t.Fatal(err)
		}
		if dpi == 0 {
			dpi = 150
		}
		if cfg.Width < int(144*float64(dpi)/72) || cfg.Width > int(144*float64(dpi)/72)+1 || cfg.Height < int(144*float64(dpi)/72) || cfg.Height > int(144*float64(dpi)/72)+1 {
			t.Fatalf("dimensions: %+v", cfg)
		}
	}
	for _, dpi := range []int{71, 301} {
		if _, err := e.RenderPNG(context.Background(), bytes.NewReader(b), int64(len(b)), 1, dpi); err == nil {
			t.Fatal("accepted DPI", dpi)
		}
	}
	huge := testpdf.Build([]testpdf.Page{{Width: 2000, Height: 2000}}, testpdf.Options{})
	if _, err := e.RenderPNG(context.Background(), bytes.NewReader(huge), int64(len(huge)), 1, 300); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("pixel cap: %v", err)
	}
	if _, err := e.RenderPNG(context.Background(), bytes.NewReader(b), int64(len(b)), 0, 150); err == nil {
		t.Fatal("page zero accepted")
	}
}
func TestEncryptedDamagedLimitsSelectionAndProgress(t *testing.T) {
	for _, password := range []string{"secret", ""} {
		_, err := extractFixture(t, []testpdf.Page{{}}, testpdf.Options{Encrypted: true, Password: password})
		if !errors.Is(err, ErrEncrypted) {
			t.Fatalf("encryption %q: %v", password, err)
		}
	}
	b := testpdf.Build([]testpdf.Page{{Damaged: true}, {Lines: []testpdf.Line{body("Surviving page", 700)}}}, testpdf.Options{})
	e := New(Options{InProcess: true})
	if _, err := e.Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{MaxPages: 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("page cap: %v", err)
	}
	calls := 0
	d, err := e.Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{Pages: []int{2}, Progress: func(done, total int) {
		calls++
		if done != 1 || total != 1 {
			t.Errorf("progress: %d/%d", done, total)
		}
	}})
	if err != nil || calls != 1 || len(d.Pages) != 1 || d.Pages[0].Number != 2 {
		t.Fatalf("selection: %+v %v", d, err)
	}
	d, err = e.Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{})
	if err != nil || len(d.Pages) != 2 || pageText(d.Pages[1]) != "Surviving page" {
		t.Fatalf("damaged: %+v %v", d, err)
	}
	for _, pages := range [][]int{{0}, {3}, {1, 1}} {
		if _, err := e.Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{Pages: pages}); err == nil {
			t.Fatal("accepted pages", pages)
		}
	}
	if _, err := e.Extract(context.Background(), bytes.NewReader(nil), maxInput+1, docextract.Options{}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := e.Extract(context.Background(), bytes.NewReader([]byte("bad")), 3, docextract.Options{}); err == nil {
		t.Fatal("accepted bad PDF")
	}
}
func TestCancellationSwitchesAndQuality(t *testing.T) {
	e := New(Options{InProcess: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Extract(ctx, bytes.NewReader(nil), 1, docextract.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := e.RenderPNG(ctx, bytes.NewReader(nil), 1, 1, 150); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	t.Setenv("EXCALIBUR_PDF_ENABLED", "false")
	if _, err := e.Extract(context.Background(), bytes.NewReader(nil), 1, docextract.Options{}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := e.RenderPNG(context.Background(), bytes.NewReader(nil), 1, 1, 150); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	t.Setenv("EXCALIBUR_PDF_TIMEOUT", "2s")
	if e.timeout() != 2*time.Second {
		t.Fatal(e.timeout())
	}
	t.Setenv("EXCALIBUR_PDF_QUALITY_MIN", ".9")
	if !PageNeedsOCR(docextract.Page{Method: docextract.MethodNative, Quality: .8}) {
		t.Fatal("threshold ignored")
	}
	for _, s := range []string{"\ue000\ue001", "\ufffd\ufffd", "\u0378\u0379", "(cid:123)", "\x01\x7f\x80"} {
		if q := quality([]docextract.Block{{Text: s}}, 1000, 0); q != 0 {
			t.Errorf("garbled %q quality %f", s, q)
		}
	}
	if q := quality([]docextract.Block{{Text: "clean text"}}, 1000, 0); q < .9 {
		t.Fatal(q)
	}
}
func TestWorkerRealAndRestart(t *testing.T) {
	b := testpdf.Build([]testpdf.Page{{Lines: []testpdf.Line{body("Worker integration text", 700)}}}, testpdf.Options{})
	e := New(Options{})
	d, err := e.Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{})
	if err != nil || pageText(d.Pages[0]) != "Worker integration text" {
		t.Fatalf("real worker: %+v %v", d, err)
	}
	pngBytes, err := e.RenderPNG(context.Background(), bytes.NewReader(b), int64(len(b)), 1, 72)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(pngBytes)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PDF_TEST_WORKER", "crash")
	starts := t.TempDir() + "/starts"
	t.Setenv("PDF_TEST_STARTS", starts)
	d, err = e.Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Pages) != 3 || len(d.Pages[1].Warnings) == 0 || d.Pages[2].Method != docextract.MethodNative {
		t.Fatalf("restart: %+v", d)
	}
	data, _ := os.ReadFile(starts)
	if len(strings.Fields(string(data))) != 2 {
		t.Fatalf("starts: %s", data)
	}
}
func TestWorkerHangCancellationAndMemory(t *testing.T) {
	t.Setenv("PDF_TEST_WORKER", "hang")
	b := testpdf.Build([]testpdf.Page{{}}, testpdf.Options{})
	e := New(Options{PageTimeout: time.Second})
	d, err := e.Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{})
	if err != nil || len(d.Pages) != 3 || len(d.Pages[1].Warnings) == 0 || d.Pages[2].Method != docextract.MethodNative {
		t.Fatalf("timeout continuation: %+v %v", d, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = New(Options{}).Extract(ctx, bytes.NewReader(b), int64(len(b)), docextract.Options{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}

	// The fake starts with zero RSS; use a tiny budget with the real sampler to
	// exercise process-group termination on memory pressure.

	limited, memoryErr := New(Options{MemoryLimit: 1}).Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{})
	if memoryErr != nil {
		if !strings.Contains(memoryErr.Error(), "memory limit") {
			t.Fatal(memoryErr)
		}
	} else {
		found := false
		for _, p := range limited.Pages {
			for _, warning := range p.Warnings {
				found = found || strings.Contains(warning, "memory limit")
			}
		}
		if !found {
			t.Fatal("memory limit did not terminate worker")
		}
	}
}
func TestWorkerProtocolRejectsMalformedRequests(t *testing.T) {
	if err := ServeWorker(context.Background(), strings.NewReader("not json\n"), io.Discard); err == nil {
		t.Fatal("accepted malformed protocol")
	}
	if err := ServeWorker(context.Background(), strings.NewReader(strings.Repeat("x", 70<<10)), io.Discard); err == nil {
		t.Fatal("accepted large request")
	}
}

func TestCropAndRotationBoxes(t *testing.T) {
	crop := [4]float64{20, 40, 600, 760}
	d, err := extractFixture(t, []testpdf.Page{{CropBox: &crop, Lines: []testpdf.Line{body("Crop coordinates", 700)}}, {Rotation: 90, Lines: []testpdf.Line{body("Rotated coordinates", 700)}}}, testpdf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := d.Pages[0]
	b := p.Blocks[0].Boxes[0]
	if p.Width != 580 || p.Height != 720 || b.X0 < 29 || b.X0 > 35 || b.Y0 < 45 || b.Y0 > 60 {
		t.Fatalf("crop: %+v", p)
	}
	p = d.Pages[1]
	b = p.Blocks[0].Boxes[0]
	if p.Width != 792 || p.Height != 612 || b.X0 < 690 || b.X0 > 710 || b.Y0 < 49 || b.Y0 > 55 {
		t.Fatalf("rotation: %+v", p)
	}
}

func TestRunningMatterRequiresStrictMajorityAndMatchingBand(t *testing.T) {
	for _, count := range []int{2, 3} {
		d := docextract.Document{}
		for i := 0; i < 4; i++ {
			p := docextract.Page{Number: i + 1, Width: 612, Height: 792}
			if i < count {
				p.Blocks = []docextract.Block{{Text: "Repeated text", Boxes: []docextract.Box{{Page: i + 1, X0: 50, Y0: 20, X1: 150, Y1: 30}}}}
			}
			d.Pages = append(d.Pages, p)
		}
		cleanup(&d)
		if got := len(d.Pages[0].Blocks); (count == 2 && got != 1) || (count == 3 && got != 0) {
			t.Fatalf("majority %d/4 yielded %d blocks", count, got)
		}
	}
	d := docextract.Document{Pages: []docextract.Page{{Number: 1, Width: 612, Height: 792, Blocks: []docextract.Block{{Text: "Different band", Boxes: []docextract.Box{{Page: 1, X0: 50, Y0: 20, X1: 150, Y1: 30}}}}}, {Number: 2, Width: 612, Height: 792, Blocks: []docextract.Block{{Text: "Different band", Boxes: []docextract.Box{{Page: 2, X0: 50, Y0: 60, X1: 150, Y1: 70}}}}}}}
	cleanup(&d)
	if len(d.Pages[0].Blocks) != 1 || len(d.Pages[1].Blocks) != 1 {
		t.Fatal("removed text at different bands")
	}
}
func TestInProcessRuntimeCancellation(t *testing.T) {
	input := testpdf.Build([]testpdf.Page{{Lines: []testpdf.Line{body("Cancellation fixture", 700)}}}, testpdf.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e, err := openEngine(ctx, bytes.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	cancel()
	if _, err := e.page(1); err == nil {
		t.Fatal("cancelled Wasm engine accepted a page request")
	}
}
