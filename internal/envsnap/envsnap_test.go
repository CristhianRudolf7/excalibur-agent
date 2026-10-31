package envsnap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The snapshot pass must be bounded and total: it never errors, even
// for a missing workdir, and finishes well inside its budget.
func TestCollectBoundedAndTotal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timeout = 10 * time.Second
	cfg.Workdir = t.TempDir()
	start := time.Now()
	snap := NewCollector(cfg).Collect(context.Background())
	if elapsed := time.Since(start); elapsed > cfg.Timeout+2*time.Second {
		t.Fatalf("collect took %s, budget %s", elapsed, cfg.Timeout)
	}
	if snap.OS == "" || snap.Arch == "" || snap.CPUs <= 0 {
		t.Fatalf("missing basics: %+v", snap)
	}
	if len(snap.Shells) == 0 {
		t.Fatal("want at least one shell on PATH")
	}
	if snap.DefaultShell == "" {
		t.Fatal("want a default shell")
	}
	if snap.Network != NetworkOpen && snap.Network != NetworkRestricted && snap.Network != NetworkUnknown {
		t.Fatalf("bad network mode %q", snap.Network)
	}
}

func TestCollectMissingWorkdir(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workdir = filepath.Join(t.TempDir(), "does-not-exist")
	snap := NewCollector(cfg).Collect(context.Background())
	if !strings.Contains(snap.Tree, "unreadable") {
		t.Fatalf("missing dir must note unreadable, got %q", snap.Tree)
	}
}

func TestTreeBoundedAndSorted(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.txt", "a.txt", "sub", "node_modules"} {
		p := filepath.Join(dir, n)
		if n == "sub" || n == "node_modules" {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "inner.go"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Workdir = dir
	cfg.MaxTreeEntries = 3
	tr := collectTree(context.Background(), dir, cfg.MaxTreeEntries, 2)
	if !tr.truncated {
		t.Fatal("over-budget tree must truncate")
	}
	if !strings.Contains(tr.text, "truncated to 3 entries") {
		t.Fatalf("truncation note missing: %q", tr.text)
	}
	if !strings.Contains(tr.text, "node_modules") || !strings.Contains(tr.text, "collapsed") {
		t.Fatalf("heavy dirs must collapse: %q", tr.text)
	}
	// Full pass stays sorted: a.txt before b.txt.
	full := collectTree(context.Background(), dir, 100, 1)
	ia, ib := strings.Index(full.text, "a.txt"), strings.Index(full.text, "b.txt")
	if ia < 0 || ib < 0 || ia > ib {
		t.Fatalf("tree must be sorted: %q", full.text)
	}
}

func TestRenderCompactBoundedAndStable(t *testing.T) {
	snap := Snapshot{
		OS: "linux", Distro: "Ubuntu 24.04.3 LTS", Arch: "amd64", CPUs: 8,
		MemTotalBytes: 8 << 30, DiskFreeBytes: 100 << 30,
		Shells: []string{"bash", "sh"}, DefaultShell: "/bin/bash",
		PkgManagers:  []string{"apt", "pip3"},
		Languages:    map[string]string{"python3": "Python 3.12.1", "go": "go version go1.24.1 linux/amd64"},
		Compilers:    map[string]string{"gcc": "gcc (Ubuntu 13.2.0) 13.2.0"},
		ToolsPresent: []string{"git", "curl"},
		ToolsMissing: []string{"docker"},
		Network:      NetworkRestricted, NetworkDetail: "dial x:443: refused",
		Workdir: "/app", Tree: "file main.py\n",
	}
	a := snap.RenderCompact(1800)
	b := snap.RenderCompact(1800)
	if a != b {
		t.Fatal("render must be deterministic")
	}
	if len(a) > 1800 {
		t.Fatalf("render exceeds budget: %d bytes", len(a))
	}
	for _, want := range []string{"os: linux", "cpus=8", "mem=", "shell:", "pkg:",
		"languages:", "python3", "compilers:", "tools present:", "tools missing: docker",
		"network: restricted", "do not retry the network in a loop",
		"listening: no local TCP listeners", "workdir /app:", "verify with tools"} {
		if !strings.Contains(a, want) {
			t.Fatalf("render missing %q:\n%s", want, a)
		}
	}
	if strings.Contains(a, "at=") || strings.Contains(a, "2026") {
		t.Fatalf("render must not carry timestamps (cache stability):\n%s", a)
	}
	// Hard cap truncates with a marker.
	tiny := snap.RenderCompact(120)
	if len(tiny) > 120 || !strings.HasSuffix(tiny, "...[truncated]\n") {
		t.Fatalf("tiny render must truncate with marker: %q", tiny)
	}
}

func TestConfigFromEnvSwitch(t *testing.T) {
	t.Setenv(EnvSnapshotSwitch, "0")
	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Enabled {
		t.Fatal("EXCALIBUR_ENV_SNAPSHOT=0 must disable")
	}
	t.Setenv(EnvSnapshotSwitch, "1")
	t.Setenv(EnvSnapshotTimeoutSwitch, "250ms")
	cfg = ConfigFromEnv(DefaultConfig())
	if !cfg.Enabled || cfg.Timeout != 250*time.Millisecond {
		t.Fatalf("switches not honored: %+v", cfg)
	}
	// A literal zero Config is ON by default through FromEnv.
	os.Unsetenv(EnvSnapshotSwitch)
	os.Unsetenv(EnvSnapshotTimeoutSwitch)
	if cfg := ConfigFromEnv(Config{}); !cfg.Enabled {
		t.Fatal("default must be ON")
	}
}

func TestCachedCollectsOnce(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workdir = t.TempDir()
	c := NewCached(cfg)
	ctx := context.Background()
	first := c.Snapshot(ctx).RenderCompact(1800)
	second := c.Snapshot(ctx).RenderCompact(1800)
	if first != second {
		t.Fatal("cached snapshot must render identical bytes across steps")
	}
	if !c.Ready() {
		t.Fatal("provider must be ready after collect")
	}
	// A fixed snapshot short-circuits collection.
	fixed := Snapshot{OS: "linux", Arch: "amd64", CPUs: 2}
	d := NewCached(cfg)
	d.SnapshotForTest(fixed)
	if got := d.Snapshot(ctx); got.CPUs != 2 {
		t.Fatalf("fixed snapshot must win, got %+v", got)
	}
}

func TestShortenVersion(t *testing.T) {
	cases := map[string]string{
		"Python 3.12.1":                   "Python 3.12.1",
		"go version go1.24.1 linux/amd64": "go1.24.1",
		"git version 2.43.0":              "2.43.0",
		"single":                          "single",
	}
	for in, want := range cases {
		if got := shortenVersion(in); got != want {
			t.Fatalf("shortenVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
