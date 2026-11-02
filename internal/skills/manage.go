package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var ErrNotFound = errors.New("skill is not indexed")
var ErrBuiltinDelete = errors.New("builtin skills can be disabled, not deleted")

// ValidationError holds the same diagnostics used by Scan.
type ValidationError struct{ Diagnostics []Diagnostic }

func (e *ValidationError) Error() string { return e.Diagnostics[0].Error() }

// Content returns the complete source file, including its frontmatter.
func (ix *Index) Content(name string) (string, error) {
	e, ok := ix.Get(name)
	if !ok {
		return "", ErrNotFound
	}
	raw, err := ix.readCandidate(candidate{root: e.Root, dir: e.Path, name: e.Name})
	return string(raw), err
}

// Editable reports whether the source is writable, including builtin overrides.
func (ix *Index) Editable(e *Entry) bool { return e.Root != RootBuiltin || ix.roots.User != "" }

// Update validates before touching disk, backs up the old content, atomically
// replaces SKILL.md, refreshes installed pins and rescans the running index.
func (ix *Index) Update(name, content string) (*Entry, error) {
	ix.mutationMu.Lock()
	defer ix.mutationMu.Unlock()
	e, ok := ix.Get(name)
	if !ok {
		return nil, ErrNotFound
	}
	sf, err := ParseSkillFile([]byte(content))
	if err != nil {
		return nil, &ValidationError{[]Diagnostic{{Skill: name, Code: CodeBadFrontmatter, Detail: err.Error()}}}
	}
	if diags := Validate(name, e.Path, sf, groupOK, ix.toolOK); len(diags) > 0 {
		return nil, &ValidationError{diags}
	}
	dest := e.Path
	if e.Root == RootBuiltin {
		if ix.roots.User == "" {
			return nil, fmt.Errorf("skills: no user root configured for builtin override")
		}
		dest = filepath.Join(ix.roots.User, name)
	}
	raw, err := ix.Content(name)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	backupRaw := []byte(raw)
	if previous, err := os.ReadFile(filepath.Join(dest, "SKILL.md")); err == nil {
		backupRaw = previous
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := backupContent(dest, backupRaw); err != nil {
		return nil, err
	}
	// Overrides retain the builtin's referenced assets and scripts.
	if e.Root == RootBuiltin {
		if err := copyEmbedded(ix.builtin, e.Path, dest); err != nil {
			return nil, err
		}
	}
	if err := atomicContent(filepath.Join(dest, "SKILL.md"), []byte(content)); err != nil {
		return nil, err
	}
	if e.Root == RootInstalled {
		if err := writeRecord(ix.roots.Installed, InstallRecord{Name: name, Version: sf.Frontmatter.Metadata.Excalibur.Version, Hash: hashContent([]byte(content)), UpdatedAt: time.Now().UTC()}); err != nil {
			_ = atomicContent(filepath.Join(dest, "SKILL.md"), []byte(raw))
			_ = ix.scan()
			return nil, err
		}
	}
	if err := ix.scan(); err != nil {
		return nil, err
	}
	updated, ok := ix.Get(name)
	if !ok {
		return nil, ErrNotFound
	}
	return updated, nil
}

func backupContent(dir string, raw []byte) error {
	f, err := os.CreateTemp(dir, "SKILL.md.backup-"+time.Now().UTC().Format("20060102T150405.000000000Z")+"-*")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err != nil {
		return err
	}
	return cerr
}

func atomicContent(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".skill-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func copyEmbedded(fsys fs.FS, src, dst string) error {
	return fs.WalkDir(fsys, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if rel == "SKILL.md" {
			return nil
		}
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return atomicContent(target, raw)
	})
}

// Delete atomically moves the whole writable skill into a timestamped backup
// directory outside discovery. A user override reveals the builtin on rescan.
func (ix *Index) Delete(name string) error {
	ix.mutationMu.Lock()
	defer ix.mutationMu.Unlock()
	e, ok := ix.Get(name)
	if !ok {
		return ErrNotFound
	}
	if e.Root == RootBuiltin {
		return ErrBuiltinDelete
	}
	backupRoot := filepath.Join(filepath.Dir(e.Path), ".backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(backupRoot, name+"-"+time.Now().UTC().Format("20060102T150405.000000000Z")+"-*")
	if err != nil {
		return err
	}
	if err := os.Rename(e.Path, filepath.Join(backup, name)); err != nil {
		return err
	}
	if e.Root == RootInstalled {
		records := readAllRecords(ix.roots.Installed)
		delete(records, name)
		list := make([]InstallRecord, 0, len(records))
		for _, r := range records {
			list = append(list, r)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		data, err := json.MarshalIndent(list, "", "  ")
		if err == nil {
			err = atomicContent(recordsPath(ix.roots.Installed), append(data, '\n'))
		}
		if err != nil {
			_ = os.Rename(filepath.Join(backup, name), e.Path)
			return err
		}
	}
	ix.mu.Lock()
	delete(ix.stateOverrides, name)
	ix.mu.Unlock()
	return ix.scan()
}
