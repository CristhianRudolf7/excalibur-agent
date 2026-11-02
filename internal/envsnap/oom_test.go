package envsnap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOOMWatchCounterEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		available, killed   bool
	}{
		{"increase", "oom 9\noom_kill 2\n", "oom 10\noom_kill 3\n", true, true},
		{"unchanged", "oom_kill 2\n", "oom_kill 2\n", true, false},
		{"reset", "oom_kill 2\n", "oom_kill 0\n", true, false},
		{"oom without kill", "oom 2\noom_kill 0\n", "oom 3\noom_kill 0\n", true, false},
		{"unreadable after", "oom_kill 2\n", "", false, false},
		{"missing before", "", "oom_kill 3\n", false, false},
		{"malformed", "oom_kill invalid\n", "oom_kill 3\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EffectiveLimitsSwitch, "1")
			r := resourceFixture(t, map[string]string{
				"sys/fs/cgroup/work/job/memory.events": tc.before,
				"sys/fs/cgroup/work/job/memory.max":    "4294967296",
				"sys/fs/cgroup/work/job/cpu.max":       "200000 100000",
			})
			w := r.WatchOOM()
			if err := os.WriteFile(filepath.Join(r.SysRoot, "fs/cgroup/work/job/memory.events"), []byte(tc.after), 0644); err != nil {
				t.Fatal(err)
			}
			o := w.Finish()
			if o.Killed != tc.killed || o.CounterAvailable != tc.available || o.MemoryBytes != 4<<30 || o.RetryJobs != 1 {
				t.Fatalf("observation: %+v", o)
			}
		})
	}
}

func TestOOMWatchV1Fallback(t *testing.T) {
	for _, kills := range []bool{true, false} {
		t.Setenv(EffectiveLimitsSwitch, "1")
		control := "oom_kill_disable 0\nunder_oom 0\n"
		if kills {
			control += "oom_kill 7\n"
		}
		r := resourceFixture(t, map[string]string{
			"proc/self/cgroup":                               "2:memory:/job\n",
			"proc/self/mountinfo":                            "",
			"sys/fs/cgroup/memory/job/memory.oom_control":    control,
			"sys/fs/cgroup/memory/job/memory.failcnt":        "9\n",
			"sys/fs/cgroup/memory/job/memory.limit_in_bytes": "2147483648",
		})
		w := r.WatchOOM()
		for file, value := range map[string]string{"memory.oom_control": control + "\noom_kill 8\n", "memory.failcnt": "10\n"} {
			if file == "memory.oom_control" && kills {
				value = "oom_kill 8\n"
			}
			if err := os.WriteFile(filepath.Join(r.SysRoot, "fs/cgroup/memory/job", file), []byte(value), 0644); err != nil {
				t.Fatal(err)
			}
		}
		o := w.Finish()
		if o.Killed != kills || o.Pressure == kills || o.CounterAvailable != kills || o.MemoryBytes != 2<<30 {
			t.Fatalf("v1 kills=%v: %+v", kills, o)
		}
	}
}

func TestOOMWatchSwitch(t *testing.T) {
	t.Setenv(EffectiveLimitsSwitch, "off")
	if w := (ResourceReader{}).WatchOOM(); w != nil || w.Finish().Killed {
		t.Fatal("disabled watch")
	}
}

func TestOOMWatchUsesNearestCounter(t *testing.T) {
	t.Setenv(EffectiveLimitsSwitch, "1")
	r := resourceFixture(t, map[string]string{
		"sys/fs/cgroup/work/job/memory.events": "oom_kill 0\n",
		"sys/fs/cgroup/work/memory.events":     "oom_kill 0\n",
	})
	w := r.WatchOOM()
	if err := os.WriteFile(filepath.Join(r.SysRoot, "fs/cgroup/work/memory.events"), []byte("oom_kill 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if w.Finish().Killed {
		t.Fatal("unrelated ancestor events must not override the readable command cgroup counter")
	}
}
