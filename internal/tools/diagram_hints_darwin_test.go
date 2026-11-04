//go:build darwin

package tools

import "testing"

func TestDarwinDiagramInstallHints(t *testing.T) {
	backend := &SVGDiagramBackend{}
	for dialect, want := range map[string]string{
		"d2":       "brew install d2",
		"graphviz": "brew install graphviz",
		"plantuml": "brew install plantuml",
		"drawio":   "brew install --cask drawio",
	} {
		if got := backend.installHint(dialect, "darwin"); got != want {
			t.Errorf("%s: got %q, want %q", dialect, got, want)
		}
	}
	if got := imageOCRInstallHint("darwin", ""); got != "package: brew install tesseract" {
		t.Errorf("OCR hint: %q", got)
	}
}
