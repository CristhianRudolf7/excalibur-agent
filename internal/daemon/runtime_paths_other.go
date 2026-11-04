//go:build !linux && !darwin && !windows

package daemon

import "os"

func rendezvousRuntimeRoot() string { return os.Getenv("XDG_RUNTIME_DIR") }
