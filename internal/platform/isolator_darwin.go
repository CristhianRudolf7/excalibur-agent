//go:build darwin

package platform

import (
	"os/exec"
)

// DarwinIsolator is the Section 20 extension point. In this version it
// runs the process as-is and only records nothing: deferred security
// (sandboxes, approvals) connects here in a later milestone.
type DarwinIsolator struct{}

// Compile-time interface check.
var _ Isolator = DarwinIsolator{}

// Isolate implements platform.Isolator: the pass-through policy
// accepts the command unchanged.
func (DarwinIsolator) Isolate(_ *exec.Cmd) error { return nil }
