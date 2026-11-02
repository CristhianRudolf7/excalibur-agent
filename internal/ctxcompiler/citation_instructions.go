package ctxcompiler

import (
	"os"
	"strings"
)

// CitationsPipelineEnabled gates document citation instructions and rendering.
func CitationsPipelineEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_CITATIONS_PIPELINE"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// CitationInstructions supplies tool-dependent guidance only for document
// retrieval tools present in the effective request. Eval has no library tools.
func CitationInstructions(name string) string {
	if !CitationsPipelineEnabled() {
		return ""
	}
	switch name {
	case "library_search", "library_view", "library_grep":
		return " Cite every factual claim from documents with the exact [[doc:document_id#chunk_id]] ref printed by this tool. Use at most three refs per sentence and cite each list item. If the documents do not contain the answer, say so clearly instead of answering from memory."
	default:
		return ""
	}
}
