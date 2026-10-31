//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strconv"
)

// LinuxPaths resolves the per-user XDG directories used by the harness
// (Section 3.4 table). Every method re-reads the environment so tests
// can override locations with t.Setenv.
type LinuxPaths struct{}

// Compile-time interface check.
var _ Paths = LinuxPaths{}

// dataHome returns $XDG_DATA_HOME or ~/.local/share.
func dataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".local", "share")
}

// configHome returns $XDG_CONFIG_HOME or ~/.config.
func configHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".config")
}

// cacheHome returns $XDG_CACHE_HOME or ~/.cache.
func cacheHome() string {
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".cache")
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "/root"
}

// DataDir implements platform.Paths: the per-user data root
// (<xdg-data>/excalibur) holding excalibur.db, vault.db,
// telemetry.db, projects/ and blobs/.
func (LinuxPaths) DataDir() (string, error) {
	return filepath.Join(dataHome(), "excalibur"), nil
}

// ConfigDir implements platform.Paths: the per-user configuration root
// (<xdg-config>/excalibur).
func (LinuxPaths) ConfigDir() (string, error) {
	return filepath.Join(configHome(), "excalibur"), nil
}

// CacheDir implements platform.Paths: the per-user cache root
// (<xdg-cache>/excalibur).
func (LinuxPaths) CacheDir() (string, error) {
	return filepath.Join(cacheHome(), "excalibur"), nil
}

// RuntimeDir implements platform.Paths: the per-user runtime root.
// It prefers $XDG_RUNTIME_DIR (memory-backed, used for vault SSH/agent
// temporaries per Section 20.1), then /run/user/<uid>, and finally a
// private directory under os.TempDir as a last resort.
func (LinuxPaths) RuntimeDir() (string, error) {
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		return v, nil
	}
	uid := os.Getuid()
	if dir := "/run/user/" + strconv.Itoa(uid); dirExists(dir) {
		return dir, nil
	}
	return filepath.Join(os.TempDir(), "excalibur-"+strconv.Itoa(uid)), nil
}

// ExtensionsDir implements platform.Paths: user extension directories
// in lookup order (Section 14.12 discovery).
func (p LinuxPaths) ExtensionsDir() []string {
	data, _ := p.DataDir()
	return []string{filepath.Join(data, "extensions")}
}

// EnsureDirs creates the data, config, cache and runtime directories
// with mode 0700. It is used by the installer (Section 25.2).
func (p LinuxPaths) EnsureDirs() error {
	dirs := []string{}
	for _, fn := range []func() (string, error){
		p.DataDir, p.ConfigDir, p.CacheDir, p.RuntimeDir,
	} {
		d, err := fn()
		if err != nil {
			return err
		}
		dirs = append(dirs, d)
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
