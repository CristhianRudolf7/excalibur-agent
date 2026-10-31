package codeintel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// Indexing budgets and debounce delays.
const (
	// DefaultMaxParseBytes caps single-file parsing (tree-sitter
	// memory stays bounded on generated files).
	DefaultMaxParseBytes = 2 << 20
	// watchDebounce coalesces editor save bursts into one reindex.
	watchDebounce = 200 * time.Millisecond
)

// heavyDirs are never indexed: VCS metadata, dependency trees and
// build outputs (the files.list contract also collapses them).
var heavyDirs = map[string]bool{
	".git": true, "node_modules": true, "target": true,
	".excalibur": true, "__pycache__": true, ".venv": true, "venv": true,
	"dist": true, "build": true, ".idea": true, ".vscode": true,
}

// IndexReport summarizes one indexing pass.
type IndexReport struct {
	Scanned int
	Updated int
	Removed int
	Skipped int
}

// Indexer builds and refreshes the project graph. Indexing is
// incremental by content hash (Section 10.10): unchanged files are
// skipped, edited files are replaced wholesale, deleted files are
// dropped. All three triggers feed the same path: IndexAll for full
// scans, NotifyChanged after each agent edit, Watch for the file
// watcher.
type Indexer struct {
	Graph    *Graph
	Root     string
	MaxBytes int64
}

// NewIndexer returns an Indexer for the project rooted at root.
func NewIndexer(g *Graph, root string) *Indexer {
	return &Indexer{Graph: g, Root: root, MaxBytes: DefaultMaxParseBytes}
}

func (x *Indexer) maxBytes() int64 {
	if x.MaxBytes <= 0 {
		return DefaultMaxParseBytes
	}
	return x.MaxBytes
}

// IndexAll scans the whole project: new and changed files are
// parsed and upserted, files missing from disk are removed.
func (x *Indexer) IndexAll(ctx context.Context) (IndexReport, error) {
	var rep IndexReport
	seen := map[string]bool{}
	ign := loadIgnoreFile(x.Root)
	err := filepath.WalkDir(x.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(x.Root, p)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			base := filepath.Base(p)
			if heavyDirs[base] || (ign != nil && ign.ignored(rel, true)) {
				return filepath.SkipDir
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, ok := DetectLanguage(rel); !ok {
			return nil
		}
		if ign != nil && ign.ignored(rel, false) {
			rep.Skipped++
			return nil
		}
		rep.Scanned++
		seen[rel] = true
		updated, err := x.indexOne(rel)
		if err != nil {
			// One broken file never stops the pass; the error is
			// recorded on the report path via skipped count.
			rep.Skipped++
			return nil
		}
		if updated {
			rep.Updated++
		}
		return nil
	})
	if err != nil {
		return rep, err
	}
	for _, rel := range x.Graph.IndexedFiles() {
		if !seen[rel] {
			if err := ctx.Err(); err != nil {
				return rep, err
			}
			if err := x.Graph.RemoveFile(rel); err != nil {
				return rep, err
			}
			rep.Removed++
		}
	}
	return rep, nil
}

// NotifyChanged reindexes the given project-relative paths after an
// agent edit or a watcher event. Missing files are dropped.
func (x *Indexer) NotifyChanged(ctx context.Context, rels []string) (IndexReport, error) {
	var rep IndexReport
	for _, rel := range rels {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		abs := filepath.Join(x.Root, rel)
		if _, err := os.Stat(abs); err != nil {
			if removeErr := x.Graph.RemoveFile(rel); removeErr != nil {
				return rep, removeErr
			}
			rep.Removed++
			continue
		}
		if _, ok := DetectLanguage(rel); !ok {
			continue
		}
		rep.Scanned++
		updated, err := x.indexOne(rel)
		if err != nil {
			rep.Skipped++
			continue
		}
		if updated {
			rep.Updated++
		}
	}
	return rep, nil
}

// indexOne parses rel when its content hash changed. It reports
// whether the graph was updated.
func (x *Indexer) indexOne(rel string) (bool, error) {
	abs := filepath.Join(x.Root, rel)
	info, err := os.Stat(abs)
	if err != nil {
		return false, err
	}
	if info.Size() > x.maxBytes() {
		return false, nil
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return false, err
	}
	if isBinaryContent(src) {
		return false, nil
	}
	sum := hashBytes(src)
	if x.Graph.FileHash(rel) == sum {
		return false, nil
	}
	upd, err := ParseFile(rel, src)
	if err != nil {
		return false, err
	}
	if upd.Lang == "go" {
		rewriteGoModuleIDs(&upd, x.goModulePrefix())
	}
	if err := x.Graph.UpsertFile(upd); err != nil {
		return false, err
	}
	return true, nil
}

// goModulePrefix reads the module path from the project go.mod, if
// any. First-party Go imports resolve against it to directory
// modules, so dependents and dependencies stay inside the project
// graph instead of dangling on raw import paths.
func (x *Indexer) goModulePrefix() string {
	data, err := os.ReadFile(filepath.Join(x.Root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				return fields[1]
			}
		}
	}
	return ""
}

// rewriteGoModuleIDs maps first-party Go import modules
// (mod:go:<prefix>/...) to directory modules
// (mod:dir:go:<reldir>), in nodes and edges alike.
func rewriteGoModuleIDs(upd *FileUpdate, prefix string) {
	if prefix == "" {
		return
	}
	rename := map[string]string{}
	for _, n := range upd.Nodes {
		if n.Kind != NodeModule {
			continue
		}
		raw, ok := strings.CutPrefix(n.ID, "mod:go:")
		if !ok {
			continue
		}
		var dir string
		switch {
		case raw == prefix:
			dir = ""
		case strings.HasPrefix(raw, prefix+"/"):
			dir = strings.TrimPrefix(raw, prefix+"/")
		default:
			continue
		}
		rename[n.ID] = ModuleID("dir:go:" + dir)
	}
	if len(rename) == 0 {
		return
	}
	for i, n := range upd.Nodes {
		if dst, ok := rename[n.ID]; ok {
			upd.Nodes[i].ID = dst
			upd.Nodes[i].Name = n.Name
			if upd.Nodes[i].Path == "" {
				upd.Nodes[i].Path = renameDir(rename[n.ID])
			}
		}
	}
	for i, e := range upd.Edges {
		if dst, ok := rename[e.Dst]; ok {
			upd.Edges[i].Dst = dst
		}
	}
}

func renameDir(modID string) string { return moduleDir(modID) }

// hashBytes returns the content sha256 hex driving incremental updates.
func hashBytes(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}

func isBinaryContent(src []byte) bool {
	head := src
	if len(head) > 512 {
		head = head[:512]
	}
	for _, c := range head {
		if c == 0 {
			return true
		}
	}
	return false
}

// Watch follows the project tree and reindexes changed files until
// ctx ends. Events debounce into one NotifyChanged per burst; new
// subdirectories join the watch. The goroutine runs under safego.Go
// (Section 3.2): a panic marks the watcher failed, never the daemon.
func (x *Indexer) Watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("codeintel: watcher: %w", err)
	}
	if err := addRecursive(w, x.Root); err != nil {
		_ = w.Close()
		return err
	}
	done := make(chan struct{})
	safego.Go("codeintel/watch", nil, func() {
		defer close(done)
		defer w.Close()
		pending := map[string]bool{}
		var timer <-chan time.Time
		flush := func() {
			if len(pending) == 0 {
				return
			}
			rels := make([]string, 0, len(pending))
			for rel := range pending {
				rels = append(rels, rel)
			}
			sort.Strings(rels)
			pending = map[string]bool{}
			_, _ = x.NotifyChanged(context.Background(), rels)
		}
		for {
			select {
			case <-ctx.Done():
				flush()
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				rel, err := filepath.Rel(x.Root, ev.Name)
				if err != nil {
					continue
				}
				rel = filepath.ToSlash(rel)
				if rel == "." || rel == "" {
					continue
				}
				if ev.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
						_ = addRecursive(w, ev.Name)
					}
				}
				if heavyPath(rel) {
					continue
				}
				pending[rel] = true
				t := time.After(watchDebounce)
				timer = t
			case <-timer:
				flush()
				timer = nil
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				_ = err // watcher errors are best-effort; next events retry
			}
		}
	})
	<-done
	return ctx.Err()
}

func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if rel != "." && heavyPath(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		// Watcher.Add on an already-watched path is a no-op error
		// the watcher tolerates; ignore duplicates.
		_ = w.Add(p)
		return nil
	})
}

func heavyPath(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if heavyDirs[seg] {
			return true
		}
	}
	return false
}

// ignoreFile is a minimal .gitignore matcher for the indexer walk:
// *, ?, **, directory prefixes and negations, scoped to the project
// root .gitignore. Deeper files reuse the same rules by relative path.
type ignoreFile struct {
	patterns []ignoreRule
}

type ignoreRule struct {
	pat string
	neg bool
	dir bool
}

func loadIgnoreFile(root string) *ignoreFile {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil
	}
	ig := &ignoreFile{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := false
		if strings.HasPrefix(line, "!") {
			neg = true
			line = line[1:]
		}
		dir := strings.HasSuffix(line, "/")
		line = strings.Trim(strings.TrimSuffix(line, "/"), " ")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		ig.patterns = append(ig.patterns, ignoreRule{pat: line, neg: neg, dir: dir})
	}
	if len(ig.patterns) == 0 {
		return nil
	}
	return ig
}

func (ig *ignoreFile) ignored(rel string, isDir bool) bool {
	ignored := false
	relSlash := filepath.ToSlash(rel)
	for _, p := range ig.patterns {
		match := false
		if strings.Contains(p.pat, "/") {
			ok, _ := matchSegments(strings.Split(p.pat, "/"), strings.Split(relSlash, "/"))
			match = ok
		} else {
			for _, seg := range strings.Split(relSlash, "/") {
				ok, _ := filepath.Match(p.pat, seg)
				if ok {
					match = true
					break
				}
			}
		}
		if match {
			if p.dir && !isDir {
				match = true // dir-only pattern still ignores paths beneath it
			}
			if p.neg {
				ignored = false
			} else {
				ignored = true
			}
		}
	}
	return ignored
}

func matchSegments(pat, name []string) (bool, error) {
	if len(pat) == 0 {
		// Consumed pattern matches the path and everything
		// beneath it (directory semantics of .gitignore).
		return true, nil
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			ok, err := matchSegments(pat[1:], name[i:])
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	}
	if len(name) == 0 {
		return false, nil
	}
	ok, err := filepath.Match(pat[0], name[0])
	if err != nil || !ok {
		return false, err
	}
	return matchSegments(pat[1:], name[1:])
}
