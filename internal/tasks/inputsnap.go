// Provided-input snapshot (agent quality, work package D, D4a).
//
// Before the first model action the run records what its workdir
// already holds: path, size, modification time and, while the total
// stays within InputSnapshotHashBudget, a SHA-256 of the content. At
// finish the snapshot answers which provided files changed or vanished.
// Such changes are soft warnings unless the instruction asked for them,
// so an agent that edited or deleted supplied data sees it before the
// run ends. Optional private byte copies support restoration; build and
// cache directories and symlinks are skipped.
package tasks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// InputSnapshotHashBudget bounds metadata hashing at run start (50 MB).
// Files beyond it use size and mtime unless preserved copies supply a hash
// within the separate InputSnapshotCopyBudget.
const InputSnapshotHashBudget int64 = 50 << 20

// InputSnapshotCopyBudget bounds the whole eligible tree copied at run start.
const InputSnapshotCopyBudget int64 = 64 << 20

// inputSnapshotMaxFiles bounds the walk so a huge tree cannot stall the
// first model call.
const inputSnapshotMaxFiles = 20000

// snapshotSkipDirs are build, dependency and cache directories.
var snapshotSkipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".cache": true, "__pycache__": true,
	".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true, ".tox": true,
	"node_modules": true, "build": true, "dist": true, "target": true,
	"venv": true, ".venv": true, ".gradle": true, ".next": true,
}

// InputEntry is one provided file at run start.
type InputEntry struct {
	Mode    fs.FileMode
	Path    string
	Size    int64
	ModTime time.Time
	// SHA256 is empty when both metadata hashing and byte copying were skipped.
	SHA256 string
	// OriginalCopy is a private byte copy, empty when copying was skipped.
	OriginalCopy string
}

// InputSnapshot is the run-start view of the provided files.
type InputSnapshot struct {
	Root    string
	Entries map[string]InputEntry
	// Hashed counts the bytes hashed at snapshot time.
	Hashed int64
}

// InputChange reports one provided file that no longer matches.
type InputChange struct {
	Path string
	// Kind is "modified" or "vanished".
	Kind string
}

// TakeInputSnapshot records the files under root. A missing or empty
// root, or the filesystem root itself, yields no snapshot (nil).
func TakeInputSnapshot(root string, hashBudget int64) (*InputSnapshot, error) {
	return TakeInputSnapshotWithCopies(root, hashBudget, "")
}

// TakeInputSnapshotWithCopies also preserves eligible input bytes in copyDir.
// A tree above 64 MB or the file-count bound gets metadata only. copyDir must
// be a run-private destination; an empty destination disables byte copying.
func TakeInputSnapshotWithCopies(root string, hashBudget int64, copyDir string) (*InputSnapshot, error) {
	if snapshotFilesystemRoot(root) {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	s := &InputSnapshot{Root: root, Entries: map[string]InputEntry{}}
	var paths []string
	complete := true
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (snapshotSkipDirs[d.Name()] || withinSnapshotDir(p, copyDir)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		paths = append(paths, p)
		if len(paths) >= inputSnapshotMaxFiles {
			complete = false
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(paths)
	var hashed int64
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		entry := InputEntry{Mode: fi.Mode().Perm(), Path: p, Size: fi.Size(), ModTime: fi.ModTime()}
		if hashed+fi.Size() <= hashBudget {
			sum, err := fileSHA256(p)
			if err == nil {
				entry.SHA256 = sum
				hashed += fi.Size()
			}
		}
		s.Entries[p] = entry
	}
	s.Hashed = hashed
	if copyDir != "" && complete {
		s.preserveCopies(copyDir)
	}
	return s, nil
}

// Has reports whether path was provided at run start.
func (s *InputSnapshot) Has(path string) bool {
	if s == nil {
		return false
	}
	_, ok := s.Entries[filepath.Clean(path)]
	return ok
}

// Changes returns the provided files that were modified or vanished,
// in path order. Files for which exempt returns true are skipped (the
// instruction asked to change them).
func (s *InputSnapshot) Changes(exempt func(path string) bool) []InputChange {
	if s == nil {
		return nil
	}
	paths := make([]string, 0, len(s.Entries))
	for p := range s.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []InputChange
	for _, p := range paths {
		if exempt != nil && exempt(p) {
			continue
		}
		old := s.Entries[p]
		fi, err := os.Stat(p)
		if err != nil {
			out = append(out, InputChange{Path: p, Kind: "vanished"})
			continue
		}
		if fi.Size() != old.Size {
			out = append(out, InputChange{Path: p, Kind: "modified"})
			continue
		}
		if old.SHA256 == "" {
			if !fi.ModTime().Equal(old.ModTime) {
				out = append(out, InputChange{Path: p, Kind: "modified"})
			}
			continue
		}
		if sum, err := fileSHA256(p); err == nil && sum != old.SHA256 {
			out = append(out, InputChange{Path: p, Kind: "modified"})
		}
	}
	return out
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// withinSnapshotDir prevents recursively capturing an existing copy destination.
func withinSnapshotDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

func (s *InputSnapshot) preserveCopies(dir string) {
	var total int64
	for _, entry := range s.Entries {
		if entry.Size < 0 || entry.Size > InputSnapshotCopyBudget-total {
			return
		}
		total += entry.Size
	}
	// OpenRoot confines reads even if a file or parent becomes a symlink
	// between the metadata walk and the copy.
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return
	}
	defer root.Close()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return
	}
	var copied int64
	for _, path := range sortedInputPaths(s.Entries) {
		entry := s.Entries[path]
		rel, err := filepath.Rel(s.Root, path)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		dest := filepath.Join(dir, rel)
		n, err := copyInput(root, rel, dest, InputSnapshotCopyBudget-copied)
		copied += n
		if err == nil {
			sum, hashErr := fileSHA256(dest)
			if hashErr != nil || (entry.SHA256 != "" && entry.SHA256 != sum) {
				_ = os.Remove(dest)
				continue
			}
			entry.SHA256 = sum
			entry.OriginalCopy = dest
			s.Entries[path] = entry
		}
	}
}

func sortedInputPaths(entries map[string]InputEntry) []string {
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func copyInput(root *os.Root, rel, dest string, remaining int64) (int64, error) {
	src, err := root.Open(rel)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > remaining {
		return 0, fs.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return 0, err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(out, io.LimitReader(src, remaining+1))
	closeErr := out.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil && n > remaining {
		copyErr = fs.ErrInvalid
	}
	if copyErr != nil {
		_ = os.Remove(dest)
	}
	return n, copyErr
}

// Restore copies only a manifest entry into the task tree after checking its hash.
// OpenRoot confines destination symlinks as well as lexical path traversal.
func (s *InputSnapshot) Restore(path, destination string) error {
	if s == nil {
		return fmt.Errorf("no input snapshot available")
	}
	resolve := func(p string) (string, error) {
		for _, part := range strings.Split(filepath.ToSlash(p), "/") {
			if part == ".." {
				return "", fmt.Errorf("path traversal refused")
			}
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(s.Root, p)
		}
		rel, err := filepath.Rel(s.Root, p)
		if err != nil || !filepath.IsLocal(rel) {
			return "", fmt.Errorf("path must be inside the task tree")
		}
		return rel, nil
	}
	rel, err := resolve(path)
	if err != nil {
		return err
	}
	entry, ok := s.Entries[filepath.Join(s.Root, rel)]
	if !ok || entry.OriginalCopy == "" || entry.SHA256 == "" {
		return fmt.Errorf("path has no preserved manifest copy")
	}
	if destination == "" {
		destination = path
	}
	dest, err := resolve(destination)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(entry.OriginalCopy)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != entry.SHA256 {
		return fmt.Errorf("preserved input hash mismatch")
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	// Create a verified sibling and rename: never truncate a linked input or a
	// candidate until all preserved bytes have passed integrity checking.
	if err := root.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	name := dest + ".restore-" + hex.EncodeToString(sum[:8])
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, entry.Mode)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	_, err = io.Copy(out, bytes.NewReader(data))
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(name, dest)
}
