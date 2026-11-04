//go:build !linux && !darwin && !windows

package main

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

// daemonSampler is the resource sampler behind the RAM watchdog. Other
// platforms have no sampler yet (Section 3.4): the watchdog stays
// inert unless EXCALIBUR_RAM_THRESHOLD_MB is set.
func daemonSampler() platform.ResourceSampler { return platform.OtherResourceSampler{} }

// physicalRAMBytes reports zero: the default threshold needs a figure
// this platform does not provide yet.
func physicalRAMBytes() uint64 { return 0 }

// daemonKeyring reports the platform keyring. Keychain and DPAPI are
// future adapters behind the same interface (Section 20.1); until then
// the vault is passphrase-only here.
func daemonKeyring() platform.Keyring { return platform.OtherKeyring{} }
