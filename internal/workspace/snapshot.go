package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Snapshot limits: shadow snapshots record command effects between
// exec steps (Section 13.7). Binaries and huge files are skipped; the
// manager records what it skipped so restores stay honest.
const (
	// SnapshotMaxFile is the largest single file kept in a shadow
	// snapshot (1 MiB, mirroring the tool output buffer).
	SnapshotMaxFile = 1 << 20
	// SnapshotMaxFiles caps the files per snapshot.
	SnapshotMaxFiles = 20000
)

// Snapshot is one shadow image of a project tree: path to blob ref
// for every tracked file, taken when a run starts using exec in a
// project and after every exec step (Section 13.7).
type Snapshot struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"project_id"`
	RunID     string            `json:"run_id,omitempty"`
	Step      int               `json:"step"`
	Files     map[string]string `json:"files"`
	Skipped   []string          `json:"skipped,omitempty"`
	At        time.Time         `json:"at"`
}

// SnapshotManager keeps the shadow store: a file-copy image per
// snapshot under the data dir plus content blobs, so command effects
// can be listed and undone without touching the project git. The
// store writes are synchronous at TakeSnapshot time, so killing the
// daemon right after still leaves a restorable image. See
// docs/decisions/0013-workspace-watcher-and-snapshots.md.
type SnapshotManager struct {
	mu      sync.Mutex
	root    string // <dataDir>/shadows
	hist    *History
	snaps   map[string]*Snapshot
	counter uint64
	now     func() time.Time
}

// NewSnapshotManager returns a manager rooted at dataDir.
func NewSnapshotManager(dataDir string, hist *History) *SnapshotManager {
	if hist == nil {
		hist = NewHistory()
	}
	return &SnapshotManager{
		root:  filepath.Join(dataDir, "shadows"),
		hist:  hist,
		snaps: map[string]*Snapshot{},
		now:   time.Now,
	}
}

// Take captures the current tree of projectRoot, skipping the
// watcher exclusions, .git directories, binaries (NUL in the first
// 8 KiB) and files over SnapshotMaxFile. It persists the manifest
// and new blobs under the data dir before returning.
func (s *SnapshotManager) Take(projectID, projectRoot, runID string, step int) (*Snapshot, error) {
	files := map[string]string{}
	var skipped []string
	count := 0
	err := filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(projectRoot, path)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		base := filepath.Base(path)
		for _, ex := range DefaultWatcherExclusions {
			if base == ex {
				if d.IsDir() {
					return filepath.SkipDir
				}
				skipped = append(skipped, rel)
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		st, serr := os.Stat(path)
		if serr != nil {
			return nil
		}
		if st.Size() > SnapshotMaxFile {
			skipped = append(skipped, rel+fmt.Sprintf(" (over %d bytes)", SnapshotMaxFile))
			return nil
		}
		if count >= SnapshotMaxFiles {
			return filepath.SkipAll
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			skipped = append(skipped, rel+" (unreadable)")
			return nil
		}
		if isBinaryHead(data) {
			skipped = append(skipped, rel+" (binary)")
			return nil
		}
		count++
		files[rel] = s.hist.PutBlob(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("workspace: snapshot walk: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	at := time.Now()
	if s.now != nil {
		at = s.now()
	}
	id := fmt.Sprintf("snap_%d_%d", at.UnixNano(), s.counter)
	snap := &Snapshot{ID: id, ProjectID: projectID, RunID: runID, Step: step, Files: files, Skipped: skipped, At: at}
	if err := s.persistLocked(snap); err != nil {
		return nil, err
	}
	s.snaps[id] = snap
	return snap, nil
}

// List returns the snapshots of a project newest first.
func (s *SnapshotManager) List(projectID string) []*Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Snapshot
	for _, snap := range s.snaps {
		if snap.ProjectID == projectID {
			cp := *snap
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// Get returns one snapshot by id.
func (s *SnapshotManager) Get(id string) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.snaps[id]
	if !ok {
		// Try the disk: the daemon may have restarted.
		if loaded, lerr := s.load(id); lerr == nil {
			s.snaps[id] = loaded
			cp := *loaded
			return &cp, nil
		}
		return nil, fmt.Errorf("workspace: unknown snapshot %q", id)
	}
	cp := *snap
	return &cp, nil
}

// Diff compares a snapshot with the current tree: changed, added and
// deleted relative paths, sorted.
func (s *SnapshotManager) Diff(projectRoot, snapshotID string) (changed, added, deleted []string, err error) {
	snap, err := s.Get(snapshotID)
	if err != nil {
		return nil, nil, nil, err
	}
	cur := map[string]string{}
	_ = filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(projectRoot, path)
		if rerr != nil || rel == "." {
			return nil
		}
		base := filepath.Base(path)
		for _, ex := range DefaultWatcherExclusions {
			if base == ex {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		st, serr := os.Stat(path)
		if serr != nil || st.Size() > SnapshotMaxFile {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil || isBinaryHead(data) {
			return nil
		}
		sum := sha256.Sum256(data)
		cur[rel] = "blob-sha256:" + hex.EncodeToString(sum[:])
		return nil
	})
	for rel, ref := range snap.Files {
		c, ok := cur[rel]
		switch {
		case !ok:
			deleted = append(deleted, rel)
		case c != ref:
			changed = append(changed, rel)
		}
	}
	for rel := range cur {
		if _, ok := snap.Files[rel]; !ok {
			added = append(added, rel)
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(deleted)
	return changed, added, deleted, nil
}

// RestoreOptions selects what Restore puts back.
type RestoreOptions struct {
	// Paths limits the restore to these relative paths; empty
	// restores everything, including re-creating deleted files and
	// removing files added after the snapshot when PruneAdded is
	// set.
	Paths []string
	// PruneAdded removes files created after the snapshot.
	PruneAdded bool
}

// Restore writes the snapshot content back into projectRoot and
// returns the restored relative paths, sorted.
func (s *SnapshotManager) Restore(projectRoot, snapshotID string, opts RestoreOptions) ([]string, error) {
	snap, err := s.Get(snapshotID)
	if err != nil {
		return nil, err
	}
	only := map[string]bool{}
	for _, p := range opts.Paths {
		only[filepath.Clean(p)] = true
	}
	var restored []string
	for rel, ref := range snap.Files {
		if len(only) > 0 && !only[filepath.Clean(rel)] {
			continue
		}
		data, ok := s.hist.Blob(ref)
		if !ok {
			return nil, fmt.Errorf("workspace: missing blob %s for %s", ref, rel)
		}
		dst := filepath.Join(projectRoot, rel)
		if err := atomicWrite(dst, data); err != nil {
			return nil, fmt.Errorf("workspace: restore %s: %w", rel, err)
		}
		restored = append(restored, rel)
	}
	if opts.PruneAdded {
		changed, added, _, derr := s.Diff(projectRoot, snapshotID)
		_ = changed
		if derr != nil {
			return nil, derr
		}
		for _, rel := range added {
			if len(only) > 0 && !only[filepath.Clean(rel)] {
				continue
			}
			_ = os.Remove(filepath.Join(projectRoot, rel))
			restored = append(restored, rel+" (removed)")
		}
	}
	sort.Strings(restored)
	return restored, nil
}

// persistLocked writes the manifest and any missing blobs. Callers
// hold s.mu.
func (s *SnapshotManager) persistLocked(snap *Snapshot) error {
	dir := filepath.Join(s.root, snap.ProjectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	blobDir := filepath.Join(s.root, "blobs")
	if err := os.MkdirAll(blobDir, 0o700); err != nil {
		return err
	}
	for _, ref := range snap.Files {
		name := strings.TrimPrefix(ref, "blob-sha256:")
		dst := filepath.Join(blobDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if data, ok := s.hist.Blob(ref); ok {
			_ = os.WriteFile(dst, data, 0o600)
		}
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, snap.ID+".json"), append(raw, '\n'), 0o644)
}

// load reads a persisted manifest plus blobs from disk.
func (s *SnapshotManager) load(id string) (*Snapshot, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.root, e.Name(), id+".json"))
		if err != nil {
			continue
		}
		var snap Snapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			continue
		}
		blobDir := filepath.Join(s.root, "blobs")
		for _, ref := range snap.Files {
			if _, ok := s.hist.Blob(ref); ok {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(blobDir, strings.TrimPrefix(ref, "blob-sha256:"))); err == nil {
				s.hist.PutBlob(data)
			}
		}
		return &snap, nil
	}
	return nil, fmt.Errorf("workspace: snapshot %q not on disk", id)
}

func isBinaryHead(data []byte) bool {
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}
	for _, b := range head {
		if b == 0 {
			return true
		}
	}
	return false
}
