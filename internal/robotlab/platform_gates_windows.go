package robotlab

import (
	"path/filepath"
	"strings"
)

func runtimeSupported() bool                     { return true }
func venvPython(dir string) string               { return filepath.Join(dir, "Scripts", "python.exe") }
func bootstrapPython() (string, []string, error) { return findWindowsPython() }
func mujocoGL() string                           { return "" }
func needsOSMesa() bool                          { return false }

func platformEnvAllowed(name string) bool {
	switch strings.ToUpper(name) {
	case "PATH", "SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "PATHEXT", "COMSPEC", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE":
		return true
	default:
		return false
	}
}

func prerequisiteMessage() string { return "Checking Python runtime prerequisites" }
