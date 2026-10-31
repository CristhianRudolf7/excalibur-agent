package eval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestTrialFromSnapshot(t *testing.T) {
	m := telemetry.NewTaskMetrics(types.NewTaskID(), types.NewRunID(), "2026-10-28")
	m.SetModel("openai", "g-test")
	m.Finish("completed")
	snap := m.Snapshot()
	tr := TrialFromSnapshot(snap, "trial-1", "digest-abc", "tb40", 1, false, "")
	if !tr.Passed() {
		t.Fatalf("passing trial not passed: %+v", tr)
	}
	if tr.TaskID != string(snap.TaskID) || tr.Benchmark != "tb40" {
		t.Fatalf("identity lost: %+v", tr)
	}
	if tr.HarnessPeakBytes != snap.DaemonRSSTotalBytes || tr.ChildPeakBytes != snap.ChildPeakBytes {
		t.Fatalf("peaks mixed: %+v vs %+v", tr, snap)
	}
	failed := TrialFromSnapshot(snap, "trial-2", "d", "tb40", 0, true, "TIMEOUT")
	if failed.Passed() || failed.FailCategory != FailTimeout {
		t.Fatalf("failure not normalized: %+v", failed)
	}
	if got := Category("bogus"); got != FailUnknown {
		t.Fatalf("unknown category: %s", got)
	}
}

func TestSmokeMetricsFeedAgentContext(t *testing.T) {
	dir := t.TempDir()
	r := SmokeRunner{Dir: dir, Profile: DefaultEvalProfile()}
	results, err := r.Run(DefaultSmokeTasks()[:1])
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].Passed {
		t.Fatalf("smoke failed: %s", results[0].Detail)
	}
	metrics := filepath.Join(dir, DefaultSmokeTasks()[0].Name, "metrics.json")
	ctx, err := FromMetricsFile(metrics)
	if err != nil {
		t.Fatalf("smoke metrics.json not AgentContext-compatible: %v", err)
	}
	_ = ctx
	if _, err := os.Stat(filepath.Join(dir, DefaultSmokeTasks()[0].Name, "trajectory.json")); err != nil {
		t.Fatalf("smoke trajectory missing: %v", err)
	}
}
