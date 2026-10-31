package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

// Trial indexing for the run-level trials.jsonl (Section 23.2,
// Appendix F, defect w8-bench-fixes/3).
//
// Every Appendix F per-trial field is filled from the per-trial
// records, with a fixed precedence: the aggregated metrics.json
// snapshot first, the incremental JSONL records second (they stay
// valid when a trial is killed mid-write), and the Harbor
// agent_result only when neither has data. Zero stays zero for
// trials that never reached the model (their JSONL files are empty
// too), so harness failures keep honest zeros instead of gaps.
//
// Failure categories follow Appendix F: verifier failures are trials
// whose harness run completed but the verifier scored 0; harness
// errors are trials the harness never completed (provider/config
// failures, crashes, kills). The pilot misclassified the latter as
// verifier, hiding the TLS outage in the verifier bucket.

// IndexTrial builds one trials.jsonl row from a Harbor trial
// directory (the directory holding result.json). It also collects
// the verifier outputs and Harbor logs next to the excalibur
// records, so the run directory is self-contained for later
// failure analysis.
func IndexTrial(trialDir string) (BenchTrialIndex, error) {
	var row BenchTrialIndex
	doc, err := readTrialResult(trialDir)
	if err != nil {
		return row, err
	}
	row.Task = doc.TaskName
	if row.Task == "" {
		row.Task = filepath.Base(trialDir)
	}
	row.Trial = doc.TrialName
	if row.Trial == "" {
		row.Trial = filepath.Base(trialDir)
	}
	for _, v := range doc.VerifierResult.Rewards {
		row.Reward = v
		break
	}

	exDir := filepath.Join(trialDir, "agent", "excalibur")
	snap, hasSnap := readTrialSnapshot(exDir)
	modelAgg := aggregateModelCalls(exDir)
	toolAgg := aggregateToolCalls(exDir)
	resources := peakResourceSample(exDir)

	row.FailCategory = classifyIndexFailure(trialDir, doc, exDir, row.Reward)
	fillIndexUsage(&row, doc, snap, hasSnap, modelAgg, toolAgg)
	fillIndexTimes(&row, doc, snap, hasSnap, modelAgg, toolAgg, resources)
	fillIndexEfficiency(&row, snap, hasSnap, exDir, modelAgg, toolAgg)
	collectTrialArtifacts(trialDir, exDir)
	return row, nil
}

// trialResultDoc mirrors the Harbor result.json fields the index reads.
type trialResultDoc struct {
	TaskName    string `json:"task_name"`
	TrialName   string `json:"trial_name"`
	AgentResult struct {
		NInput  *int64   `json:"n_input_tokens"`
		NCache  *int64   `json:"n_cache_tokens"`
		NOutput *int64   `json:"n_output_tokens"`
		Cost    *float64 `json:"cost_usd"`
	} `json:"agent_result"`
	VerifierResult struct {
		Rewards map[string]float64 `json:"rewards"`
	} `json:"verifier_result"`
	ExceptionInfo *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		// Harbor 0.25.0 spells the same fields exception_type and
		// exception_message.
		ExType string `json:"exception_type"`
		ExMsg  string `json:"exception_message"`
	} `json:"exception_info"`
	AgentExecution *struct {
		StartedAt  string `json:"started_at"`
		FinishedAt string `json:"finished_at"`
	} `json:"agent_execution"`
}

func readTrialResult(trialDir string) (trialResultDoc, error) {
	var doc trialResultDoc
	raw, err := os.ReadFile(filepath.Join(trialDir, "result.json"))
	if err != nil {
		return doc, err
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return doc, err
	}
	return doc, nil
}

// harnessFinal mirrors agent/excalibur/final.json and the exec
// result.json outcome document.
type harnessFinal struct {
	State    string `json:"state"`
	Reason   string `json:"reason"`
	Summary  string `json:"summary"`
	ExitCode int    `json:"exit_code"`
}

func readHarnessFinal(exDir, trialDir string) (harnessFinal, bool) {
	for _, path := range []string{
		filepath.Join(exDir, "final.json"),
		filepath.Join(trialDir, "agent", "result.json"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var fin harnessFinal
		if err := json.Unmarshal(raw, &fin); err != nil {
			continue
		}
		if fin.State != "" {
			return fin, true
		}
	}
	return harnessFinal{}, false
}

// classifyIndexFailure maps a reward-0 trial to its Appendix F
// failure category from the Harbor exception and the harness
// records. Reward > 0 passes with no category.
func classifyIndexFailure(trialDir string, doc trialResultDoc, exDir string, reward float64) string {
	if doc.ExceptionInfo != nil {
		info := doc.ExceptionInfo
		msg := info.Type + " " + info.Message
		if info.ExType != "" || info.ExMsg != "" {
			msg = info.ExType + " " + info.ExMsg
		}
		return classifyException(msg)
	}
	if reward > 0 {
		return ""
	}
	fin, ok := readHarnessFinal(exDir, trialDir)
	if !ok {
		// No harness records at all: the harness never ran.
		return FailHarness
	}
	return classifyHarnessFailure(fin)
}

// classifyException maps Harbor exception info to Appendix F,
// keeping the established timeout/oom/budget/blocked mapping and
// defaulting to harness_error.
func classifyException(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "timeout"):
		return FailTimeout
	case strings.Contains(lower, "oom") || strings.Contains(lower, "out of memory") || strings.Contains(lower, "memory"):
		return FailOOM
	case strings.Contains(lower, "budget"):
		return FailBudget
	case strings.Contains(lower, "cancel") || strings.Contains(lower, "interrupt"):
		return FailBlocked
	default:
		return FailHarness
	}
}

// classifyHarnessFailure maps an exception-free, reward-0 trial from
// the harness final state: only a completed harness run that the
// verifier scored 0 is a verifier failure; anything the harness did
// not complete is a harness_error (or the matching Appendix F
// category). Exit 137 is the container OOM kill signature.
func classifyHarnessFailure(fin harnessFinal) string {
	switch fin.State {
	case "completed":
		return FailVerifier
	case "paused":
		if isTimeoutLike(fin.Reason + " " + fin.Summary) {
			return FailTimeout
		}
		return FailBudget
	case "blocked":
		return FailBlocked
	case "failed":
		if fin.ExitCode == 137 {
			return FailOOM
		}
		text := fin.Reason + " " + fin.Summary
		switch {
		case strings.Contains(strings.ToLower(text), "panic"):
			return FailCrash
		case isTimeoutLike(text):
			return FailTimeout
		case isOOMLike(text):
			return FailOOM
		case strings.Contains(strings.ToLower(text), "budget"):
			return FailBudget
		default:
			return FailHarness
		}
	default:
		return FailHarness
	}
}

func isTimeoutLike(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "deadline exceeded")
}

func isOOMLike(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "out of memory") ||
		strings.Contains(lower, "oom") ||
		strings.Contains(lower, "memory limit")
}

// readTrialSnapshot loads the aggregated metrics.json snapshot when
// present. Absent or corrupt snapshots are not errors: the JSONL
// records below still index the trial.
func readTrialSnapshot(exDir string) (telemetry.Snapshot, bool) {
	var snap telemetry.Snapshot
	raw, err := os.ReadFile(filepath.Join(exDir, "metrics.json"))
	if err != nil {
		return snap, false
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return snap, false
	}
	return snap, true
}

// modelAgg sums one trial's model_calls.jsonl.
type modelAgg struct {
	rows                                            int
	input, cacheRead, cacheWrite, output, reasoning int64
	latencySecs                                     float64
	retries                                         int
	errClasses                                      map[string]int
}

func aggregateModelCalls(exDir string) modelAgg {
	agg := modelAgg{errClasses: map[string]int{}}
	for _, line := range readJSONLLines(filepath.Join(exDir, "model_calls.jsonl")) {
		var m ModelCall
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		agg.rows++
		agg.input += m.InputTokens
		agg.cacheRead += m.CacheRead
		agg.cacheWrite += m.CacheWrite
		agg.output += m.OutputTokens
		agg.reasoning += m.ReasoningTok
		agg.latencySecs += m.LatencySecs
		agg.retries += m.Retries
		if m.ProviderError != "" {
			agg.errClasses[m.ProviderError]++
		}
	}
	return agg
}

// toolAgg sums one trial's tool_calls.jsonl.
type toolAgg struct {
	rows         int
	durationSecs float64
	byTool       map[string]int64
	subagents    int
}

func aggregateToolCalls(exDir string) toolAgg {
	agg := toolAgg{byTool: map[string]int64{}}
	for _, line := range readJSONLLines(filepath.Join(exDir, "tool_calls.jsonl")) {
		var t ToolCall
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			continue
		}
		agg.rows++
		agg.durationSecs += t.DurationSec
		agg.byTool[t.Tool]++
		if t.Tool == "subagent" {
			agg.subagents++
		}
	}
	return agg
}

// peakResourceSample folds every resources.jsonl row into peak
// counters. Peaks keep the MAX across rows and the metrics snapshot:
// the recorder appends a closing "final" row after the engine's own
// final row, so the last row may hold zeros while mid-run rows carry
// the real peak (taking the last row hid the peak entirely and
// indexed 0). Zero rows never clear a real peak; an all-zero record
// stays honestly zero (a trial that never reached the sampler).
func peakResourceSample(exDir string) ResourceSample {
	var peak ResourceSample
	for _, line := range readJSONLLines(filepath.Join(exDir, "resources.jsonl")) {
		var s ResourceSample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		if s.HarnessRSS > peak.HarnessRSS {
			peak.HarnessRSS = s.HarnessRSS
		}
		if s.CPUSeconds > peak.CPUSeconds {
			peak.CPUSeconds = s.CPUSeconds
		}
		if s.ChildPeakRSS > peak.ChildPeakRSS {
			peak.ChildPeakRSS = s.ChildPeakRSS
		}
		if s.Processes > peak.Processes {
			peak.Processes = s.Processes
		}
		if s.OOMEvents > peak.OOMEvents {
			peak.OOMEvents = s.OOMEvents
		}
		if peak.At.IsZero() || (!s.At.IsZero() && s.At.After(peak.At)) {
			peak.At = s.At
		}
	}
	return peak
}

// readJSONLLines reads non-empty lines, tolerating torn tail writes
// from killed trials.
func readJSONLLines(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// countJSONLLines counts parseable rows of the named JSONL file,
// excluding synthetic "final" marker rows the recorder writes so
// every file exists even for killed trials.
func countDataRows(path string, isFinal func(raw json.RawMessage) bool) int {
	n := 0
	for _, line := range readJSONLLines(path) {
		raw := json.RawMessage(line)
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		if isFinal(raw) {
			continue
		}
		n++
	}
	return n
}

// fillIndexUsage fills tokens and cost: metrics.json first, JSONL
// sums for fields the snapshot left at zero, Harbor agent_result
// only when both are absent.
func fillIndexUsage(row *BenchTrialIndex, doc trialResultDoc, snap telemetry.Snapshot, hasSnap bool, m modelAgg, t toolAgg) {
	if hasSnap {
		row.InputTokens = snap.Usage.Input
		row.CacheRead = snap.Usage.CacheRead
		row.CacheWrite = snap.Usage.CacheWrite
		row.OutputTokens = snap.Usage.Output + snap.Usage.Reasoning
		row.CostUSD = snap.CostUSD
	}
	if m.rows > 0 {
		if row.InputTokens == 0 {
			row.InputTokens = m.input
		}
		if row.CacheRead == 0 {
			row.CacheRead = m.cacheRead
		}
		if row.CacheWrite == 0 {
			row.CacheWrite = m.cacheWrite
		}
		if row.OutputTokens == 0 {
			row.OutputTokens = m.output + m.reasoning
		}
	}
	if row.InputTokens == 0 && row.OutputTokens == 0 && row.CostUSD == 0 {
		if doc.AgentResult.NInput != nil {
			row.InputTokens = *doc.AgentResult.NInput
		}
		if doc.AgentResult.NCache != nil {
			row.CacheRead = *doc.AgentResult.NCache
		}
		if doc.AgentResult.NOutput != nil {
			row.OutputTokens = *doc.AgentResult.NOutput
		}
		if doc.AgentResult.Cost != nil {
			row.CostUSD = *doc.AgentResult.Cost
		}
	}
	row.Steps = m.rows
	if row.Steps == 0 && hasSnap {
		for _, n := range snap.CallsByRole {
			row.Steps += int(n)
		}
	}
	row.ModelCalls = int64(row.Steps)
	row.ToolCalls = t.byTool
	if sumByTool(t.byTool) == 0 && hasSnap {
		row.ToolCalls = snap.CallsByTool
	}
	row.ToolCallSum = sumByTool(row.ToolCalls)
	if row.ToolCallSum == 0 {
		row.ToolCallSum = int64(t.rows)
	}
}

func sumByTool(m map[string]int64) int64 {
	var total int64
	for _, v := range m {
		total += v
	}
	return total
}

// fillIndexTimes fills the wall/model/tool time split and the RAM
// and CPU peaks. RAM peaks take the max of the harness snapshot, the
// container (child) snapshot and every resources.jsonl row peak, so a
// zero source never hides a real peak. Inside a task container the
// harness process is the container workload: HarnessPeakRSS and
// PeakRSS converge on the same observed max (kept separate per
// Section 22.2 for the external route, where they differ). CPU keeps
// the max of the cumulative counters.
func fillIndexTimes(row *BenchTrialIndex, doc trialResultDoc, snap telemetry.Snapshot, hasSnap bool, m modelAgg, t toolAgg, res ResourceSample) {
	if hasSnap {
		row.WallSecs = snap.WallSeconds
		row.ModelSecs = snap.ModelSeconds
		row.ToolSecs = snap.ToolSeconds
	}
	if row.WallSecs == 0 && doc.AgentExecution != nil {
		if start, err := time.Parse(time.RFC3339, doc.AgentExecution.StartedAt); err == nil {
			if end, err := time.Parse(time.RFC3339, doc.AgentExecution.FinishedAt); err == nil {
				if d := end.Sub(start).Seconds(); d > 0 {
					row.WallSecs = d
				}
			}
		}
	}
	if row.ModelSecs == 0 && m.rows > 0 {
		row.ModelSecs = m.latencySecs
	}
	if row.ToolSecs == 0 && t.rows > 0 {
		row.ToolSecs = t.durationSecs
	}
	var child, daemon uint64
	var cpu float64
	if hasSnap {
		child = snap.ChildPeakBytes
		daemon = snap.DaemonRSSTotalBytes
		cpu = snap.CPUSeconds
	}
	harness := daemon
	if res.HarnessRSS > harness {
		harness = res.HarnessRSS
	}
	if res.ChildPeakRSS > child {
		child = res.ChildPeakRSS
	}
	row.HarnessPeakRSS = harness
	row.PeakRSS = child
	if harness > row.PeakRSS {
		row.PeakRSS = harness
	}
	row.CPUSeconds = cpu
	if row.CPUSeconds == 0 || res.CPUSeconds > row.CPUSeconds {
		row.CPUSeconds = res.CPUSeconds
	}
}

// fillIndexEfficiency fills retries, subagents and the gate, loop,
// masking, compaction and interruption counters: metrics.json first,
// JSONL data rows (excluding synthetic final markers) when the
// snapshot has nothing.
func fillIndexEfficiency(row *BenchTrialIndex, snap telemetry.Snapshot, hasSnap bool, exDir string, m modelAgg, t toolAgg) {
	if hasSnap {
		row.ModelRetries = snap.ModelRetries
		row.ToolRetries = snap.ToolRetries
		row.Subagents = snap.SubagentsCreated
		row.Compactions = snap.Compactions
		row.MaskBatches = snap.MaskBatches
		row.LoopsCaught = snap.LoopsCaught
		row.GateAttempts = snap.GateAttempts
	}
	if row.ModelRetries == 0 && m.rows > 0 {
		row.ModelRetries = m.retries
	}
	if row.Subagents == 0 {
		row.Subagents = t.subagents
	}
	if row.LoopsCaught == 0 {
		row.LoopsCaught = countDataRows(filepath.Join(exDir, "loops.jsonl"), func(raw json.RawMessage) bool {
			var s LoopSignal
			if err := json.Unmarshal(raw, &s); err != nil {
				return true
			}
			return s.Stage == "final"
		})
	}
	if row.MaskBatches == 0 {
		row.MaskBatches = countDataRows(filepath.Join(exDir, "masking.jsonl"), func(raw json.RawMessage) bool {
			var e MaskEvent
			if err := json.Unmarshal(raw, &e); err != nil {
				return true
			}
			return e.Note == "final"
		})
	}
	if row.Compactions == 0 {
		row.Compactions = countDataRows(filepath.Join(exDir, "compaction.jsonl"), func(raw json.RawMessage) bool {
			var e CompactEvent
			if err := json.Unmarshal(raw, &e); err != nil {
				return true
			}
			if e.Note == "final" || e.Note == "engine history length" {
				return true
			}
			return (e.FromSeq == 0 || e.FromSeq == e.ToSeq) && e.KeptBytes == 0
		})
	}
	if row.GateAttempts == 0 {
		// The recorder writes one closing row per run on top of the
		// per-evaluation rows, so data rows minus one approximate
		// the evaluation count for records predating the metrics
		// counter.
		if n := len(readJSONLLines(filepath.Join(exDir, "gate_attempts.jsonl"))); n > 0 {
			row.GateAttempts = n - 1
		}
	}
	row.Interruptions = countDataRows(filepath.Join(exDir, "transitions.jsonl"), func(raw json.RawMessage) bool {
		var tr Transition
		if err := json.Unmarshal(raw, &tr); err != nil {
			return true
		}
		return tr.To != "paused" && tr.To != "canceled"
	})
}

// collectTrialArtifacts copies the verifier outputs and Harbor logs
// next to the excalibur records for later failure analysis.
func collectTrialArtifacts(trialDir, exDir string) {
	for _, name := range []string{"reward.txt", "reward.json", "test-stdout.txt", "test-stderr.txt", "ctrf.json"} {
		src := filepath.Join(trialDir, "verifier", name)
		if raw, err := os.ReadFile(src); err == nil {
			_ = os.WriteFile(filepath.Join(exDir, "verifier_"+name), raw, 0o644)
		}
	}
	for _, name := range []string{"result.json", "trial.log", "config.json"} {
		if raw, err := os.ReadFile(filepath.Join(trialDir, name)); err == nil {
			_ = os.WriteFile(filepath.Join(exDir, "harbor_"+name), raw, 0o644)
		}
	}
}
