package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AgentContext is the Harbor usage record filled from
// /logs/agent/excalibur/metrics.json on the installed route
// (Section 23.2): input incl. cached, cache read/write, output and
// cost, per model.
type AgentContext struct {
	InputTokens  int64   `json:"input_tokens"`
	CacheRead    int64   `json:"cache_read_tokens"`
	CacheWrite   int64   `json:"cache_write_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Model        string  `json:"model"`
}

// FromMetricsFile reads the metrics snapshot written by the harness
// and builds the Harbor AgentContext. Missing files are an error: the
// adapter must fail the trial loudly rather than report zero usage.
func FromMetricsFile(path string) (AgentContext, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AgentContext{}, fmt.Errorf("eval: read metrics %s: %w", path, err)
	}
	var snap struct {
		Usage struct {
			Input      int64 `json:"input"`
			CacheRead  int64 `json:"cache_read"`
			CacheWrite int64 `json:"cache_write"`
			Output     int64 `json:"output"`
			Reasoning  int64 `json:"reasoning"`
		} `json:"usage"`
		CostUSD float64 `json:"cost_usd"`
		Model   string  `json:"model"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return AgentContext{}, fmt.Errorf("eval: parse metrics %s: %w", path, err)
	}
	return AgentContext{
		InputTokens:  snap.Usage.Input,
		CacheRead:    snap.Usage.CacheRead,
		CacheWrite:   snap.Usage.CacheWrite,
		OutputTokens: snap.Usage.Output + snap.Usage.Reasoning,
		CostUSD:      snap.CostUSD,
		Model:        snap.Model,
	}, nil
}

// MetricsPath returns the canonical metrics location inside a Harbor
// trial log directory.
func MetricsPath(logsDir string) string {
	return filepath.Join(logsDir, "excalibur", "metrics.json")
}
