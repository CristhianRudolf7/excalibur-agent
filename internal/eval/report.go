package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

// TrialOutcome is the minimal per-trial record aggregated by eval
// report (Appendix F): identifiers, reward, failure category, token
// usage, cost, timings and resource peaks. Reward follows Harbor: a
// trial passes when reward > 0; errored trials count as 0.
type TrialOutcome struct {
	TrialID      string    `json:"trial_id"`
	TaskID       string    `json:"task_id"`
	TaskDigest   string    `json:"task_digest"`
	Benchmark    string    `json:"benchmark"`
	Reward       float64   `json:"reward"`
	Failed       bool      `json:"failed"`
	FailCategory string    `json:"fail_category,omitempty"`
	At           time.Time `json:"at"`

	InputTokens  int64   `json:"input_tokens"`
	CacheRead    int64   `json:"cache_read"`
	CacheWrite   int64   `json:"cache_write"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	WallSeconds  float64 `json:"wall_seconds"`
	ModelSeconds float64 `json:"model_seconds"`
	ToolSeconds  float64 `json:"tool_seconds"`

	ChildPeakBytes   uint64  `json:"child_peak_bytes"`
	HarnessPeakBytes uint64  `json:"harness_peak_bytes"`
	CPUSeconds       float64 `json:"cpu_seconds"`
	ModelRetries     int     `json:"model_retries"`
	ToolRetries      int     `json:"tool_retries"`
	ModelCalls       int64   `json:"model_calls"`
	ToolCalls        int64   `json:"tool_calls"`
	Subagents        int     `json:"subagents_created"`
	Compactions      int     `json:"compactions"`
	MaskBatches      int     `json:"mask_batches"`
	LoopsDetected    int     `json:"loops_detected"`
	GateAttempts     int     `json:"gate_attempts"`
}

// Passed reports the Harbor rule: reward > 0 and not failed.
func (t TrialOutcome) Passed() bool { return !t.Failed && t.Reward > 0 }

// Failure categories of Appendix F.
const (
	FailVerifier = "verifier"
	FailTimeout  = "timeout"
	FailHarness  = "harness_error"
	FailCrash    = "crash"
	FailOOM      = "oom"
	FailBudget   = "budget_exhausted"
	FailBlocked  = "blocked"
	FailUnknown  = "unknown"
)

// Category normalizes a raw category string.
func Category(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FailVerifier, FailTimeout, FailHarness, FailCrash, FailOOM, FailBudget, FailBlocked:
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return FailUnknown
	}
}

// Report aggregates trial directories with the Section 23.7
// statistics: success with CI 95%, pass@k and pass^k, tokens and cost
// per success, and failure categories.
type Report struct {
	GeneratedAt      time.Time              `json:"generated_at"`
	Trials           int                    `json:"trials"`
	Tasks            int                    `json:"tasks"`
	Success          MeanCI                 `json:"success"`
	PassAtK          float64                `json:"pass_at_k"`
	PassPowK         float64                `json:"pass_pow_k"`
	K                int                    `json:"k"`
	TokensPerSuccess float64                `json:"tokens_per_success"`
	CostPerSuccess   float64                `json:"cost_per_success"`
	Failures         map[string]int         `json:"failures"`
	ByTask           map[string]TaskSummary `json:"by_task"`
}

// TaskSummary groups one task's trials.
type TaskSummary struct {
	Trials  int     `json:"trials"`
	Success int     `json:"success"`
	Rate    float64 `json:"rate"`
}

// TrialFromSnapshot builds the canonical trial.json record from one
// finished harness snapshot (Appendix F minimum per trial). Harness
// and container peaks stay separate; reward follows Harbor (reward >
// 0 passes, errored trials count 0).
func TrialFromSnapshot(snap telemetry.Snapshot, trialID, taskDigest, benchmark string, reward float64, failed bool, failCategory string) TrialOutcome {
	return TrialOutcome{
		TrialID: trialID, TaskID: string(snap.TaskID), TaskDigest: taskDigest,
		Benchmark: benchmark, Reward: reward, Failed: failed,
		FailCategory: Category(failCategory), At: snap.EndedAt,
		InputTokens:  snap.Usage.Input,
		CacheRead:    snap.Usage.CacheRead,
		CacheWrite:   snap.Usage.CacheWrite,
		OutputTokens: snap.Usage.Output + snap.Usage.Reasoning,
		CostUSD:      snap.CostUSD,
		WallSeconds:  snap.WallSeconds, ModelSeconds: snap.ModelSeconds, ToolSeconds: snap.ToolSeconds,
		ChildPeakBytes:   snap.ChildPeakBytes,
		HarnessPeakBytes: snap.DaemonRSSTotalBytes,
		CPUSeconds:       snap.CPUSeconds,
		ModelRetries:     snap.ModelRetries,
		ToolRetries:      snap.ToolRetries,
		ModelCalls:       sumValues(snap.CallsByRole),
		ToolCalls:        sumValues(snap.CallsByTool),
		Subagents:        snap.SubagentsCreated,
		Compactions:      snap.Compactions,
		MaskBatches:      snap.MaskBatches,
		LoopsDetected:    snap.LoopsCaught,
		GateAttempts:     snap.GateAttempts,
	}
}

func sumValues(m map[string]int64) int64 {
	var total int64
	for _, v := range m {
		total += v
	}
	return total
}

// LoadTrials reads every trial.json directly inside dir (one level).
// Subdirectories of a Harbor trial directory each holding trial.json
// are also accepted.
func LoadTrials(dir string) ([]TrialOutcome, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("eval: read trials dir: %w", err)
	}
	var out []TrialOutcome
	for _, e := range entries {
		var path string
		if e.IsDir() {
			path = filepath.Join(dir, e.Name(), "trial.json")
		} else if e.Name() == "trial.json" {
			path = filepath.Join(dir, e.Name())
		} else if strings.HasSuffix(e.Name(), ".json") {
			path = filepath.Join(dir, e.Name())
		} else {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if e.IsDir() {
				continue
			}
			return nil, fmt.Errorf("eval: read %s: %w", path, err)
		}
		var t TrialOutcome
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("eval: parse %s: %w", path, err)
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskID == out[j].TaskID {
			return out[i].TrialID < out[j].TrialID
		}
		return out[i].TaskID < out[j].TaskID
	})
	return out, nil
}

// Aggregate builds the report over trials. k is the pass@k horizon
// (default 5 when <= 0).
func Aggregate(trials []TrialOutcome, k int) (Report, error) {
	if len(trials) == 0 {
		return Report{}, fmt.Errorf("eval: no trials")
	}
	if k <= 0 {
		k = 5
	}
	byTask := map[string][]TrialOutcome{}
	for _, t := range trials {
		byTask[t.TaskID] = append(byTask[t.TaskID], t)
	}
	rates := make([]float64, 0, len(byTask))
	bySummary := map[string]TaskSummary{}
	var passSum, powSum float64
	var totalIn, totalOut int64
	var totalCost float64
	var successes int
	failures := map[string]int{}
	for task, ts := range byTask {
		var c int
		for _, t := range ts {
			if t.Passed() {
				c++
				successes++
				totalIn += t.InputTokens + t.CacheRead + t.CacheWrite
				totalOut += t.OutputTokens
				totalCost += t.CostUSD
			} else {
				failures[Category(t.FailCategory)]++
			}
		}
		bySummary[task] = TaskSummary{Trials: len(ts), Success: c, Rate: float64(c) / float64(len(ts))}
		rates = append(rates, float64(c)/float64(len(ts)))
		pk, err := PassAtK(len(ts), c, k)
		if err != nil {
			return Report{}, err
		}
		pw, err := PassPowK(len(ts), c, k)
		if err != nil {
			return Report{}, err
		}
		passSum += pk
		powSum += pw
	}
	ci, err := BootstrapCI(rates, 10000, 7)
	if err != nil {
		return Report{}, err
	}
	rep := Report{
		GeneratedAt: time.Now().UTC(),
		Trials:      len(trials),
		Tasks:       len(byTask),
		Success:     ci,
		PassAtK:     passSum / float64(len(byTask)),
		PassPowK:    powSum / float64(len(byTask)),
		K:           k,
		Failures:    failures,
		ByTask:      bySummary,
	}
	if successes > 0 {
		rep.TokensPerSuccess = float64(totalIn+totalOut) / float64(successes)
		rep.CostPerSuccess = totalCost / float64(successes)
	}
	return rep, nil
}

// WriteTrial writes one trial.json into a trial directory.
func WriteTrial(dir string, t TrialOutcome) error {
	buf, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal trial: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: mkdir trial: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trial.json"), append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: write trial: %w", err)
	}
	return nil
}

// WriteReport writes report.json into dir.
func WriteReport(dir string, rep Report) error {
	buf, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal report: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: mkdir report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: write report: %w", err)
	}
	return nil
}

// HashTrials returns the sha256 of the canonical trial list for the
// suite registry's deterministic-setup check (Section 23.8).
func HashTrials(trials []TrialOutcome) string {
	h := sha256.New()
	for _, t := range trials {
		fmt.Fprintf(h, "%s\t%s\t%v\n", t.TaskID, t.TrialID, t.Reward)
	}
	return hex.EncodeToString(h.Sum(nil))
}
