//go:build !linux && !darwin && !windows

package tasks

import "os"

func artifactExecutable(_ string, mode os.FileMode) bool { return mode.Perm()&0o111 != 0 }
