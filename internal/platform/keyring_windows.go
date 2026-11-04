package platform

import (
	"errors"

	"github.com/zalando/go-keyring"
	"golang.org/x/sys/windows"
)

// WindowsKeyring stores the vault master key in the Windows Credential
// Manager (wincred) (Section 20.1). When no
// Credential Manager is reachable (servers, SSH sessions), every
// operation falls back to Fallback when set: the passphrase hook owned
// by Section 20.1. A nil Fallback surfaces the Credential Manager error.
type WindowsKeyring struct {
	// Fallback serves Get/Set/Delete when the Credential Manager is
	// unreachable. The vault injects its passphrase-backed store
	// here; tests inject an in-memory fake.
	Fallback Keyring
}

// Compile-time interface check.
var _ Keyring = WindowsKeyring{}

// Get implements platform.Keyring.
func (k WindowsKeyring) Get(service, user string) ([]byte, error) {
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
func (k WindowsKeyring) Set(service, user string, secret []byte) error {
	if err := keyring.Set(service, user, string(secret)); err == nil {
		return nil
	} else if k.Fallback == nil || !isServiceUnavailable(err) {
		return err
	}
	return k.Fallback.Set(service, user, secret)
}

// Delete implements platform.Keyring. A missing entry in both stores
// reports the Credential Manager ErrNotFound.
func (k WindowsKeyring) Delete(service, user string) error {
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

// Available reports whether Credential Manager accepts queries.
func (WindowsKeyring) Available() bool {
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

// Credential Manager can be unavailable in non-interactive logon sessions.
func isServiceUnavailable(err error) bool {
	return errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED) ||
		errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE)
}
