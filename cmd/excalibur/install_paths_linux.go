//go:build linux

package main

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"os"
)

// installDirs wires the real Linux XDG directories (25.2 step 1).
func installDirs() daemon.Dirs { return platform.LinuxPaths{} }

// installService wires the systemd user unit manager (25.2 step 4).
func installService() daemon.ServiceUnit {
	name := os.Getenv("EXCALIBUR_SERVICE_NAME")
	if name != "" {
		name += ".service"
	}
	return platform.LinuxServiceManager{UnitName: name}
}
