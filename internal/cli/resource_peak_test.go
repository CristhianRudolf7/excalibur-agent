package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TestExecResourcePeakRecorded pins the resource-sampler fix at the
// recorder level: even on the deterministic stub path (no engine, no
// sampler ticks) the trial records a nonzero harness/container peak
// from a live sample instead of zeros.
func TestExecResourcePeakRecorded(t *testing.T) {
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "do the thing", LogsDir: logs, Profile: ProfileEval,
		ForceOutcome: types.RunCompleted, ForceReason: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != ExitOK {
		t.Fatalf("exit = %d", out.ExitCode)
	}
	exDir := filepath.Join(logs, "excalibur")
	raw, err := os.ReadFile(filepath.Join(exDir, "resources.jsonl"))
	if err != nil {
		t.Fatalf("resources.jsonl missing: %v", err)
	}
	var peak uint64
	var cpu float64
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s eval.ResourceSample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		if s.HarnessRSS > peak {
			peak = s.HarnessRSS
		}
		if s.CPUSeconds > cpu {
			cpu = s.CPUSeconds
		}
	}
	if peak == 0 {
		t.Fatal("no nonzero harness RSS sample recorded")
	}
	// metrics.json carries the same peak (sampler feed, not zeros).
	mraw, err := os.ReadFile(eval.MetricsPath(logs))
	if err != nil {
		t.Fatalf("metrics.json missing: %v", err)
	}
	var snap struct {
		DaemonRSSTotalBytes uint64  `json:"daemon_rss_total_bytes"`
		CPUSeconds          float64 `json:"cpu_seconds"`
	}
	if err := json.Unmarshal(mraw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.DaemonRSSTotalBytes == 0 {
		t.Fatal("metrics daemon peak is zero")
	}
	if snap.CPUSeconds < 0 {
		t.Fatal("metrics cpu negative")
	}
	_ = cpu
}
