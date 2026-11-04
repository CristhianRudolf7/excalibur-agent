package tools

import "os"

func platformBoundaryPath(p string, harness []string) (bool, bool) {
	roots := []string{os.Getenv("SystemRoot"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramData")}
	// Keep the standard roots protected when the caller supplies a minimal env.
	roots = append(roots, `C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`, `C:\ProgramData`)
	return true, windowsPathOutsideBoundary(p, harness, roots)
}
func boundaryCommandWords(command string) []string { return windowsBoundaryWords(command) }
func commandWordBase(word string) string           { return commandBaseForOS(word, true) }

func commandStageWords(stage string) []string { return windowsLexicalWords(stage, " \t\r\n") }
