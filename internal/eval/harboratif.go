package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HarborATIFWriter emits a Harbor-compatible ATIF trajectory.json at
// <logs>/trajectory.json (Section 23.2): the installed-route verifier
// audit reads this file with Harbor's own Trajectory validator, which
// accepts ATIF-v1.0 through v1.8 and defaults to v1.8. The harness
// keeps its richer custom trajectory under excalibur/ for analysis;
// this file carries the minimal valid Harbor shape so the pinned
// Harbor (0.25.0) validator accepts every trial.
//
// Minimal valid shape (verified against harbor 0.25.0 pydantic models):
//
//	{"schema_version":"ATIF-v1.8",
//	 "agent":{"name":"excalibur","version":"...","model_name":"..."},
//	 "steps":[{"step_id":1,"source":"user","message":"..."}],
//	 "final_metrics":{...}}
//
// Every Append rewrites the file atomically so killed trials keep a
// valid partial trajectory.
type HarborATIFWriter struct {
	mu   sync.Mutex
	path string
	doc  harborTrajectory
}

type harborAgent struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	ModelName string `json:"model_name,omitempty"`
}

type harborStep struct {
	StepID    int    `json:"step_id"`
	Timestamp string `json:"timestamp,omitempty"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	ModelName string `json:"model_name,omitempty"`
}

type harborFinalMetrics struct {
	TotalPromptTokens     *int64   `json:"total_prompt_tokens,omitempty"`
	TotalCompletionTokens *int64   `json:"total_completion_tokens,omitempty"`
	TotalCachedTokens     *int64   `json:"total_cached_tokens,omitempty"`
	TotalCostUSD          *float64 `json:"total_cost_usd,omitempty"`
}

type harborTrajectory struct {
	SchemaVersion string              `json:"schema_version"`
	SessionID     string              `json:"session_id,omitempty"`
	Agent         harborAgent         `json:"agent"`
	Steps         []harborStep        `json:"steps"`
	FinalMetrics  *harborFinalMetrics `json:"final_metrics,omitempty"`
}

// NewHarborATIFWriter creates <logs>/trajectory.json with the header.
func NewHarborATIFWriter(logsDir, version, model string) (*HarborATIFWriter, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("eval: mkdir harbor atif: %w", err)
	}
	w := &HarborATIFWriter{
		path: filepath.Join(logsDir, "trajectory.json"),
		doc: harborTrajectory{
			SchemaVersion: "ATIF-v1.8",
			Agent:         harborAgent{Name: "excalibur", Version: version, ModelName: model},
		},
	}
	if err := w.flushLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

// AppendStep adds one step: source is system, user or agent.
func (w *HarborATIFWriter) AppendStep(source, message, model string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if source != "system" && source != "user" && source != "agent" {
		source = "agent"
	}
	if message == "" {
		message = "(empty)"
	}
	w.doc.Steps = append(w.doc.Steps, harborStep{
		StepID: len(w.doc.Steps) + 1, Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source: source, Message: message, ModelName: model,
	})
	return w.flushLocked()
}

// SetFinal records the closing metrics.
func (w *HarborATIFWriter) SetFinal(prompt, completion, cached int64, cost float64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, c, k, co := prompt, completion, cached, cost
	w.doc.FinalMetrics = &harborFinalMetrics{
		TotalPromptTokens: &p, TotalCompletionTokens: &c,
		TotalCachedTokens: &k, TotalCostUSD: &co,
	}
	return w.flushLocked()
}

func (w *HarborATIFWriter) flushLocked() error {
	if len(w.doc.Steps) == 0 {
		w.doc.Steps = []harborStep{{StepID: 1, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Source: "system", Message: "trial started"}}
	}
	buf, err := json.MarshalIndent(w.doc, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal harbor atif: %w", err)
	}
	buf = append(buf, '\n')
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("eval: write harbor atif tmp: %w", err)
	}
	if err := os.Rename(tmp, w.path); err != nil {
		return fmt.Errorf("eval: rename harbor atif: %w", err)
	}
	return nil
}

// Path returns the trajectory path.
func (w *HarborATIFWriter) Path() string { return w.path }
