package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// Local quick runs: each own task executes in an isolated work
// directory under the run dir, works there with the eval profile, and
// is checked by its exact verifier. One trial per task (K=1); arms
// are compared with `bench compare` exactly like bench jobs.

// QuickExecFunc runs one task prompt with the eval profile in
// workdir, writing trial records under logsDir. It reports the exec
// exit code; a non-nil error means the launcher itself failed.
type QuickExecFunc func(ctx context.Context, bin, prompt, workdir, logsDir string, env []string, timeout time.Duration) (int, error)

// DefaultQuickExec shells out to the excalibur binary: `exec --profile
// eval --json --timeout S --logs-dir L <prompt>` with cwd set to the
// task work directory (the container task directory equivalent) and
// the arm switches appended to the environment.
func DefaultQuickExec(ctx context.Context, bin, prompt, workdir, logsDir string, env []string, timeout time.Duration) (int, error) {
	secs := int64(timeout.Seconds())
	if secs <= 0 {
		secs = QuickDefaultTimeout
	}
	cmd := exec.CommandContext(ctx, bin, "exec",
		"--profile", "eval", "--json",
		"--timeout", fmt.Sprintf("%d", secs),
		"--logs-dir", logsDir, prompt)
	cmd.Dir = workdir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	// The agent's stdout is noise for the runner; records land in
	// logsDir. Keep stderr for launcher diagnostics.
	cmd.Stdout = nil
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return 1, fmt.Errorf("eval: quick exec launcher: %w: %s", err, stderr.String())
	}
	return 0, nil
}

// QuickRunner executes the own quick suite.
type QuickRunner struct {
	// Dir is the run directory (work dirs, logs, trials, manifest).
	Dir string
	// Bin is the excalibur binary for DefaultQuickExec.
	Bin string
	// Concurrency bounds parallel tasks (default 4).
	Concurrency int
	// TaskTimeout bounds one agent run (default QuickDefaultTimeout).
	TaskTimeout time.Duration
	// Arm and Switches name the configuration under test; switches
	// are appended to every task's environment and recorded in the
	// manifest for ablation reporting.
	Arm      string
	Switches map[string]string
	// Env carries extra environment (credentials come from the
	// operator environment, never from task definitions).
	Env []string
	// Model/Provider/Effort annotate the manifest.
	Model    string
	Provider string
	Effort   string
	// Filter runs only tasks whose name contains the substring.
	Filter string
	// SkipDocker skips tasks needing the docker daemon.
	SkipDocker bool
	// MissingBins skips tasks needing the listed executables
	// (injected for tests; normally resolved via PATH).
	MissingBins map[string]bool
	// Exec runs one task (default DefaultQuickExec; fakes in tests).
	Exec QuickExecFunc
}

// QuickOutcome is one task result.
type QuickOutcome struct {
	Task     string  `json:"task"`
	Category string  `json:"category"`
	Passed   bool    `json:"passed"`
	Skipped  bool    `json:"skipped"`
	Detail   string  `json:"detail"`
	WallSecs float64 `json:"wall_seconds"`
	ExitCode int     `json:"exit_code"`

	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`

	InputTokens  int64   `json:"input_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`

	HarnessPeakBytes uint64  `json:"harness_peak_bytes"`
	ChildPeakBytes   uint64  `json:"child_peak_bytes"`
	CPUSeconds       float64 `json:"cpu_seconds"`
	ModelSeconds     float64 `json:"model_seconds"`
	ToolSeconds      float64 `json:"tool_seconds"`
	ModelCalls       int64   `json:"model_calls"`
	ToolCalls        int64   `json:"tool_calls"`
}

func (r QuickRunner) execFunc() QuickExecFunc {
	if r.Exec != nil {
		return r.Exec
	}
	return DefaultQuickExec
}

func (r QuickRunner) concurrency() int {
	if r.Concurrency <= 0 {
		return 4
	}
	return r.Concurrency
}

func (r QuickRunner) taskTimeout() time.Duration {
	if r.TaskTimeout <= 0 {
		return QuickDefaultTimeout * time.Second
	}
	return r.TaskTimeout
}

func (r QuickRunner) bin() string {
	if r.Bin != "" {
		return r.Bin
	}
	exe, err := os.Executable()
	if err != nil {
		return "excalibur"
	}
	return exe
}

// missingTool reports whether name is unavailable (test override or
// PATH lookup).
func (r QuickRunner) missingTool(name string) bool {
	if r.MissingBins != nil {
		return r.MissingBins[name]
	}
	_, err := exec.LookPath(name)
	return err != nil
}

// taskEnv builds the task environment: operator env plus the arm
// identity and mechanism switches.
func (r QuickRunner) taskEnv() []string {
	env := append([]string(nil), r.Env...)
	if r.Arm != "" {
		env = append(env, "EXCALIBUR_ARM="+r.Arm)
	}
	keys := make([]string, 0, len(r.Switches))
	for k := range r.Switches {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+r.Switches[k])
	}
	return env
}

// Run executes the selected tasks and returns one outcome per task in
// stable task order.
func (r QuickRunner) Run(ctx context.Context) ([]QuickOutcome, error) {
	tasks := QuickTasks()
	if r.Filter != "" {
		var kept []QuickTask
		for _, t := range tasks {
			if contains(t.Name, r.Filter) {
				kept = append(kept, t)
			}
		}
		tasks = kept
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("eval: no quick tasks match %q", r.Filter)
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("eval: mkdir quick run: %w", err)
	}
	out := make([]QuickOutcome, len(tasks))
	sem := make(chan struct{}, r.concurrency())
	var wg sync.WaitGroup
	for i, task := range tasks {
		i, task := i, task
		// Docker-gated and tool-gated skips are decided before
		// spending a model call.
		if r.SkipDocker && task.Category == "docker" {
			out[i] = QuickOutcome{Task: task.Name, Category: task.Category, Skipped: true, Detail: "docker skipped"}
			continue
		}
		var missing []string
		for _, need := range task.Needs {
			if r.missingTool(need) {
				missing = append(missing, need)
			}
		}
		if len(missing) > 0 {
			out[i] = QuickOutcome{Task: task.Name, Category: task.Category, Skipped: true, Detail: "missing tool: " + stringsJoin(missing, ", ")}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		safego.Go("quick-task", nil, func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = r.runOne(ctx, task)
		})
	}
	wg.Wait()
	return out, ctx.Err()
}

func (r QuickRunner) runOne(ctx context.Context, task QuickTask) QuickOutcome {
	start := time.Now()
	oc := QuickOutcome{Task: task.Name, Category: task.Category}
	workdir := filepath.Join(r.Dir, "work", task.Name)
	logsDir := filepath.Join(r.Dir, "logs", task.Name)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		oc.Detail = err.Error()
		return oc
	}
	for name, content := range task.Files {
		p := filepath.Join(workdir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			oc.Detail = err.Error()
			return oc
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			oc.Detail = err.Error()
			return oc
		}
	}
	if err := quickSetupFiles(task, workdir); err != nil {
		oc.Detail = err.Error()
		return oc
	}
	timeout := r.taskTimeout()
	if task.TimeoutSecs > 0 {
		timeout = time.Duration(task.TimeoutSecs) * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	code, err := r.execFunc()(tctx, r.bin(), task.Prompt, workdir, logsDir, r.taskEnv(), timeout)
	oc.WallSecs = time.Since(start).Seconds()
	if err != nil {
		oc.ExitCode = 1
		oc.Detail = "launcher: " + err.Error()
		return oc
	}
	oc.ExitCode = code
	r.fillUsage(logsDir, &oc)
	if code != 0 {
		oc.Detail = fmt.Sprintf("exec exit %d (harness)", code)
		return oc
	}
	if verr := task.Verifies(workdir); verr != nil {
		oc.Detail = "verifier: " + verr.Error()
		return oc
	}
	oc.Passed = true
	oc.Detail = "ok"
	return oc
}

// quickMetrics mirrors the usage subset of metrics.json the runner
// collects best-effort for per-success cost and resource reporting.
type quickMetrics struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Usage    struct {
		Input      int64 `json:"input"`
		CacheRead  int64 `json:"cache_read"`
		CacheWrite int64 `json:"cache_write"`
		Output     int64 `json:"output"`
		Reasoning  int64 `json:"reasoning"`
	} `json:"usage"`
	CostUSD             float64          `json:"cost_usd"`
	DaemonRSSTotalBytes uint64           `json:"daemon_rss_total_bytes"`
	ChildPeakBytes      uint64           `json:"child_peak_bytes"`
	CPUSeconds          float64          `json:"cpu_seconds"`
	ModelSeconds        float64          `json:"model_seconds"`
	ToolSeconds         float64          `json:"tool_seconds"`
	CallsByRole         map[string]int64 `json:"calls_by_role"`
	CallsByTool         map[string]int64 `json:"calls_by_tool"`
}

func (r QuickRunner) fillUsage(logsDir string, oc *QuickOutcome) {
	raw, err := os.ReadFile(filepath.Join(logsDir, "excalibur", "metrics.json"))
	if err != nil {
		return
	}
	var m quickMetrics
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}
	oc.InputTokens = m.Usage.Input
	oc.CacheTokens = m.Usage.CacheRead + m.Usage.CacheWrite
	oc.OutputTokens = m.Usage.Output + m.Usage.Reasoning
	oc.CostUSD = m.CostUSD
	oc.Model = m.Model
	oc.Provider = m.Provider
	oc.HarnessPeakBytes = m.DaemonRSSTotalBytes
	oc.ChildPeakBytes = m.ChildPeakBytes
	oc.CPUSeconds = m.CPUSeconds
	oc.ModelSeconds = m.ModelSeconds
	oc.ToolSeconds = m.ToolSeconds
	for _, n := range m.CallsByRole {
		oc.ModelCalls += n
	}
	for _, n := range m.CallsByTool {
		oc.ToolCalls += n
	}
}

// QuickOutcomeToTrial maps an outcome to the canonical trial record
// (TaskID quick/<name>): exec failures are harness errors, verifier
// failures are verifier failures — the same Appendix F split bench
// trials use.
func QuickOutcomeToTrial(oc QuickOutcome) TrialOutcome {
	failed := !oc.Passed
	cat := ""
	if !oc.Passed {
		if oc.ExitCode != 0 {
			cat = FailHarness
		} else {
			cat = FailVerifier
		}
	}
	var reward float64
	if oc.Passed {
		reward = 1
	}
	return TrialOutcome{
		TrialID: oc.Task, TaskID: "quick/" + oc.Task, Benchmark: "quick",
		Reward: reward, Failed: failed, FailCategory: cat, At: time.Now().UTC(),
		InputTokens: oc.InputTokens, CacheRead: oc.CacheTokens, OutputTokens: oc.OutputTokens,
		CostUSD: oc.CostUSD, WallSeconds: oc.WallSecs,
		ModelSeconds: oc.ModelSeconds, ToolSeconds: oc.ToolSeconds,
		ChildPeakBytes: oc.ChildPeakBytes, HarnessPeakBytes: oc.HarnessPeakBytes,
		CPUSeconds: oc.CPUSeconds, ModelCalls: oc.ModelCalls, ToolCalls: oc.ToolCalls,
	}
}

// WriteQuickResults persists one trial.json per executed task, the
// trials.jsonl index, the run manifest (arm, switches, profile
// hashes) and report.json, so `bench compare` and `eval report`
// consume quick dirs exactly like bench jobs.
func (r QuickRunner) WriteResults(outcomes []QuickOutcome) error {
	var trials []TrialOutcome
	for _, oc := range outcomes {
		if oc.Skipped {
			continue
		}
		t := QuickOutcomeToTrial(oc)
		trials = append(trials, t)
		if err := WriteTrial(filepath.Join(r.Dir, "trials", oc.Task), t); err != nil {
			return err
		}
	}
	indexPath := filepath.Join(r.Dir, "trials.jsonl")
	_ = os.Remove(indexPath)
	for _, t := range trials {
		row := BenchTrialIndex{
			Task: t.TaskID, Trial: t.TrialID, Reward: t.Reward, FailCategory: t.FailCategory,
			InputTokens: t.InputTokens, CacheRead: t.CacheRead,
			OutputTokens: t.OutputTokens, CostUSD: t.CostUSD, WallSecs: t.WallSeconds,
			ModelSecs: t.ModelSeconds, ToolSecs: t.ToolSeconds,
			PeakRSS: t.ChildPeakBytes, HarnessPeakRSS: t.HarnessPeakBytes,
			CPUSeconds: t.CPUSeconds, ModelCalls: t.ModelCalls, ToolCallSum: t.ToolCalls,
		}
		if err := AppendTrialIndex(r.Dir, row); err != nil {
			return err
		}
	}
	profileHash := DefaultEvalProfile().Hash()
	// Annotate the manifest with the measured model when the flags
	// did not name one (the common quick-loop case).
	model, provider, effort := r.Model, r.Provider, r.Effort
	for _, oc := range outcomes {
		if oc.Skipped {
			continue
		}
		if model == "" {
			model = oc.Model
		}
		if provider == "" {
			provider = oc.Provider
		}
		break
	}
	manifest := RunManifest{
		Dataset: "quick/local", DatasetDigest: "local",
		Model: model, Provider: provider, Effort: effort,
		K: 1, Budgets: map[string]string{"model": "task-default", "timeout": "per-task"},
		Machine:        MachineCharacteristics(),
		StartedAt:      time.Now().UTC(),
		Arm:            r.Arm,
		Switches:       r.Switches,
		ProfileHash:    profileHash,
		ArmProfileHash: ArmProfileHash(profileHash, r.Arm, r.Switches),
	}
	if err := WriteManifest(r.Dir, manifest); err != nil {
		return err
	}
	if len(trials) == 0 {
		return fmt.Errorf("eval: no quick trials executed (all skipped)")
	}
	rep, err := Aggregate(trials, 1)
	if err != nil {
		return err
	}
	return WriteReport(r.Dir, rep)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && searchSub(s, sub))
}

func searchSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func stringsJoin(xs []string, sep string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}
