package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalPathExistingAndFutureOutputs(t *testing.T) {
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "existing.txt"), []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "existing.txt", filepath.Join("future", "nested", "output.txt")} {
		want := filepath.Join(resolved, suffix)
		if runtime.GOOS == "windows" {
			want = strings.ToLower(want)
		}
		if got := CanonicalPath(filepath.Join(alias, suffix)); got != want {
			t.Errorf("canonical %q = %q, want %q", suffix, got, want)
		}
	}
	if got := CanonicalPath(""); got != "" {
		t.Fatalf("empty path = %q", got)
	}
}
