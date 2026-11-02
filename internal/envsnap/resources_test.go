package envsnap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resourceFixture(t *testing.T, files map[string]string) ResourceReader {
	t.Helper()
	root := t.TempDir()
	defaults := map[string]string{
		"proc/meminfo":                  "MemTotal:       33554432 kB\n",
		"sys/devices/system/cpu/online": "0-7\n",
		"proc/self/cgroup":              "0::/work/job\n",
		"proc/self/mountinfo":           "1 0 0:1 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
	}
	for k, v := range files {
		defaults[k] = v
	}
	for k, v := range defaults {
		path := filepath.Join(root, k)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(v), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return ResourceReader{ProcRoot: filepath.Join(root, "proc"), SysRoot: filepath.Join(root, "sys")}
}

func TestEffectiveResourceLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		files   map[string]string
		cpus    float64
		memory  int64
		limited bool
	}{
		{"v2", map[string]string{
			"sys/fs/cgroup/work/job/memory.max": "4294967296",
			"sys/fs/cgroup/work/job/cpu.max":    "200000 100000",
		}, 2, 4 << 30, true},
		{"v2 high and ancestor quota", map[string]string{
			"sys/fs/cgroup/work/job/memory.max":  "8589934592",
			"sys/fs/cgroup/work/job/memory.high": "3221225472",
			"sys/fs/cgroup/work/job/cpu.max":     "400000 100000",
			"sys/fs/cgroup/work/cpu.max":         "150000 100000",
			"sys/fs/cgroup/work/memory.max":      "2147483648",
		}, 1.5, 2 << 30, true},
		{"cpuset", map[string]string{
			"sys/fs/cgroup/work/job/cpuset.cpus.effective": "1,3-4",
			"sys/fs/cgroup/work/job/cpu.max":               "400000 100000",
		}, 3, 32 << 30, false},
		{"fractional", map[string]string{"sys/fs/cgroup/work/job/cpu.max": "50000 100000"}, .5, 32 << 30, false},
		{"unlimited", map[string]string{
			"sys/fs/cgroup/work/job/memory.max":  "max",
			"sys/fs/cgroup/work/job/memory.high": "max",
			"sys/fs/cgroup/work/job/cpu.max":     "max 100000",
		}, 8, 32 << 30, false},
		{"invalid", map[string]string{
			"sys/fs/cgroup/work/job/memory.max":            "999999999999999999999",
			"sys/fs/cgroup/work/job/memory.high":           "-1",
			"sys/fs/cgroup/work/job/cpu.max":               "200000 0",
			"sys/fs/cgroup/work/job/cpuset.cpus.effective": "3-1",
		}, 8, 32 << 30, false},
		{"host smaller", map[string]string{
			"sys/fs/cgroup/work/job/memory.max": "68719476736",
			"sys/fs/cgroup/work/job/cpu.max":    "1000000 100000",
		}, 8, 32 << 30, false},
		{"v1", map[string]string{
			"proc/self/cgroup":    "2:memory:/work/job\n3:cpu,cpuacct:/work/job\n4:cpuset:/work/job\n",
			"proc/self/mountinfo": "1 0 0:1 / /sys/fs/cgroup/memory rw - cgroup cgroup rw,memory\n2 0 0:2 / /sys/fs/cgroup/cpu,cpuacct rw - cgroup cgroup rw,cpu,cpuacct\n3 0 0:3 / /sys/fs/cgroup/cpuset rw - cgroup cgroup rw,cpuset\n",
			"sys/fs/cgroup/memory/work/job/memory.limit_in_bytes":  "4294967296",
			"sys/fs/cgroup/cpu,cpuacct/work/job/cpu.cfs_quota_us":  "300000",
			"sys/fs/cgroup/cpu,cpuacct/work/job/cpu.cfs_period_us": "100000",
			"sys/fs/cgroup/cpuset/work/job/cpuset.cpus":            "2-3",
		}, 2, 4 << 30, true},
		{"subtree mount", map[string]string{
			"proc/self/mountinfo":          "1 0 0:1 /work /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			"sys/fs/cgroup/job/memory.max": "4294967296",
			"sys/fs/cgroup/cpu.max":        "200000 100000",
		}, 2, 4 << 30, true},
		{"namespace mount", map[string]string{
			"proc/self/cgroup":             "0::/job\n",
			"proc/self/mountinfo":          "1 0 0:1 /containers/worker /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			"sys/fs/cgroup/job/memory.max": "4294967296",
		}, 8, 4 << 30, true},
		{"v1 unlimited", map[string]string{
			"proc/self/cgroup":    "2:memory:/work/job\n3:cpu:/work/job\n",
			"proc/self/mountinfo": "",
			"sys/fs/cgroup/memory/work/job/memory.limit_in_bytes": "9223372036854771712",
			"sys/fs/cgroup/cpu/work/job/cpu.cfs_quota_us":         "-1",
			"sys/fs/cgroup/cpu/work/job/cpu.cfs_period_us":        "100000",
		}, 8, 32 << 30, false},
		{"no mountinfo", map[string]string{
			"proc/self/mountinfo":               "",
			"sys/fs/cgroup/work/job/memory.max": "4294967296",
		}, 8, 4 << 30, true},
		{"missing membership", map[string]string{"proc/self/cgroup": ""}, 8, 32 << 30, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EffectiveLimitsSwitch, "1")
			got := resourceFixture(t, tc.files).Limits()
			if got.CPUs != tc.cpus || got.MemoryBytes != tc.memory || got.MemoryLimited != tc.limited || got.HostCPUs != 8 || got.HostMemory != 32<<30 {
				t.Fatalf("limits: %+v", got)
			}
		})
	}
}

func TestCollectEffectiveResourcesAndSwitch(t *testing.T) {
	r := resourceFixture(t, map[string]string{
		"sys/fs/cgroup/work/job/memory.max": "4294967296",
		"sys/fs/cgroup/work/job/cpu.max":    "150000 100000",
	})
	// Avoid external version probes: this test only exercises resource injection.
	t.Setenv("PATH", "")
	for _, enabled := range []bool{true, false} {
		t.Setenv(EffectiveLimitsSwitch, "off")
		if enabled {
			t.Setenv(EffectiveLimitsSwitch, "")
		}
		cfg := DefaultConfig()
		cfg.ProcRoot, cfg.SysRoot, cfg.Workdir = r.ProcRoot, r.SysRoot, t.TempDir()
		s := NewCollector(cfg).Collect(context.Background())
		text := s.RenderCompact(1800)
		if enabled {
			if s.MemTotalBytes != 4<<30 || s.CPUs != 1 || s.EffectiveCPUs != 1.5 {
				t.Fatalf("snapshot: %+v", s)
			}
			for _, want := range []string{"cpus=1.5", "mem=4.0G", "host mem 32GiB", "host cpus 8"} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q: %s", want, text)
				}
			}
		} else if s.MemTotalBytes != 32<<30 || s.CPUs != 8 || strings.Contains(text, "host mem") || s.MemoryLimited {
			t.Fatalf("disabled snapshot: %+v", s)
		}
	}
}

func TestParallelismGuidance(t *testing.T) {
	t.Setenv(EffectiveLimitsSwitch, "1")
	for _, tc := range []struct {
		cpus float64
		mem  int64
		jobs int
	}{
		{2, 4 << 30, 2}, {8, 4 << 30, 2}, {8, 3 << 30, 2},
		{1.5, 4 << 30, 1}, {.5, 4 << 30, 1}, {8, 1 << 30, 1},
	} {
		if got := (ResourceLimits{CPUs: tc.cpus, MemoryBytes: tc.mem}).ParallelJobs(); got != tc.jobs {
			t.Fatalf("cpus=%g mem=%d: jobs=%d, want %d", tc.cpus, tc.mem, got, tc.jobs)
		}
	}
	s := Snapshot{OS: "linux", CPUs: 2, EffectiveCPUs: 2, MemTotalBytes: 4 << 30, MemoryLimited: true}
	with := s.RenderCompact(1800)
	s.MemoryLimited = false
	without := s.RenderCompact(1800)
	want := "Build/test with at most 2 parallel jobs; lower after an out-of-memory kill.\n"
	if !strings.Contains(with, want) || strings.Contains(without, "parallel jobs") {
		t.Fatalf("guidance: %q / %q", with, without)
	}
	// Keep the advice near the resource facts and measure its full byte cost.
	if added := len(with) - len(without); added != len(want) || added > 80 {
		t.Fatalf("guidance adds %d bytes, want %d (cap 80)", added, len(want))
	}
	if !strings.Contains(s.RenderCompact(300), "cpus=2") {
		t.Fatal("compact facts missing")
	}
	s.MemoryLimited = true
	t.Setenv(EffectiveLimitsSwitch, "off")
	if strings.Contains(s.RenderCompact(1800), "parallel jobs") {
		t.Fatal("disabled switch still adds guidance")
	}
}
