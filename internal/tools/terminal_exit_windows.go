package tools

import "os/exec"

// Windows exception and application exit codes are not POSIX signals.
func exitSignalOf(ee *exec.ExitError) (int, bool) { return 0, false }
func shellExitSignal(code int) string             { return "" }
