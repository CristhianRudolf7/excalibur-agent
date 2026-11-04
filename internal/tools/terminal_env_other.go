//go:build !linux && !darwin && !windows

package tools

func windowsEnvironment() bool     { return false }
func normEnvKey(key string) string { return normalizeEnvKey(key, false) }
