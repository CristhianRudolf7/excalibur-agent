package config

import (
	"os"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"golang.org/x/sys/windows"
)

func UserDefaultPath() string {
	dir, err := (platform.WindowsPaths{}).ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "config.toml")
}
func systemDefaultPath() string {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if err != nil || root == "" {
		root = os.Getenv("PROGRAMDATA")
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, "Excalibur", "config.toml")
}
