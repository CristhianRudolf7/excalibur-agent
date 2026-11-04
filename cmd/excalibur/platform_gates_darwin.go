//go:build darwin

package main

// argvReexecSupported enables the private request-file handoff in
// exec.go. Darwin syscall.Exec replaces the image while retaining PID,
// environment and signal ownership, so the file is consumed once by
// the replacement process and the instruction disappears from argv.
func argvReexecSupported() bool { return true }
