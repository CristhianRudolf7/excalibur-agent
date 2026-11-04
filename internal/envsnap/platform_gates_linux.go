package envsnap

import "os"

// Per-OS gate for cgroup resource discovery. Each OS flips its own file.
// cgroupsSupported reports whether the host exposes cgroup counters.
func cgroupsSupported() bool { return true }

func platformDistro() (string, bool) { return "", false }
func probeEnvironment() []string     { return []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"} }
func shellCandidates() []string      { return []string{"bash", "zsh", "fish", "sh", "dash"} }
func shellPreferences() []string     { return []string{"bash", "zsh", "sh"} }
func shellFallback() string          { return "/bin/sh" }
