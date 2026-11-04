package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// WindowsPaths resolves per-user application directories (Section 3.4).
// Known folders support redirected profiles; environment variables are the
// fallback when the shell folder API is unavailable.
type WindowsPaths struct{}

var _ Paths = WindowsPaths{}

var knownFolderPath = windows.KnownFolderPath

func windowsFolder(id *windows.KNOWNFOLDERID, env string) (string, error) {
	if path, err := knownFolderPath(id, 0); err == nil && path != "" {
		return path, nil
	}
	if path := os.Getenv(env); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("platform: cannot resolve %s known folder", env)
}
func (WindowsPaths) DataDir() (string, error) {
	root, err := windowsFolder(windows.FOLDERID_LocalAppData, "LOCALAPPDATA")
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Excalibur"), nil
}
func (WindowsPaths) ConfigDir() (string, error) {
	root, err := windowsFolder(windows.FOLDERID_RoamingAppData, "APPDATA")
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Excalibur"), nil
}
func (p WindowsPaths) CacheDir() (string, error) {
	root, err := p.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "cache"), nil
}
func (p WindowsPaths) RuntimeDir() (string, error) {
	root, err := p.DataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := RestrictToOwner(dir); err != nil {
		return "", err
	}
	return dir, nil
}
func (p WindowsPaths) ExtensionsDir() []string {
	root, err := p.DataDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(root, "extensions")}
}
func (p WindowsPaths) EnsureDirs() error {
	for _, resolve := range []func() (string, error){p.DataDir, p.ConfigDir, p.CacheDir, p.RuntimeDir} {
		dir, err := resolve()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := RestrictToOwner(dir); err != nil {
			return err
		}
	}
	return nil
}
