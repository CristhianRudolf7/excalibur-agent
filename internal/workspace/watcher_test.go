package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Section 16.6 row 5: the user edits leased files (fingerprints
// invalidated, evidence stale, graph updated, holder notified) while
// excluded files (node_modules) are ignored.
func TestWatcherExternalChange(t *testing.T) {
	root := t.TempDir()
	leased := filepath.Join(root, "src.go")
	if err := os.WriteFile(leased, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(root, "node_modules", "dep.js")
	if err := os.MkdirAll(filepath.Dir(ignored), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignored, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var invalidated []string
	var stale []string
	var graph [][]string
	var directives []string
	w := NewWatcher([]string{root}, 50*time.Millisecond, WatcherHooks{
		FingerprintInvalidate: func(p string) { invalidated = append(invalidated, p) },
		EvidenceStale:         func(p string) { stale = append(stale, p) },
		GraphUpdate:           func(ps []string) { graph = append(graph, ps) },
		LeaseHolder: func(p string) (string, string, bool) {
			if p == leased {
				return "run-1", "editing", true
			}
			return "", "", false
		},
		Directive: func(holder, text string) { directives = append(directives, holder+":"+text) },
	})
	w.Start()
	defer w.Stop()
	time.Sleep(120 * time.Millisecond) // let the baseline settle
	// External user edit of the leased file.
	if err := os.WriteFile(leased, []byte("v2 by user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Change inside node_modules must be ignored.
	if err := os.WriteFile(ignored, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w.ScanOnce()
		if len(invalidated) > 0 {
			break
		}
		time.Sleep(60 * time.Millisecond)
	}
	if len(invalidated) != 1 || invalidated[0] != leased {
		t.Fatalf("want one invalidation for the leased file, got %v", invalidated)
	}
	if len(stale) != 1 || stale[0] != leased {
		t.Fatalf("evidence must go stale: %v", stale)
	}
	if len(graph) != 1 || len(graph[0]) != 1 || graph[0][0] != leased {
		t.Fatalf("graph must update once with the file: %v", graph)
	}
	if len(directives) != 1 {
		t.Fatalf("holder must get the directive: %v", directives)
	}
}

// .gitignore rules exclude, ! re-includes.
func TestWatcherGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n!important.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "important.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var seen []string
	w := NewWatcher([]string{root}, time.Second, WatcherHooks{})
	snap := w.scanSnapshot()
	for p := range snap {
		seen = append(seen, filepath.Base(p))
	}
	for _, s := range seen {
		if s == "a.log" {
			t.Fatalf("a.log must be excluded by .gitignore, got %v", seen)
		}
	}
	found := map[string]bool{}
	for _, s := range seen {
		found[s] = true
	}
	if !found["important.log"] || !found["keep.go"] {
		t.Fatalf("negation and normal files must scan: %v", seen)
	}
}

// Agent writes marked with MarkAgentWrite are not external events.
func TestWatcherAgentWriteSuppressed(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "a.go")
	if err := os.WriteFile(f, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewWatcher([]string{root}, 50*time.Millisecond, WatcherHooks{})
	w.Start()
	defer w.Stop()
	time.Sleep(120 * time.Millisecond)
	if err := os.WriteFile(f, []byte("v2 by agent"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.MarkAgentWrite(f)
	if evs := w.ScanOnce(); len(evs) != 0 {
		t.Fatalf("agent write must not report: %v", evs)
	}
}
