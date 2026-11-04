package platform

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestPrivateDACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RestrictToOwner(dir); err != nil {
		t.Fatal(err)
	}
	assertProtectedOwnerDACL(t, dir)
	if ok, err := IsOwnerOnly(dir); err != nil || !ok {
		t.Fatalf("directory private = %v, %v", ok, err)
	}
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestrictToOwner(path); err != nil {
		t.Fatal(err)
	}
	assertProtectedOwnerDACL(t, path)
	if ok, err := IsOwnerOnly(path); err != nil || !ok {
		t.Fatalf("file private = %v, %v", ok, err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
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
	if ok, err := IsOwnerOnly(path); err != nil || ok {
		t.Fatalf("public file private = %v, %v", ok, err)
	}
}

func TestPrivateDACLTrustees(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	current := user.User.Sid.String()
	for _, tc := range []struct {
		name, dacl string
		want       bool
	}{
		{"owner and system", "D:P(A;;FA;;;" + current + ")(A;;FA;;;SY)", true},
		{"inherited user system administrators", "D:(A;ID;FA;;;" + current + ")(A;ID;FA;;;SY)(A;ID;FA;;;BA)", true},
		{"administrators owner", "D:P(A;;FA;;;BA)", true},
		{"everyone read", "D:P(A;;FA;;;" + current + ")(A;;FR;;;WD)", false},
		{"users read", "D:P(A;;FA;;;" + current + ")(A;;FR;;;BU)", false},
		{"authenticated users read", "D:P(A;;FA;;;" + current + ")(A;;FR;;;AU)", false},
		{"other user read", "D:P(A;;FA;;;" + current + ")(A;;FR;;;S-1-5-21-123-456-789-1001)", false},
		{"deny is not a grant", "D:P(D;;FR;;;BU)(A;;FA;;;" + current + ")", true},
		{"inherit only does not grant file access", "D:P(A;OIIO;FR;;;WD)(A;;FA;;;" + current + ")", true},
		{"null DACL", "D:NO_ACCESS_CONTROL", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secret")
			if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
				t.Fatal(err)
			}
			// Restore the ACL before TempDir cleanup, including for deny/null cases.
			t.Cleanup(func() {
				if err := RestrictToOwner(path); err != nil {
					t.Error(err)
				}
			})
			sd, err := windows.SecurityDescriptorFromString(tc.dacl)
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
			if got, err := IsOwnerOnly(path); err != nil || got != tc.want {
				t.Fatalf("private = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestPrivateDACLDefaultInheritance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := IsOwnerOnly(path); err != nil || !ok {
		t.Fatalf("default inherited file private = %v, %v", ok, err)
	}
}

// Excalibur-created files retain the stricter protected user+SYSTEM ACL.
func assertProtectedOwnerDACL(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("created file DACL is not protected")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if acl == nil || acl.AceCount != 2 {
		t.Fatal("created file must grant access to exactly user and SYSTEM")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	seenUser, seenSystem := false, false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			t.Fatal("unexpected created file ACE")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		seenUser = seenUser || windows.EqualSid(sid, user.User.Sid)
		seenSystem = seenSystem || windows.EqualSid(sid, system)
	}
	if !seenUser || !seenSystem {
		t.Fatal("created file DACL must contain user and SYSTEM grants")
	}
}
