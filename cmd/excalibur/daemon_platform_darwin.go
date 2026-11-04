//go:build darwin

package main

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

// daemonSampler supplies live macOS RSS/CPU and descendant counts to
// the RAM watchdog (Sections 3.4, 5.2).
func daemonSampler() platform.ResourceSampler { return platform.DarwinResourceSampler{} }

// physicalRAMBytes returns hw.memsize, or zero when unavailable.
func physicalRAMBytes() uint64 {
	bytes, err := platform.PhysicalMemoryBytes()
	if err != nil {
		return 0
	}
	return bytes
}

// daemonKeyring uses Keychain (20.1). Like Linux, a nil Fallback leaves
// an unavailable session passphrase-only through OpenVault's hook.
func daemonKeyring() platform.Keyring { return platform.DarwinKeyring{} }
