package tools

import (
	"os/exec"
)

func diagramChromium(lookup LookPathFunc) (string, error) {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := lookup(name); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

func diagramFileURL(p string) string { return fileURL(p, false) }
func plantUMLJars() []string {
	return []string{"/usr/share/plantuml/plantuml.jar", "/usr/share/java/plantuml.jar", "plantuml.jar"}
}
func diagramJava() string                   { return "java" }
func runDiagramCommand(cmd *exec.Cmd) error { return cmd.Run() }
