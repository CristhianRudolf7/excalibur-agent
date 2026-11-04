//go:build darwin

package pdf

// Per-OS gate for the out-of-process PDF worker. Each OS flips its own
// file.
// workerSupported reports whether the sandboxed PDF worker can run.
func workerSupported() bool { return true }
