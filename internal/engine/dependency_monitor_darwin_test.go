//go:build darwin

package engine

import "testing"

func TestDarwinDependencyMonitorIsIncomplete(t *testing.T) {
	monitor := startDependencyMonitor(t.TempDir())
	if monitor != nil {
		t.Fatal("read tracking must remain unavailable")
	}
	paths, complete := monitor.finish()
	if paths != nil || complete {
		t.Fatalf("unsupported tracking claimed completeness: %v %v", paths, complete)
	}
}
