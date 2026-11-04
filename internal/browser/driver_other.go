//go:build !linux && !darwin && !windows

package browser

import (
	"context"
	"errors"
)

// InstallOptions selects the driver cache directory.
type InstallOptions struct{ DriverDirectory string }

func supported() error { return errors.ErrUnsupported }

// Available reports that the engine is Linux-only.
func Available() error { return errors.ErrUnsupported }

// Install reports that the engine is Linux-only.
func Install(context.Context, InstallOptions) error { return errors.ErrUnsupported }
func processStats(string) (int64, []string)         { return 0, nil }

// AvailableForChannel reports that the browser is Linux-only.
func AvailableForChannel(string) error { return errors.ErrUnsupported }
func chromiumPath() (string, error)    { return "", errors.ErrUnsupported }
