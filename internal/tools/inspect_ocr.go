package tools

import (
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const imageOCRTimeout = 3 * time.Second
const imageOCRMaxBytes = 2048

type imageOCRRunner interface {
	LookPath(string) (string, error)
	Run(context.Context, string, []string, io.Writer) error
}
type localImageOCR struct{}

func (localImageOCR) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (localImageOCR) Run(ctx context.Context, path string, args []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Run()
}

// Discard excess bytes while returning a successful write, so OCR cannot
// retain unbounded output or block while the process is being canceled.
type imageOCRBuffer struct {
	data      []byte
	truncated bool
}

func (b *imageOCRBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := imageOCRMaxBytes - len(b.data)
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (t *InspectImageTool) imageOCR(ctx context.Context, img image.Image) string {
	runner := t.ocrRunner
	if runner == nil {
		runner = localImageOCR{}
	}
	executable, err := runner.LookPath("tesseract")
	if err != nil {
		release, _ := os.ReadFile("/etc/os-release")
		return "OCR: unavailable (tesseract not installed); " + imageOCRInstallHint(runtime.GOOS, string(release)) + "; tool installs nothing\n"
	}
	ctx, cancel := context.WithTimeout(ctx, imageOCRTimeout)
	defer cancel()
	prepared, err := prepareImageOCR(ctx, img)
	if err != nil {
		return "OCR: canceled or timed out (limit 3s)\n"
	}
	// A private temporary PNG ensures every supported input format reaches
	// tesseract as a validated, two-color raster. Never modify the source file.
	f, err := os.CreateTemp("", "excalibur-ocr-*.png")
	if err != nil {
		return "OCR: image preparation failed; pixel inspection remains available\n"
	}
	defer os.Remove(f.Name())
	if err := restrictOCRFile(f.Name()); err != nil {
		_ = f.Close()
		return "OCR: image preparation failed; pixel inspection remains available\n"
	}
	err = png.Encode(imageOCRWriter{ctx, f}, prepared)
	closeErr := f.Close()
	if ctx.Err() != nil {
		return "OCR: canceled or timed out (limit 3s)\n"
	}
	if err != nil || closeErr != nil {
		return "OCR: image preparation failed; pixel inspection remains available\n"
	}
	var out imageOCRBuffer
	err = runner.Run(ctx, executable, []string{f.Name(), "stdout"}, &out)
	if ctx.Err() != nil {
		return "OCR: canceled or timed out (limit 3s)\n"
	}
	if err != nil {
		return "OCR: tesseract failed; pixel inspection remains available\n"
	}
	text := strings.TrimSpace(strings.ToValidUTF8(string(out.data), ""))
	if text == "" {
		return "OCR: no text recognized\n"
	}
	label := "OCR (optional text hint):\n"
	if out.truncated {
		label = "OCR (optional text hint, truncated at 2048 bytes):\n"
	}
	return label + text + "\n"
}

type imageOCRWriter struct {
	ctx context.Context
	w   io.Writer
}

func (w imageOCRWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

// Quantize to the two dominant source colors, then normalize their nearest
// pixels to white background/black foreground. Integer 3x nearest scaling is
// limited to small images (at most 1024x768), bounding allocation below 8 MP.
func prepareImageOCR(ctx context.Context, img image.Image) (*image.Gray, error) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	counts := map[uint32]uint32{}
	var dense []uint32
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < w; x++ {
			c := imageRGB(img, x, y)
			if dense != nil {
				dense[c]++
			} else {
				counts[c]++
				if len(counts) > 4096 {
					dense = make([]uint32, 1<<24)
					for c, n := range counts {
						dense[c] = n
					}
					counts = nil
				}
			}
		}
	}
	var top [2]imageColorCount
	add := func(c, n uint32) {
		if n == 0 {
			return
		}
		entry := imageColorCount{c, n}
		if n > top[0].count || n == top[0].count && c < top[0].rgb {
			top[1], top[0] = top[0], entry
		} else if n > top[1].count || n == top[1].count && c < top[1].rgb {
			top[1] = entry
		}
	}
	if dense == nil {
		for c, n := range counts {
			add(c, n)
		}
	} else {
		for c, n := range dense {
			if c%65536 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			add(uint32(c), n)
		}
	}
	scale := 1
	if w <= 1024 && h <= 768 {
		scale = 3
	}
	out := image.NewGray(image.Rect(0, 0, w*scale, h*scale))
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < w; x++ {
			c := imageRGB(img, x, y)
			var value byte = 255
			if top[1].count > 0 && screenColorDistance(c, top[1].rgb) < screenColorDistance(c, top[0].rgb) {
				value = 0
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					out.Pix[(y*scale+dy)*out.Stride+x*scale+dx] = value
				}
			}
		}
	}
	return out, nil
}

func imageOCRInstallHint(goos, release string) string {
	switch goos {
	case "darwin":
		return "package: brew install tesseract"
	case "windows":
		return "package: Tesseract OCR Windows installer from UB Mannheim"
	case "linux":
		values := map[string]string{}
		for _, line := range strings.Split(release, "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
			}
		}
		for _, id := range strings.Fields(values["ID"] + " " + values["ID_LIKE"]) {
			switch id {
			case "debian", "ubuntu":
				return "package: apt-get install -y tesseract-ocr"
			case "fedora", "rhel", "centos":
				return "package: dnf install -y tesseract"
			case "arch":
				return "package: pacman -S tesseract tesseract-data-eng"
			case "alpine":
				return "package: apk add tesseract-ocr tesseract-ocr-data-eng"
			case "suse", "opensuse", "opensuse-tumbleweed", "opensuse-leap":
				return "package: zypper install -y tesseract-ocr"
			}
		}
	}
	return "package: install tesseract/tesseract-ocr using the OS package manager"
}
