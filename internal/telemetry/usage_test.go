package telemetry

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestUsageFromSeededTelemetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	db := openTelemetryDB(t, path)
	s := OpenTelemetryStore(db)
	start := time.Date(2026, 10, 1, 1, 10, 0, 0, time.UTC)
	rows := []Snapshot{
		{TaskID: "task-one", RunID: "run-one", Model: "alpha", StartedAt: start, EndedAt: start.Add(2 * time.Second), Usage: providers.Usage{Input: 100, CacheRead: 20, CacheWrite: 5, Output: 12}, CostUSD: 0.25, CallsByTool: map[string]int64{"exec": 3}, FinalState: "completed"},
		{TaskID: "task-two", RunID: "run-two", Model: "beta", StartedAt: start.Add(time.Hour), WallSeconds: 3.5, Usage: providers.Usage{Input: 200, CacheRead: 40, Output: 24}, CostUSD: 0.5, CallsByTool: map[string]int64{"exec": 2, "read": 1}, FinalState: "failed"},
	}
	for _, snap := range rows {
		if err := s.SaveMetrics(snap); err != nil {
			t.Fatal(err)
		}
	}
	sp := NewTracer("trace", "run-two").StartTool(Span{}, "exec", "call")
	sp.StartedAt = start.Add(time.Hour)
	Finish(&sp, "error", nil)
	if err := s.AppendSpan(sp, "run-two"); err != nil {
		t.Fatal(err)
	}
	// Reopen the same DB: summary reads durable rows rather than bus history.
	reopened := OpenTelemetryStore(openTelemetryDB(t, path))
	snapshots, err := reopened.UsageSnapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	until := start.Add(2 * time.Hour)
	params := api.UsageSummaryParams{Since: &start, Until: &until, Bucket: "hour"}
	failures, err := reopened.UsageFailures(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	// Cumulative RAM snapshots of an existing run must not double count.
	snapshots = append(snapshots, rows[0])
	got := AggregateUsage(snapshots, failures, params)
	if got.Totals.Runs != 2 || got.Totals.InputTokens != 300 || got.Totals.CachedTokens != 65 || got.Totals.OutputTokens != 36 || got.Totals.CostUSD != 0.75 || got.Totals.ToolCalls != 6 {
		t.Fatalf("totals: %+v", got)
	}
	if len(got.ByBucket) != 2 || got.ByBucket[0].Start.Hour() != 1 || got.ByBucket[1].ToolCalls != 3 {
		t.Fatalf("buckets: %+v", got.ByBucket)
	}
	if len(got.ByModel) != 2 || got.ByModel[0].Model != "alpha" || got.ByModel[1].InputTokens != 200 {
		t.Fatalf("models: %+v", got.ByModel)
	}
	if len(got.ByTool) != 2 || got.ByTool[0].Calls != 5 || got.ByTool[0].Failures != 1 || got.ByTool[1].Failures != 0 {
		t.Fatalf("tools: %+v", got.ByTool)
	}
	if len(got.RecentRuns) != 2 || got.RecentRuns[0].RunID != "run-two" || got.RecentRuns[0].DurationMS != 3500 || got.RecentRuns[1].DurationMS != 2000 {
		t.Fatalf("runs: %+v", got.RecentRuns)
	}
	params.Until = &rows[1].StartedAt
	got = AggregateUsage(snapshots, nil, params)
	if got.Totals.Runs != 1 {
		t.Fatalf("until must be exclusive: %+v", got)
	}
	params = api.UsageSummaryParams{Bucket: "day"}
	got = AggregateUsage(snapshots, nil, params)
	if len(got.ByBucket) != 1 || got.ByBucket[0].Start.Hour() != 0 {
		t.Fatalf("day: %+v", got)
	}
}

func TestEmptyUsageAndRecentLimit(t *testing.T) {
	got := AggregateUsage(nil, nil, api.UsageSummaryParams{})
	if got.ByBucket == nil || got.ByModel == nil || got.ByTool == nil || got.RecentRuns == nil || got.Totals.Runs != 0 {
		t.Fatalf("empty: %+v", got)
	}
	start := time.Now().UTC()
	rows := []Snapshot{}
	for i := 0; i < 60; i++ {
		rows = append(rows, Snapshot{TaskID: types.TaskID(fmt.Sprintf("task-%d", i)), StartedAt: start.Add(time.Duration(i) * time.Hour)})
	}
	got = AggregateUsage(rows, nil, api.UsageSummaryParams{})
	if got.Totals.Runs != 60 || len(got.RecentRuns) != 50 {
		t.Fatal("recent limit truncated totals")
	}
}
