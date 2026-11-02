// Package visionocr adapts model vision transcription to document pages.
package visionocr

import (
	"context"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Backend is the in-memory image seam of tools.ModelVisionBackend.
type Backend interface {
	ExtractPNG(context.Context, []byte, int) (tools.VisionResult, error)
}

// OCR converts faithful page transcription into ordered document blocks.
// Usage remains available on the underlying ModelVisionBackend.
type OCR struct{ Backend Backend }

// New binds the backend without taking ownership of provider resources.
func New(backend Backend) *OCR { return &OCR{Backend: backend} }

// OCRPage preserves physical page numbers and omits unknown coordinates.
func (o *OCR) OCRPage(ctx context.Context, png []byte, page int) (docextract.Page, error) {
	if err := ctx.Err(); err != nil {
		return docextract.Page{}, err
	}
	if !tools.VisionOCREnabled() {
		return docextract.Page{}, fmt.Errorf("visionocr: disabled by EXCALIBUR_VISION_OCR")
	}
	if o.Backend == nil || page < 1 {
		return docextract.Page{}, fmt.Errorf("visionocr: backend and positive page number required")
	}
	result, err := o.Backend.ExtractPNG(ctx, png, page)
	if err != nil {
		return docextract.Page{}, err
	}
	if len(result.Pages) != 1 || result.Pages[0].Page != page || strings.TrimSpace(result.Pages[0].Text) == "" {
		return docextract.Page{}, fmt.Errorf("visionocr: missing transcription for page %d", page)
	}
	blocks := split(result.Pages[0].Text, page)
	return docextract.Page{Number: page, Method: docextract.MethodVision, Quality: 0.8, Blocks: blocks}, nil
}

func heading(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n > 0 && n <= 6 && n < len(line) && line[n] == ' ' {
		return n
	}
	return 0
}

func split(text string, page int) []docextract.Block {
	var blocks []docextract.Block
	var lines []string
	flush := func() {
		if len(lines) == 0 {
			return
		}
		body := strings.Join(lines, "\n")
		kind := docextract.KindParagraph
		if strings.HasPrefix(lines[0], "|") && len(lines) > 1 {
			kind = docextract.KindTable
		}
		blocks = append(blocks, docextract.Block{Kind: kind, Text: body, Anchor: fmt.Sprintf("p%d/b%d", page, len(blocks)+1)})
		lines = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		if level := heading(line); level > 0 {
			flush()
			blocks = append(blocks, docextract.Block{Kind: docextract.KindHeading, Level: level, Text: strings.TrimSpace(line[level:]), Anchor: fmt.Sprintf("p%d/b%d", page, len(blocks)+1)})
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return blocks
}

var _ docextract.OCR = (*OCR)(nil)
