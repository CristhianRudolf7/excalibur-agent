//go:build darwin

package cli

import "syscall"

// newChildAttr detaches the background task child into its own
// process group so daemon shutdowns and terminal signals do not take
// it down with the parent (Section 3.6: the child outlives start).
func newChildAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
