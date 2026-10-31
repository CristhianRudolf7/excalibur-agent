package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Pinned Harbor and dataset identities (Appendix F: fixed conditions).
// Harbor 0.25.0 == git commit fdba28fc411e47e8765420a1a063b4918fce3def
// (tag v0.25.0, verified 2026-10-31 via the GitHub tags API).
const (
	PinnedHarborVersion = "0.25.0"
	PinnedHarborCommit  = "fdba28fc411e47e8765420a1a063b4918fce3def"
)

// BenchDataset names one supported benchmark family.
type BenchDataset string

const (
	DatasetTB21 BenchDataset = "tb21"
	DatasetTB40 BenchDataset = "tb40"
)

// DatasetRef pins the Harbor dataset and digest (Appendix F).
type DatasetRef struct {
	HarborDataset string `json:"harbor_dataset"`
	Digest        string `json:"digest"`
	Revision      int    `json:"revision"`
	Version       string `json:"version"`
}

// PinnedDatasets is the frozen mapping bench run enforces.
func PinnedDatasets() map[BenchDataset]DatasetRef {
	return map[BenchDataset]DatasetRef{
		DatasetTB21: {
			HarborDataset: "terminal-bench/terminal-bench-2-1",
			Digest:        "sha256:7d7bdc1cbedad549fc1140404bd4dc45e5fd0ea7c4186773687d177ad3a0699a",
			Revision:      6,
			Version:       "2.0.2",
		},
		DatasetTB40: {
			HarborDataset: "terminal-bench/terminal-bench",
			Digest:        "sha256:39d9f44b40420cde8fdcc087579c0d72a7e14fa3656d603c3f0d22fb35e27732",
			Revision:      4,
			Version:       "4.0.0",
		},
	}
}

// RunManifest is the run-directory manifest (Appendix F minimums plus
// machine characteristics and the frozen conditions).
type RunManifest struct {
	ExcaliburCommit  string            `json:"excalibur_commit"`
	BinarySHA256     string            `json:"binary_sha256"`
	HarborVersion    string            `json:"harbor_version"`
	HarborCommit     string            `json:"harbor_commit"`
	Dataset          string            `json:"dataset"`
	DatasetDigest    string            `json:"dataset_digest"`
	Model            string            `json:"model"`
	Provider         string            `json:"provider"`
	Effort           string            `json:"effort"`
	K                int               `json:"k"`
	Budgets          map[string]string `json:"budgets"`
	Machine          map[string]string `json:"machine"`
	StartedAt        time.Time         `json:"started_at"`
	TimeoutChanged   bool              `json:"timeout_changed"`
	ResourcesChanged bool              `json:"resources_changed"`
	Uploaded         bool              `json:"uploaded"`
}

// MachineCharacteristics describes the host (Appendix F: machine
// characteristics).
func MachineCharacteristics() map[string]string {
	return map[string]string{
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
		"go":   runtime.Version(),
	}
}

// WriteManifest writes manifest.json into runDir.
func WriteManifest(runDir string, m RunManifest) error {
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal manifest: %w", err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("eval: mkdir run: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "manifest.json"), append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: write manifest: %w", err)
	}
	return nil
}

// BenchTrialIndex is one row of trials.jsonl (the run-level index).
type BenchTrialIndex struct {
	Task         string           `json:"task"`
	Trial        string           `json:"trial"`
	Reward       float64          `json:"reward"`
	FailCategory string           `json:"fail_category"`
	InputTokens  int64            `json:"input_tokens"`
	CacheRead    int64            `json:"cache_read"`
	CacheWrite   int64            `json:"cache_write"`
	OutputTokens int64            `json:"output_tokens"`
	CostUSD      float64          `json:"cost_usd"`
	WallSecs     float64          `json:"wall_seconds"`
	ModelSecs    float64          `json:"model_seconds"`
	ToolSecs     float64          `json:"tool_seconds"`
	PeakRSS      uint64           `json:"peak_rss_bytes"`
	Steps        int              `json:"steps"`
	ToolCalls    map[string]int64 `json:"tool_calls_by_tool,omitempty"`
}

// AppendTrialIndex appends one row to trials.jsonl.
func AppendTrialIndex(runDir string, row BenchTrialIndex) error {
	buf, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("eval: marshal trial index: %w", err)
	}
	buf = append(buf, '\n')
	f, err := os.OpenFile(filepath.Join(runDir, "trials.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("eval: open trials index: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("eval: write trials index: %w", err)
	}
	return f.Sync()
}

// RenderReportMarkdown renders the Section 23.7 report as Markdown.
func RenderReportMarkdown(rep Report, manifest RunManifest) string {
	out := "# Bench report\n\n"
	out += "## Run\n\n"
	out += "Dataset: " + manifest.Dataset + " (" + manifest.DatasetDigest + ")\n\n"
	out += "Model: " + manifest.Provider + "/" + manifest.Model + " (effort " + manifest.Effort + ")\n\n"
	out += "Excalibur: " + manifest.ExcaliburCommit + " binary " + manifest.BinarySHA256 + "\n\n"
	out += "Harbor: " + manifest.HarborVersion + " (" + manifest.HarborCommit + ")\n\n"
	out += "## Summary\n\n"
	out += "Trials: " + itoa(rep.Trials) + " tasks: " + itoa(rep.Tasks) + "\n\n"
	out += "Success: " + ftoa(rep.Success.Mean) + " 95%CI [" + ftoa(rep.Success.Low) + ", " + ftoa(rep.Success.High) + "]\n\n"
	out += "pass@k: " + ftoa(rep.PassAtK) + " (k=" + itoa(rep.K) + ") pass^k: " + ftoa(rep.PassPowK) + "\n\n"
	out += "Tokens/success: " + ftoa(rep.TokensPerSuccess) + " cost/success: $" + ftoa(rep.CostPerSuccess) + "\n\n"
	out += "## Failures\n\n"
	for cat, n := range rep.Failures {
		out += "- " + cat + ": " + itoa(n) + "\n"
	}
	out += "\n## Per task\n\n| task | trials | success | rate |\n|---|---|---|---|\n"
	for task, s := range rep.ByTask {
		out += "| " + task + " | " + itoa(s.Trials) + " | " + itoa(s.Success) + " | " + ftoa(s.Rate) + " |\n"
	}
	return out
}

func itoa(n int) string     { return fmt.Sprintf("%d", n) }
func ftoa(f float64) string { return fmt.Sprintf("%.4f", f) }
