package platform

import (
	"os"
	"path/filepath"
)

// CanonicalPath resolves symlinks in a path, including in the existing parent
// of an output that has not been created yet. Other resolution errors retain
// the cleaned spelling. Windows identities are case-insensitive; other platforms preserve case.
func CanonicalPath(path string) string {
	if path == "" {
		return ""
	}
	clean := filepath.Clean(path)
	parent := clean
	var suffix []string
	for {
		resolved, err := resolvePathLinks(parent)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return pathIdentityCase(resolved)
		}
		if !os.IsNotExist(err) || filepath.Dir(parent) == parent {
			return pathIdentityCase(clean)
		}
		suffix = append(suffix, filepath.Base(parent))
		parent = filepath.Dir(parent)
	}
}
