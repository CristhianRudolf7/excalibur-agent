//go:build !windows

package platform

import "path/filepath"

func pathIdentityCase(path string) string { return path }

func resolvePathLinks(path string) (string, error) { return filepath.EvalSymlinks(path) }
