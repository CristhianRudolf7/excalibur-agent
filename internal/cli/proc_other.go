//go:build !linux

package cli

import "syscall"

// newChildAttr is the non-Linux stub: process-group detachment is a
// Linux (internal/platform) behavior, so other platforms spawn the
// child in the parent group and still build (Section 3.4).
func newChildAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
