//go:build darwin

package platform

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/zalando/go-keyring"
)

type testDarwinKeyring struct {
	value   []byte
	err     error
	deleted bool
}

func (b *testDarwinKeyring) Get(_, _ string) (string, error) { return string(b.value), b.err }
func (b *testDarwinKeyring) Set(_, _, value string) error    { b.value = []byte(value); return b.err }
func (b *testDarwinKeyring) Delete(_, _ string) error        { b.deleted = true; return b.err }

type testDarwinFallback struct {
	value   []byte
	deleted bool
}

func (b *testDarwinFallback) Get(_, _ string) ([]byte, error) { return b.value, nil }
func (b *testDarwinFallback) Set(_, _ string, value []byte) error {
	b.value = append([]byte(nil), value...)
	return nil
}
func (b *testDarwinFallback) Delete(_, _ string) error { b.deleted = true; return nil }

func TestDarwinKeyringFallback(t *testing.T) {
	primary := &testDarwinKeyring{err: errors.New("User interaction is not allowed")}
	fallback := &testDarwinFallback{}
	k := DarwinKeyring{backend: primary, Fallback: fallback}
	secret := []byte{0, 255, 10, 32, 0, 128}
	if k.Available() {
		t.Fatal("locked keychain available")
	}
	if err := k.Set("test", "user", secret); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get("test", "user"); err != nil || !bytes.Equal(got, secret) {
		t.Fatal("binary fallback roundtrip failed")
	}
	if err := k.Delete("test", "user"); err != nil || !fallback.deleted {
		t.Fatal("fallback deletion failed")
	}
	primary.err = keyring.ErrNotFound
	if !k.Available() {
		t.Fatal("missing item means available keychain")
	}
	if _, err := k.Get("test", "user"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("missing primary must not expose stale fallback")
	}
	if err := k.Set("test", "user", secret); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("unexpected fallback")
	}
	if err := k.Delete("test", "user"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("unexpected delete fallback")
	}
	primary.err = nil
	if err := k.Set("test", "user", secret); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get("test", "user"); err != nil || !bytes.Equal(got, secret) {
		t.Fatal("binary primary roundtrip failed")
	}
	fallback.deleted = false
	if err := k.Delete("test", "user"); err != nil || !fallback.deleted {
		t.Fatal("primary delete did not sync fallback")
	}
	k.Fallback = nil
	primary.err = errors.New("keychain unavailable")
	if _, err := k.Get("test", "user"); err == nil {
		t.Fatal("unavailable keychain without fallback succeeded")
	}
}

func TestDarwinKeychainErrors(t *testing.T) {
	for _, message := range []string{"User interaction is not allowed", "User interaction is required", "A default keychain could not be found", "The specified keychain could not be found", "No keychain is available"} {
		if !isDarwinKeychainUnavailable(errors.New(message)) {
			t.Fatalf("unrecognized unavailable diagnostic: %s", message)
		}
	}
	for _, err := range []error{nil, keyring.ErrNotFound, keyring.ErrSetDataTooBig, errors.New("unexpected error")} {
		if isDarwinKeychainUnavailable(err) {
			t.Fatal("unexpected unavailable classification")
		}
	}
	for _, tc := range []struct {
		code string
		want bool
	}{{"36", true}, {"37", true}, {"50", true}, {"53", true}, {"29", true}, {"44", false}, {"1", false}} {
		err := exec.Command("/bin/sh", "-c", "exit "+tc.code).Run()
		if got := isDarwinKeychainUnavailable(err); got != tc.want {
			t.Fatalf("exit %s unavailable=%v", tc.code, got)
		}
	}
}

func TestDarwinKeyringLiveBinaryRoundtrip(t *testing.T) {
	if os.Getenv("EXCALIBUR_KEYCHAIN_TEST") != "1" {
		t.Skip("set EXCALIBUR_KEYCHAIN_TEST=1 on a macOS keychain session")
	}
	k := DarwinKeyring{}
	if !k.Available() {
		t.Skip("no available keychain")
	}
	service := fmt.Sprintf("excalibur-test-%d", os.Getpid())
	secret := make([]byte, 256)
	for i := range secret {
		secret[i] = byte(i)
	}
	if err := k.Set(service, "binary", secret); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Delete(service, "binary") }()
	if got, err := k.Get(service, "binary"); err != nil || !bytes.Equal(got, secret) {
		t.Fatal("keychain binary roundtrip failed")
	}
	if err := k.Delete(service, "binary"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(service, "binary"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("deleted entry found")
	}
}
