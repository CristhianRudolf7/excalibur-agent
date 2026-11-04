//go:build !linux && !darwin && !windows

package codeintel

func windowsPaths() bool { return false }
