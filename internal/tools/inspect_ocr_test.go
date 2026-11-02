package tools

import (
	"context"
	"errors"
	"image/color"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type fakeImageOCR struct {
	t       *testing.T
	missing bool
	run     func(context.Context, []string, io.Writer) error
	calls   int
}

func (f *fakeImageOCR) LookPath(name string) (string, error) {
	if name != "tesseract" {
		f.t.Fatal(name)
	}
	if f.missing {
		return "", exec.ErrNotFound
	}
	return "/fake/tesseract", nil
}
func (f *fakeImageOCR) Run(ctx context.Context, exe string, args []string, w io.Writer) error {
	f.calls++
	if exe != "/fake/tesseract" || len(args) != 2 || args[1] != "stdout" {
		f.t.Fatalf("invalid OCR command: %s %v", exe, args)
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > imageOCRTimeout {
		f.t.Fatal("missing short OCR deadline")
	}
	return f.run(ctx, args, w)
}
func TestInspectImageOptionalOCR(t *testing.T) {
	root := t.TempDir()
	path := imageFixture(t, root, "screen.png", uniformImage(color.Black))
	fake := &fakeImageOCR{t: t, run: func(ctx context.Context, args []string, w io.Writer) error {
		if args[0] != path {
			t.Fatalf("OCR must inspect validated path, got %v", args)
		}
		_, err := io.WriteString(w, "A local window\nReady\n")
		return err
	}}
	tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}, ocrRunner: fake}
	requireImageOutput(t, fileCall(t, tool, map[string]any{"path": "screen.png"}), "blank/uniform: true")
	if fake.calls != 0 {
		t.Fatal("OCR ran without opt-in")
	}
	requireImageOutput(t, fileCall(t, tool, map[string]any{"path": "screen.png", "ocr": true}), "OCR (optional text hint):\nA local window\nReady")
	if fake.calls != 1 {
		t.Fatal("OCR did not run")
	}
	fake.missing = true
	r := fileCall(t, tool, map[string]any{"path": "screen.png", "ocr": true})
	requireImageOutput(t, r, "OCR: unavailable (tesseract not installed)")
	if fake.calls != 1 {
		t.Fatal("missing OCR executed")
	}
}
func TestInspectImageOCRBoundFailureAndTimeout(t *testing.T) {
	for _, mode := range []string{"large", "failure", "timeout", "empty"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeImageOCR{t: t, run: func(ctx context.Context, args []string, w io.Writer) error {
				switch mode {
				case "large":
					for i := 0; i < 10; i++ {
						if _, err := io.WriteString(w, strings.Repeat("a", 2047)+"é"); err != nil {
							return err
						}
					}
				case "failure":
					return errors.New("local OCR failed")
				case "timeout":
					<-ctx.Done()
					return ctx.Err()
				}
				return nil
			}}
			tool := &InspectImageTool{ocrRunner: fake}
			ctx := context.Background()
			cancel := func() {}
			if mode == "timeout" {
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
			}
			defer cancel()
			out := tool.imageOCR(ctx, "/screen.png")
			switch mode {
			case "large":
				if len(out) > imageOCRMaxBytes+80 || !strings.Contains(out, "truncated at 2048 bytes") {
					t.Fatalf("unbounded OCR hint: %d", len(out))
				}
			case "failure":
				if !strings.Contains(out, "tesseract failed") {
					t.Fatal(out)
				}
			case "timeout":
				if !strings.Contains(out, "timed out") {
					t.Fatal(out)
				}
			case "empty":
				if !strings.Contains(out, "no text recognized") {
					t.Fatal(out)
				}
			}
		})
	}
}

func TestInspectImageMaximumReportBound(t *testing.T) {
	root := t.TempDir()
	imageFixture(t, root, "before.png", uniformImage(color.Black))
	imageFixture(t, root, "after.png", uniformImage(color.White))
	fake := &fakeImageOCR{t: t, run: func(ctx context.Context, args []string, w io.Writer) error {
		_, err := io.WriteString(w, strings.Repeat("text ", 1000))
		return err
	}}
	tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}, ocrRunner: fake}
	r := fileCall(t, tool, map[string]any{"path": "after.png", "compare_to": "before.png", "width": 96, "height": 32, "ocr": true})
	requireImageOutput(t, r, "changed_pixels: 4800 / 4800 (100.0000%", "difference_mask:", "OCR (optional text hint, truncated at 2048 bytes)")
	if int64(len(r.Output)) > tool.Definition().OutputBudget {
		t.Fatalf("report exceeds budget: %d", len(r.Output))
	}
}
