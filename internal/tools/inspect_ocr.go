package tools

import (
	"context"
	"io"
	"os/exec"
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
func (t *InspectImageTool) imageOCR(ctx context.Context, path string) string {
	runner := t.ocrRunner
	if runner == nil {
		runner = localImageOCR{}
	}
	executable, err := runner.LookPath("tesseract")
	if err != nil {
		return "OCR: unavailable (tesseract not installed)\n"
	}
	ctx, cancel := context.WithTimeout(ctx, imageOCRTimeout)
	defer cancel()
	var out imageOCRBuffer
	err = runner.Run(ctx, executable, []string{path, "stdout"}, &out)
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
