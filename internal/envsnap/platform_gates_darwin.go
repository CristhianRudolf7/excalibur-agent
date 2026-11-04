//go:build darwin

package envsnap

import "os"

// This gate controls Linux /proc membership and /sys cgroup discovery, not
// general host resource reporting. Darwin has no cgroups. Adding sysctl host
// capacity and vm_stat memory reporting requires a separate shared hook in
// ResourceReader; enabling this gate would still read Linux paths.
func cgroupsSupported() bool { return false }

func platformDistro() (string, bool) { return "", false }
func probeEnvironment() []string     { return []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"} }
func shellCandidates() []string      { return []string{"bash", "zsh", "fish", "sh", "dash"} }
func shellPreferences() []string     { return []string{"bash", "zsh", "sh"} }
func shellFallback() string          { return "/bin/sh" }
