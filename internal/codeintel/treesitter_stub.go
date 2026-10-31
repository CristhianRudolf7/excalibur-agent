//go:build !cgo

package codeintel

// TreeSitterAvailable reports whether the tree-sitter backend is
// compiled in. It is false here: without cgo the pure-Go fallback
// parses instead, so the binary still builds for darwin and windows
// (Section 3.4) and plain `go test` runs everywhere.
func TreeSitterAvailable() bool { return false }

// treeSitterParserFor always misses without cgo; PreferredParser uses
// the deterministic fallback.
func treeSitterParserFor(_ string) Parser { return nil }

// TreeSitterLanguages lists the languages with a bundled grammar:
// none without cgo.
func TreeSitterLanguages() []string { return nil }
