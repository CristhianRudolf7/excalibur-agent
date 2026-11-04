package platform

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsPaths(t *testing.T) {
	local, roaming := filepath.Join(t.TempDir(), "local"), filepath.Join(t.TempDir(), "roaming")
	old := knownFolderPath
	knownFolderPath = func(id *windows.KNOWNFOLDERID, flags uint32) (string, error) {
		if id == windows.FOLDERID_LocalAppData {
			return local, nil
		}
		return roaming, nil
	}
	t.Cleanup(func() { knownFolderPath = old })
	p := WindowsPaths{}
	for _, tc := range []struct {
		resolve func() (string, error)
		want    string
	}{
		{p.DataDir, filepath.Join(local, "Excalibur")},
		{p.ConfigDir, filepath.Join(roaming, "Excalibur")},
		{p.CacheDir, filepath.Join(local, "Excalibur", "cache")},
		{p.RuntimeDir, filepath.Join(local, "Excalibur", "run")},
	} {
		got, err := tc.resolve()
		if err != nil || got != tc.want {
			t.Fatalf("directory = %q, %v; want %q", got, err, tc.want)
		}
	}
	dir, err := p.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := IsOwnerOnly(dir); err != nil || !ok {
		t.Fatalf("runtime private = %v, %v", ok, err)
	}
	if got := p.ExtensionsDir(); len(got) != 1 || got[0] != filepath.Join(local, "Excalibur", "extensions") {
		t.Fatalf("extensions = %v", got)
	}
}

func TestKnownFolderEnvironmentFallback(t *testing.T) {
	old := knownFolderPath
	knownFolderPath = func(*windows.KNOWNFOLDERID, uint32) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { knownFolderPath = old })
	t.Setenv("LOCALAPPDATA", t.TempDir())
	got, err := (WindowsPaths{}).DataDir()
	if err != nil || got != filepath.Join(os.Getenv("LOCALAPPDATA"), "Excalibur") {
		t.Fatalf("environment fallback = %q, %v", got, err)
	}
	t.Setenv("LOCALAPPDATA", "")
	if _, err := (WindowsPaths{}).DataDir(); err == nil {
		t.Fatal("missing known folder and environment must fail")
	}
}
