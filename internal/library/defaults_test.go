package library

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

func TestDefaultExtractorsCoverLibraryFormats(t *testing.T) {
	registry := docextract.NewRegistry()
	if err := RegisterDefaultExtractors(registry); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"pdf", "docx", "xlsx", "pptx", "odt", "ods", "odp", "epub", "txt", "md", "html", "csv", "json", "png", "jpeg"} {
		extractor, err := registry.Get(format)
		if err != nil || extractor == nil {
			t.Fatalf("%s: %v", format, err)
		}
	}
	if _, err := registry.Get("unknown"); err == nil {
		t.Fatal("unexpected unsupported extractor")
	}
}
