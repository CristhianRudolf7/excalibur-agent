//go:build !linux

package cli

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func browserDataDir() (string, error) { return (platform.OtherPaths{}).DataDir() }
