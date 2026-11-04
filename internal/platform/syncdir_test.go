package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "entry"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SyncDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := SyncDir(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing directory: %v", err)
	}
}
