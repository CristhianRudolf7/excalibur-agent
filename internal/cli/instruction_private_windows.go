package cli

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func protectInstructionFile(path string) error { return platform.RestrictToOwner(path) }
