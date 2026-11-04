package cli

import (
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func defaultUserStateDir() (string, error) { return (platform.WindowsPaths{}).DataDir() }
func defaultExtensionsRoot() string {
	dir, err := (platform.WindowsPaths{}).DataDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "extensions")
}
