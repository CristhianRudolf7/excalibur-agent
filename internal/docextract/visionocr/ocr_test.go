package visionocr

import (
	"context"
	"errors"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"testing"
)

type fakeBackend struct {
	text string
	err  error
}

func (f fakeBackend) ExtractPNG(ctx context.Context, png []byte, page int) (tools.VisionResult, error) {
	return tools.VisionResult{Model: "fake", Pages: []tools.VisionPage{{Page: page, Text: f.text}}}, f.err
}
func TestOCRPage(t *testing.T) {
	o := New(fakeBackend{text: "# Title\nFirst line\nsecond line\n\n## Section\n\n| A | B |\n| --- | --- |\n| 1 | 2 |"})
	p, err := o.OCRPage(context.Background(), []byte("fake"), 12)
	if err != nil {
		t.Fatal(err)
	}
	if p.Number != 12 || p.Method != docextract.MethodVision || p.Quality != 0.8 || p.Width != 0 || p.Height != 0 || len(p.Blocks) != 4 {
		t.Fatalf("page=%+v", p)
	}
	for i, b := range p.Blocks {
		if len(b.Boxes) != 0 || b.Anchor == "" {
			t.Fatalf("block %d=%+v", i, b)
		}
	}
	if p.Blocks[0].Kind != docextract.KindHeading || p.Blocks[0].Text != "Title" || p.Blocks[2].Level != 2 || p.Blocks[3].Kind != docextract.KindTable {
		t.Fatalf("blocks=%+v", p.Blocks)
	}
}
func TestOCRErrors(t *testing.T) {
	sentinel := errors.New("provider unavailable")
	for _, o := range []*OCR{New(nil), New(fakeBackend{}), New(fakeBackend{err: sentinel})} {
		if p, err := o.OCRPage(context.Background(), nil, 1); err == nil || len(p.Blocks) > 0 {
			t.Fatalf("page=%+v err=%v", p, err)
		}
	}
	_, err := New(fakeBackend{err: sentinel}).OCRPage(context.Background(), nil, 1)
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(fakeBackend{}).OCRPage(ctx, nil, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	t.Setenv("EXCALIBUR_VISION_OCR", "0")
	if _, err := New(fakeBackend{text: "hello"}).OCRPage(context.Background(), nil, 1); err == nil {
		t.Fatal("disabled OCR succeeded")
	}
}
