package tasks

import (
	"path"
	"strings"
)

// Windows exposes no Unix execute bits. Use the shell's executable extensions,
// with the native defaults when PATHEXT is unset.
func windowsExecutableExtension(filename, pathext string) bool {
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	ext := path.Ext(strings.ReplaceAll(filename, `\`, "/"))
	if ext == "" {
		return false
	}
	for _, allowed := range strings.Split(pathext, ";") {
		if strings.EqualFold(ext, strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}
