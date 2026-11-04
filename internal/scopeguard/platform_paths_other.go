//go:build !linux && !darwin && !windows

package scopeguard

import (
	"os"
	"path/filepath"
	"strings"
)

func originalsDir() string                         { return "/tmp/excalibur-originals" }
func fingerprintMode(mode os.FileMode) os.FileMode { return mode.Perm() }
func inputExecutable(mode os.FileMode) bool        { return mode.Perm()&0111 != 0 }
func backupRelativePath(path string) string {
	return strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
}
