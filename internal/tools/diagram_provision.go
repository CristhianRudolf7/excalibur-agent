package tools

import (
	"fmt"
	"runtime"
	"strings"
)

func (b *SVGDiagramBackend) missingRenderer(dialect string) error {
	binary := map[string]string{"mermaid": "mmdc", "d2": "d2", "graphviz": "dot", "plantuml": "plantuml (or java with plantuml.jar)", "drawio": "drawio"}[dialect]
	var available []string
	for _, other := range []struct{ dialect, binary string }{{"graphviz", "dot"}, {"d2", "d2"}, {"mermaid", "mmdc"}, {"plantuml", "plantuml"}, {"drawio", "drawio"}} {
		if other.dialect == dialect {
			continue
		}
		if _, err := b.lookup(other.binary); err == nil {
			available = append(available, other.dialect)
		}
	}
	alternative := "Another dialect such as graphviz (dot) may be available."
	if len(available) > 0 {
		alternative = "Available alternative dialects: " + strings.Join(available, ", ") + "."
	}
	return fmt.Errorf("%s renderer missing: install %s on %s: %s. %s SVG input also works without a renderer; EXCALIBUR_KROKI_URL may point to your self-hosted Kroki service", dialect, binary, runtime.GOOS, b.installHint(dialect, runtime.GOOS), alternative)
}

func (b *SVGDiagramBackend) installHint(dialect, system string) string {
	if dialect == "mermaid" {
		return "npm install -g @mermaid-js/mermaid-cli (includes Chromium; mmdc must be on PATH)"
	}
	if system == "darwin" {
		return map[string]string{"d2": "brew install d2", "graphviz": "brew install graphviz", "plantuml": "brew install plantuml", "drawio": "brew install --cask drawio"}[dialect]
	}
	if dialect == "d2" {
		return "go install oss.terrastruct.com/d2@latest and add the Go bin directory to PATH"
	}
	if dialect == "drawio" {
		if system == "linux" {
			if _, err := b.lookup("apt-get"); err == nil {
				return "download the draw.io Desktop .deb from https://github.com/jgraph/drawio-desktop/releases and run sudo apt-get install ./drawio-*.deb"
			}
			if _, err := b.lookup("dnf"); err == nil {
				return "download the draw.io Desktop .rpm from https://github.com/jgraph/drawio-desktop/releases and run sudo dnf install ./drawio-*.rpm"
			}
		}
		return "install draw.io Desktop from https://github.com/jgraph/drawio-desktop/releases and add drawio to PATH"
	}
	if system == "linux" {
		binary := map[string]string{"graphviz": "dot", "plantuml": "plantuml"}[dialect]
		if plan := PlanInstall(&MissingDep{Kind: KindCommand, Name: binary}, b.lookup); plan != nil {
			return strings.Join(plan.Commands, " && ")
		}
	}
	if dialect == "plantuml" {
		return "install Java and plantuml.jar from https://plantuml.com/download; set EXCALIBUR_PLANTUML_JAR to its absolute path"
	}
	return "install Graphviz from https://graphviz.org/download/ and add dot to PATH"
}
