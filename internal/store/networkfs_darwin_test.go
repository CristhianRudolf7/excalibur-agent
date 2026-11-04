package store

import "testing"

func TestDarwinNetworkFSTypes(t *testing.T) {
	for name, want := range map[string]bool{
		"apfs": false, "hfs": false, "devfs": false, "tmpfs": false,
		"nfs": true, "smbfs": true, "afpfs": true, "webdav": true, "macfuse": true, "SMBFS": true,
	} {
		if got := isDarwinNetworkFSType(name); got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
}

func TestDarwinLocalTempDirIsNotNetworkFS(t *testing.T) {
	if detectPlatformNetworkFS(t.TempDir()) {
		t.Fatal("local APFS temp dir reported as network filesystem")
	}
	if detectPlatformNetworkFS(t.TempDir() + "/missing/child") {
		t.Fatal("missing child of a local dir reported as network filesystem")
	}
}
