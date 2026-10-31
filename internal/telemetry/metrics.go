package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// PriceTable is the versioned per-model price list of Section 19.6.
// Prices are USD per million tokens.
type PriceTable struct {
	// Version pins the table (recorded in metrics and ATIF metadata).
	Version string `json:"version"`
	// Input, CachedRead, CachedWrite, Output are USD per 1M tokens.
	// Missing models fall back to Default* below.
	Prices map[string]ModelPrice `json:"prices"`
}

// ModelPrice holds one model's rates.
type ModelPrice struct {
	Input      float64 `json:"input_per_1m"`
	CachedRead float64 `json:"cached_read_per_1m"`
	Output     float64 `json:"output_per_1m"`
}

// DefaultPrice applies to unlisted models so cost is never silently zero.
var DefaultPrice = ModelPrice{Input: 3.0, CachedRead: 0.3, Output: 12.0}

// Cost computes the USD cost of usage under the table.
func (t PriceTable) Cost(model string, u providers.Usage) float64 {
	p, ok := t.Prices[model]
	if !ok {
		p = DefaultPrice
	}
	uncached := float64(u.Input) - float64(u.CacheRead) - float64(u.CacheWrite)
	if uncached < 0 {
		uncached = 0
	}
	return uncached/1e6*p.Input +
		float64(u.CacheRead)/1e6*p.CachedRead +
		float64(u.CacheWrite)/1e6*p.CachedRead +
		(float64(u.Output)+float64(u.Reasoning))/1e6*p.Output
}

// TaskMetrics accumulates the per-task metrics of Section 22.2 from
// the first version on: RAM, tokens, cost, calls, time, quality and
// efficiency. It is safe for concurrent use.
type TaskMetrics struct {
	mu sync.Mutex

	TaskID types.TaskID `json:"task_id"`
	RunID  types.RunID  `json:"run_id"`

	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`

	// Model and Provider name the evaluated model for the single-model
	// check of Section 23.2; empty means unrecorded.
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`

	// Tokens and cost.
	Usage      providers.Usage `json:"usage"`
	CostUSD    float64         `json:"cost_usd"`
	PriceTable string          `json:"price_table"`

	// Calls by role and by tool.
	CallsByRole map[string]int64 `json:"calls_by_role"`
	CallsByTool map[string]int64 `json:"calls_by_tool"`

	// Time split.
	ModelSeconds float64 `json:"model_seconds"`
	ToolSeconds  float64 `json:"tool_seconds"`
	WaitingSecs  float64 `json:"waiting_seconds"`
	WallSeconds  float64 `json:"wall_seconds"`

	// Resources: daemon delta/total and child peaks are kept separate
	// so harness RAM and container RAM never mix (Section 22.2).
	DaemonRSSDeltaBytes uint64  `json:"daemon_rss_delta_bytes"`
	DaemonRSSTotalBytes uint64  `json:"daemon_rss_total_bytes"`
	ChildPeakBytes      uint64  `json:"child_peak_bytes"`
	ChildOOMKills       int     `json:"child_oom_kills"`
	ChildPeakPIDs       int     `json:"child_peak_pids"`
	CPUSeconds          float64 `json:"cpu_seconds"`

	// Quality.
	FinalState        string `json:"final_state"`
	CriteriaSatisfied int    `json:"criteria_satisfied"`
	CriteriaTotal     int    `json:"criteria_total"`
	UserInterventions int    `json:"user_interventions"`
	Corrections       int    `json:"corrections"`

	// Efficiency.
	Compactions  int `json:"compactions"`
	MaskBatches  int `json:"mask_batches"`
	LoopsCaught  int `json:"loops_detected"`
	ModelRetries int `json:"model_retries"`
	ToolRetries  int `json:"tool_retries"`

	// Engine counters of Appendix F.
	SubagentsCreated int `json:"subagents_created"`
	MaxDepth         int `json:"max_depth"`
	Interruptions    int `json:"interruptions"`
	GateAttempts     int `json:"gate_attempts"`
}

// Snapshot is an exported copy of TaskMetrics without the lock, for
// export to metrics.json and AgentContext.
type Snapshot struct {
	TaskID types.TaskID `json:"task_id"`
	RunID  types.RunID  `json:"run_id"`

	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`

	// Model and Provider name the evaluated model (Section 23.2).
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`

	// Tokens and cost.
	Usage      providers.Usage `json:"usage"`
	CostUSD    float64         `json:"cost_usd"`
	PriceTable string          `json:"price_table"`

	// Calls by role and by tool.
	CallsByRole map[string]int64 `json:"calls_by_role"`
	CallsByTool map[string]int64 `json:"calls_by_tool"`

	// Time split.
	ModelSeconds float64 `json:"model_seconds"`
	ToolSeconds  float64 `json:"tool_seconds"`
	WaitingSecs  float64 `json:"waiting_seconds"`
	WallSeconds  float64 `json:"wall_seconds"`

	// Resources: daemon delta/total and child peaks are kept separate
	// so harness RAM and container RAM never mix (Section 22.2).
	DaemonRSSDeltaBytes uint64  `json:"daemon_rss_delta_bytes"`
	DaemonRSSTotalBytes uint64  `json:"daemon_rss_total_bytes"`
	ChildPeakBytes      uint64  `json:"child_peak_bytes"`
	ChildOOMKills       int     `json:"child_oom_kills"`
	ChildPeakPIDs       int     `json:"child_peak_pids"`
	CPUSeconds          float64 `json:"cpu_seconds"`

	// Quality.
	FinalState        string `json:"final_state"`
	CriteriaSatisfied int    `json:"criteria_satisfied"`
	CriteriaTotal     int    `json:"criteria_total"`
	UserInterventions int    `json:"user_interventions"`
	Corrections       int    `json:"corrections"`

	// Efficiency.
	Compactions  int `json:"compactions"`
	MaskBatches  int `json:"mask_batches"`
	LoopsCaught  int `json:"loops_detected"`
	ModelRetries int `json:"model_retries"`
	ToolRetries  int `json:"tool_retries"`

	// Engine counters of Appendix F.
	SubagentsCreated int `json:"subagents_created"`
	MaxDepth         int `json:"max_depth"`
	Interruptions    int `json:"interruptions"`
	GateAttempts     int `json:"gate_attempts"`
}

// CacheHitRate is cached input over total input.
func (s Snapshot) CacheHitRate() float64 {
	if s.Usage.Input == 0 {
		return 0
	}
	return float64(s.Usage.CacheRead+s.Usage.CacheWrite) / float64(s.Usage.Input)
}

// NewTaskMetrics starts accumulation for one task run.
func NewTaskMetrics(taskID types.TaskID, runID types.RunID, priceVersion string) *TaskMetrics {
	return &TaskMetrics{
		TaskID:      taskID,
		RunID:       runID,
		StartedAt:   time.Now().UTC(),
		PriceTable:  priceVersion,
		CallsByRole: map[string]int64{},
		CallsByTool: map[string]int64{},
	}
}

// ObserveModel records one model call with its usage, role and cost.
func (m *TaskMetrics) ObserveModel(role types.ModelRole, model string, u providers.Usage, prices PriceTable, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Usage.Add(u)
	m.CostUSD += prices.Cost(model, u)
	m.CallsByRole[string(role)]++
	m.ModelSeconds += latency.Seconds()
}

// ObserveTool records one tool call.
func (m *TaskMetrics) ObserveTool(tool string, latency time.Duration, isRetry bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallsByTool[tool]++
	m.ToolSeconds += latency.Seconds()
	if isRetry {
		m.ToolRetries++
	}
}

// ObserveWait records time blocked waiting for the user.
func (m *TaskMetrics) ObserveWait(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WaitingSecs += d.Seconds()
}

// ObserveDaemon records one daemon RSS/CPU sample from the resource
// sampler. Child peaks stay untouched: they arrive from the cgroup v2
// memory.peak counter via ObserveResources, so harness RAM and
// container RAM never mix (Section 22.2).
//
// NOTE: cpuSecs here must be a per-interval DELTA (the Sampler feeds
// deltas); cumulative /proc counters must go through
// ObserveDaemonPeak instead, which keeps the max.
func (m *TaskMetrics) ObserveDaemon(rssBytes uint64, cpuSecs float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rssBytes > m.DaemonRSSTotalBytes {
		m.DaemonRSSTotalBytes = rssBytes
	}
	m.CPUSeconds += cpuSecs
}

// ObserveDaemonPeak records one cumulative resource observation (for
// example a direct /proc sample): RSS keeps the max and CPU keeps the
// max of the cumulative process time. It exists because the exec
// sampler reads cumulative utime+stime counters, which must never be
// summed across ticks (summing cumulative counters inflates CPU).
// Short and killed trials, which may see a single sample, still
// record an honest peak instead of zero (Appendix F minimums).
func (m *TaskMetrics) ObserveDaemonPeak(rssBytes uint64, cpuTotalSecs float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rssBytes > m.DaemonRSSTotalBytes {
		m.DaemonRSSTotalBytes = rssBytes
	}
	if cpuTotalSecs > m.CPUSeconds {
		m.CPUSeconds = cpuTotalSecs
	}
}

// ObserveResources records one resource sample: daemon RSS and child peaks.
func (m *TaskMetrics) ObserveResources(daemonDelta, daemonTotal, childPeak uint64, oomKills, pids int, cpuSecs float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if childPeak > m.ChildPeakBytes {
		m.ChildPeakBytes = childPeak
	}
	if daemonTotal > m.DaemonRSSTotalBytes {
		m.DaemonRSSTotalBytes = daemonTotal
	}
	m.DaemonRSSDeltaBytes = daemonDelta
	m.ChildOOMKills += oomKills
	if pids > m.ChildPeakPIDs {
		m.ChildPeakPIDs = pids
	}
	m.CPUSeconds += cpuSecs
}

// ObserveEfficiency records one background counter event.
func (m *TaskMetrics) ObserveEfficiency(compaction, mask, loop, modelRetry, subagent bool, depth int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if compaction {
		m.Compactions++
	}
	if mask {
		m.MaskBatches++
	}
	if loop {
		m.LoopsCaught++
	}
	if modelRetry {
		m.ModelRetries++
	}
	if subagent {
		m.SubagentsCreated++
	}
	if depth > m.MaxDepth {
		m.MaxDepth = depth
	}
}

// AddGateAttempts records n completion-gate evaluations at once (the
// engine reports its total after the run; the index prefers this over
// counting gate_attempts.jsonl rows, which carry one extra final row).
func (m *TaskMetrics) AddGateAttempts(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GateAttempts += n
}

// AddLoopDetections records n loop-detector hits at once (same
// batching rationale as AddGateAttempts).
func (m *TaskMetrics) AddLoopDetections(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoopsCaught += n
}

// Finish closes the wall clock and records the final state.
func (m *TaskMetrics) Finish(state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EndedAt = time.Now().UTC()
	m.FinalState = state
	m.WallSeconds = m.EndedAt.Sub(m.StartedAt).Seconds()
}

// SetModel records the evaluated provider and model for the
// single-model check of Section 23.2.
func (m *TaskMetrics) SetModel(provider, model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Provider = provider
	m.Model = model
}

// Snapshot returns a copy for export (metrics.json, AgentContext).
func (m *TaskMetrics) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		TaskID: m.TaskID, RunID: m.RunID,
		StartedAt: m.StartedAt, EndedAt: m.EndedAt,
		Model: m.Model, Provider: m.Provider,
		Usage: m.Usage, CostUSD: m.CostUSD, PriceTable: m.PriceTable,
		CallsByRole: copyMap(m.CallsByRole), CallsByTool: copyMap(m.CallsByTool),
		ModelSeconds: m.ModelSeconds, ToolSeconds: m.ToolSeconds,
		WaitingSecs: m.WaitingSecs, WallSeconds: m.WallSeconds,
		DaemonRSSDeltaBytes: m.DaemonRSSDeltaBytes, DaemonRSSTotalBytes: m.DaemonRSSTotalBytes,
		ChildPeakBytes: m.ChildPeakBytes, ChildOOMKills: m.ChildOOMKills,
		ChildPeakPIDs: m.ChildPeakPIDs, CPUSeconds: m.CPUSeconds,
		FinalState: m.FinalState, CriteriaSatisfied: m.CriteriaSatisfied,
		CriteriaTotal: m.CriteriaTotal, UserInterventions: m.UserInterventions,
		Corrections: m.Corrections,
		Compactions: m.Compactions, MaskBatches: m.MaskBatches,
		LoopsCaught: m.LoopsCaught, ModelRetries: m.ModelRetries,
		ToolRetries:      m.ToolRetries,
		SubagentsCreated: m.SubagentsCreated, MaxDepth: m.MaxDepth,
		Interruptions: m.Interruptions, GateAttempts: m.GateAttempts,
	}
}

func copyMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// WriteMetricsFile writes snap as metrics.json into dir: the canonical
// file the Harbor adapter reads for AgentContext (Section 23.2). The
// write is atomic (temp + rename) and carries no secrets.
func WriteMetricsFile(dir string, snap Snapshot) error {
	buf, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("telemetry: marshal metrics: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("telemetry: mkdir metrics: %w", err)
	}
	tmp := filepath.Join(dir, "metrics.json.tmp")
	if err := os.WriteFile(tmp, append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("telemetry: write metrics tmp: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "metrics.json")); err != nil {
		return fmt.Errorf("telemetry: rename metrics: %w", err)
	}
	return nil
}

// Validate checks internal consistency: cached tokens are subsets of
// input and counters are non-negative (Section 22.6).
func (m *TaskMetrics) Validate() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Usage.CacheRead+m.Usage.CacheWrite > m.Usage.Input {
		return fmt.Errorf("telemetry: cached %d+%d exceeds input %d",
			m.Usage.CacheRead, m.Usage.CacheWrite, m.Usage.Input)
	}
	for k, v := range m.CallsByRole {
		if v < 0 {
			return fmt.Errorf("telemetry: negative call count for role %q", k)
		}
	}
	for k, v := range m.CallsByTool {
		if v < 0 {
			return fmt.Errorf("telemetry: negative call count for tool %q", k)
		}
	}
	return nil
}
