//go:build linux

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLinuxSamplerSelf(t *testing.T) {
	s, err := (LinuxResourceSampler{}).Sample(0)
	if err != nil {
		t.Fatalf("Sample(0): %v", err)
	}
	if s.RSSBytes == 0 {
		t.Fatal("RSSBytes must be positive")
	}
	if s.CPUSeconds < 0 {
		t.Fatal("CPUSeconds must be non-negative")
	}
	if s.Processes < 1 {
		t.Fatalf("Processes = %d, want >= 1", s.Processes)
	}
	if s.OOMKills < 0 {
		t.Fatal("OOMKills must be non-negative")
	}
	if s.At.IsZero() || time.Since(s.At) > time.Minute {
		t.Fatalf("At = %v", s.At)
	}
}

func TestLinuxSamplerChild(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	s, err := (LinuxResourceSampler{}).Sample(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Sample(child): %v", err)
	}
	if s.RSSBytes == 0 {
		t.Fatal("child RSSBytes must be positive")
	}
}

func TestLinuxSamplerMissingPid(t *testing.T) {
	if _, err := (LinuxResourceSampler{}).Sample(1 << 30); err == nil {
		t.Fatal("Sample on missing pid must fail")
	}
}

func TestLinuxSamplerStubCgroup(t *testing.T) {
	root := t.TempDir()
	// Resolve our own cgroup dir under the stub root with the same
	// helper the sampler uses.
	dir, err := cgroupDirForPID(os.Getpid(), root)
	if err != nil {
		t.Skipf("no cgroup entry: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pids.current"), []byte("7"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.events"), []byte("low 0\nhigh 1\nmax 2\noom 3\noom_kill 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := (LinuxResourceSampler{CgroupRoot: root}).Sample(os.Getpid())
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if s.Processes != 7 {
		t.Fatalf("Processes = %d, want 7 from stub pids.current", s.Processes)
	}
	if s.OOMKills != 4 {
		t.Fatalf("OOMKills = %d, want 4 from stub memory.events", s.OOMKills)
	}
}
