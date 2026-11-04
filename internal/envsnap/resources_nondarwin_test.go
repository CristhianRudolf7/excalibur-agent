//go:build !darwin

package envsnap

import (
	"strings"
	"testing"
)

func TestNonDarwinResourceOutputUnchanged(t *testing.T) {
	t.Setenv(EffectiveLimitsSwitch, "1")
	r := resourceFixture(t, map[string]string{
		"sys/fs/cgroup/work/job/memory.max": "4294967296",
		"sys/fs/cgroup/work/job/cpu.max":    "150000 100000",
	})
	s := Snapshot{OS: "linux", Arch: "amd64"}
	s.applyResources(r.Limits())
	lines := strings.Split(s.RenderCompact(1800), "\n")
	want := "Environment snapshot (auto-detected at task start; verify with tools before relying on it):\n" +
		"os: linux arch=amd64 cpus=1.5 mem=4.0G (host cpus 8) (host mem 32GiB)\n" +
		"Build/test with at most 1 parallel job; lower after an out-of-memory kill."
	if got := strings.Join(lines[:3], "\n"); got != want {
		t.Fatalf("resource output changed:\n%s", got)
	}
}
