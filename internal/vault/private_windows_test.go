package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"

	"golang.org/x/sys/windows"
)

func TestPassphraseDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "passphrase")
	if err := writePrivateFile(path, []byte("test passphrase\r\n")); err != nil {
		t.Fatal(err)
	}
	if ok, err := platform.IsOwnerOnly(path); err != nil || !ok {
		t.Fatalf("private = %v, %v", ok, err)
	}
	pp, err := LoadPassphraseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ZeroBytes(pp)
	if string(pp) != "test passphrase" {
		t.Fatal("incorrect passphrase")
	}
	if _, err := LoadPassphraseFile(filepath.Dir(path)); err == nil {
		t.Fatal("accepted directory")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestPassphraseDefaultDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(path, []byte("test passphrase"), 0o600); err != nil {
		t.Fatal(err)
	}
	pp, err := LoadPassphraseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ZeroBytes(pp)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;BU)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPassphraseFile(path); err == nil || !strings.Contains(err.Error(), "icacls") {
		t.Fatalf("unsafe passphrase: %v", err)
	}
}

func assertLoosePassphraseRejected(t *testing.T, dir, loose string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;BU)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(loose, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if v, err := Open(context.Background(), filepath.Join(dir, "v2.db"), Options{PassphraseFile: loose}); err == nil {
		_ = v.Close()
		t.Fatal("public passphrase file must be rejected")
	} else if !strings.Contains(err.Error(), "icacls") {
		t.Fatalf("wrong error: %v", err)
	}
}
