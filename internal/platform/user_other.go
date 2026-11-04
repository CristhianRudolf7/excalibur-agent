//go:build !linux && !darwin && !windows

package platform

import "errors"

func CurrentUserName() (string, error) { return "", errors.ErrUnsupported }
func LocalTimezone() (string, error)   { return "", errors.ErrUnsupported }
