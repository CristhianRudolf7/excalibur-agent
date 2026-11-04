package daemon

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func rendezvousRuntimeRoot() string {
	dir, _ := (platform.WindowsPaths{}).RuntimeDir()
	return dir
}
