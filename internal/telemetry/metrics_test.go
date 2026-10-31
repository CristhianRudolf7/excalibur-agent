package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestMetricsModelAndFile(t *testing.T) {
	m := NewTaskMetrics(types.NewTaskID(), types.NewRunID(), "2026-10-28")
	m.SetModel("openai", "g-test")
	m.ObserveModel(types.RoleExecutor, "g-test",
		providers.Usage{Input: 100, CacheRead: 20, Output: 10}, PriceTable{}, 0)
	m.Finish("completed")
	snap := m.Snapshot()
	if snap.Model != "g-test" || snap.Provider != "openai" {
		t.Fatalf("model not recorded: %+v", snap)
	}
	dir := t.TempDir()
	if err := WriteMetricsFile(dir, snap); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "metrics.json"))
	if err != nil || len(raw) == 0 {
		t.Fatalf("metrics.json missing: %v", err)
	}
	var back Snapshot
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Model != "g-test" || back.Usage.Input != 100 {
		t.Fatalf("roundtrip mismatch: %+v", back)
	}
}
