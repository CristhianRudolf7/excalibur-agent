//go:build darwin

package main

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"os"
)

// installDirs keeps the installer and daemon on the same XDG paths (25.2).
func installDirs() daemon.Dirs { return platform.DarwinPaths{} }

// installService wires the launchd user LaunchAgent (25.2).
func installService() daemon.ServiceUnit {
	name := os.Getenv("EXCALIBUR_SERVICE_NAME")
	return platform.DarwinServiceManager{Label: name}
}
