package ctxcompiler

import (
	"strings"
	"testing"
)

func TestCitationInstructionsDependOnToolsAndSwitch(t *testing.T) {
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "")
	for _, name := range []string{"library_search", "library_view", "library_grep"} {
		got := CitationInstructions(name)
		for _, want := range []string{"exact [[doc:", "three refs per sentence", "each list item", "instead of answering from memory"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s missing %q", name, want)
			}
		}
	}
	if CitationInstructions("exec") != "" || CitationInstructions("library_ingest") != "" {
		t.Fatal("instructions leaked to unrelated tools")
	}
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "off")
	if CitationsPipelineEnabled() || CitationInstructions("library_search") != "" {
		t.Fatal("off path")
	}
}
