//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// stubCgroupRoot builds a writable stub cgroup tree mirroring the
// calling process's real cgroup path, so LowerProcess exercises the
// delegated branch without privileges.
func stubCgroupRoot(t *testing.T) (root string, dir string) {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Skipf("no /proc/self/cgroup: %v", err)
	}
	rel := ""
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(parts) == 3 && parts[0] == "0" {
			rel = strings.TrimPrefix(parts[2], "/")
			break
		}
	}
	root = t.TempDir()
	dir = root
	if rel != "" {
		dir = filepath.Join(root, filepath.Clean(rel))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cpu.weight"), []byte("100"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, dir
}

func TestLinuxPriorityDelegatedCgroup(t *testing.T) {
	root, dir := stubCgroupRoot(t)
	p := LinuxProcessPriority{CgroupRoot: root, Weight: 20}
	if !p.Delegated(os.Getpid()) {
		t.Fatal("Delegated must be true with a writable stub tree")
	}
	if err := p.LowerProcess(os.Getpid()); err != nil {
		t.Fatalf("LowerProcess (delegated): %v", err)
	}
	got, err := EffectiveWeight(dir)
	if err != nil || got != 20 {
		t.Fatalf("EffectiveWeight = %d, %v; want 20", got, err)
	}
}

func TestLinuxPrioritySetMemoryMax(t *testing.T) {
	root, dir := stubCgroupRoot(t)
	if err := os.WriteFile(filepath.Join(dir, "memory.max"), []byte("max"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := LinuxProcessPriority{CgroupRoot: root}
	if err := p.SetMemoryMax(os.Getpid(), 256<<20); err != nil {
		t.Fatalf("SetMemoryMax: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "memory.max"))
	if err != nil || strings.TrimSpace(string(raw)) != strconv.Itoa(256<<20) {
		t.Fatalf("memory.max = %q, %v", raw, err)
	}
	if err := p.SetMemoryMax(os.Getpid(), 0); err != nil {
		t.Fatalf("SetMemoryMax(0): %v", err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "memory.max"))
	if strings.TrimSpace(string(raw)) != "max" {
		t.Fatalf("memory.max after clear = %q", raw)
	}
}

func TestLinuxPriorityFallbackNice(t *testing.T) {
	// A nonexistent cgroup root forces the nice+ioprio fallback.
	p := LinuxProcessPriority{CgroupRoot: filepath.Join(t.TempDir(), "no-cgroup")}
	if p.Delegated(os.Getpid()) {
		t.Fatal("Delegated must be false without control files")
	}
	before, err := EffectiveNice(os.Getpid())
	if err != nil {
		t.Fatalf("EffectiveNice: %v", err)
	}
	if err := p.LowerProcess(os.Getpid()); err != nil {
		t.Fatalf("LowerProcess (fallback): %v", err)
	}
	after, err := EffectiveNice(os.Getpid())
	if err != nil {
		t.Fatalf("EffectiveNice: %v", err)
	}
	if after < before+DefaultToolNice && after < 19 {
		t.Fatalf("nice did not rise: before=%d after=%d", before, after)
	}
}

func TestLinuxPriorityLowerCurrentThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tid := unix.Gettid()
	before, err := EffectiveNice(tid)
	if err != nil {
		t.Fatalf("getpriority: %v", err)
	}
	if err := (LinuxProcessPriority{}).LowerCurrentThread(); err != nil {
		t.Fatalf("LowerCurrentThread: %v", err)
	}
	after, err := EffectiveNice(tid)
	if err != nil {
		t.Fatalf("getpriority: %v", err)
	}
	if after < before && after < 19 {
		t.Fatalf("thread nice did not rise: before=%d after=%d", before, after)
	}
}

func TestLinuxPriorityInvalidPid(t *testing.T) {
	p := LinuxProcessPriority{}
	if err := p.LowerProcess(0); err == nil {
		t.Fatal("LowerProcess(0) must fail")
	}
	if err := p.SetMemoryMax(-1, 100); err == nil {
		t.Fatal("SetMemoryMax(-1) must fail")
	}
}
