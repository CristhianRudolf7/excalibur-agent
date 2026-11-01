//go:build !linux

package platform

import "errors"

type SignalTarget struct {
	PID, Group    int
	Name, Command string
}

func ProtectedSignalTargets() ([]SignalTarget, error) { return nil, errors.ErrUnsupported }
func Reexec(argv, env []string) error                 { return errors.ErrUnsupported }
