package telemetry

import (
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// RenderStatus translates a deterministic run snapshot into
// plain-language sentences for users and for token/cost tracking UIs
// (Sections 15.6, 22.4). It uses only the snapshot: no model call.
func RenderStatus(s api.RunStatusResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s is %s", s.RunID, plainState(s.State))
	if s.CurrentStep != "" {
		fmt.Fprintf(&b, ", working on %s", s.CurrentStep)
	} else if s.PlanTotal > 0 {
		fmt.Fprintf(&b, ", step %d of %d", s.PlanDone, s.PlanTotal)
	}
	if s.ToolInFlight != "" {
		fmt.Fprintf(&b, ". Right now it runs %s", s.ToolInFlight)
		if s.LiveOutput != "" {
			fmt.Fprintf(&b, " (latest output: %s)", truncate(s.LiveOutput, 160))
		} else if s.LastProgress != "" {
			fmt.Fprintf(&b, " (%s)", truncate(s.LastProgress, 160))
		}
	}
	if s.PlanTotal > 0 && s.CurrentStep == "" {
		fmt.Fprintf(&b, ". Plan progress %d of %d", s.PlanDone, s.PlanTotal)
	}
	if s.BudgetUsedPct > 0 {
		fmt.Fprintf(&b, ". Budget used %.0f%%", s.BudgetUsedPct*100)
	}
	if s.Reason != "" {
		fmt.Fprintf(&b, ". Note: %s", s.Reason)
	}
	b.WriteString(".")
	return b.String()
}

// RenderMetricsLine renders one metrics_query answer in plain language.
func RenderMetricsLine(m *TaskMetrics) string {
	snap := m.Snapshot()
	return fmt.Sprintf(
		"Task %s used %d input tokens (%d cached), %d output tokens, cost $%.4f, %d model calls, %d tool calls, %.0fs wall time, child peak %s.",
		snap.TaskID, snap.Usage.Input, snap.Usage.CacheRead+snap.Usage.CacheWrite,
		snap.Usage.Output, snap.CostUSD,
		sumCalls(snap.CallsByRole), sumCalls(snap.CallsByTool),
		snap.WallSeconds, formatBytes(snap.ChildPeakBytes),
	)
}

func plainState(s any) string {
	str := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmt.Sprintf("%v", s), "_", " "), "  ", " ")))
	if str == "" {
		return "in an unknown state"
	}
	return str
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func sumCalls(m map[string]int64) int64 {
	var total int64
	for _, v := range m {
		total += v
	}
	return total
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	f := float64(b)
	for _, suffix := range []string{"KB", "MB", "GB", "TB"} {
		f /= unit
		if f < unit || suffix == "TB" {
			return fmt.Sprintf("%.1f %s", f, suffix)
		}
	}
	return fmt.Sprintf("%d B", b)
}
