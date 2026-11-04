//go:build !linux && !darwin && !windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

func defaultUserStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cli: home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "excalibur"), nil
}
func defaultExtensionsRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "excalibur-extensions")
	}
	return filepath.Join(home, ".local", "share", "excalibur", "extensions")
}
