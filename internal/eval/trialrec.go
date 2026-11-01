package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// TrialRecorder writes the Appendix F minimum records plus the rich
// per-trial files required for later failure analysis, incrementally
// into <logs>/excalibur/ so killed trials stay valid (Section 23.2:
// every Append is O_APPEND plus fsync, never a rewrite).
//
// Layout under dir (which is <logs>/excalibur):
//
//	trajectory.json      custom rich ATIF (existing telemetry.Writer file lives here too)
//	harbor_trajectory.json (unused; Harbor ATIF lives at <logs>/trajectory.json)
//	model_calls.jsonl    one line per model call
//	tool_calls.jsonl     one line per tool call (inline truncated output)
//	blobs/<sha256>       full tool outputs by hash
//	transitions.jsonl    run state transitions
//	gate_attempts.jsonl  completion-gate attempts
//	loops.jsonl          loop-detector signals
//	masking.jsonl        masking events
//	compaction.jsonl     compaction events
//	pairing.jsonl        call/result pairing repairs
//	budget.jsonl         budget consumption over time
//	environment.json     detected environment profile (once)
//	resources.jsonl      resource samples
//	journal.jsonl        durable journal export
//	final.json           final state, reason, exit code
//
// Secrets never reach these files: keys travel by environment only and
// the recorder redacts any value that looks like a key before writing.
type TrialRecorder struct {
	mu  sync.Mutex
	dir string
}

// NewTrialRecorder creates dir and returns the recorder.
func NewTrialRecorder(dir string) (*TrialRecorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("eval: mkdir trial recorder: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o755); err != nil {
		return nil, fmt.Errorf("eval: mkdir blobs: %w", err)
	}
	return &TrialRecorder{dir: dir}, nil
}

// Dir returns the recorder directory.
func (r *TrialRecorder) Dir() string { return r.dir }

func (r *TrialRecorder) append(name string, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("eval: marshal %s: %w", name, err)
	}
	buf = append(buf, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(r.dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("eval: open %s: %w", name, err)
	}
	if _, err := f.Write(buf); err != nil {
		_ = f.Close()
		return fmt.Errorf("eval: write %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("eval: sync %s: %w", name, err)
	}
	return f.Close()
}

func (r *TrialRecorder) writeFile(name string, v any) error {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal %s: %w", name, err)
	}
	buf = append(buf, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	tmp := filepath.Join(r.dir, name+".tmp")
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("eval: write %s: %w", name, err)
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("eval: open %s: %w", name, err)
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	if syncErr != nil {
		return fmt.Errorf("eval: sync %s: %w", name, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("eval: close %s: %w", name, closeErr)
	}
	if err := os.Rename(tmp, filepath.Join(r.dir, name)); err != nil {
		return fmt.Errorf("eval: rename %s: %w", name, err)
	}
	return nil
}

// ModelCall is one JSONL line in model_calls.jsonl (Appendix F plus
// failure-analysis fields).
type ModelCall struct {
	MaxOutputTokens    int               `json:"max_output_tokens,omitempty"`
	VerifierVerdict    bool              `json:"verifier_verdict"`
	CallSite           string            `json:"call_site"`
	UsageAvailable     bool              `json:"usage_available"`
	FinishReasonSource string            `json:"finish_reason_source"`
	At                 time.Time         `json:"at"`
	Seq                int               `json:"seq"`
	Role               string            `json:"role"`
	Model              string            `json:"model"`
	Effort             string            `json:"effort,omitempty"`
	LayerHashes        map[string]string `json:"layer_hashes,omitempty"`
	LayerSizes         map[string]int    `json:"layer_sizes,omitempty"`
	InputTokens        int64             `json:"input_tokens"`
	CacheRead          int64             `json:"cache_read"`
	CacheWrite         int64             `json:"cache_write"`
	OutputTokens       int64             `json:"output_tokens"`
	ReasoningTok       int64             `json:"reasoning_tokens"`
	LatencySecs        float64           `json:"latency_secs"`
	FinishReason       string            `json:"finish_reason,omitempty"`
	ProviderError      string            `json:"provider_error_class,omitempty"`
	Retries            int               `json:"retries"`
	RetryWaitCount     int               `json:"retry_wait_count"`
	RetryWaitSeconds   float64           `json:"retry_wait_seconds"`
	// Attempt tags the best-of-N attempt index that produced this
	// call (Section 18.2 fan-out merge). Zero means the single-run
	// path or a legacy row without a tag.
	Attempt int `json:"attempt,omitempty"`
}

// RecordModelCall appends one model-call line.
func (r *TrialRecorder) RecordModelCall(m ModelCall) error {
	if m.At.IsZero() {
		m.At = time.Now().UTC()
	}
	return r.append("model_calls.jsonl", m)
}

// ToolCall is one JSONL line in tool_calls.jsonl.
type ToolCall struct {
	At            time.Time `json:"at"`
	Seq           int       `json:"seq"`
	Tool          string    `json:"tool"`
	Arguments     string    `json:"arguments"`
	ExitCode      int       `json:"exit_code"`
	DurationSec   float64   `json:"duration_secs"`
	StdoutInline  string    `json:"stdout_inline,omitempty"`
	StderrInline  string    `json:"stderr_inline,omitempty"`
	OutputHash    string    `json:"output_hash,omitempty"`
	OutputPath    string    `json:"output_path,omitempty"`
	Truncated     bool      `json:"truncated"`
	IsError       bool      `json:"is_error"`
	FrictionError string    `json:"friction_error,omitempty"`
	// Attempt tags the best-of-N attempt index that produced this
	// call (Section 18.2 fan-out merge). Zero means the single-run
	// path or a legacy row without a tag.
	Attempt int `json:"attempt,omitempty"`
}

// RecordToolCall stores the full output as a blob and appends the line.
// Inline output is truncated to 4 KiB; the blob holds the full bytes.
func (r *TrialRecorder) RecordToolCall(t ToolCall, fullOutput string) error {
	if t.At.IsZero() {
		t.At = time.Now().UTC()
	}
	if fullOutput != "" {
		sum := sha256.Sum256([]byte(fullOutput))
		hash := hex.EncodeToString(sum[:])
		t.OutputHash = hash
		t.OutputPath = filepath.Join("blobs", hash)
		p := filepath.Join(r.dir, t.OutputPath)
		if _, err := os.Stat(p); err != nil {
			if err := os.WriteFile(p, []byte(fullOutput), 0o644); err != nil {
				return fmt.Errorf("eval: write blob: %w", err)
			}
		}
		const inlineLimit = 4096
		if len(fullOutput) > inlineLimit {
			t.StdoutInline = fullOutput[:inlineLimit] + "...[truncated]"
			t.Truncated = true
		} else {
			t.StdoutInline = fullOutput
		}
	}
	t.FrictionError = classifyFriction(t, t.StdoutInline+t.StderrInline)
	return r.append("tool_calls.jsonl", t)
}

// Transition is one run-state change.
type Transition struct {
	At     time.Time `json:"at"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Reason string    `json:"reason,omitempty"`
}

// RecordTransition appends a state transition.
func (r *TrialRecorder) RecordTransition(from, to, reason string) error {
	return r.append("transitions.jsonl", Transition{At: time.Now().UTC(), From: from, To: to, Reason: reason})
}

// GateAttempt is one completion-gate check with criteria and evidence.
type GateAttempt struct {
	At       time.Time `json:"at"`
	Attempt  int       `json:"attempt"`
	Passed   bool      `json:"passed"`
	Criteria []string  `json:"criteria,omitempty"`
	Evidence []string  `json:"evidence,omitempty"`
	Note     string    `json:"note,omitempty"`
	// FanoutAttempt tags the best-of-N attempt index that produced
	// this gate check when merged into the outer record (Section
	// 18.2). Zero means the single-run path or a legacy row.
	FanoutAttempt int `json:"fanout_attempt,omitempty"`
}

// RecordGate appends a gate attempt.
func (r *TrialRecorder) RecordGate(g GateAttempt) error {
	if g.At.IsZero() {
		g.At = time.Now().UTC()
	}
	return r.append("gate_attempts.jsonl", g)
}

// LoopSignal is one loop-detector signal.
type LoopSignal struct {
	At     time.Time `json:"at"`
	Stage  string    `json:"stage"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail,omitempty"`
}

// RecordLoop appends a loop signal.
func (r *TrialRecorder) RecordLoop(stage, kind, detail string) error {
	return r.append("loops.jsonl", LoopSignal{At: time.Now().UTC(), Stage: stage, Kind: kind, Detail: detail})
}

// MaskEvent is one masking batch.
type MaskEvent struct {
	At      time.Time `json:"at"`
	Batch   int       `json:"batch"`
	Dropped int       `json:"dropped_bytes"`
	Note    string    `json:"note,omitempty"`
}

// RecordMask appends a masking event.
func (r *TrialRecorder) RecordMask(batch, dropped int, note string) error {
	return r.append("masking.jsonl", MaskEvent{At: time.Now().UTC(), Batch: batch, Dropped: dropped, Note: note})
}

// CompactEvent is one compaction.
type CompactEvent struct {
	At        time.Time `json:"at"`
	FromSeq   int       `json:"from_seq"`
	ToSeq     int       `json:"to_seq"`
	KeptBytes int       `json:"kept_bytes"`
	Note      string    `json:"note,omitempty"`
}

// RecordCompaction appends a compaction event.
func (r *TrialRecorder) RecordCompaction(from, to, kept int, note string) error {
	return r.append("compaction.jsonl", CompactEvent{At: time.Now().UTC(), FromSeq: from, ToSeq: to, KeptBytes: kept, Note: note})
}

// PairRepairEvent is one call/result pairing repair: orphan halves
// converted to plain-text summaries (pre-request check or orphan-400
// retry), so evaluation trials show when the invariant fired.
type PairRepairEvent struct {
	At       time.Time `json:"at"`
	Repaired int       `json:"repaired"`
	Note     string    `json:"note,omitempty"`
}

// RecordPairRepair appends a pairing repair event.
func (r *TrialRecorder) RecordPairRepair(repaired int, note string) error {
	return r.append("pairing.jsonl", PairRepairEvent{At: time.Now().UTC(), Repaired: repaired, Note: note})
}

// BudgetPoint is budget consumption at one instant.
type BudgetPoint struct {
	At         time.Time `json:"at"`
	TokensUsed int64     `json:"tokens_used"`
	CostUSD    float64   `json:"cost_usd"`
	StepsUsed  int       `json:"steps_used"`
	WallSecs   float64   `json:"wall_secs"`
	Note       string    `json:"note,omitempty"`
}

// RecordBudget appends a budget point.
func (r *TrialRecorder) RecordBudget(b BudgetPoint) error {
	if b.At.IsZero() {
		b.At = time.Now().UTC()
	}
	return r.append("budget.jsonl", b)
}

// JournalEntry is one exported durable-journal row.
type JournalEntry struct {
	At      time.Time `json:"at"`
	Seq     int64     `json:"seq"`
	Type    string    `json:"type"`
	Payload string    `json:"payload,omitempty"`
	State   string    `json:"state,omitempty"`
}

// RecordJournal appends one journal row.
func (r *TrialRecorder) RecordJournal(seq int64, typ, payload, state string) error {
	return r.append("journal.jsonl", JournalEntry{At: time.Now().UTC(), Seq: seq, Type: typ, Payload: payload, State: state})
}

// ResourceSample is one harness/container resource observation.
type ResourceSample struct {
	At           time.Time `json:"at"`
	HarnessRSS   uint64    `json:"harness_rss_bytes"`
	CPUSeconds   float64   `json:"cpu_seconds"`
	Processes    int       `json:"processes"`
	OOMEvents    int       `json:"oom_events"`
	ChildPeakRSS uint64    `json:"child_peak_rss_bytes,omitempty"`
	Note         string    `json:"note,omitempty"`
}

// RecordResource appends a resource sample.
func (r *TrialRecorder) RecordResource(s ResourceSample) error {
	if s.At.IsZero() {
		s.At = time.Now().UTC()
	}
	return r.append("resources.jsonl", s)
}

// Environment is the detected profile written once to environment.json.
type Environment struct {
	At        time.Time         `json:"at"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	CPUs      int               `json:"cpus"`
	GoVersion string            `json:"go_version"`
	Tools     map[string]bool   `json:"tools_present,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// DetectEnvironment builds the environment profile: OS/arch/CPU plus
// which executables the failure analysis cares about (the most common
// TB failure is a missing binary in PATH, Section 23.2).
func DetectEnvironment() Environment {
	tools := []string{"git", "python3", "node", "go", "gcc", "make", "curl", "rg", "docker"}
	present := map[string]bool{}
	for _, t := range tools {
		present[t] = toolPresent(t)
	}
	return Environment{
		At: time.Now().UTC(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		CPUs: runtime.NumCPU(), GoVersion: runtime.Version(), Tools: present,
	}
}

func toolPresent(name string) bool {
	for _, dir := range []string{"/usr/local/bin", "/usr/bin", "/bin"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// WriteEnvironment writes environment.json once.
func (r *TrialRecorder) WriteEnvironment(e Environment) error {
	return r.writeFile("environment.json", e)
}

// FinalState is written once at the end to final.json.
type FinalState struct {
	At                 time.Time      `json:"at"`
	State              string         `json:"state"`
	Reason             string         `json:"reason,omitempty"`
	ExitCode           int            `json:"exit_code"`
	FrictionErrors     map[string]int `json:"friction_errors"`
	FrictionErrorCount int            `json:"friction_error_count"`
}

// WriteFinal writes final.json.
func (r *TrialRecorder) WriteFinal(state, reason string, code int) error {
	counts := map[string]int{}
	total := 0
	raw, err := os.ReadFile(filepath.Join(r.dir, "tool_calls.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var call ToolCall
		if json.Unmarshal(line, &call) != nil {
			continue
		}
		kind := call.FrictionError
		if kind == "" {
			kind = classifyFriction(call, call.StdoutInline+call.StderrInline)
		}
		if kind != "" {
			counts[kind]++
			total++
		}
	}
	return r.writeFile("final.json", FinalState{At: time.Now().UTC(), State: state, Reason: reason, ExitCode: code, FrictionErrors: counts, FrictionErrorCount: total})
}

// classifyFriction excludes command failures: these categories describe tool protocol mistakes.
func classifyFriction(call ToolCall, output string) string {
	if !call.IsError {
		return ""
	}
	switch call.Tool {
	case "call_tool":
		if strings.Contains(output, "not deferred") {
			return "call_core_tool"
		}
		if strings.Contains(output, "name is required") || strings.Contains(output, `missing required property "name"`) {
			return "missing_tool_name"
		}
	case "subagent":
		if strings.Contains(output, "action is required") || strings.Contains(output, `missing required property "action"`) {
			return "missing_subagent_action"
		}
		if strings.Contains(output, "run is required") {
			return "missing_subagent_run"
		}
	case "get_tool_output":
		if strings.Contains(output, "unknown blob ref") && strings.Contains(output, "sess-") {
			return "session_as_blob"
		}
	case "search_tools":
		if strings.Contains(output, "query is required") || strings.Contains(output, `missing required property "query"`) {
			return "missing_tool_query"
		}
	}
	return ""
}
