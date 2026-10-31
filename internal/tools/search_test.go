package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepSummarizedBounded(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("foo bar\nfoo baz\nnope\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("nothing\n"), 0o644)
	g := &GrepTool{}
	raw, _ := json.Marshal(map[string]any{"pattern": "foo", "paths": []string{dir}, "max_results": 10})
	res, err := g.Invoke(context.Background(), Invocation{Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOK {
		t.Fatalf("got %+v", res)
	}
	if !strings.Contains(res.Output, "match(es)") || !strings.Contains(res.Output, "a.txt") {
		t.Fatalf("summarized per-file output: %q", res.Output)
	}
	// Bad regex is an input error, not a crash.
	raw2, _ := json.Marshal(map[string]any{"pattern": "(["})
	res2, _ := g.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusInputError {
		t.Fatalf("bad pattern: %+v", res2)
	}
}

func TestGlobRespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.log"), []byte("x"), 0o644)
	g := &GlobTool{}
	raw, _ := json.Marshal(map[string]any{"pattern": "**", "dir": dir})
	res, _ := g.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("got %+v", res)
	}
	if !strings.Contains(res.Output, "a.txt") {
		t.Fatalf("must match a.txt: %q", res.Output)
	}
	if strings.Contains(res.Output, "b.log") {
		t.Fatalf("must respect .gitignore: %q", res.Output)
	}
	// Opt-out finds it.
	raw2, _ := json.Marshal(map[string]any{"pattern": "*.log", "dir": dir, "respect_gitignore": false})
	res2, _ := g.Invoke(context.Background(), Invocation{Arguments: raw2})
	if !strings.Contains(res2.Output, "b.log") {
		t.Fatalf("opt-out: %q", res2.Output)
	}
}

func TestMatchGlobStars(t *testing.T) {
	ok, err := matchGlob("**/*.txt", "a/b/c.txt")
	if err != nil || !ok {
		t.Fatalf("** must span dirs: %v %v", ok, err)
	}
	ok, _ = matchGlob("*.txt", "a.txt")
	if !ok {
		t.Fatal("simple glob")
	}
}
