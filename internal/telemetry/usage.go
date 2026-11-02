package telemetry

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// UsageSnapshots reads the retained metrics snapshots. The existing table keeps
// the latest snapshot per task, rather than a complete history of attempts.
func (s *SQLStore) UsageSnapshots(ctx context.Context) ([]Snapshot, error) {
	out := []Snapshot{}
	err := s.R.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT snapshot_json,updated_ms FROM task_metrics ORDER BY updated_ms`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			var updated int64
			if err := rows.Scan(&raw, &updated); err != nil {
				return err
			}
			var snap Snapshot
			if err := json.Unmarshal([]byte(raw), &snap); err != nil {
				return err
			}
			if snap.StartedAt.IsZero() {
				snap.StartedAt = fromMs(updated)
			}
			out = append(out, snap)
		}
		return rows.Err()
	})
	return out, err
}

// UsageFailures counts only recorded error spans in the selected time window.
// It does not infer failures from retries or unsuccessful task outcomes.
func (s *SQLStore) UsageFailures(ctx context.Context, p api.UsageSummaryParams) (map[string]int64, error) {
	out := map[string]int64{}
	err := s.R.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		query := `SELECT attrs_json FROM spans WHERE name=? AND status='error'`
		args := []any{SpanExecuteTool}
		if p.Since != nil {
			query += ` AND started_ms>=?`
			args = append(args, ms(*p.Since))
		}
		if p.Until != nil {
			query += ` AND started_ms<?`
			args = append(args, ms(*p.Until))
		}
		rows, err := q.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var attrs map[string]any
			if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
				return err
			}
			if tool, ok := attrs["gen_ai.tool.name"].(string); ok && tool != "" {
				out[tool]++
			}
		}
		return rows.Err()
	})
	return out, err
}

// AggregateUsage uses stored counts and costs without repricing or estimating.
// Run snapshots are assigned to their UTC start bucket. Repeated snapshots of
// the same run replace previous values instead of counting cumulative totals twice.
func AggregateUsage(snapshots []Snapshot, failures map[string]int64, p api.UsageSummaryParams) api.UsageSummaryResult {
	out := api.UsageSummaryResult{ByBucket: []api.UsageBucket{}, ByModel: []api.UsageModel{}, ByTool: []api.UsageTool{}, RecentRuns: []api.UsageRun{}}
	latest := map[string]Snapshot{}
	for _, s := range snapshots {
		key := "run:" + string(s.RunID)
		if s.RunID == "" {
			key = "task:" + string(s.TaskID)
		}
		latest[key] = s
	}
	buckets := map[time.Time]api.UsageCounters{}
	models := map[string]api.UsageModel{}
	tools := map[string]api.UsageTool{}
	keys := make([]string, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s := latest[key]
		start := s.StartedAt.UTC()
		if p.Since != nil && start.Before(*p.Since) || p.Until != nil && !start.Before(*p.Until) {
			continue
		}
		c := api.UsageCounters{InputTokens: s.Usage.Input, CachedTokens: s.Usage.CacheRead + s.Usage.CacheWrite, OutputTokens: s.Usage.Output, CostUSD: s.CostUSD}
		for name, calls := range s.CallsByTool {
			c.ToolCalls += calls
			t := tools[name]
			t.Tool = name
			t.Calls += calls
			tools[name] = t
		}
		addCounters(&out.Totals.UsageCounters, c)
		out.Totals.Runs++
		var bucket time.Time
		if p.Bucket == "hour" {
			bucket = start.Truncate(time.Hour)
		} else {
			bucket = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		}
		b := buckets[bucket]
		addCounters(&b, c)
		buckets[bucket] = b
		if s.Model != "" {
			m := models[s.Model]
			m.Model = s.Model
			m.InputTokens += c.InputTokens
			m.OutputTokens += c.OutputTokens
			m.CostUSD += c.CostUSD
			models[s.Model] = m
		}
		duration := int64(s.WallSeconds * 1000)
		if !s.EndedAt.IsZero() && !s.StartedAt.IsZero() {
			duration = s.EndedAt.Sub(s.StartedAt).Milliseconds()
		}
		out.RecentRuns = append(out.RecentRuns, api.UsageRun{RunID: string(s.RunID), StartedAt: start, DurationMS: duration, InputTokens: c.InputTokens, OutputTokens: c.OutputTokens, CostUSD: c.CostUSD, ToolCalls: c.ToolCalls, Status: s.FinalState})
	}
	for tool, n := range failures {
		t := tools[tool]
		t.Tool = tool
		t.Failures = n
		tools[tool] = t
	}
	for start, c := range buckets {
		out.ByBucket = append(out.ByBucket, api.UsageBucket{Start: start, UsageCounters: c})
	}
	for _, m := range models {
		out.ByModel = append(out.ByModel, m)
	}
	for _, t := range tools {
		out.ByTool = append(out.ByTool, t)
	}
	sort.Slice(out.ByBucket, func(i, j int) bool { return out.ByBucket[i].Start.Before(out.ByBucket[j].Start) })
	sort.Slice(out.ByModel, func(i, j int) bool { return out.ByModel[i].Model < out.ByModel[j].Model })
	sort.Slice(out.ByTool, func(i, j int) bool { return out.ByTool[i].Tool < out.ByTool[j].Tool })
	sort.SliceStable(out.RecentRuns, func(i, j int) bool {
		if out.RecentRuns[i].StartedAt.Equal(out.RecentRuns[j].StartedAt) {
			return out.RecentRuns[i].RunID < out.RecentRuns[j].RunID
		}
		return out.RecentRuns[i].StartedAt.After(out.RecentRuns[j].StartedAt)
	})
	if len(out.RecentRuns) > 50 {
		out.RecentRuns = out.RecentRuns[:50]
	}
	return out
}

func addCounters(dst *api.UsageCounters, src api.UsageCounters) {
	dst.InputTokens += src.InputTokens
	dst.CachedTokens += src.CachedTokens
	dst.OutputTokens += src.OutputTokens
	dst.CostUSD += src.CostUSD
	dst.ToolCalls += src.ToolCalls
}
