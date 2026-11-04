package tools

import (
	"os"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
)

func diagramChromium(lookup LookPathFunc) (string, error) { return browser.ChromiumExecutable(lookup) }
func diagramFileURL(p string) string                      { return fileURL(p, true) }
func diagramJava() string                                 { return "java.exe" }
func plantUMLJars() []string {
	jars := []string{"plantuml.jar"}
	if root := os.Getenv("ProgramData"); root != "" {
		jars = append(jars, filepath.Join(root, "chocolatey", "lib", "plantuml", "tools", "plantuml.jar"))
	}
	roots := []string{os.Getenv("SCOOP"), os.Getenv("SCOOP_GLOBAL")}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "scoop"))
	}
	if root := os.Getenv("ProgramData"); root != "" {
		roots = append(roots, filepath.Join(root, "scoop"))
	}
	for _, root := range roots {
		if root != "" {
			jars = append(jars, filepath.Join(root, "apps", "plantuml", "current", "plantuml.jar"))
		}
	}
	return jars
}
