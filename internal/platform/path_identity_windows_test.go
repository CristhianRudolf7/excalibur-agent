package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCanonicalPathWindowsCaseAndAliases(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Mixed Case Directory")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	future := filepath.Join(dir, "Future", "Result.txt")
	want := CanonicalPath(future)
	for _, path := range []string{strings.ToUpper(future), filepath.ToSlash(future)} {
		if got := CanonicalPath(path); got != want {
			t.Fatalf("identity for %q = %q, want %q", path, got, want)
		}
	}
	t.Run("junction", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "alias")
		if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", alias, dir).CombinedOutput(); err != nil {
			t.Fatalf("create junction: %v: %s", err, output)
		}
		if got := CanonicalPath(filepath.Join(alias, "Future", "Result.txt")); got != want {
			t.Fatalf("junction identity = %q, want %q", got, want)
		}
	})
	t.Run("short name", func(t *testing.T) {
		input, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			t.Fatal(err)
		}
		buffer := make([]uint16, 32768)
		n, err := windows.GetShortPathName(input, &buffer[0], uint32(len(buffer)))
		if err != nil || n == 0 {
			t.Fatalf("GetShortPathName: %v", err)
		}
		short := windows.UTF16ToString(buffer[:n])
		if strings.EqualFold(short, dir) {
			t.Skip("8.3 name generation is disabled on the test volume")
		}
		if got := CanonicalPath(filepath.Join(short, "Future", "Result.txt")); got != want {
			t.Fatalf("short-name identity = %q, want %q", got, want)
		}
	})
}
