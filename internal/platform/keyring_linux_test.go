//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

// mapKeyring is the in-memory passphrase-hook stand-in for Section
// 20.1 tests: the vault injects its own store the same way.
type mapKeyring struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMapKeyring() *mapKeyring { return &mapKeyring{data: map[string][]byte{}} }

func mapKey(k1, k2 string) string { return k1 + "\x00" + k2 }

func (m *mapKeyring) Get(service, user string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[mapKey(service, user)]
	if !ok {
		return nil, keyring.ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

func (m *mapKeyring) Set(service, user string, secret []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[mapKey(service, user)] = append([]byte(nil), secret...)
	return nil
}

func (m *mapKeyring) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[mapKey(service, user)]; !ok {
		return keyring.ErrNotFound
	}
	delete(m.data, mapKey(service, user))
	return nil
}

func TestLinuxKeyringFallbackRoundtrip(t *testing.T) {
	// Break the session bus so the test never touches a real
	// keyring even where one exists.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "disabled:")
	fb := newMapKeyring()
	k := LinuxKeyring{Fallback: fb}
	svc := fmt.Sprintf("excalibur-test-%d", os.Getpid())
	if err := k.Set(svc, "u", []byte("s3cr3t")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := k.Get(svc, "u")
	if err != nil || string(got) != "s3cr3t" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := k.Delete(svc, "u"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := k.Get(svc, "u"); err == nil {
		t.Fatal("Get after Delete must fail")
	}
}

func TestLinuxKeyringNoFallbackError(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "disabled:")
	k := LinuxKeyring{}
	if _, err := k.Get("excalibur-test-missing", "nobody"); err == nil {
		t.Fatal("Get without Secret Service and without fallback must fail")
	}
}

func TestLinuxKeyringUnavailableDetection(t *testing.T) {
	if !isServiceUnavailable(errors.New("cannot connect to D-Bus session bus")) {
		t.Fatal("D-Bus dial error must count as unavailable")
	}
	if !isServiceUnavailable(errors.New("secret service operation failed")) {
		t.Fatal("secret service error must count as unavailable")
	}
	if isServiceUnavailable(keyring.ErrNotFound) {
		t.Fatal("ErrNotFound means the service answered: entry missing, not unavailable")
	}
	if isServiceUnavailable(nil) {
		t.Fatal("nil error is not unavailable")
	}
}
