package daemon

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func clientFeatureEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "false", "off":
		return false
	}
	return true
}

func usageSummaryOp(ctx context.Context, metrics *MetricsStore, durable *telemetry.SQLStore, params json.RawMessage) (any, error) {
	if !clientFeatureEnabled("EXCALIBUR_USAGE_SUMMARY") {
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "usage.summary is disabled"}
	}
	var p api.UsageSummaryParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil || string(params) == "null" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "invalid usage.summary parameters"}
		}
	}
	if p.Bucket != "" && p.Bucket != "hour" && p.Bucket != "day" || p.Since != nil && p.Until != nil && !p.Since.Before(*p.Until) {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "expected hour/day bucket and since before until"}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshots := []telemetry.Snapshot{}
	failures := map[string]int64{}
	if durable != nil {
		var err error
		snapshots, err = durable.UsageSnapshots(ctx)
		if err != nil {
			return nil, err
		}
		failures, err = durable.UsageFailures(ctx, p)
		if err != nil {
			return nil, err
		}
	}
	if metrics != nil {
		metrics.mu.Lock()
		for _, r := range metrics.rows {
			s := r.Snapshot
			if s.StartedAt.IsZero() {
				s.StartedAt = r.At
			}
			if s.TaskID == "" {
				s.TaskID = types.TaskID(r.TaskID)
			}
			snapshots = append(snapshots, s)
		}
		metrics.mu.Unlock()
	}
	return telemetry.AggregateUsage(snapshots, failures, p), nil
}
