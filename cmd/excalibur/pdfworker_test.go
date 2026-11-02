package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf/testpdf"
)

func TestPDFWorkerIsHiddenAndReadsDocument(t *testing.T) {
	if strings.Contains(topUsage(commands()), "pdf-worker") {
		t.Fatal("private worker appeared in global help")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input := testpdf.Build([]testpdf.Page{{Lines: []testpdf.Line{{Text: "Production worker dispatch", X: 50, Y: 700}}}}, testpdf.Options{Title: "Worker fixture"})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d, err := pdf.New(pdf.Options{WorkerPath: executable}).Extract(ctx, bytes.NewReader(input), int64(len(input)), docextract.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Worker fixture" || len(d.Pages) != 1 || len(d.Pages[0].Blocks) != 1 || d.Pages[0].Blocks[0].Text != "Production worker dispatch" {
		t.Fatalf("worker extraction: %+v", d)
	}
}
