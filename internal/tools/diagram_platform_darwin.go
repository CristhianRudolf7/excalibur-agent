package tools

import (
	"os"
	"os/exec"
	"path/filepath"
)

func diagramChromium(lookup LookPathFunc) (string, error) {
	home, _ := os.UserHomeDir()
	return darwinDiagramChromium(home, lookup, os.Stat)
}

func darwinDiagramChromium(home string, lookup LookPathFunc, stat func(string) (os.FileInfo, error)) (string, error) {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := lookup(name); err == nil {
			return p, nil
		}
	}
	roots := []string{"/Applications"}
	if home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	for _, root := range roots {
		for _, bundle := range []string{"Google Chrome.app/Contents/MacOS/Google Chrome", "Chromium.app/Contents/MacOS/Chromium"} {
			// Honor injected lookups for app bundles as well as PATH names,
			// including the executable path they return.
			path, err := lookup(filepath.Join(root, bundle))
			if err != nil {
				continue
			}
			if info, err := stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
				return path, nil
			}
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
