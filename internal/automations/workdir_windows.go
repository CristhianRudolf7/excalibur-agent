package automations

import (
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func defaultWorkDir() (string, error) {
	dir, err := (platform.WindowsPaths{}).DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "automations"), nil
}
