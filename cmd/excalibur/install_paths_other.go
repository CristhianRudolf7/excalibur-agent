//go:build !linux

package main

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// Non-Linux install wiring: every operation reports
// errors.ErrUnsupported so the binary still builds for darwin and
// windows (Section 3.4). macOS and Windows arrive as new
// implementations behind the same interfaces.
func installDirs() daemon.Dirs { return platform.OtherPaths{} }

func installService() daemon.ServiceUnit { return platform.OtherServiceManager{} }
