package cli

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// newChildAttr keeps background tasks alive after the console parent exits.
func newChildAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
