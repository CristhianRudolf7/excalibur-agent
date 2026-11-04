//go:build !windows

package attempts

import (
	"path/filepath"
	"strings"
)

func sameWorkdir(real, attempt string) bool     { return real == attempt }
func commandPathIndex(command, real string) int { return strings.Index(command, real) }

func resolveAttemptPath(abs string) string {
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
