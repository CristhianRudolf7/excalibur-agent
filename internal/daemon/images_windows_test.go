package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsImageSwapRollback(t *testing.T) {
	s := FileImageStore{Dir: t.TempDir()}
	cur, next, old := s.binaryPaths()
	if filepath.Base(cur) != "excalibur.exe" || old != cur+".old" {
		t.Fatal("incorrect Windows image names")
	}
	if err := os.WriteFile(cur, []byte("previous"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage([]byte("candidate")); err != nil {
		t.Fatal(err)
	}
	if err := s.Swap(); err != nil {
		t.Fatal(err)
	}
	assertImage := func(path, want string) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil || string(b) != want {
			t.Fatalf("image %s = %q, %v", path, b, err)
		}
	}
	assertImage(cur, "candidate")
	assertImage(old, "previous")
	if _, err := os.Stat(next); !os.IsNotExist(err) {
		t.Fatal("staged image still present")
	}
	if err := s.RollbackBinary(); err != nil {
		t.Fatal(err)
	}
	assertImage(cur, "previous")
	assertImage(cur+".bad", "candidate")
	// Missing candidates cannot move the current executable away.
	if err := s.Swap(); err == nil {
		t.Fatal("swap without candidate succeeded")
	}
	assertImage(cur, "previous")
}
