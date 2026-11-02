package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/attempts"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// Version identifies the binary in ATIF metadata and reports.
// The release build overrides it with -ldflags -X.
var Version = "dev"

// ExecOptions configures one non-interactive run (Section 6.5): no
// master, no conversation, a level-1 root execution with the request
// file built from Request. Exec never asks: the assume branch of
// Section 6.4 applies and the most conservative interpretation wins.
type ExecOptions struct {
	// Request is the user petition; empty means a mandatory datum is
	// missing and the run ends blocked (Section 6.5).
	Request string
	// ProjectDir scopes the run; empty means no project.
	ProjectDir string
	// OutputSchema validates the final result document when set.
	OutputSchema string
	// EmitJSON streams the JSONL event flow to Emit (usually stdout).
	EmitJSON bool
	// Emit receives streamed events when EmitJSON is set.
	Emit io.Writer
	// BudgetUSD caps spend; a non-positive explicit budget means the
	// budget is already exhausted and the run pauses.
	BudgetUSD *float64
	// MaxSteps caps engine steps; negative is an invalid flag.
	MaxSteps int
	// TimeoutSecs bounds wall time; expiry records stopped_by_timeout
	// with exit code 4 (Section 6.5; decision 0063).
	TimeoutSecs int64
	// Profile selects default, delegate or eval. Empty is default.
	Profile ProfileName
	// NoMemory disables memory entirely (delegate profile).
	NoMemory bool
	// Ephemeral uses a temporary database removed on exit; rejected
	// with the default profile (Section 3.6).
	Ephemeral bool
	// ProfileOverride declares an eval deviation; recorded in
	// metadata and ATIF, and the trial never compares clean.
	ProfileOverride string
	// LogsDir overrides trial log resolution (tests, Harbor).
	LogsDir string
	// RunDir pins the per-run artifact directory (task runs reuse
	// their task directory so status/wait/result read one place).
	RunDir string
	// TaskID and RunID link artifacts when running inside a task.
	TaskID types.TaskID
	RunID  types.RunID
	// AllowAsk parks with a question instead of assuming, for
	// delegated tasks whose client answers via reply (Section 3.6).
	// Exec itself always assumes (Section 6.5).
	AllowAsk bool
	// NeverAsk forces the assume branch even when AllowAsk is set
	// (--never-ask / never_ask:true, Section 3.6).
	NeverAsk bool
	// ForceOutcome pins the terminal state for tests that cover the
	// Section 6.5 exit-code table without a model. The CLI never
	// sets it.
	ForceOutcome types.RunState
	// ForceReason is recorded with a forced outcome.
	ForceReason string
	// Work is an optional hook that performs the run's work between
	// the opening and closing ATIF steps. Nil runs the embedded
	// deterministic step. Tests use it to simulate tool effects,
	// questions and failures.
	Work func(ctx context.Context, run *ExecRun) error
	// Provider, when non-nil, drives the real embedded engine loop
	// (Section 7.3) with the full wired catalog instead of the
	// deterministic stub: the scripted fake provider in tests, the
	// live gateway provider in production. Nil keeps the
	// deterministic step for the rows that never call a model.
	Provider providers.Provider
	// Vault wires secret resolution and output redaction
	// (Section 20.1) into the engine path. Nil keeps pattern-only
	// redaction. The eval profile never passes a vault: keys arrive
	// by environment (Section 23.2).
	Vault *vault.Vault
	// JournalStore persists the engine write-ahead journal for the
	// run. Nil uses an in-memory store.
	JournalStore journal.Store
	// DisableAttempts forces a single attempt even when
	// EXCALIBUR_ATTEMPTS>1 (used for the fan-out sub-runs so they
	// never recurse).
	DisableAttempts bool
	// attemptMap maps the real working directory onto one attempt's
	// copy for its tool calls and artifact checks. Only the best-of-N
	// fan-out sets it (internal/cli/attempts.go).
	attemptMap *attempts.WorkdirMap
}

// ExecRun is the live handle passed to the Work hook.
type ExecRun struct {
	startedAt  time.Time
	wallSource string
	TaskID     types.TaskID
	RunID      types.RunID
	Dir        string
	// ProjectDir is the isolated working copy for this run (the
	// best-of-N fan-out sets one copy per attempt). Work hooks must
	// write through it to stay isolated.
	ProjectDir string
	ATIF       *telemetry.Writer
	Metrics    *telemetry.TaskMetrics
	Events     []map[string]any
	emit       io.Writer
	emitJSON   bool
	// Recorder holds the rich per-trial files under excalibur/ when the
	// eval profile is active (Section 23.2, Appendix F). Nil elsewhere.
	Recorder *eval.TrialRecorder
	// Harbor mirrors the custom ATIF into the Harbor-compatible
	// trajectory.json at the trial log root. Nil outside eval.
	Harbor *eval.HarborATIFWriter
	// modelSeq and toolSeq order the per-call JSONL files.
	modelSeq int
	toolSeq  int
	// FinalText, FinishSummary and FinishEvidence are the accepted finish's
	// detail: the model's final assistant text, the finish summary and the
	// evidence it cited. Empty when the run did not complete through finish.
	FinalText      string
	FinishSummary  string
	FinishEvidence string
	// stopEventWritten is set once the stop path has persisted run_finished
	// (state "timeout") to events.jsonl. Only the watchdog and finalize touch
	// it, both under finalMu.
	stopEventWritten bool
	finishAuditWired bool
}

// EmitStep records one ATIF step plus a matching JSONL event, and
// mirrors it into the Harbor-compatible trajectory when present.
func (r *ExecRun) EmitStep(step telemetry.ATIFStep, event string) error {
	step.At = time.Now().UTC()
	if err := r.ATIF.Append(step); err != nil {
		return err
	}
	if r.Harbor != nil {
		source := "agent"
		switch step.Role {
		case "user":
			source = "user"
		case "system":
			source = "system"
		}
		content := step.Content
		if content == "" && step.ToolResult != nil {
			content = "tool " + step.ToolResult.Name + ": " + step.ToolResult.Output
		}
		if len(step.ToolCalls) > 0 && content == "" {
			content = "tool call " + step.ToolCalls[0].Name
		}
		_ = r.Harbor.AppendStep(source, content, "")
	}
	return r.EmitEvent(event, map[string]any{"step": step})
}

// recordingJournalStore exports every journal append to the trial
// recorder so killed trials keep a valid journal.jsonl.
type recordingJournalStore struct {
	base journal.Store
	rec  *eval.TrialRecorder
}

func (s *recordingJournalStore) Append(ctx context.Context, e journal.Entry) (journal.Entry, error) {
	out, err := s.base.Append(ctx, e)
	if err != nil {
		return out, err
	}
	_ = s.rec.RecordJournal(int64(out.Seq), string(out.Type), string(out.Payload), out.State)
	return out, nil
}

func (s *recordingJournalStore) List(ctx context.Context, runID string) ([]journal.Entry, error) {
	return s.base.List(ctx, runID)
}

// EmitEvent appends one event to the in-memory flow and, with
// EmitJSON, streams it to the caller.
func (r *ExecRun) EmitEvent(event string, fields map[string]any) error {
	rec := eventRecord(event, fields)
	r.Events = append(r.Events, rec)
	return r.emitStdout(rec)
}

// eventRecord builds one event row with its timestamp.
func eventRecord(event string, fields map[string]any) map[string]any {
	rec := map[string]any{"event": event, "at": time.Now().UTC().Format(time.RFC3339Nano)}
	for k, v := range fields {
		rec[k] = v
	}
	return rec
}

// emitStdout streams one already-recorded event to the caller in JSON mode
// without adding it to the in-memory flow.
func (r *ExecRun) emitStdout(rec map[string]any) error {
	if r.emitJSON && r.emit != nil {
		buf, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(r.emit, "%s\n", buf); err != nil {
			return err
		}
	}
	return nil
}

// ExecOutcome is the finished run: final state, stable exit code,
// artifact locations and the validated result document.
type ExecOutcome struct {
	State       types.RunState `json:"state"`
	Reason      string         `json:"reason,omitempty"`
	Summary     string         `json:"summary"`
	Assumptions []string       `json:"assumptions,omitempty"`
	Question    string         `json:"question,omitempty"`
	TaskID      types.TaskID   `json:"task_id"`
	RunID       types.RunID    `json:"run_id"`
	Dir         string         `json:"dir"`
	ATIFPath    string         `json:"atif_path"`
	MetricsPath string         `json:"metrics_path"`
	Profile     ProfileName    `json:"profile"`
	ProfileHash string         `json:"profile_hash"`
	ResultJSON  []byte         `json:"-"`
	ExitCode    int            `json:"exit_code"`
	Overridden  bool           `json:"overridden,omitempty"`
}

// QuestionMarker delimits a deterministic data request inside a task
// petition: {{ask: what is missing}}. Without NeverAsk the run parks
// in waiting(user) and wait returns the question; with NeverAsk (or
// in exec, always) the assume branch records it and continues.
const (
	askOpen  = "{{ask:"
	askClose = "}}"
)

// ExtractQuestion returns the first ask-marker payload in the
// request, if any.
func ExtractQuestion(request string) (string, bool) {
	start := strings.Index(request, askOpen)
	if start < 0 {
		return "", false
	}
	rest := request[start+len(askOpen):]
	end := strings.Index(rest, askClose)
	if end < 0 {
		return "", false
	}
	q := strings.TrimSpace(rest[:end])
	if q == "" {
		return "", false
	}
	return q, true
}

// stripFirstAsk removes the first ask marker from a request: the
// delegating answer consumes the question it resolves.
func stripFirstAsk(request string) string {
	start := strings.Index(request, askOpen)
	if start < 0 {
		return request
	}
	rest := request[start+len(askOpen):]
	end := strings.Index(rest, askClose)
	if end < 0 {
		return request
	}
	return strings.TrimSpace(request[:start] + " " + rest[end+len(askClose):])
}

// EffectiveTools declares the deterministic tool surface per profile
// (Sections 3.6 table, 23.2 table). The engine clamps real tool calls
// to this list; the hashes below pin it for Appendix F records.
func EffectiveTools(profile ProfileName) (core, deferred []string) {
	core = append([]string(nil), eval.DefaultEvalProfile().EnabledTools...)
	deferred = append([]string(nil), eval.DefaultEvalProfile().DeferredTools...)
	// The eval table trims tools that cannot act in a single
	// autonomous run; assistant and delegate runs keep them.
	if profile != ProfileEval {
		core = append(core, eval.TrimmedFromEvalTools()...)
	}
	if profile == ProfileDefault {
		core = append(core, "send_message", "question_answer")
	}
	return tools.WithoutDisabledSkills(core), tools.WithoutDisabledSkills(deferred)
}

// ToolsHash pins the exposed catalog for a profile.
func ToolsHash(profile ProfileName) string {
	core, deferred := EffectiveTools(profile)
	sort.Strings(core)
	sort.Strings(deferred)
	h := sha256.New()
	fmt.Fprintf(h, "profile=%s\n", profile)
	for _, t := range core {
		fmt.Fprintf(h, "tool=%s\n", t)
	}
	for _, t := range deferred {
		fmt.Fprintf(h, "deferred=%s\n", t)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// EvalToolAllowed gates one operation under the eval profile table
// (Section 23.2): project-management operations of task_db, general
// memory search, ask_user and personal-assistant tools are rejected
// or hidden while the task-internal planning surface stays enabled.
// op names the task_db operation ("" for non-task_db tools).
func EvalToolAllowed(tool, op string) error {
	hidden := map[string]bool{
		"send_message": true, "ask_user": true, "settings": true,
		"email": true, "calendar": true, "meeting": true,
		"contact_message": true, "github": true, "userdb": true,
		"gpu_training": true, "browser_navigate": true,
		"nodes": true, "memory_search": true, "install_skill": true,
	}
	if hidden[tool] {
		return fmt.Errorf("cli: tool %q is disabled under the eval profile", tool)
	}
	if tool == "task_db" {
		switch op {
		case "register_project", "update_project", "archive_project":
			return fmt.Errorf("cli: task_db op %q is disabled under the eval profile (single task scope)", op)
		}
	}
	return nil
}

// RunExec executes one non-interactive request to a terminal state
// (or to waiting when AllowAsk parks a question) and writes the
// Appendix F minimum records: profile config and hashes, the ATIF
// trajectory, events and metrics.json. An injected Provider (tests,
// explicit live wiring) drives the real engine loop; otherwise the
// operator layers resolve to the explicit stub (EXCALIBUR_PROVIDER=
// stub, the only silent-run mode), a registry-built provider behind
// the models gateway, or a stable harness error (exit 1) when
// nothing is configured.
func RunExec(ctx context.Context, opts ExecOptions) (*ExecOutcome, error) {
	startedAt := time.Now().UTC()
	profile := opts.Profile
	if profile == "" {
		profile = ProfileDefault
	}
	switch profile {
	case ProfileDefault, ProfileDelegate, ProfileEval:
	default:
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: unknown profile %q", opts.Profile)}
	}
	if err := ValidateEphemeral(profile, opts.Ephemeral); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	if opts.MaxSteps < 0 {
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: --max-steps must not be negative")}
	}
	if opts.TimeoutSecs < 0 {
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: --timeout must not be negative")}
	}

	logsDir, err := ResolveLogsDir(opts.LogsDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}

	runID := opts.RunID
	if runID == "" {
		runID = types.NewRunID()
	}
	taskID := opts.TaskID
	if taskID == "" {
		taskID = types.NewTaskID()
	}

	runDir := opts.RunDir
	var cleanup func()
	if runDir == "" {
		if opts.Ephemeral {
			ephem, err := EphemeralDir()
			if err != nil {
				return nil, &CodedError{Code: ExitError, Err: err}
			}
			cleanup = func() { os.RemoveAll(ephem) }
			runDir = ephem
		} else {
			dataDir, err := DataDir(profile, opts.ProjectDir, logsDir)
			if err != nil {
				return nil, &CodedError{Code: ExitError, Err: err}
			}
			if profile == ProfileEval {
				if err := EvalDataReady(dataDir, opts.ProfileOverride); err != nil {
					return nil, &CodedError{Code: ExitError, Err: err}
				}
			}
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: data dir: %w", err)}
			}
			if profile == ProfileEval {
				runDir = logsDir
			} else {
				runDir = filepath.Join(dataDir, "run-"+string(runID))
				if err := os.MkdirAll(runDir, 0o755); err != nil {
					return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: run dir: %w", err)}
				}
			}
		}
	} else if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: run dir: %w", err)}
	}
	if cleanup != nil {
		defer cleanup()
	}

	wallSecs := opts.TimeoutSecs
	if engine.DeadlineEnabled() {
		wallSecs = EffectiveBudgetsFor(opts, profile).HardWallSecs
	}
	if wallSecs > 0 {
		var cancel context.CancelFunc
		if engine.DeadlineEnabled() {
			ctx, cancel = EffectiveBudgetsFor(opts, profile).wallContext(ctx, startedAt)
		} else {
			ctx, cancel = context.WithDeadline(ctx, startedAt.Add(time.Duration(wallSecs)*time.Second))
		}
		defer cancel()
	}

	// Production model wiring (Sections 19.1, 19.2, 20.1): an
	// injected provider (tests, explicit live wiring) wins; else
	// the operator layers resolve to the explicit stub (the only
	// silent-run mode), a real registry provider, or a stable
	// startup error when nothing is configured. There is no silent
	// default provider.
	settings, layerCfg, err := resolveSettings()
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	built, eff, err := resolveExecProvider(ctx, opts, profile, settings, string(runID))
	if err != nil {
		return nil, err
	}
	if built != nil {
		opts.Provider = built
		defer func() { _ = built.Close() }()
	}
	provider, model := eff.Provider, eff.Model
	// Effective window (owner decision 0051): the eval profile hash
	// and records report the model's own limits, not the old fixed
	// ceiling. Reference runs keep the frozen hash; unknown models
	// report the conservative fallback.
	profileCfg := eval.ProfileForModel(model)
	if opts.ProfileOverride != "" {
		profileCfg.Override = opts.ProfileOverride
	}
	// Preserve an explicit model label when the caller set one on
	// the profile (tests); production leaves it empty as before.
	profileHash := profileCfg.Hash()
	if profile != ProfileEval {
		profileHash = ToolsHash(profile)
	}
	// Effective run-level budgets (W8): binding wall clock plus
	// safety caps. Recorded in profile.json (so profile.sha256 moves
	// with them) and in budgets.json for the ablation.
	effBudgets := EffectiveBudgetsFor(opts, profile)
	if effBudgets.HardWallSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = effBudgets.wallContext(ctx, startedAt)
		defer cancel()
	}

	if err := writeRunProfile(runDir, profile, profileCfg, opts.ProfileOverride, effBudgets); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	writeBudgetsRecord(runDir, effBudgets)

	meta := telemetry.ATIFMetadata{
		TaskID:          string(taskID),
		ExcaliburCommit: Version,
		Profile:         string(profile),
		ProfileHash:     profileHash,
		Model:           model,
		Provider:        provider,
		StartedAt:       time.Now().UTC(),
	}
	// Trial records layout (Section 23.2, Appendix F):
	// - Harbor-compatible ATIF at <logs>/trajectory.json (audited).
	// - Rich custom ATIF plus per-trial JSONL files under
	//   <logs>/excalibur/ (incremental, survive kills).
	// Non-eval profiles keep the single custom trajectory at runDir.
	var writer *telemetry.Writer
	var harborWriter *eval.HarborATIFWriter
	var recorder *eval.TrialRecorder
	customDir := runDir
	if profile == ProfileEval {
		excaliburDir := filepath.Join(logsDir, "excalibur")
		if err := os.MkdirAll(excaliburDir, 0o755); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		rec, err := eval.NewTrialRecorder(excaliburDir)
		if err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		recorder = rec
		if err := recorder.WriteEnvironment(eval.DetectEnvironment()); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		if err := recorder.RecordTransition("", "created", "run started"); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		hw, err := eval.NewHarborATIFWriter(logsDir, Version, model)
		if err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		harborWriter = hw
		_ = harborWriter.AppendStep("user", opts.Request, model)
		customDir = excaliburDir
	}
	atifPath := filepath.Join(customDir, "trajectory.json")
	if _, err := os.Stat(atifPath); err == nil && opts.RunDir != "" {
		writer, err = telemetry.OpenWriter(atifPath)
		if err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
	} else {
		writer, err = telemetry.NewWriter(customDir, telemetry.ATIFv18, meta)
		if err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
	}

	metrics := telemetry.NewTaskMetrics(taskID, runID, "v1")
	metrics.SetModel(provider, model)

	run := &ExecRun{
		TaskID: taskID, RunID: runID, Dir: runDir,
		startedAt:  startedAt,
		wallSource: effBudgets.WallSource,
		ProjectDir: opts.ProjectDir,
		ATIF:       writer, Metrics: metrics,
		emit: opts.Emit, emitJSON: opts.EmitJSON,
		Recorder: recorder, Harbor: harborWriter,
	}
	// Journal export wraps the store when a recorder exists: every
	// journal append also lands in excalibur/journal.jsonl.
	if recorder != nil {
		if opts.JournalStore == nil {
			opts.JournalStore = journal.NewMemStore()
		}
		opts.JournalStore = &recordingJournalStore{base: opts.JournalStore, rec: recorder}
	}
	outcome := &ExecOutcome{
		TaskID: taskID, RunID: runID, Dir: runDir,
		ATIFPath: writer.Path(), Profile: profile,
		ProfileHash: profileHash, Overridden: opts.ProfileOverride != "",
	}
	if profile == ProfileEval && harborWriter != nil {
		// The audited ATIF is the Harbor-compatible file at the log
		// root; the rich custom trajectory stays under excalibur/.
		outcome.ATIFPath = harborWriter.Path()
	}
	outcome.MetricsPath = metricsPathFor(runDir, profile, logsDir)

	request := strings.TrimSpace(opts.Request)
	assumptions := []string{}
	if opts.NoMemory && profile == ProfileDelegate {
		assumptions = append(assumptions, "memory disabled by --no-memory")
	}

	if err := run.EmitEvent("run_started", map[string]any{
		"task_id": string(taskID), "run_id": string(runID),
		"profile": string(profile), "profile_hash": profileHash,
		"override": opts.ProfileOverride,
	}); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	if err := run.EmitStep(telemetry.ATIFStep{Role: "user", Content: opts.Request}, "message"); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}

	// The watchdog persists a thread-safe snapshot even if a tool ignores cancellation.
	var finalMu sync.Mutex
	finished := false
	stopFinal := make(chan struct{})
	finalDone := make(chan struct{})
	safego.Go("exec-final-records", nil, func() {
		defer close(finalDone)
		select {
		case <-stopFinal:
			return
		case <-ctx.Done():
		}
		finalMu.Lock()
		defer finalMu.Unlock()
		if !finished {
			_ = writeStoppedRecords(run, outcome.MetricsPath, ctx.Err())
		}
	})
	defer func() { close(stopFinal); <-finalDone }()
	finalize := func(state types.RunState, reason, summary string, question string) (*ExecOutcome, error) {
		finalMu.Lock()
		defer finalMu.Unlock()
		if ctx.Err() != nil {
			state, reason, summary, question = stoppedByTimeout, "timeout", "stopped by timeout or signal", ""
			if errors.Is(ctx.Err(), context.Canceled) && !errors.Is(context.Cause(ctx), ErrTermination) {
				reason = "signal"
			}
			// Delegated task callers use the shared durable state vocabulary.
			if opts.TaskID != "" {
				state = types.RunPaused
				if errors.Is(ctx.Err(), context.Canceled) {
					state = types.RunCanceled
				}
			}
			// Persist before streaming the closing event: stdout may be blocked
			// or disconnected when the timeout owner stops reading.
			_ = writeStoppedRecords(run, outcome.MetricsPath, ctx.Err())
		}
		finished = true
		outcome.State = state
		outcome.Reason = reason
		outcome.Summary = summary
		outcome.Question = question
		outcome.Assumptions = append([]string(nil), assumptions...)
		outcome.ExitCode = CodeForRunState(state)
		if state == stoppedByTimeout {
			outcome.ExitCode = ExitPaused
		}
		payload := map[string]any{"state": string(state), "reason": reason, "summary": summary}
		if run.FinalText != "" {
			payload["final_text"] = run.FinalText
		}
		if run.FinishEvidence != "" {
			payload["evidence"] = run.FinishEvidence
		}
		if run.stopEventWritten {
			// The stop path already persisted run_finished with state "timeout";
			// stream the closing event without a second copy in events.jsonl.
			payload["state"] = stopEventState
			if err := run.emitStdout(eventRecord("run_finished", payload)); err != nil {
				return nil, &CodedError{Code: ExitError, Err: err}
			}
		} else if err := run.EmitEvent("run_finished", payload); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		if err := finishRun(runDir, run, outcome, metrics, state, profile, logsDir); err != nil {
			return nil, err
		}
		if opts.OutputSchema != "" && state != types.RunWaiting {
			if err := ValidateAgainstSchema(outcome.ResultJSON, opts.OutputSchema); err != nil {
				return nil, &CodedError{Code: ExitError, Err: err}
			}
		}
		return outcome, nil
	}

	if ctx.Err() != nil {
		return finalize(types.RunCanceled, "context", "run canceled before start", "")
	}
	if opts.BudgetUSD != nil && *opts.BudgetUSD <= 0 {
		return finalize(types.RunPaused, "budget", "budget exhausted before start; paused", "")
	}
	if question, ok := ExtractQuestion(request); ok && opts.AllowAsk && !opts.NeverAsk {
		if err := run.EmitStep(telemetry.ATIFStep{Role: "assistant", Content: "waiting for user input"}, "waiting"); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		return finalize(types.RunWaiting, "user", "waiting for user input", question)
	}
	if request == "" {
		summary := "blocked: the request is empty, a mandatory datum (what to do) is missing"
		if err := run.EmitStep(telemetry.ATIFStep{Role: "assistant", Content: summary}, "blocked"); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		return finalize(types.RunBlocked, "missing_data", summary, "")
	}
	if question, ok := ExtractQuestion(request); ok {
		assumptions = append(assumptions, "assumed default for open question: "+question)
	}

	// Best-of-N with self-selection (Section 18.2): when enabled via
	// EXCALIBUR_ATTEMPTS, each attempt runs the normal loop in an
	// isolated working copy and only the winner's changes land in the
	// real directory. The default stays single-attempt until measured.
	if shouldFanOut(opts) {
		return runAttemptsFanOut(ctx, opts, run, outcome, metrics, profile, settings, layerCfg, finalize)
	}

	engineSummary := ""
	if opts.Work != nil {
		if err := opts.Work(ctx, run); err != nil {
			var q *NeedInputError
			if errors.As(err, &q) {
				return finalize(types.RunWaiting, "user", "waiting for user input", q.Question)
			}
			return finalize(types.RunFailed, "work", fmt.Sprintf("failed: %v", err), "")
		}
	} else if opts.Provider != nil {
		gw := BuildGateway(eff, layerCfg, opts.Provider)
		st, usage, summary, err := runEnginePath(ctx, opts, run, eff, gw, layerCfg)
		engineSummary = summary
		// Recorded runs already observed every model call (with real
		// latency, including failures) through recordingModelEndpoint;
		// the aggregate below only covers the unrecorded path so
		// eval metrics are never double-counted.
		if run.Recorder == nil && usage != (providers.Usage{}) {
			metrics.ObserveModel(types.RoleExecutor, model, usage, telemetry.PriceTable{}, 0)
		}
		if err != nil {
			if errors.Is(err, models.ErrProviderQuotaExhausted) {
				return finalize(types.RunWaiting, "provider_quota_exhausted", "provider_quota_exhausted: all configured API credentials have exhausted their quota; restore credits and restart the process", "")
			}
			var q *NeedInputError
			if errors.As(err, &q) {
				return finalize(types.RunWaiting, "user", "waiting for user input", q.Question)
			}
			return finalize(types.RunFailed, "work", fmt.Sprintf("failed: %v", err), "")
		}
		if st != "" && st != types.RunCompleted {
			reason := ""
			summary := string(st)
			if st == types.RunBlocked {
				reason = "missing_data"
				summary = "blocked"
				if engineSummary != "" {
					summary = engineSummary
				}
			} else if st == types.RunFailed {
				reason = "work"
				summary = "failed"
				if engineSummary != "" {
					summary = engineSummary
				}
			} else if st == types.RunPaused {
				// The engine parks on budget exhaustion (steps, cost,
				// time): keep the honest reason instead of an empty
				// one so attempt records and final.json stay
				// auditable.
				reason = "budget"
				summary = "paused: budget exhausted"
				if engineSummary != "" {
					summary = engineSummary
				}
			}
			return finalize(st, reason, summary, "")
		}
	} else {
		toolArgs, _ := json.Marshal(map[string]string{"request": request})
		if err := run.EmitStep(telemetry.ATIFStep{
			Role: "assistant", Content: "working on the request",
			ToolCalls: []telemetry.ATIFToolCall{{ID: "call_1", Name: "exec", Arguments: string(toolArgs)}},
		}, "tool_call"); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		if err := run.EmitStep(telemetry.ATIFStep{
			Role:       "tool",
			ToolResult: &telemetry.ATIFToolResult{CallID: "call_1", Name: "exec", Output: "ok"},
		}, "tool_result"); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		if err := run.EmitStep(telemetry.ATIFStep{Role: "assistant", Content: "done"}, "message"); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		return finalize(types.RunPaused, "timeout", "timeout expired; paused", "")
	}
	if ctx.Err() == context.Canceled {
		return finalize(types.RunCanceled, "signal", "canceled", "")
	}
	if opts.ForceOutcome != "" {
		state := opts.ForceOutcome
		reason := opts.ForceReason
		if reason == "" {
			reason = "forced"
		}
		summary := reason
		if state == types.RunWaiting {
			return finalize(state, reason, summary, outcome.Question)
		}
		return finalize(state, reason, summary, "")
	}
	// Explicit finish (agent quality): the delivery summary is the
	// engine's `finish.summary` and never echoes the instruction. Off
	// restores the legacy echo for ablation.
	if engine.ExplicitFinishSwitchOn() {
		if engineSummary != "" {
			return finalize(types.RunCompleted, "", engineSummary, "")
		}
		return finalize(types.RunCompleted, "", "completed", "")
	}
	return finalize(types.RunCompleted, "", "completed", "")
}

// resolveExecProvider decides how RunExec answers model calls and
// returns the effective wiring: an injected provider (tests,
// explicit live wiring) routes the role table at its own adapter
// name; the explicit stub keeps the deterministic embedded step;
// otherwise the operator wiring builds a registry provider. The
// returned built provider is non-nil only when RunExec constructed
// it (the caller registers and closes it). Missing wiring is a
// stable harness error (exit 1).
func resolveExecProvider(ctx context.Context, opts ExecOptions, profile ProfileName, settings models.ModelSettings, runID string) (built providers.Provider, eff models.ModelSettings, err error) {
	if opts.Provider != nil {
		eff = settings
		eff.Provider = opts.Provider.Name()
		if eff.Model == "" {
			eff.Model = "embedded"
		}
		return nil, eff, nil
	}
	if settings.Stub() {
		eff = settings
		if eff.Model == "" {
			eff.Model = models.StubProvider
		}
		return nil, eff, nil
	}
	if verr := settings.Validate(); verr != nil {
		return nil, models.ModelSettings{}, &CodedError{Code: ExitError, Err: verr}
	}
	if settings.Model == "" {
		return nil, models.ModelSettings{}, &CodedError{Code: ExitError, Err: fmt.Errorf(
			"cli: no model configured (set EXCALIBUR_MODEL, [models].model, or the operator credentials file)")}
	}
	// The eval profile never opens the vault: keys arrive by
	// environment only (Section 23.2).
	var resolve registry.SecretResolver
	if profile != ProfileEval {
		resolve = VaultSecretResolver(opts.Vault, runID)
	}
	p, berr := BuildProvider(ctx, settings, resolve)
	if berr != nil {
		return nil, models.ModelSettings{}, berr
	}
	return p, settings, nil
}

// NeedInputError parks a Work hook in waiting(user) with a question
// for the delegating client to answer via reply.
type NeedInputError struct {
	Question string
}

// Error implements error.
func (e *NeedInputError) Error() string { return "need input: " + e.Question }

func metricsPathFor(runDir string, profile ProfileName, logsDir string) string {
	if profile == ProfileEval {
		return eval.MetricsPath(logsDir)
	}
	return filepath.Join(runDir, "metrics.json")
}

func writeRunProfile(runDir string, profile ProfileName, cfg eval.EvalProfileConfig, override string, budgets EffectiveBudgets) error {
	core, deferred := EffectiveTools(profile)
	rec := map[string]any{
		"version":              cfg.Version,
		"profile":              string(profile),
		"enabled_tools":        core,
		"deferred_tools":       deferred,
		"enabled_skill_groups": cfg.EnabledSkillGroups,
		"skills_catalog_hash":  cfg.SkillsCatalogHash,
		"single_model":         cfg.SingleModel,
		"tools_hash":           ToolsHash(profile),
		"eval_profile_hash":    cfg.Hash(),
		"override":             override,
		"memory":               memoryMode(profile),
		"personal_memory":      profile == ProfileDefault,
		"timezone":             "UTC",
		"language":             "en",
		"context_window":       cfg.ContextWindow,
		"max_input":            cfg.MaxInput,
		"max_output":           cfg.MaxOutput,
		"wall_secs":            budgets.WallSecs,
		"wall_source":          budgets.WallSource,
		"deadline":             budgets.Deadline,
		"max_steps":            budgets.MaxSteps,
		"cost_cap_usd":         budgets.CostCapUSD,
		"cost_source":          budgets.CostSource,
		"time_budget_only":     budgets.TimeBudgetOnly,
		"strict_blocked":       budgets.StrictBlocked,
	}
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("cli: marshal profile: %w", err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("cli: mkdir run dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "profile.json"), append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("cli: write profile: %w", err)
	}
	hash := sha256.Sum256(buf)
	if err := os.WriteFile(filepath.Join(runDir, "profile.sha256"), []byte(hex.EncodeToString(hash[:])+"\n"), 0o644); err != nil {
		return fmt.Errorf("cli: write profile hash: %w", err)
	}
	return nil
}

// memoryMode names the Section 3.6 memory scope per profile.
func memoryMode(profile ProfileName) string {
	switch profile {
	case ProfileDefault:
		return "full"
	case ProfileDelegate:
		return "project"
	default:
		return "none"
	}
}

func finishRun(runDir string, run *ExecRun, outcome *ExecOutcome, metrics *telemetry.TaskMetrics, state types.RunState, profile ProfileName, logsDir string) error {
	if err := telemetry.ValidateFile(run.ATIF.Path()); err != nil {
		metrics.Finish("failed")
		return &CodedError{Code: ExitError, Err: fmt.Errorf("cli: invalid ATIF: %w", err)}
	}
	// Live resource peak for paths where the engine sampler never ran
	// (stub, forced outcome, early failure): feed the cumulative /proc
	// sample as a peak so metrics.json never records a zero peak.
	// Cumulative counters keep the max via ObserveDaemonPeak — never
	// the sum.
	if sample, err := newResourceSampler().Sample(os.Getpid()); err == nil {
		metrics.ObserveDaemonPeak(sample.RSSBytes, sample.CPUSeconds)
	}
	metrics.Finish(string(state))
	snap := metrics.Snapshot()
	dest := metricsPathFor(runDir, profile, logsDir)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return &CodedError{Code: ExitError, Err: fmt.Errorf("cli: mkdir metrics: %w", err)}
	}
	if err := telemetry.WriteMetricsFile(filepath.Dir(dest), snap); err != nil {
		return &CodedError{Code: ExitError, Err: err}
	}
	if dest != filepath.Join(runDir, "metrics.json") {
		if err := telemetry.WriteMetricsFile(runDir, snap); err != nil {
			return &CodedError{Code: ExitError, Err: err}
		}
	}
	if err := writeFinalRecord(runDir, string(state), outcome.Reason, outcome.ExitCode); err != nil {
		return err
	}
	// Trial recorder finalization (eval only): Harbor final metrics,
	// final state and closing transition. All writes are incremental
	// elsewhere, so a killed trial still keeps valid partial files.
	// The deterministic stub path (no engine) still gets minimal
	// gate/resource/journal rows so the record set is complete.
	if run.Recorder != nil {
		if run.Harbor != nil {
			_ = run.Harbor.AppendStep("agent", outcome.Summary, snap.Model)
			_ = run.Harbor.SetFinal(snap.Usage.Input, snap.Usage.Output+snap.Usage.Reasoning, snap.Usage.CacheRead+snap.Usage.CacheWrite, snap.CostUSD)
		}
		_ = run.Recorder.RecordTransition(string(state)+":running", string(state), outcome.Reason)
		_ = run.Recorder.RecordBudget(eval.BudgetPoint{TokensUsed: snap.Usage.Input + snap.Usage.Output, CostUSD: snap.CostUSD, WallSecs: snap.WallSeconds, WallSource: run.wallSource, Note: "final"})
		if !run.finishAuditWired {
			_ = run.Recorder.RecordGate(eval.GateAttempt{Attempt: 1, Passed: state == types.RunCompleted, Note: outcome.Summary})
		}
		// Final resource sample: the metrics peak wins, but when the
		// engine sampler never ran (stub path, early failure) the
		// snapshot holds zeros, so take a live sample rather than
		// recording a zero peak. Inside a task container the harness
		// process is the container workload, so its peak is the
		// container signal (Appendix F minimums).
		finalRSS, finalCPU := snap.DaemonRSSTotalBytes, snap.CPUSeconds
		if sample, err := newResourceSampler().Sample(os.Getpid()); err == nil {
			if sample.RSSBytes > finalRSS {
				finalRSS = sample.RSSBytes
			}
			if sample.CPUSeconds > finalCPU {
				finalCPU = sample.CPUSeconds
			}
		}
		_ = run.Recorder.RecordResource(eval.ResourceSample{HarnessRSS: finalRSS, CPUSeconds: finalCPU, Processes: 1, OOMEvents: snap.ChildOOMKills, Note: "final"})
		_ = run.Recorder.RecordJournal(0, "run_finished", string(state), string(state))
		_ = run.Recorder.RecordMask(0, 0, "final")
		_ = run.Recorder.RecordCompaction(0, 0, 0, "final")
		_ = run.Recorder.RecordLoop("final", "none", "no loop")
		run.Recorder.SetFinishDetail(eval.FinishDetail{Summary: outcome.Summary, FinalText: run.FinalText, Evidence: run.FinishEvidence})
		_ = run.Recorder.WriteFinal(string(state), outcome.Reason, outcome.ExitCode)
		// Mirror profile hashes under excalibur/ for Appendix F.
		if profile == ProfileEval {
			excaliburDir := filepath.Join(logsDir, "excalibur")
			for _, name := range []string{"profile.json", "profile.sha256", "budgets.json", "services.json", "explicit_finish.json"} {
				src := filepath.Join(runDir, name)
				dst := filepath.Join(excaliburDir, name)
				if raw, err := os.ReadFile(src); err == nil {
					_ = os.WriteFile(dst, raw, 0o644)
				}
			}
		}
	}
	doc := map[string]any{
		"state": string(state), "summary": outcome.Summary,
		"task_id": string(outcome.TaskID), "run_id": string(outcome.RunID),
	}
	if outcome.Reason != "" {
		doc["reason"] = outcome.Reason
	}
	if len(outcome.Assumptions) > 0 {
		doc["assumptions"] = outcome.Assumptions
	}
	if outcome.Question != "" {
		doc["question"] = outcome.Question
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return &CodedError{Code: ExitError, Err: fmt.Errorf("cli: marshal result: %w", err)}
	}
	outcome.ResultJSON = append(buf, '\n')
	if err := os.WriteFile(filepath.Join(runDir, "result.json"), outcome.ResultJSON, 0o644); err != nil {
		return &CodedError{Code: ExitError, Err: fmt.Errorf("cli: write result: %w", err)}
	}
	if err := writeEventsFile(runDir, run.Events); err != nil {
		return &CodedError{Code: ExitError, Err: err}
	}
	return nil
}

func writeEventsFile(runDir string, events []map[string]any) error {
	f, err := os.OpenFile(filepath.Join(runDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("cli: open events: %w", err)
	}
	defer f.Close()
	for _, ev := range events {
		buf, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("cli: marshal event: %w", err)
		}
		if _, err := fmt.Fprintf(f, "%s\n", buf); err != nil {
			return fmt.Errorf("cli: write event: %w", err)
		}
	}
	return f.Close()
}
