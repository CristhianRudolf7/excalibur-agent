package attempts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry fingerprints one path of a working tree: its kind, its
// permission bits and its content hash (files) or target (links).
// Permission bits are part of the fingerprint so a build product that
// only gained the execute bit still counts as a change.
type Entry struct {
	Kind string      // "file", "dir" or "link"
	Mode fs.FileMode // permission bits
	Sum  string      // sha256 of the content (files) or the target (links)
}

// Manifest maps slash-free relative paths to their fingerprints.
type Manifest map[string]Entry

// Changes lists the differences between two manifests, sorted. Dirs
// carry the structure (new or removed directories) and are not counted
// in Files.
type Changes struct {
	Changed []string
	Added   []string
	Deleted []string
	// Files counts changed, added and deleted paths that are not
	// directories (the number the selection and the record report).
	Files int
}

// Total is the number of changed, added and deleted paths, dirs
// included.
func (c Changes) Total() int { return len(c.Changed) + len(c.Added) + len(c.Deleted) }

// Paths returns every changed, added and deleted path, sorted.
func (c Changes) Paths() []string {
	out := make([]string, 0, c.Total())
	out = append(out, c.Changed...)
	out = append(out, c.Added...)
	out = append(out, c.Deleted...)
	sort.Strings(out)
	return out
}

// Summary renders a capped one-line description for the trial record.
func (c Changes) Summary() string {
	if c.Total() == 0 {
		return "no changes"
	}
	names := c.Paths()
	if len(names) > 5 {
		names = append(names[:5], fmt.Sprintf("…+%d more", len(names)-5))
	}
	return fmt.Sprintf("%d changed, %d added, %d deleted: %s",
		len(c.Changed), len(c.Added), len(c.Deleted), strings.Join(names, ", "))
}

// isHarnessState reports whether a relative path sits under the
// attempt-local harness state directory.
func isHarnessState(rel string) bool {
	first := rel
	if i := strings.IndexRune(rel, filepath.Separator); i >= 0 {
		first = rel[:i]
	}
	return first == ".excalibur"
}

// isVersionControl reports whether a relative path sits under .git.
// Isolation never versions .git: copies keep it for the agent's own
// tools, but the winner's changes never include it.
func isVersionControl(rel string) bool {
	first := rel
	if i := strings.IndexRune(rel, filepath.Separator); i >= 0 {
		first = rel[:i]
	}
	return first == ".git"
}

// Scan fingerprints every file, directory and link under root, skipping
// .git and the harness state. Unreadable entries and special files
// (devices, sockets, fifos) are skipped, which matches CopyDir, so a
// baseline and a copy taken from the same tree always agree.
func Scan(root string) (Manifest, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("attempts: scan %q: %w", root, err)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("attempts: scan %q: %w", root, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("attempts: scan %q: not a directory", root)
	}
	out := Manifest{}
	err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == resolved {
				return err
			}
			return nil
		}
		rel, rerr := filepath.Rel(resolved, path)
		if rerr != nil || rel == "." {
			return nil
		}
		if isVersionControl(rel) || isHarnessState(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			out[rel] = Entry{Kind: "dir", Mode: mode.Perm()}
		case mode&fs.ModeSymlink != 0:
			if target, lerr := os.Readlink(path); lerr == nil {
				out[rel] = Entry{Kind: "link", Sum: target}
			}
		case mode.IsRegular():
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			sum := sha256.Sum256(data)
			out[rel] = Entry{Kind: "file", Mode: mode.Perm(), Sum: hex.EncodeToString(sum[:])}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("attempts: scan %q: %w", root, err)
	}
	return out, nil
}

// Diff compares base with cur. A path present in both with a different
// fingerprint is changed, a path only in cur is added, a path only in
// base is deleted.
func Diff(base, cur Manifest) Changes {
	var c Changes
	for rel, e := range cur {
		old, ok := base[rel]
		switch {
		case !ok:
			c.Added = append(c.Added, rel)
			if e.Kind != "dir" {
				c.Files++
			}
		case old != e:
			c.Changed = append(c.Changed, rel)
			if e.Kind != "dir" || old.Kind != "dir" {
				c.Files++
			}
		}
	}
	for rel, e := range base {
		if _, ok := cur[rel]; !ok {
			c.Deleted = append(c.Deleted, rel)
			if e.Kind != "dir" {
				c.Files++
			}
		}
	}
	sort.Strings(c.Changed)
	sort.Strings(c.Added)
	sort.Strings(c.Deleted)
	return c
}

// Touched reports what changed in the real directory since baseline,
// independently of any winner. It is the isolation audit: attempts
// never write there, so anything listed was written by a command that
// escaped the workdir mapping.
func Touched(baseline Manifest, realDir string) (Changes, error) {
	cur, err := Scan(realDir)
	if err != nil {
		return Changes{}, err
	}
	return Diff(baseline, cur), nil
}
