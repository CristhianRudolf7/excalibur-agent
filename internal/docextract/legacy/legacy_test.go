package legacy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
)

func TestLegacyConversion(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux converter")
	}
	for _, format := range []string{"xls", "doc", "ppt"} {
		t.Run(format, func(t *testing.T) {
			target := map[string]string{"xls": "xlsx", "doc": "docx", "ppt": "pptx"}[format]
			dir := t.TempDir()
			native := filepath.Join(dir, "native."+target)
			spec := create.Spec{Title: "Legacy", Sheets: []create.Sheet{{Name: "Sheet1", Cells: []create.Cell{{Ref: "A1", Value: "hello"}}}}}
			if format == "doc" {
				spec.Blocks = []create.Block{{Kind: "paragraph", Text: "hello"}}
			}
			if format == "ppt" {
				spec.Slides = []create.Slide{{Kind: "title", Title: "hello"}}
			}
			pkg, err := create.Build(target, spec, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := pkg.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(native, data, 0600); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(dir, "converter")
			// Fixed script uses its own directory for the native fixture and receives
			// private conversion paths only through positional arguments.
			script := "#!/bin/sh\nset -eu\ntarget=$4\nout=$6\ncp \"$(dirname \"$0\")/native.$target\" \"$out/input.$target\"\n"
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			e := &Extractor{Format: format, Binary: binary}
			doc, err := e.Extract(context.Background(), bytes.NewReader([]byte("legacy")), 6, docextract.Options{})
			if err != nil || doc.Format != format || len(doc.Pages) == 0 {
				t.Fatalf("%+v %v", doc, err)
			}
			if format == "xls" {
				found := false
				for _, block := range doc.Pages[0].Blocks {
					if block.Kind == docextract.KindCell && block.Anchor == "Sheet1!A1" && block.Text == "hello" {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing legacy cells: %+v", doc)
				}
			}
		})
	}
}
func TestLegacyMissingConverterAndCancellation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux converter")
	}
	t.Setenv("PATH", t.TempDir())
	e := New("xls")
	_, err := e.Extract(context.Background(), strings.NewReader("old"), 3, docextract.Options{})
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.Binary = "/bin/false"
	_, err = e.Extract(ctx, strings.NewReader("old"), 3, docextract.Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}
