//go:build linux

package platform

import (
	"os/exec"
)

// LinuxIsolator is the Section 20 extension point. In this version it
// runs the process as-is and only records nothing: deferred security
// (sandboxes, approvals) connects here in a later milestone.
type LinuxIsolator struct{}

// Compile-time interface check.
var _ Isolator = LinuxIsolator{}

// Isolate implements platform.Isolator: the pass-through policy
// accepts the command unchanged.
func (LinuxIsolator) Isolate(_ *exec.Cmd) error { return nil }
