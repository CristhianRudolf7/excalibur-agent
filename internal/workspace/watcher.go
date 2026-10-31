package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// DefaultWatcherExclusions are always skipped (Section 16.5), plus
// every .gitignore rule of the project.
var DefaultWatcherExclusions = []string{".git", "node_modules", "target"}

// WatchEvent is one external change detected by the watcher.
type WatchEvent struct {
	Path string
	Kind string // created, modified, deleted
	At   time.Time
}

// WatcherHooks are the Section 16.5 reactions to an external change:
// invalidate read fingerprints, mark evidence stale, update the code
// graph, and send an informative directive to the lease holder.
type WatcherHooks struct {
	// FingerprintInvalidate drops the read fingerprint of path.
	FingerprintInvalidate func(path string)
	// EvidenceStale marks evidence covering path stale.
	EvidenceStale func(path string)
	// GraphUpdate refreshes the code graph for the changed paths.
	GraphUpdate func(paths []string)
	// LeaseHolder returns the run holding path, if any.
	LeaseHolder func(path string) (holder, activity string, ok bool)
	// Directive sends an informative directive to a run.
	Directive func(holder, text string)
}

// Watcher polls the active project roots and reports changes not made
// by the agent (Section 16.5). Polling keeps the core free of a file
// notification dependency and behaves the same on every platform; the
// background loop runs under safego.Go. See
// docs/decisions/0013-workspace-watcher-and-snapshots.md.
type Watcher struct {
	mu         sync.Mutex
	roots      []string
	extraExcl  []string
	interval   time.Duration
	last       map[string]string // path -> sha256 hex ("" for missing)
	agentMarks map[string]time.Time
	hooks      WatcherHooks
	stopCh     chan struct{}
	stoppedCh  chan struct{}
	running    bool
	now        func() time.Time
}

// NewWatcher returns a watcher over roots.
func NewWatcher(roots []string, interval time.Duration, hooks WatcherHooks) *Watcher {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &Watcher{
		roots:      append([]string(nil), roots...),
		interval:   interval,
		last:       map[string]string{},
		agentMarks: map[string]time.Time{},
		hooks:      hooks,
		now:        time.Now,
	}
}

// SetExclusions adds project-level extra exclusions.
func (w *Watcher) SetExclusions(excl []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.extraExcl = append([]string(nil), excl...)
}

// MarkAgentWrite records that path was just written by the agent, so
// the next scan does not report it as external. Marks expire after
// one interval.
func (w *Watcher) MarkAgentWrite(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.agentMarks[filepath.Clean(path)] = w.now()
}

// Start begins the background poll loop under safego.Go. It first
// baselines the tree so pre-existing files are not reported.
func (w *Watcher) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.last = w.scanSnapshot()
	w.stopCh = make(chan struct{})
	w.stoppedCh = make(chan struct{})
	w.running = true
	w.mu.Unlock()
	safego.Go("workspace/watcher", nil, func() {
		defer close(w.stoppedCh)
		t := time.NewTicker(w.interval)
		defer t.Stop()
		for {
			select {
			case <-w.stopCh:
				return
			case <-t.C:
				w.ScanOnce()
			}
		}
	})
}

// Stop ends the background loop.
func (w *Watcher) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	close(w.stopCh)
	ch := w.stoppedCh
	w.running = false
	w.mu.Unlock()
	<-ch
}

// ScanOnce polls once and applies the Section 16.5 reactions. It
// returns the external events found.
func (w *Watcher) ScanOnce() []WatchEvent {
	w.mu.Lock()
	cur := w.scanSnapshot()
	var events []WatchEvent
	now := w.now()
	for path, hash := range cur {
		old, known := w.last[path]
		if !known || old != hash {
			if marked, ok := w.agentMarks[path]; ok && now.Sub(marked) < 2*w.interval {
				continue
			}
			kind := "modified"
			if !known {
				kind = "created"
			}
			events = append(events, WatchEvent{Path: path, Kind: kind, At: now})
		}
	}
	for path := range w.last {
		if _, ok := cur[path]; !ok {
			if marked, ok := w.agentMarks[path]; ok && now.Sub(marked) < 2*w.interval {
				continue
			}
			events = append(events, WatchEvent{Path: path, Kind: "deleted", At: now})
		}
	}
	w.last = cur
	// Expire agent marks.
	for p, at := range w.agentMarks {
		if now.Sub(at) >= 2*w.interval {
			delete(w.agentMarks, p)
		}
	}
	hooks := w.hooks
	w.mu.Unlock()

	if len(events) == 0 {
		return nil
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Path < events[j].Path })
	var changed []string
	for _, e := range events {
		changed = append(changed, e.Path)
		if hooks.FingerprintInvalidate != nil {
			hooks.FingerprintInvalidate(e.Path)
		}
		if hooks.EvidenceStale != nil {
			hooks.EvidenceStale(e.Path)
		}
		if hooks.LeaseHolder != nil && hooks.Directive != nil {
			if holder, _, ok := hooks.LeaseHolder(e.Path); ok {
				hooks.Directive(holder, e.Path+" changed outside Excalibur")
			}
		}
	}
	if hooks.GraphUpdate != nil {
		hooks.GraphUpdate(changed)
	}
	return events
}

// scanSnapshot walks every root honoring exclusions. Callers hold w.mu.
func (w *Watcher) scanSnapshot() map[string]string {
	out := map[string]string{}
	matcher := w.buildMatcher()
	for _, root := range w.roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if rel == "." {
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
			for _, ex := range w.extraExcl {
				if base == ex {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if matcher.excluded(rel, d.IsDir()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			// Skip huge files: hash only the first 1 MiB for change
			// detection, enough for the watcher signal.
			data, err := readHead(path, 1<<20)
			if err != nil {
				return nil
			}
			sum := sha256.Sum256(data)
			// Mix the size in so truncations past the head still trip
			// the signal.
			var sizeMix [8]byte
			if st, err := os.Stat(path); err == nil {
				sz := uint64(st.Size())
				for i := 0; i < 8; i++ {
					sizeMix[i] = byte(sz >> (8 * i))
				}
			}
			h := sha256.New()
			h.Write(sum[:])
			h.Write(sizeMix[:])
			out[filepath.Clean(path)] = hex.EncodeToString(h.Sum(nil))
			return nil
		})
	}
	return out
}

func (w *Watcher) buildMatcher() *ignoreMatcher {
	m := &ignoreMatcher{}
	for _, root := range w.roots {
		data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			m.add(line)
		}
	}
	return m
}

func readHead(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, max)
	n, _ := f.Read(buf)
	return buf[:n], nil
}

// ignoreMatcher is a small .gitignore subset: blank lines and #
// comments skipped, trailing / marks directories, * and ** supported,
// ! negates. Later rules win. It is intentionally dependency-free.
type ignoreMatcher struct {
	rules []ignoreRule
}

type ignoreRule struct {
	pattern string
	dirOnly bool
	negate  bool
}

func (m *ignoreMatcher) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}
	r := ignoreRule{}
	if strings.HasPrefix(line, "!") {
		r.negate = true
		line = strings.TrimPrefix(line, "!")
	}
	if strings.HasSuffix(line, "/") {
		r.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	line = strings.TrimPrefix(line, "/")
	r.pattern = line
	m.rules = append(m.rules, r)
}

func (m *ignoreMatcher) excluded(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	hit := false
	for _, r := range m.rules {
		if r.dirOnly && !isDir {
			// A dir-only rule also excludes everything beneath the
			// directory.
			if matchIgnorePattern(r.pattern, rel) || matchIgnorePattern(r.pattern, dirPrefix(rel)) {
				hit = !r.negate
			}
			continue
		}
		if matchIgnorePattern(r.pattern, rel) || matchIgnorePattern(r.pattern, filepath.ToSlash(filepath.Base(rel))) {
			hit = !r.negate
		}
	}
	return hit
}

func dirPrefix(rel string) string {
	if i := strings.Index(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return rel
}

// matchIgnorePattern matches a single .gitignore-ish pattern with *
// and ** against a slash path or base name.
func matchIgnorePattern(pattern, name string) bool {
	if !strings.Contains(pattern, "/") {
		ok, _ := filepath.Match(pattern, name)
		return ok
	}
	// Segment-wise match with ** spanning segments.
	psegs := strings.Split(pattern, "/")
	nsegs := strings.Split(name, "/")
	return matchSegments(psegs, nsegs)
}

func matchSegments(p, n []string) bool {
	if len(p) == 0 {
		return len(n) == 0
	}
	if p[0] == "**" {
		for i := 0; i <= len(n); i++ {
			if matchSegments(p[1:], n[i:]) {
				return true
			}
		}
		return false
	}
	if len(n) == 0 {
		return false
	}
	ok, _ := filepath.Match(p[0], n[0])
	if !ok {
		return false
	}
	return matchSegments(p[1:], n[1:])
}
