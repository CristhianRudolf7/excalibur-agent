package robotlab

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// runtimeSource includes robotlab/, pyproject.toml and requirements.lock when
// present. all: preserves Python __init__.py files; the root pattern excludes
// tests/, and extraction filters nested tests, dot directories and bytecode caches.
//
//go:embed all:runtime/[p-r]*
var runtimeSource embed.FS
var extractMu sync.Mutex

// ExtractRuntime atomically materializes the embedded runtime in a content-addressed
// directory. Existing versions are reused; obsolete completed versions are pruned.
func ExtractRuntime(dataDir string) (string, error) {
	extractMu.Lock()
	defer extractMu.Unlock()
	files := map[string][]byte{}
	hash := sha256.New()
	err := fs.WalkDir(runtimeSource, "runtime", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "tests" || d.Name() == "__pycache__" || strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".pyc") || strings.HasSuffix(name, ".pyo") {
			return nil
		}
		b, err := runtimeSource.ReadFile(name)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(name, "runtime/")
		files[rel] = b
		hash.Write([]byte(rel))
		hash.Write([]byte{0})
		hash.Write(b)
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	root := filepath.Join(dataDir, "robotlab", "src")
	dest := filepath.Join(root, hex.EncodeToString(hash.Sum(nil)))
	if st, err := os.Stat(dest); err == nil && st.IsDir() {
		return dest, nil
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".extract-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for name, b := range files {
		target := filepath.Join(tmp, filepath.FromSlash(path.Clean(name)))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, b, 0600); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		if st, e := os.Stat(dest); e != nil || !st.IsDir() {
			return "", err
		}
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) == 64 && e.Name() != filepath.Base(dest) {
			if _, err := hex.DecodeString(e.Name()); err == nil {
				_ = os.RemoveAll(filepath.Join(root, e.Name()))
			}
		}
	}
	return dest, nil
}
