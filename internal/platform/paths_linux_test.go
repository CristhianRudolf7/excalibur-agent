//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxPathsXDG(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "run"))

	p := LinuxPaths{}
	data, err := p.DataDir()
	if err != nil || data != filepath.Join(base, "data", "excalibur") {
		t.Fatalf("DataDir = %q, %v", data, err)
	}
	cfg, err := p.ConfigDir()
	if err != nil || cfg != filepath.Join(base, "cfg", "excalibur") {
		t.Fatalf("ConfigDir = %q, %v", cfg, err)
	}
	cache, err := p.CacheDir()
	if err != nil || cache != filepath.Join(base, "cache", "excalibur") {
		t.Fatalf("CacheDir = %q, %v", cache, err)
	}
	rt, err := p.RuntimeDir()
	if err != nil || rt != filepath.Join(base, "run") {
		t.Fatalf("RuntimeDir = %q, %v", rt, err)
	}
	ext := p.ExtensionsDir()
	if len(ext) != 1 || !strings.HasSuffix(ext[0], "extensions") {
		t.Fatalf("ExtensionsDir = %q", ext)
	}
}

func TestLinuxPathsEnsureDirs(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "run"))

	p := LinuxPaths{}
	if err := p.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, fn := range []func() (string, error){p.DataDir, p.ConfigDir, p.CacheDir, p.RuntimeDir} {
		d, _ := fn()
		fi, err := os.Stat(d)
		if err != nil || !fi.IsDir() {
			t.Fatalf("missing dir %s: %v", d, err)
		}
		if fi.Mode().Perm()&0o700 != 0o700 {
			t.Fatalf("dir %s mode %o, want 0700", d, fi.Mode().Perm())
		}
	}
}
