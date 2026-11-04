package tools

// shellArgs returns the arguments that make shell run script as one
// command string.
func shellArgs(shell, script string) []string {
	_ = shell
	return []string{"-c", script}
}

// defaultShell is the shell used when no supported shell is found on
// PATH.
func defaultShell() string { return "/bin/sh" }
