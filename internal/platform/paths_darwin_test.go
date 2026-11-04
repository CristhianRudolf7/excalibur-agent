//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDarwinPathsXDG(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "run"))

	p := DarwinPaths{}
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

func TestDarwinPathsEnsureDirs(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "run"))

	p := DarwinPaths{}
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

func TestDarwinPathsFallbacks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(key, "")
	}
	t.Setenv("TMPDIR", filepath.Join(home, "tmp"))
	p := DarwinPaths{}
	for _, tc := range []struct {
		fn   func() (string, error)
		want string
	}{
		{p.DataDir, filepath.Join(home, ".local", "share", "excalibur")},
		{p.ConfigDir, filepath.Join(home, ".config", "excalibur")},
		{p.CacheDir, filepath.Join(home, ".cache", "excalibur")},
		{p.RuntimeDir, filepath.Join(os.TempDir(), "excalibur-"+strconv.Itoa(os.Getuid()))},
	} {
		if got, err := tc.fn(); got != tc.want || err != nil {
			t.Fatalf("path = %q, %v; want %q", got, err, tc.want)
		}
	}
}
