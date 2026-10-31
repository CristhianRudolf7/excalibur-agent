package telemetry

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TestObserveDaemonPeak pins the max semantics for cumulative /proc
// counters: repeated cumulative samples keep the peak (never the sum),
// so feeding every sampler tick cannot inflate CPU, and a single
// sample still records an honest peak instead of zero.
func TestObserveDaemonPeak(t *testing.T) {
	m := NewTaskMetrics(types.NewTaskID(), types.NewRunID(), "v1")
	m.ObserveDaemonPeak(20<<20, 0.16)
	m.ObserveDaemonPeak(23<<20, 0.36)
	m.ObserveDaemonPeak(21<<20, 0.30)
	snap := m.Snapshot()
	if snap.DaemonRSSTotalBytes != 23<<20 {
		t.Fatalf("rss peak = %d, want %d", snap.DaemonRSSTotalBytes, 23<<20)
	}
	if snap.CPUSeconds != 0.36 {
		t.Fatalf("cpu peak = %v, want 0.36 (max, not 0.82 sum)", snap.CPUSeconds)
	}
	// The delta-based ObserveDaemon path is untouched: one delta still
	// accumulates.
	m.ObserveDaemon(1<<20, 0.5)
	snap = m.Snapshot()
	if snap.DaemonRSSTotalBytes != 23<<20 {
		t.Fatalf("delta sample must not lower the peak: %d", snap.DaemonRSSTotalBytes)
	}
	if snap.CPUSeconds != 0.86 {
		t.Fatalf("delta cpu = %v, want 0.86", snap.CPUSeconds)
	}
}
