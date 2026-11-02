package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/bmp"
)

func imageFixture(t *testing.T, root, name string, img image.Image) string {
	t.Helper()
	var b bytes.Buffer
	var err error
	switch filepath.Ext(name) {
	case ".png":
		err = png.Encode(&b, img)
	case ".jpg":
		err = jpeg.Encode(&b, img, &jpeg.Options{Quality: 100})
	case ".gif":
		err = gif.Encode(&b, img, nil)
	case ".bmp":
		err = bmp.Encode(&b, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func uniformImage(c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}
func requireImageOutput(t *testing.T, r Result, want ...string) {
	t.Helper()
	if r.Status != StatusOK {
		t.Fatalf("inspection failed: %+v", r)
	}
	for _, s := range want {
		if !strings.Contains(r.Output, s) {
			t.Fatalf("missing %q in %s", s, r.Output)
		}
	}
}
func TestInspectImageBlankAndFormats(t *testing.T) {
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "1")
	root := t.TempDir()
	tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}}
	for _, ext := range []string{"png", "jpg", "gif", "bmp"} {
		path := imageFixture(t, root, "black."+ext, uniformImage(color.Black))
		r := fileCall(t, tool, map[string]any{"path": path})
		format := ext
		if ext == "jpg" {
			format = "jpeg"
		}
		requireImageOutput(t, r, "format: "+format, "size: 80x60", "background: #000000", "non_background_fraction: 0.000000", "blank/uniform: true (color #000000)", "mean_brightness: 0.000 / 255", "#000000 1.000000")
	}
	path := imageFixture(t, root, "color.png", uniformImage(color.NRGBA{R: 255, A: 255}))
	r := fileCall(t, tool, map[string]any{"path": path})
	requireImageOutput(t, r, "blank/uniform: true (color #ff0000)", "mean_brightness: 76.245 / 255")
}
func TestInspectImageWindowAndDifference(t *testing.T) {
	root := t.TempDir()
	tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}}
	before := uniformImage(color.Black)
	imageFixture(t, root, "before.png", before)
	after := uniformImage(color.Black)
	draw.Draw(after, image.Rect(20, 10, 60, 40), image.NewUniform(color.White), image.Point{}, draw.Src)
	imageFixture(t, root, "after.png", after)
	r := fileCall(t, tool, map[string]any{"path": "after.png", "compare_to": "before.png"})
	requireImageOutput(t, r, "non_background_fraction: 0.250000", "blank/uniform: false", "#000000 0.750000", "#ffffff 0.250000", "mean_brightness: 63.750 / 255", "changed_pixels: 1200 / 4800 (25.0000%", "changed_bbox: x=20 y=10 width=40 height=30", "difference_mask:", strings.Repeat("@", 32))
	if len(r.Output) > 5000 {
		t.Fatalf("default report too large: %d", len(r.Output))
	}
	if next := fileCall(t, tool, map[string]any{"path": "after.png", "compare_to": "before.png"}); r.Output != next.Output {
		t.Fatal("non-deterministic report")
	}
	unchanged := fileCall(t, tool, map[string]any{"path": "after.png", "compare_to": "after.png"})
	requireImageOutput(t, unchanged, "changed_pixels: 0 / 4800 (0.0000%", "changed_bbox: none")
	near := uniformImage(color.NRGBA{R: 3, G: 3, B: 3, A: 255})
	near.Set(79, 59, color.NRGBA{R: 4, A: 255})
	imageFixture(t, root, "near.png", near)
	requireImageOutput(t, fileCall(t, tool, map[string]any{"path": "near.png", "compare_to": "before.png"}), "changed_pixels: 1 / 4800", "changed_bbox: x=79 y=59 width=1 height=1")
}
func TestInspectImagePortableMaps(t *testing.T) {
	root := t.TempDir()
	tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}}
	for name, data := range map[string]string{
		"ascii.ppm": "P3\n# header comment\n2 1\n255\n0 0 0 255 255 255",
		"raw.ppm":   "P6\n2 1\n255\n\x00\x00\x00\xff\xff\xff",
		"ascii.pgm": "P2\n2 1\n15\n0 15",
		"raw.pgm":   "P5\r\n2 1\r\n65535\r\n\x00\x00\xff\xff",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		r := fileCall(t, tool, map[string]any{"path": name})
		requireImageOutput(t, r, "size: 2x1", "non_background_fraction: 0.500000", "#000000 0.500000", "#ffffff 0.500000", "mean_brightness: 127.500 / 255")
		if strings.HasSuffix(name, "pgm") {
			requireImageOutput(t, r, "format: pgm")
		} else {
			requireImageOutput(t, r, "format: ppm")
		}
	}
	// Binary raster bytes must never be stripped as whitespace or comments.
	for _, v := range []byte{'\n', '\r', ' ', '#'} {
		data := append([]byte("P6\n1 1\n255\n"), v, v, v)
		img, err := decodePNM(bufio.NewReader(bytes.NewReader(data)))
		if err != nil {
			t.Fatal(err)
		}
		if got := imageRGB(img, 0, 0); got != uint32(v)*0x010101 {
			t.Fatalf("binary sample lost: %x", got)
		}
	}
}
func TestInspectImageRejectsUnsafeAndInvalidInput(t *testing.T) {
	root := t.TempDir()
	outside := imageFixture(t, t.TempDir(), "outside.png", uniformImage(color.Black))
	tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}}
	imageFixture(t, root, "inside.png", uniformImage(color.Black))
	if err := os.Symlink(outside, filepath.Join(root, "escape.png")); err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{
		{"path": outside}, {"path": "escape.png"}, {"path": "inside.png", "compare_to": outside},
		{"path": "inside.png", "width": 1000}, {"path": "inside.png", "height": -1}, {"path": ""},
	} {
		if r := fileCall(t, tool, args); r.Status == StatusOK {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
	for name, data := range map[string]string{"bad.ppm": "P6\n8001 8000\n255\n", "bad-sample.ppm": "P3\n1 1\n15\n16 0 0", "truncated.ppm": "P6\n1 1\n255\n\x00", "bad.png": "not an image"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if r := fileCall(t, tool, map[string]any{"path": name}); r.Status == StatusOK {
			t.Fatalf("accepted %s", name)
		}
	}
	// PNG header dimensions are rejected before a missing/huge raster is decoded.
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	chunk := make([]byte, 17)
	copy(chunk, "IHDR")
	binary.BigEndian.PutUint32(chunk[4:], 8001)
	binary.BigEndian.PutUint32(chunk[8:], 8000)
	chunk[12], chunk[13] = 8, 2
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	if err := os.WriteFile(filepath.Join(root, "large.png"), b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if r := fileCall(t, tool, map[string]any{"path": "large.png"}); !strings.Contains(r.Output, "64 MP") {
		t.Fatalf("no header guard: %+v", r)
	}
	f, err := os.Create(filepath.Join(root, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(inspectImageMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if r := fileCall(t, tool, map[string]any{"path": "large.bin"}); !strings.Contains(r.Output, "512 MiB") {
		t.Fatalf("no byte limit: %+v", r)
	}
	small := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	imageFixture(t, root, "small.png", small)
	if r := fileCall(t, tool, map[string]any{"path": "inside.png", "compare_to": "small.png"}); !strings.Contains(r.Output, "dimensions differ") {
		t.Fatalf("size mismatch: %+v", r)
	}
}
func TestInspectImageThumbnailAspectAndBound(t *testing.T) {
	for _, size := range []image.Point{{800, 600}, {10, 1000}, {1000, 10}, {1, 1}} {
		s := imageThumbnail(context.Background(), size.X, size.Y, 64, 24, func(x, y int) float64 { return 255 })
		rows := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
		if len(rows) > 24 || len(rows[0]) > 64 || len(s) > 65*24 {
			t.Fatalf("oversized thumbnail %dx%d", len(rows[0]), len(rows))
		}
		if size.X == 800 && (len(rows) != 24 || len(rows[0]) != 64) {
			t.Fatalf("incorrect cell aspect %dx%d", len(rows[0]), len(rows))
		}
	}
}
func TestInspectImageCancellationAndSwitch(t *testing.T) {
	root := t.TempDir()
	path := imageFixture(t, root, "black.png", uniformImage(color.Black))
	tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := loadInspectImage(ctx, path); err == nil {
		t.Fatal("ignored cancellation")
	}
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "0")
	if r := fileCall(t, tool, map[string]any{"path": path}); r.Status != StatusDenied {
		t.Fatal(r)
	}
	for _, tool := range BuiltinTools(nil) {
		if tool.Definition().Name == "inspect_image" {
			t.Fatal("disabled tool registered")
		}
	}
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "1")
	found := false
	for _, tool := range BuiltinTools(nil) {
		if tool.Definition().Name == "inspect_image" {
			found = true
			if err := tool.Definition().Validate(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Fatal("missing built-in")
	}
	keys, err := tool.ResourceKeys(json.RawMessage(`{"path":"black.png","compare_to":"black.png"}`))
	if err != nil || len(keys) != 2 {
		t.Fatalf("resource keys: %v %v", keys, err)
	}
}

func TestInspectImageDenseHistogramAndAlpha(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for i := 0; i < 5000; i++ {
		img.SetNRGBA(i%100, i/100, color.NRGBA{R: uint8(i >> 8), G: uint8(i), B: 1, A: 255})
	}
	// Transparent half composites to white and stays the dominant color.
	out, err := inspectPixels(context.Background(), img, nil, "png", 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"background: #ffffff", "non_background_fraction: 0.500000", "#ffffff 0.500000", "#000001 0.000100", "#000101 0.000100"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s: %s", want, out)
		}
	}
}
