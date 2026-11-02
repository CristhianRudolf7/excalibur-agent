package tools

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
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
	paths   []string
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
	f.paths = append(f.paths, args[0])
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
		if args[0] == path {
			t.Fatal("OCR must receive a prepared copy")
		}
		img, err := loadOCRFixture(args[0])
		if err != nil || img.Bounds().Dx() != 240 || img.Bounds().Dy() != 180 || imageRGB(img, 0, 0) != 0xffffff {
			t.Fatalf("invalid prepared raster: %v %v", img, err)
		}
		_, err = io.WriteString(w, "A local window\nReady\n")
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
	requireOCRCleanup(t, fake)
	if img, err := loadOCRFixture(path); err != nil || imageRGB(img, 0, 0) != 0 {
		t.Fatal("OCR changed the source image")
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
			out := tool.imageOCR(ctx, uniformImage(color.Black))
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
			requireOCRCleanup(t, fake)
		})
	}
}

func loadOCRFixture(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func requireOCRCleanup(t *testing.T, fake *fakeImageOCR) {
	t.Helper()
	for _, path := range fake.paths {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("OCR temporary file retained: %v", err)
		}
	}
}

func TestInspectImageOCRPreparesTwoColorNearestRaster(t *testing.T) {
	for _, size := range []image.Point{{12, 8}, {1024, 768}, {1025, 8}, {8, 769}} {
		t.Run(size.String(), func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
			for y := 0; y < size.Y; y++ {
				for x := 0; x < size.X; x++ {
					c := color.NRGBA{B: 168, A: 255}
					if x%4 == 1 {
						c = color.NRGBA{R: 240, G: 240, B: 240, A: 255}
					} else if x%8 == 3 && y == 0 {
						// A third color close to foreground must become black.
						c = color.NRGBA{R: 220, G: 220, B: 220, A: 255}
					}
					img.SetNRGBA(x, y, c)
				}
			}
			scale := 1
			if size.X <= 1024 && size.Y <= 768 {
				scale = 3
			}
			fake := &fakeImageOCR{t: t, run: func(ctx context.Context, args []string, w io.Writer) error {
				prepared, err := loadOCRFixture(args[0])
				if err != nil {
					t.Fatal(err)
				}
				if info, err := os.Stat(args[0]); err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("temporary raster must be private: %v %v", info, err)
				}
				if prepared.Bounds().Size() != image.Pt(size.X*scale, size.Y*scale) {
					t.Fatalf("wrong OCR size: %v", prepared.Bounds())
				}
				for y := 0; y < size.Y*scale; y++ {
					for x := 0; x < size.X*scale; x++ {
						want := uint32(0xffffff)
						if imageRGB(img, x/scale, y/scale)>>16 > 0 {
							want = 0
						}
						if got := imageRGB(prepared, x, y); got != want {
							t.Fatalf("pixel %d,%d got #%06x want #%06x", x, y, got, want)
						}
					}
				}
				_, err = io.WriteString(w, "Prepared raster read")
				return err
			}}
			tool := &InspectImageTool{ocrRunner: fake}
			if out := tool.imageOCR(context.Background(), img); !strings.Contains(out, "Prepared raster read") {
				t.Fatal(out)
			}
			requireOCRCleanup(t, fake)
		})
	}
}

func TestInspectImageOCRInstallHints(t *testing.T) {
	for _, tc := range []struct{ goos, release, want string }{
		{"linux", "ID=debian\n", "apt-get install -y tesseract-ocr"},
		{"linux", "ID=ubuntu\nID_LIKE=debian", "apt-get install -y tesseract-ocr"},
		{"linux", "ID=derivative\nID_LIKE=\"ubuntu debian\"", "apt-get install -y tesseract-ocr"},
		{"linux", "ID=fedora", "dnf install -y tesseract"},
		{"linux", "ID=arch", "pacman -S tesseract tesseract-data-eng"},
		{"linux", "ID=alpine", "apk add tesseract-ocr tesseract-ocr-data-eng"},
		{"linux", "ID=opensuse-leap", "zypper install -y tesseract-ocr"},
		{"linux", "", "OS package manager"},
		{"linux", "ID=unknown\nNAME=Ubuntu", "OS package manager"},
		{"darwin", "", "brew install tesseract"},
		{"windows", "", "UB Mannheim"},
	} {
		if got := imageOCRInstallHint(tc.goos, tc.release); !strings.Contains(got, tc.want) {
			t.Fatalf("%s %s: %s", tc.goos, tc.release, got)
		}
	}
	fake := &fakeImageOCR{t: t, missing: true}
	if out := (&InspectImageTool{ocrRunner: fake}).imageOCR(context.Background(), uniformImage(color.Black)); !strings.Contains(out, "package:") || !strings.Contains(out, "tool installs nothing") || fake.calls != 0 {
		t.Fatalf("missing dependency hint: %s", out)
	}
}

func TestPrepareImageOCRCancellationAndDenseColors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareImageOCR(ctx, uniformImage(color.Black)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: byte(x), G: byte(y), A: 255})
		}
	}
	out, err := prepareImageOCR(context.Background(), img)
	if err != nil || out.Bounds().Size() != image.Pt(384, 384) {
		t.Fatalf("dense histogram: %v %v", out, err)
	}
	for _, pixel := range out.Pix {
		if pixel != 0 && pixel != 255 {
			t.Fatal("dense input was not binarized")
		}
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
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = strings.Repeat("║", 80)
	}
	img := renderScreen(t, screenGrid{8, 16, 1, 80, 30}, lines, color.White, color.Black)
	imageFixture(t, root, "full.png", img)
	r = fileCall(t, tool, map[string]any{"path": "full.png", "compare_to": "full.png", "width": 96, "height": 32, "ocr": true})
	requireImageOutput(t, r, "[screen_text truncated]", "OCR (optional text hint, truncated at 2048 bytes)", "difference_mask:")
	if int64(len(r.Output)) > tool.Definition().OutputBudget || strings.Index(r.Output, "OCR (") > strings.Index(r.Output, "ASCII (") {
		t.Fatalf("bounded text evidence must precede thumbnails: %d", len(r.Output))
	}
}
