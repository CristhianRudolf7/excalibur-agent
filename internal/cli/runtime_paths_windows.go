package cli

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func cliRuntimeDir() string {
	root, err := (platform.WindowsPaths{}).RuntimeDir()
	if err != nil {
		return ""
	}
	return daemon.RuntimeAddrDir(root)
}
