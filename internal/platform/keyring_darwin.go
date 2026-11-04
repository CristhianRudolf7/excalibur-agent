//go:build darwin

package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// DarwinKeyring stores the vault master key in the macOS Keychain
// (Section 20.1). go-keyring v0.2.6 sends base64-prefixed binary secrets
// to /usr/bin/security -i over stdin, never through process arguments.
// Fallback is used only when the keychain cannot serve this session.
type DarwinKeyring struct {
	Fallback Keyring
	backend  darwinKeyringBackend
}

type darwinKeyringBackend interface {
	Get(string, string) (string, error)
	Set(string, string, string) error
	Delete(string, string) error
}

type systemDarwinKeyring struct{}

func (systemDarwinKeyring) Get(service, user string) (string, error) {
	secret, err := keyring.Get(service, user)
	return secret, checkDarwinKeychainMissing(err)
}
func (systemDarwinKeyring) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}
func (systemDarwinKeyring) Delete(service, user string) error {
	return checkDarwinKeychainMissing(keyring.Delete(service, user))
}

// go-keyring maps both missing items and missing default keychains to
// ErrNotFound (their security diagnostics contain "could not be found").
// Probe the default keychain to distinguish those cases, without ever
// reading or displaying a secret.
func checkDarwinKeychainMissing(err error) error {
	if !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if probeErr := exec.CommandContext(ctx, "/usr/bin/security", "default-keychain", "-d", "user").Run(); probeErr != nil {
		return errors.New("platform: default keychain unavailable")
	}
	return err
}

var _ Keyring = DarwinKeyring{}

func (k DarwinKeyring) primary() darwinKeyringBackend {
	if k.backend != nil {
		return k.backend
	}
	return systemDarwinKeyring{}
}

// Get returns raw bytes; the dependency decodes its versioned base64
// prefix before returning the string, preserving NULs and whitespace.
func (k DarwinKeyring) Get(service, user string) ([]byte, error) {
	secret, err := k.primary().Get(service, user)
	if err == nil {
		return []byte(secret), nil
	}
	if k.Fallback != nil && isDarwinKeychainUnavailable(err) {
		return k.Fallback.Get(service, user)
	}
	return nil, err
}

// Set stores binary master-key material, or invokes the passphrase hook.
func (k DarwinKeyring) Set(service, user string, secret []byte) error {
	err := k.primary().Set(service, user, string(secret))
	if err != nil && k.Fallback != nil && isDarwinKeychainUnavailable(err) {
		return k.Fallback.Set(service, user, secret)
	}
	return err
}

// Delete keeps a configured fallback in sync after a primary deletion.
func (k DarwinKeyring) Delete(service, user string) error {
	err := k.primary().Delete(service, user)
	if err == nil {
		if k.Fallback != nil {
			_ = k.Fallback.Delete(service, user)
		}
		return nil
	}
	if k.Fallback != nil && isDarwinKeychainUnavailable(err) {
		return k.Fallback.Delete(service, user)
	}
	return err
}

// Available probes an absent entry without modifying the keychain.
func (k DarwinKeyring) Available() bool {
	_, err := k.primary().Get("excalibur-availability-probe", "nobody")
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}

func isDarwinKeychainUnavailable(err error) bool {
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return true
	}
	// go-keyring discards security's stderr, leaving only ExitError.
	// security returns OSStatus modulo 256. Never treat errSecItemNotFound
	// (-25300) as an unavailable keychain.
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		for _, status := range []int{-25291, -25294, -25307, -25308, -25315} {
			if exit.ExitCode() == status&255 {
				return true
			}
		}
	}
	message := strings.ToLower(err.Error())
	for _, hint := range []string{"keychain unavailable", "no keychain is available", "user interaction is not allowed", "user interaction is required", "no default keychain", "default keychain could not be found", "specified keychain could not be found", "no such file"} {
		if strings.Contains(message, hint) {
			return true
		}
	}
	return false
}
