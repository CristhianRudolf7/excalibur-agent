package main

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// installDirs supplies native per-user Windows directories.
func installDirs() daemon.Dirs { return platform.WindowsPaths{} }

func installService() daemon.ServiceUnit { return platform.WindowsServiceManager{} }
