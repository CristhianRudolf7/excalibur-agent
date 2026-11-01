package repoexplore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(workdir string) Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Workdir = workdir
	return cfg
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The brief covers layout, build, entry, tests, configs, data,
// versions and pitfalls in at most MaxBriefLines lines.
func TestCollectCoversSections(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/demo\n\ngo 1.24\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "main_test.go"), "package main\n")
	writeFile(t, filepath.Join(dir, "README.md"), "# demo\n")
	writeFile(t, filepath.Join(dir, "data.csv"), "a,b,c\n1,2,3\n")
	writeFile(t, filepath.Join(dir, ".nvmrc"), "22\n")

	brief := NewCollector(testConfig(dir)).Collect(context.Background(), dir, "fix main.go")
	if brief.Empty {
		t.Fatal("brief must not be empty")
	}
	if len(brief.Lines) == 0 || len(brief.Lines) > 60 {
		t.Fatalf("lines = %d, want 1..60", len(brief.Lines))
	}
	joined := strings.Join(brief.Lines, "\n")
	for _, want := range []string{"Repo brief", "layout", "build:", "go.mod", "entry points:", "main.go", "tests:", "data:", "data.csv", "versions", "Verify"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("brief missing %q:\n%s", want, joined)
		}
	}
	// This healthy fixture has a README, build file, entry and
	// tests, so no pitfalls section is expected.
	if strings.Contains(joined, "pitfalls:") {
		t.Fatalf("healthy repo must not list pitfalls:\n%s", joined)
	}
	// A bare directory does list pitfalls.
	bare := t.TempDir()
	bareBrief := NewCollector(testConfig(bare)).Collect(context.Background(), bare, "do work")
	if !strings.Contains(strings.Join(bareBrief.Lines, "\n"), "pitfalls:") {
		t.Fatalf("bare repo must list pitfalls:\n%s", strings.Join(bareBrief.Lines, "\n"))
	}
}

// Every line is single-line and determinism holds across calls.
func TestCollectDeterministicAndSingleLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"x","scripts":{"test":"jest","build":"tsc"}}`)
	writeFile(t, filepath.Join(dir, "src/index.ts"), "export {};\n")
	writeFile(t, filepath.Join(dir, "src/index.test.ts"), "export {};\n")

	c := NewCollector(testConfig(dir))
	a := c.Collect(context.Background(), dir, "run npm test")
	b := c.Collect(context.Background(), dir, "run npm test")
	if strings.Join(a.Lines, "\n") != strings.Join(b.Lines, "\n") {
		t.Fatalf("brief not deterministic:\n%s\n---\n%s", strings.Join(a.Lines, "\n"), strings.Join(b.Lines, "\n"))
	}
	for _, l := range a.Lines {
		if strings.Contains(l, "\n") {
			t.Fatalf("line carries newline: %q", l)
		}
		if len(l) > 160 {
			t.Fatalf("line exceeds 160 chars: %q", l)
		}
	}
	if got := a.Render(); !strings.HasSuffix(got, "\n") {
		t.Fatal("Render must end with newline")
	}
}

// Instruction-mentioned paths resolve to exists/missing.
func TestMentionedPaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.py"), "print(1)\n")
	brief := NewCollector(testConfig(dir)).Collect(context.Background(), dir, "fix app.py and missing.py")
	joined := strings.Join(brief.Lines, "\n")
	if !strings.Contains(joined, "app.py: exists") {
		t.Fatalf("mentioned existing path missing:\n%s", joined)
	}
	if !strings.Contains(joined, "missing.py: not found") {
		t.Fatalf("mentioned missing path missing:\n%s", joined)
	}
}

// Empty or missing workdir degrades to a short brief, never an error.
func TestCollectEmptyWorkdir(t *testing.T) {
	for _, wd := range []string{"", "/nonexistent-repoexplore-xyz"} {
		brief := NewCollector(DefaultConfig()).Collect(context.Background(), wd, "do things")
		if len(brief.Lines) == 0 {
			t.Fatalf("workdir %q must yield a brief", wd)
		}
		if len(brief.Lines) > 60 {
			t.Fatalf("lines = %d, want <=60", len(brief.Lines))
		}
	}
}

// Heavy directories collapse instead of being walked.
func TestHeavyDirsCollapse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "node_modules/pkg/index.js"), "x\n")
	writeFile(t, filepath.Join(dir, "main.py"), "print(1)\n")
	brief := NewCollector(testConfig(dir)).Collect(context.Background(), dir, "hi")
	joined := strings.Join(brief.Lines, "\n")
	if !strings.Contains(joined, "node_modules/") || !strings.Contains(joined, "collapsed") {
		t.Fatalf("heavy dir must collapse:\n%s", joined)
	}
}

// Data hints name CSV headers and SQLite inspection.
func TestDataHints(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "in.csv"), "u,y,r\n1,2,3\n")
	writeFile(t, filepath.Join(dir, "store.sqlite"), "x")
	brief := NewCollector(testConfig(dir)).Collect(context.Background(), dir, "inspect data")
	joined := strings.Join(brief.Lines, "\n")
	if !strings.Contains(joined, "header: u,y,r") {
		t.Fatalf("csv header missing:\n%s", joined)
	}
	if !strings.Contains(joined, "SQLite") {
		t.Fatalf("sqlite hint missing:\n%s", joined)
	}
}

// Brief never exceeds the configured cap.
func TestBriefLineCap(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 30; i++ {
		writeFile(t, filepath.Join(dir, "f"+itoa(i)+".csv"), "a\n1\n")
	}
	cfg := testConfig(dir)
	cfg.MaxBriefLines = 20
	brief := NewCollector(cfg).Collect(context.Background(), dir, "many files")
	if len(brief.Lines) > 20 {
		t.Fatalf("lines = %d, want <=20", len(brief.Lines))
	}
	if !brief.Truncated {
		t.Fatal("capped brief must mark truncated")
	}
	last := brief.Lines[len(brief.Lines)-1]
	if !strings.Contains(last, "Verify") {
		t.Fatalf("capped brief must end with verify footer, got %q", last)
	}
}

// Config defaults to off until measured.
func TestConfigDefaultOff(t *testing.T) {
	if err := os.Unsetenv(ExploreSwitch); err != nil {
		t.Fatal(err)
	}
	// Unset keeps the literal: explicit true survives, zero stays off.
	if got := ConfigFromEnv(Config{}); got.Enabled {
		t.Fatal("default must be off")
	}
	if got := ConfigFromEnv(Config{Enabled: true}); !got.Enabled {
		t.Fatal("explicit true must survive when switch unset")
	}
	t.Setenv(ExploreSwitch, "on")
	if got := ConfigFromEnv(Config{}); !got.Enabled {
		t.Fatal("EXCALIBUR_EXPLORE=on must enable")
	}
	t.Setenv(ExploreSwitch, "off")
	if got := ConfigFromEnv(Config{Enabled: true}); got.Enabled {
		t.Fatal("EXCALIBUR_EXPLORE=off must disable")
	}
	for _, v := range []string{"1", "true", "yes", "on"} {
		t.Setenv(ExploreSwitch, v)
		if !ConfigFromEnv(Config{}).Enabled {
			t.Fatalf("%q must enable", v)
		}
	}
	for _, v := range []string{"0", "false", "no", "off", "bogus"} {
		t.Setenv(ExploreSwitch, v)
		if ConfigFromEnv(Config{}).Enabled {
			t.Fatalf("%q must disable", v)
		}
	}
}
