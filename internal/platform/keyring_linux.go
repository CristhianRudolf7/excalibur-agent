//go:build linux

package platform

import (
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

// LinuxKeyring stores the vault master key in the desktop Secret
// Service (GNOME Keyring, KWallet) over D-Bus (Section 20.1). When no
// Secret Service is reachable (servers, SSH sessions), every
// operation falls back to Fallback when set: the passphrase hook owned
// by Section 20.1. A nil Fallback surfaces the Secret Service error.
type LinuxKeyring struct {
	// Fallback serves Get/Set/Delete when the Secret Service is
	// unreachable. The vault injects its passphrase-backed store
	// here; tests inject an in-memory fake.
	Fallback Keyring
}

// Compile-time interface check.
var _ Keyring = LinuxKeyring{}

// Get implements platform.Keyring.
func (k LinuxKeyring) Get(service, user string) ([]byte, error) {
	secret, err := keyring.Get(service, user)
	if err == nil {
		return []byte(secret), nil
	}
	if errors.Is(err, keyring.ErrNotFound) || k.Fallback == nil || !isServiceUnavailable(err) {
		return nil, err
	}
	return k.Fallback.Get(service, user)
}

// Set implements platform.Keyring.
func (k LinuxKeyring) Set(service, user string, secret []byte) error {
	if err := keyring.Set(service, user, string(secret)); err == nil {
		return nil
	} else if k.Fallback == nil || !isServiceUnavailable(err) {
		return err
	}
	return k.Fallback.Set(service, user, secret)
}

// Delete implements platform.Keyring. A missing entry in both stores
// reports the Secret Service ErrNotFound.
func (k LinuxKeyring) Delete(service, user string) error {
	err := keyring.Delete(service, user)
	if err == nil {
		if k.Fallback != nil {
			// Best effort: keep the fallback in sync; its error
			// never masks a successful primary delete.
			_ = k.Fallback.Delete(service, user)
		}
		return nil
	}
	if errors.Is(err, keyring.ErrNotFound) || k.Fallback == nil || !isServiceUnavailable(err) {
		return err
	}
	return k.Fallback.Delete(service, user)
}

// Available probes whether a Secret Service answers on the session
// bus without touching any entry. It returns false on headless
// servers and SSH sessions without a forwarded bus.
func (LinuxKeyring) Available() bool {
	probeService := "excalibur-availability-probe"
	probeUser := "nobody"
	if _, err := keyring.Get(probeService, probeUser); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return true
		}
		return !isServiceUnavailable(err)
	}
	return true
}

// isServiceUnavailable distinguishes "the bus/daemon is not there"
// from "the entry is not there". go-keyring surfaces D-Bus dial,
// exec and secret-service errors as plain errors; entry misses are
// always keyring.ErrNotFound, handled before this helper runs.
func isServiceUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, hint := range []string{
		"dbus",
		"d-bus",
		"secret service",
		"secret-service",
		"connection refused",
		"no such file",
		"cannot autolaunch",
		"exec:",
		"not available",
		"session bus",
	} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}
