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
	TrialID      string  `json:"trial_id"`
	TaskID       string  `json:"task_id"`
	TaskDigest   string  `json:"task_digest"`
	Benchmark    string  `json:"benchmark"`
	Reward       float64 `json:"reward"`
	Failed       bool    `json:"failed"`
	FailCategory string  `json:"fail_category,omitempty"`
	// InfraReason names why the trial measured the infrastructure rather
	// than the agent (see TrialInfraReason). Infra trials are excluded from
	// the headline pass rate and reported separately.
	InfraReason string    `json:"infra_reason,omitempty"`
	At          time.Time `json:"at"`

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
	Interruptions    int     `json:"interruptions"`
}

// Passed reports the Harbor rule: reward > 0 and not failed.
func (t TrialOutcome) Passed() bool { return !t.Failed && t.Reward > 0 }

// Infra reports whether the trial is an infrastructure failure.
func (t TrialOutcome) Infra() bool { return t.InfraReason != "" }

// Failure categories of Appendix F.
const (
	FailVerifier      = "verifier"
	FailTimeout       = "timeout"
	FailHarness       = "harness_error"
	FailCrash         = "crash"
	FailOOM           = "oom"
	FailBudget        = "budget_exhausted"
	FailBlocked       = "blocked"
	FailUnknown       = "unknown"
	FailProviderQuota = "provider_quota_exhausted"
)

// Infrastructure reasons (TrialInfraReason). An infra trial did not measure
// the agent: the environment, the verifier, the provider or the harness
// lifecycle failed first.
const (
	InfraAgentInstall      = "agent_install_failed"
	InfraEnvironmentStart  = "environment_start_failed"
	InfraProviderBilling   = "provider_billing"
	InfraProviderError     = "provider_error"
	InfraVerifierNoResults = "verifier_no_results"
	InfraCancelled         = "cancelled_before_finish"
)

// Category normalizes a raw category string.
func Category(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FailVerifier, FailTimeout, FailHarness, FailCrash, FailOOM, FailBudget, FailBlocked, FailProviderQuota:
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return FailUnknown
	}
}

// Report aggregates trial directories with the Section 23.7
// statistics: success with CI 95%, pass@k and pass^k, tokens and cost
// per success, and failure categories.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	// Trials counts every trial, infra included. ScoredTrials excludes infra.
	Trials       int `json:"trials"`
	ScoredTrials int `json:"scored_trials"`
	InfraTrials  int `json:"infra_trials"`
	Tasks        int `json:"tasks"`
	// Success is the pass rate without infra trials (the headline rate).
	Success MeanCI `json:"success"`
	// SuccessWithInfra is the pass rate with every recorded reward included.
	SuccessWithInfra MeanCI                 `json:"success_with_infra"`
	PassAtK          float64                `json:"pass_at_k"`
	PassPowK         float64                `json:"pass_pow_k"`
	K                int                    `json:"k"`
	TokensPerSuccess float64                `json:"tokens_per_success"`
	CostPerSuccess   float64                `json:"cost_per_success"`
	Failures         map[string]int         `json:"failures"`
	InfraReasons     map[string]int         `json:"infra_reasons,omitempty"`
	ByTask           map[string]TaskSummary `json:"by_task"`
}

// TaskSummary groups one task's scored trials; Infra counts the excluded ones.
type TaskSummary struct {
	Trials  int     `json:"trials"`
	Success int     `json:"success"`
	Rate    float64 `json:"rate"`
	Infra   int     `json:"infra,omitempty"`
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
// (default 5 when <= 0). Infra trials are excluded from the headline rates,
// pass@k, pass^k and the per-task rates, and are counted in InfraReasons; the
// with-infra success rate includes every recorded reward so both rates are reported.
func Aggregate(trials []TrialOutcome, k int) (Report, error) {
	if len(trials) == 0 {
		return Report{}, fmt.Errorf("eval: no trials")
	}
	if k <= 0 {
		k = 5
	}
	rep := Report{
		GeneratedAt:  time.Now().UTC(),
		Trials:       len(trials),
		K:            k,
		Failures:     map[string]int{},
		InfraReasons: map[string]int{},
		ByTask:       map[string]TaskSummary{},
	}
	scored := map[string][]TrialOutcome{}
	all := map[string][]TrialOutcome{}
	var totalIn, totalOut int64
	var totalCost float64
	var successes int
	for _, t := range trials {
		all[t.TaskID] = append(all[t.TaskID], t)
		if t.Infra() {
			rep.InfraTrials++
			rep.InfraReasons[t.InfraReason]++
			continue
		}
		scored[t.TaskID] = append(scored[t.TaskID], t)
		if t.Passed() {
			successes++
			totalIn += t.InputTokens + t.CacheRead + t.CacheWrite
			totalOut += t.OutputTokens
			totalCost += t.CostUSD
		} else {
			rep.Failures[Category(t.FailCategory)]++
		}
	}
	rep.ScoredTrials = len(trials) - rep.InfraTrials
	rep.Tasks = len(all)
	if len(rep.InfraReasons) == 0 {
		rep.InfraReasons = nil
	}
	rates, passK, powK, err := taskRates(scored, k)
	if err != nil {
		return Report{}, err
	}
	if len(rates) > 0 {
		ci, err := BootstrapCI(rates, 10000, 7)
		if err != nil {
			return Report{}, err
		}
		rep.Success = ci
		rep.PassAtK = passK / float64(len(rates))
		rep.PassPowK = powK / float64(len(rates))
	}
	withInfra, _, _, err := taskRates(all, k)
	if err != nil {
		return Report{}, err
	}
	if len(withInfra) > 0 {
		ci, err := BootstrapCI(withInfra, 10000, 7)
		if err != nil {
			return Report{}, err
		}
		rep.SuccessWithInfra = ci
	}
	for task, ts := range all {
		sc := scored[task]
		var c, infra int
		for _, t := range ts {
			if t.Infra() {
				infra++
			}
		}
		for _, t := range sc {
			if t.Passed() {
				c++
			}
		}
		summary := TaskSummary{Trials: len(sc), Success: c, Infra: infra}
		if len(sc) > 0 {
			summary.Rate = float64(c) / float64(len(sc))
		}
		rep.ByTask[task] = summary
	}
	if successes > 0 {
		rep.TokensPerSuccess = float64(totalIn+totalOut) / float64(successes)
		rep.CostPerSuccess = totalCost / float64(successes)
	}
	return rep, nil
}

// taskRates computes per-task success rates and the pass@k and pass^k sums
// over the tasks that have at least one trial in groups. The caller divides
// the sums by the number of rates.
func taskRates(groups map[string][]TrialOutcome, k int) (rates []float64, passSum, powSum float64, err error) {
	for _, ts := range groups {
		if len(ts) == 0 {
			continue
		}
		var c int
		for _, t := range ts {
			if t.Passed() {
				c++
			}
		}
		rates = append(rates, float64(c)/float64(len(ts)))
		pk, err := PassAtK(len(ts), c, k)
		if err != nil {
			return nil, 0, 0, err
		}
		pw, err := PassPowK(len(ts), c, k)
		if err != nil {
			return nil, 0, 0, err
		}
		passSum += pk
		powSum += pw
	}
	return rates, passSum, powSum, nil
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
