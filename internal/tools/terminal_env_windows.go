package tools

func windowsEnvironment() bool     { return true }
func normEnvKey(key string) string { return normalizeEnvKey(key, true) }
