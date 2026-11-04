package platform

import "os/exec"

// WindowsIsolator is the Section 20 extension point. In this version it
// runs the process as-is; deferred security connects here in a later milestone.
type WindowsIsolator struct{}

var _ Isolator = WindowsIsolator{}

// Isolate accepts the command unchanged, matching the Linux policy.
func (WindowsIsolator) Isolate(_ *exec.Cmd) error { return nil }
