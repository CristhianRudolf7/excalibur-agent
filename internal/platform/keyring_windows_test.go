package platform

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/sys/windows"
)

func TestWindowsKeyringRoundTrip(t *testing.T) {
	k := WindowsKeyring{}
	service := fmt.Sprintf("excalibur-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	if err := k.Set(service, "user", []byte("test-value")); err != nil {
		if isServiceUnavailable(err) {
			t.Skipf("Credential Manager unavailable: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Delete(service, "user") })
	got, err := k.Get(service, "user")
	if err != nil || string(got) != "test-value" {
		t.Fatalf("round trip failed: %v", err)
	}
	if err := k.Delete(service, "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(service, "user"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("missing entry = %v", err)
	}
}
func TestWindowsKeyringUnavailable(t *testing.T) {
	if !isServiceUnavailable(windows.ERROR_NO_SUCH_LOGON_SESSION) {
		t.Fatal("logon session must fall back")
	}
	if isServiceUnavailable(keyring.ErrNotFound) || isServiceUnavailable(keyring.ErrSetDataTooBig) {
		t.Fatal("entry and input errors must not fall back")
	}
}
