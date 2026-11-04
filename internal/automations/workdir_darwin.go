package automations

import (
	"os"
	"path/filepath"
)

func defaultWorkDir() (string, error) {
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(root, "excalibur", "automations"), nil
}
