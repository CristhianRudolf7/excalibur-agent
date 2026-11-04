package config

import (
	"os"
	"path/filepath"
)

// UserDefaultPath resolves the XDG user file.
func UserDefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "excalibur", "config.toml")
}

func systemDefaultPath() string { return SystemDefaultPath }
