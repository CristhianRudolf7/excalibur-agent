//go:build darwin

package pdf

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf/testpdf"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func TestDarwinWorkerPlatform(t *testing.T) {
	group, sampler := workerPlatform()
	if _, ok := group.(platform.DarwinProcessGroup); !ok {
		t.Fatal("incorrect process group")
	}
	if _, ok := sampler.(platform.DarwinResourceSampler); !ok {
		t.Fatal("incorrect resource sampler")
	}
	sample, err := sampler.Sample(os.Getpid())
	if err != nil || sample.RSSBytes == 0 {
		t.Fatalf("worker RSS unavailable: %+v %v", sample, err)
	}
}

func TestDarwinColdPDFWorkerWithinRSSBudget(t *testing.T) {
	// macOS UserCacheDir derives from HOME. Start with no compiled Wasm,
	// the condition that failed while warm-cache worker tests passed.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOMEMLIMIT", "")
	if err := os.Unsetenv("GOMEMLIMIT"); err != nil {
		t.Fatal(err)
	}
	input := testpdf.Build([]testpdf.Page{{Lines: []testpdf.Line{{Text: "Cold worker text", X: 50, Y: 700}}}}, testpdf.Options{Title: "Cold worker"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	doc, err := New(Options{}).Extract(ctx, bytes.NewReader(input), int64(len(input)), docextract.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Cold worker" || len(doc.Pages) != 1 || pageText(doc.Pages[0]) != "Cold worker text" {
		t.Fatalf("cold extraction: %+v", doc)
	}
}
