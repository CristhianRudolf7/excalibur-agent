//go:build !linux

package vault

import "errors"

// lockMemory is a no-op outside Linux; the daemon only ships the Linux
// backend in this version and other OSes use the platform stubs.
func lockMemory(b []byte) {}

// unlockMemory is a no-op outside Linux.
func unlockMemory(b []byte) {}

// errUnsupportedKEK is returned by helpers needing OS keyring support
// on non-Linux builds.
var errUnsupportedKEK = errors.New("vault: keyring unsupported on this platform")
