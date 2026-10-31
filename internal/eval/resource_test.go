package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// pilotResources mirrors the w8 pilot shape: mid-run rows with a real
// peak, then the engine "final" row, then the zero "final" row the
// old finishRun appended. The index must report the peak, not zero.
func writePilotResources(t *testing.T, exDir string) {
	t.Helper()
	rows := []ResourceSample{
		{HarnessRSS: 23142400, CPUSeconds: 0.16, Processes: 9},
		{HarnessRSS: 23621632, CPUSeconds: 0.36, Processes: 21, Note: "final"},
		{HarnessRSS: 0, CPUSeconds: 0, Processes: 1, Note: "final"},
	}
	toAny := func(rows []ResourceSample) []any {
		out := make([]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, r)
		}
		return out
	}
	writeJSONL(t, filepath.Join(exDir, "resources.jsonl"), toAny(rows))
}

func writeZeroSnapshot(t *testing.T, exDir string) {
	t.Helper()
	m := telemetry.NewTaskMetrics(types.NewTaskID(), types.NewRunID(), "v1")
	m.Finish("completed")
	snap := m.Snapshot()
	raw, _ := json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(exDir, "metrics.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIndexTrialPeakResources pins the resource-sampler fix: a trial
// whose snapshot holds zeros (sampler never fed metrics) still
// indexes the observed container peak from resources.jsonl, and the
// trailing zero final row cannot hide it.
func TestIndexTrialPeakResources(t *testing.T) {
	dir := t.TempDir()
	writeTrialDoc(t, dir, "terminal-bench/x", "x__1", 1, "")
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeZeroSnapshot(t, exDir)
	writePilotResources(t, exDir)
	row, err := IndexTrial(dir)
	if err != nil {
		t.Fatal(err)
	}
	if row.HarnessPeakRSS != 23621632 {
		t.Fatalf("harness peak = %d, want 23621632 (max, not last)", row.HarnessPeakRSS)
	}
	if row.PeakRSS != 23621632 {
		t.Fatalf("container peak = %d, want the harness max inside the container", row.PeakRSS)
	}
	if row.CPUSeconds != 0.36 {
		t.Fatalf("cpu = %v, want 0.36 (max of cumulative counters)", row.CPUSeconds)
	}
}

// TestIndexTrialAllZeroStaysZero: a trial that never reached the
// sampler keeps honest zeros instead of gaps.
func TestIndexTrialAllZeroStaysZero(t *testing.T) {
	dir := t.TempDir()
	writeTrialDoc(t, dir, "terminal-bench/x", "x__1", 0, "")
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeZeroSnapshot(t, exDir)
	writeJSONL(t, filepath.Join(exDir, "resources.jsonl"), []any{
		ResourceSample{Note: "final"},
	})
	row, err := IndexTrial(dir)
	if err != nil {
		t.Fatal(err)
	}
	if row.HarnessPeakRSS != 0 || row.PeakRSS != 0 || row.CPUSeconds != 0 {
		t.Fatalf("all-zero record must stay zero: %+v", row)
	}
}

// TestIndexTrialSnapshotPeakWins: when metrics carry a larger peak
// than any JSONL row (sampler fed metrics, rows pruned), the snapshot
// wins.
func TestIndexTrialSnapshotPeakWins(t *testing.T) {
	dir := t.TempDir()
	writeTrialDoc(t, dir, "terminal-bench/x", "x__1", 1, "")
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := telemetry.NewTaskMetrics(types.NewTaskID(), types.NewRunID(), "v1")
	m.ObserveDaemonPeak(64<<20, 1.5)
	m.Finish("completed")
	snap := m.Snapshot()
	raw, _ := json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(exDir, "metrics.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(exDir, "resources.jsonl"), []any{
		ResourceSample{HarnessRSS: 8 << 20, CPUSeconds: 0.2},
	})
	row, err := IndexTrial(dir)
	if err != nil {
		t.Fatal(err)
	}
	if row.HarnessPeakRSS != 64<<20 || row.CPUSeconds != 1.5 {
		t.Fatalf("snapshot peak lost: %+v", row)
	}
}

// TestIndexTrialChildPeak: container (child) peaks recorded via
// ChildPeakRSS flow into the container peak.
func TestIndexTrialChildPeak(t *testing.T) {
	dir := t.TempDir()
	writeTrialDoc(t, dir, "terminal-bench/x", "x__1", 1, "")
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeZeroSnapshot(t, exDir)
	writeJSONL(t, filepath.Join(exDir, "resources.jsonl"), []any{
		ResourceSample{HarnessRSS: 8 << 20, ChildPeakRSS: 96 << 20, CPUSeconds: 0.4},
	})
	row, err := IndexTrial(dir)
	if err != nil {
		t.Fatal(err)
	}
	if row.PeakRSS != 96<<20 {
		t.Fatalf("child peak lost: %+v", row)
	}
	if row.HarnessPeakRSS != 8<<20 {
		t.Fatalf("harness peak wrong: %+v", row)
	}
}
