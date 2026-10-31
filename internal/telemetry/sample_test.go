package telemetry

import (
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
)

func TestSamplerPollsDaemon(t *testing.T) {
	src := &platformfake.ResourceSampler{
		Next: platform.ResourceSample{RSSBytes: 8 << 20, CPUSeconds: 1.5, Processes: 3},
	}
	m := NewTaskMetrics("tsk_01sample000000000000000", "run_01sample00000000000000", "v")
	s := NewSampler(src, m, 0, 5*time.Millisecond)
	s.Start("telemetry-test")
	time.Sleep(25 * time.Millisecond)
	s.Stop()
	got := s.Samples()
	if len(got) == 0 {
		t.Fatal("sampler recorded no samples")
	}
	for _, smp := range got {
		if smp.DaemonRSS != 8<<20 || smp.Processes != 3 {
			t.Fatalf("sample not from platform source: %+v", smp)
		}
	}
	snap := m.Snapshot()
	if snap.DaemonRSSTotalBytes != 8<<20 {
		t.Fatalf("daemon total %d", snap.DaemonRSSTotalBytes)
	}
	if snap.CPUSeconds < 1.5 {
		t.Fatalf("cpu seconds %v", snap.CPUSeconds)
	}
	// Daemon sampling never fabricates a child peak: that arrives
	// from the cgroup counter via ObserveResources.
	if snap.ChildPeakBytes != 0 {
		t.Fatalf("child peak polluted by daemon sampler: %d", snap.ChildPeakBytes)
	}
	if len(src.Sampled) == 0 {
		t.Fatal("platform sampler never called")
	}
}
