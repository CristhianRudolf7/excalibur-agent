package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
)

const (
	inspectImageMaxPixels = 64_000_000
	inspectImageMaxBytes  = 512 << 20
	inspectImageTolerance = 3
)

// InspectImageEnabled gates local image inspection and its verification guidance.
func InspectImageEnabled() bool { return !off("EXCALIBUR_INSPECT_IMAGE") }

// InspectImageTool provides read-only pixel evidence without a model call.
// Like the file tools, it belongs to worker runs, including assistant delegates.
type InspectImageTool struct {
	Deps      *FileDeps
	ocrRunner imageOCRRunner
}

type inspectImageArgs struct {
	Path      string `json:"path"`
	CompareTo string `json:"compare_to"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	OCR       bool   `json:"ocr"`
}

func (t *InspectImageTool) Definition() Definition {
	return Definition{
		Name: "inspect_image", Description: "Inspect a local screenshot/image: blank screen, colors, brightness, ASCII layout; compare_to shows visible changes",
		InputSchema: schemaFor(map[string]any{
			"type": "object", "properties": map[string]any{
				"path":       map[string]any{"type": "string", "minLength": 1},
				"ocr":        map[string]any{"type": "boolean", "description": "Try installed tesseract with a short timeout; no installation"},
				"compare_to": map[string]any{"type": "string", "minLength": 1},
				"width":      map[string]any{"type": "integer", "minimum": 8, "maximum": 96, "description": "Thumbnail columns, default 64"},
				"height":     map[string]any{"type": "integer", "minimum": 4, "maximum": 32, "description": "Thumbnail row limit, default 24; preserves aspect ratio"},
			}, "required": []string{"path"}, "additionalProperties": false,
		}),
		Effect: "read_only", Idempotency: "idempotent", Cancel: "cooperative", Timeout: 30 * time.Second,
		OutputBudget: 12_000, Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "files", Tags: []string{"image", "screenshot"}, Keywords: []string{"image", "screen", "visual", "compare", "blank"},
	}
}

// imagePath confines both inputs to the run's allowed file tree, resolving
// symlinks before checking containment. An unspecified workdir means cwd.
func (t *InspectImageTool) imagePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("image path is required")
	}
	root := t.Deps.runWorkdir()
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("image path must be inside the allowed tree %s", root)
	}
	if err := CheckPathBoundary(path); err != nil {
		return "", err
	}
	return path, nil
}

func (t *InspectImageTool) ResourceKeys(raw json.RawMessage) ([]string, error) {
	var a inspectImageArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	var keys []string
	for _, p := range []string{a.Path, a.CompareTo} {
		if p == "" {
			continue
		}
		path, err := t.imagePath(p)
		if err != nil {
			return nil, err
		}
		keys = append(keys, "file:"+path)
	}
	return keys, nil
}

func (t *InspectImageTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	fail := func(err error) (Result, error) { return Result{Status: StatusInputError, Output: err.Error()}, nil }
	if !InspectImageEnabled() {
		return Result{Status: StatusDenied, Output: "inspect_image disabled by EXCALIBUR_INSPECT_IMAGE"}, nil
	}
	var a inspectImageArgs
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return fail(err)
	}
	if a.Width == 0 {
		a.Width = 64
	}
	if a.Height == 0 {
		a.Height = 24
	}
	if a.Width < 8 || a.Width > 96 || a.Height < 4 || a.Height > 32 {
		return fail(fmt.Errorf("thumbnail bounds: width 8..96, height 4..32"))
	}
	path, err := t.imagePath(a.Path)
	if err != nil {
		return fail(err)
	}
	img, format, err := loadInspectImage(ctx, path)
	if err != nil {
		return fail(err)
	}
	var prior image.Image
	if a.CompareTo != "" {
		other, err := t.imagePath(a.CompareTo)
		if err != nil {
			return fail(err)
		}
		prior, _, err = loadInspectImage(ctx, other)
		if err != nil {
			return fail(err)
		}
		if img.Bounds().Size() != prior.Bounds().Size() {
			return fail(fmt.Errorf("compare_to dimensions differ; capture both images at the same size"))
		}
	}
	out, err := inspectPixels(ctx, img, prior, format, a.Width, a.Height)
	if err != nil {
		return Result{Status: StatusCanceled, Output: err.Error()}, nil
	}
	if a.OCR {
		out += t.imageOCR(ctx, path)
	}
	return Result{Status: StatusOK, Output: out}, nil
}

func imageDimensionsOK(w, h int) bool {
	return w > 0 && h > 0 && int64(w) <= inspectImageMaxPixels && int64(h) <= inspectImageMaxPixels/int64(w)
}

// Headers are checked before allocation. GIF inspection deliberately uses the
// first frame; multi-frame animations cannot establish an interactive response.
func loadInspectImage(ctx context.Context, path string) (image.Image, string, error) {
	// Refuse special files before opening: opening a FIFO could otherwise block
	// before the context-aware reader or file-size checks can run.
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > inspectImageMaxBytes {
		return nil, "", fmt.Errorf("image must be a regular file of at most 512 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if !st.Mode().IsRegular() || st.Size() > inspectImageMaxBytes {
		return nil, "", fmt.Errorf("image must be a regular file of at most 512 MiB")
	}
	reader := func() *bufio.Reader {
		return bufio.NewReader(&imageContextReader{ctx, io.LimitReader(f, inspectImageMaxBytes)})
	}
	r := reader()
	magic, _ := r.Peek(2)
	pnm := len(magic) == 2 && magic[0] == 'P' && strings.ContainsRune("2356", rune(magic[1]))
	var cfg image.Config
	var format string
	if pnm {
		cfg, format, err = pnmConfig(r)
	} else {
		cfg, format, err = image.DecodeConfig(r)
	}
	if err != nil {
		return nil, "", fmt.Errorf("decode image header: %w", err)
	}
	if !imageDimensionsOK(cfg.Width, cfg.Height) {
		return nil, "", fmt.Errorf("image dimensions exceed 64 MP or are invalid")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	var img image.Image
	if pnm {
		img, err = decodePNM(reader())
	} else {
		img, _, err = image.Decode(reader())
	}
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}
	return img, format, nil
}

type imageContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *imageContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// Composite transparency over white, so fully transparent pixels do not look
// like an opaque black screen. Every statistic and comparison uses this RGB.
func imageRGB(img image.Image, x, y int) uint32 {
	r, g, b, a := img.At(x+img.Bounds().Min.X, y+img.Bounds().Min.Y).RGBA()
	return ((r+65535-a)>>8)<<16 | ((g+65535-a)>>8)<<8 | ((b + 65535 - a) >> 8)
}
func imageBrightness(c uint32) float64 {
	return float64(299*(c>>16)+587*((c>>8)&255)+114*(c&255)) / 1000
}
func imageChanged(a, b uint32) bool {
	for shift := 0; shift <= 16; shift += 8 {
		d := int((a>>shift)&255) - int((b>>shift)&255)
		if d > inspectImageTolerance || d < -inspectImageTolerance {
			return true
		}
	}
	return false
}

type imageColorCount struct {
	rgb   uint32
	count uint32
}

func inspectPixels(ctx context.Context, img, prior image.Image, format string, cols, rows int) (string, error) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	n := float64(w * h)
	// Sparse counts cover ordinary screens. A dense 24-bit histogram caps
	// memory for photographic noise instead of growing an unbounded map.
	counts := map[uint32]uint32{}
	var dense []uint32
	var total float64
	changed := 0
	box := image.Rectangle{}
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for x := 0; x < w; x++ {
			c := imageRGB(img, x, y)
			total += imageBrightness(c)
			if dense != nil {
				dense[c]++
			} else {
				counts[c]++
				if len(counts) > 4096 {
					dense = make([]uint32, 1<<24)
					for k, v := range counts {
						dense[k] = v
					}
					counts = nil
				}
			}
			if prior != nil && imageChanged(c, imageRGB(prior, x, y)) {
				changed++
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	top := []imageColorCount{}
	add := func(c uint32, v uint32) {
		if v == 0 {
			return
		}
		entry := imageColorCount{c, v}
		pos := 0
		for pos < len(top) && (top[pos].count > v || top[pos].count == v && top[pos].rgb < c) {
			pos++
		}
		if pos >= 5 {
			return
		}
		top = append(top, entry)
		copy(top[pos+1:], top[pos:len(top)-1])
		top[pos] = entry
		if len(top) > 5 {
			top = top[:5]
		}
	}
	if dense == nil {
		for c, v := range counts {
			add(c, v)
		}
	} else {
		for c, v := range dense {
			if c%65536 == 0 {
				if err := ctx.Err(); err != nil {
					return "", err
				}
			}
			add(uint32(c), v)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "format: %s (first frame)\nsize: %dx%d\nbackground: #%06x\nnon_background_fraction: %.6f\nblank/uniform: %t", format, w, h, top[0].rgb, 1-float64(top[0].count)/n, float64(top[0].count) == n)
	if float64(top[0].count) == n {
		fmt.Fprintf(&b, " (color #%06x)", top[0].rgb)
	}
	fmt.Fprintf(&b, "\nmean_brightness: %.3f / 255\ntop_colors:", total/n)
	for _, c := range top {
		fmt.Fprintf(&b, "\n#%06x %.6f", c.rgb, float64(c.count)/n)
	}
	b.WriteByte('\n')
	if prior != nil {
		fmt.Fprintf(&b, "changed_pixels: %d / %d (%.4f%%; per-channel tolerance %d)\n", changed, w*h, float64(changed)*100/n, inspectImageTolerance)
		if changed == 0 {
			b.WriteString("changed_bbox: none\n")
		} else {
			fmt.Fprintf(&b, "changed_bbox: x=%d y=%d width=%d height=%d (zero-based)\n", box.Min.X, box.Min.Y, box.Dx(), box.Dy())
		}
	}
	fmt.Fprintf(&b, "ASCII (dark to light: space .:-=+*#%%@):\n%s", imageThumbnail(ctx, w, h, cols, rows, func(x, y int) float64 { return imageBrightness(imageRGB(img, x, y)) }))
	if prior != nil {
		b.WriteString("difference_mask:\n")
		b.WriteString(imageThumbnail(ctx, w, h, cols, rows, func(x, y int) float64 {
			if imageChanged(imageRGB(img, x, y), imageRGB(prior, x, y)) {
				return 255
			}
			return 0
		}))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Typical terminal cells are twice as high as wide. Fit inside the requested
// bounds and area-average every cell to retain thin lines and small windows.
func imageThumbnail(ctx context.Context, w, h, cols, rows int, pixel func(int, int) float64) string {
	rowsNeeded := float64(h) * float64(cols) / (2 * float64(w))
	if rowsNeeded > float64(rows) {
		cols = max(1, int(math.Round(float64(rows)*2*float64(w)/float64(h))))
	} else {
		rows = max(1, int(math.Round(rowsNeeded)))
	}
	const ramp = " .:-=+*#%@"
	var b strings.Builder
	for y := 0; y < rows; y++ {
		if ctx.Err() != nil {
			break
		}
		for x := 0; x < cols; x++ {
			x0, y0 := x*w/cols, y*h/rows
			x1, y1 := max(x0+1, (x+1)*w/cols), max(y0+1, (y+1)*h/rows)
			var sum float64
			for py := y0; py < y1; py++ {
				for px := x0; px < x1; px++ {
					sum += pixel(px, py)
				}
			}
			b.WriteByte(ramp[int(math.Round(sum/float64((x1-x0)*(y1-y0))*float64(len(ramp)-1)/255))])
		}
		b.WriteByte('\n')
	}
	return b.String()
}
