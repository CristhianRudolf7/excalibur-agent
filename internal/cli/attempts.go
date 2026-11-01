package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/attempts"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// attemptsCount reads the EXCALIBUR_ATTEMPTS switch (1 means off, the
// default until measured). Values clamp to attempts.MaxAttempts.
func attemptsCount() (int, bool) {
	cfg := attempts.ConfigFromEnv()
	return cfg.N, cfg.Enabled()
}

// shouldFanOut reports whether RunExec fans out to best-of-N attempts.
// The deterministic stub (no Work, no Provider) never fans out: with
// no model and no verification there is nothing to select between.
// Forced outcomes (exit-table tests) also stay single.
func shouldFanOut(opts ExecOptions) bool {
	if opts.DisableAttempts {
		return false
	}
	if opts.ForceOutcome != "" {
		return false
	}
	_, enabled := attemptsCount()
	if !enabled {
		return false
	}
	return opts.Work != nil || opts.Provider != nil
}

// runAttemptsFanOut executes N attempts of the same request, each in its
// own full copy of the real working directory, selects the winner by
// its own evidence (named outputs first, then changes, verified checks
// and fewer changed files), promotes only the winner's changes into the
// real directory and finalizes the outer run with the winner's state.
//
// When the promotion fails (apply error, failed verification, or
// "applied 0 file(s)" for a winner that changed files), the run falls
// back to one normal attempt in the real directory with the remaining
// time (EXCALIBUR_ATTEMPTS_FALLBACK). Each attempt keeps the full
// parent budget (steps and explicit USD) and shares the wall clock
// through the parent context.
func runAttemptsFanOut(
	ctx context.Context,
	opts ExecOptions,
	run *ExecRun,
	outcome *ExecOutcome,
	metrics *telemetry.TaskMetrics,
	profile ProfileName,
	settings models.ModelSettings,
	layerCfg config.Config,
	finalize func(types.RunState, string, string, string) (*ExecOutcome, error),
) (*ExecOutcome, error) {
	fanStart := time.Now()
	cfg := attempts.ConfigFromEnv()
	n := cfg.N
	if n < 2 {
		return nil, fmt.Errorf("cli: attempts fan-out needs N>=2")
	}
	src := strings.TrimSpace(opts.ProjectDir)
	isolated := src != ""
	_ = run.EmitEvent("attempts_started", map[string]any{"attempts": n, "isolated": isolated})

	ws, baseline, cleanup, err := attempts.PrepareWorkspaces(opts.ProjectDir, n, "")
	if err != nil {
		return runFallbackOnly(ctx, opts, run, metrics, n, fmt.Sprintf("prepare attempts: %v", err), finalize)
	}
	defer cleanup()

	logDirs := make([]string, n)
	workDirs := make([]string, n)
	for i := range logDirs {
		logDirs[i] = filepath.Join(run.Dir, "attempts", fmt.Sprintf("attempt-%d", i))
		workDirs[i] = ws[i].Dir
	}

	perStep, perBudget := attemptBudgets(opts, profile, n)
	reqs := attempts.DeliverableRequirements(opts.Request)
	mapPaths := isolated && attempts.PathMapEnabled()

	outcomes := attempts.RunConcurrent(ctx, cfg.MaxConcurrency, workDirs, logDirs,
		func(actx context.Context, idx int, _, logDir string) (attempts.Outcome, error) {
			var m *attempts.WorkdirMap
			if mapPaths {
				mapping := ws[idx].Map
				m = &mapping
			}
			return runOneAttempt(actx, opts, ws[idx], logDir, baseline, reqs, metrics, perStep, perBudget, m), nil
		})

	// Self-selection (see attempts.selectionKey). An exact tie goes to
	// the cheap model comparison of evidence; without a gateway it stays
	// launch order.
	winnerPos := attempts.SelectBest(outcomes)
	if tied := attempts.Tied(outcomes, winnerPos); len(tied) > 1 && opts.Provider != nil {
		if pick := pickWinnerWithModel(ctx, settings, layerCfg, opts.Provider, opts.Request, tied, outcomes); pick >= 0 {
			for i, o := range outcomes {
				if o.Index == pick {
					winnerPos = i
					break
				}
			}
		}
	}
	winner := outcomes[winnerPos]

	// Isolation audit: attempts never write to the real directory, so
	// anything that changed there is a command that escaped the mapping.
	if isolated {
		if touched, terr := attempts.Touched(baseline, src); terr == nil && touched.Total() > 0 {
			_ = run.EmitEvent("attempts_real_dir_touched", map[string]any{
				"count": touched.Total(), "paths": capStrings(touched.Paths(), 20),
			})
		}
	}

	// Promote the winner's changes. Any failure, including an apply that
	// moves zero files for a winner that changed files, is recorded and
	// handed to the fallback.
	var applied []string
	var applyErr error
	if isolated {
		applied, applyErr = attempts.ApplyChanges(winner.WorkDir, src, baseline)
	}
	fallbackReason := ""
	if applyErr != nil {
		_ = run.EmitEvent("attempts_apply_failed", map[string]any{"error": applyErr.Error(), "winner": winner.Index})
		fallbackReason = "apply: " + applyErr.Error()
	}
	if fallbackReason != "" && !attempts.FallbackEnabled() {
		return finalize(types.RunFailed, "work", fmt.Sprintf("failed: apply winner: %v", applyErr), "")
	}

	var fb *ExecOutcome
	fallbackLog := filepath.Join(run.Dir, "attempts", "fallback")
	if fallbackReason != "" {
		_ = run.EmitEvent("attempts_fallback", map[string]any{"reason": fallbackReason})
		// The fallback gets only the wall time the attempts left.
		fbOpts := opts
		if opts.TimeoutSecs > 0 {
			left := opts.TimeoutSecs - int64(time.Since(fanStart).Seconds())
			fbOpts.TimeoutSecs = max(left, 1)
		}
		fb, err = runSubRun(ctx, fbOpts, src, fallbackLog, opts.MaxSteps, opts.BudgetUSD, nil)
		mergeAttemptMetrics(metrics, fallbackLog)
		if err != nil {
			fb = &ExecOutcome{State: types.RunFailed, Reason: "work", Summary: fmt.Sprintf("failed: %v", err)}
		}
	}

	// Merge every attempt's model/tool calls (and the fallback's) into
	// the outer trial records tagged by attempt, so standard evaluation
	// and auditing sees the work even though the outer run never calls
	// the model itself. Gated by EXCALIBUR_ATTEMPTS_MERGE_RECORDS.
	logs := make([]attemptLog, 0, n+1)
	for i, d := range logDirs {
		logs = append(logs, attemptLog{index: i, dir: d})
	}
	if fb != nil {
		logs = append(logs, attemptLog{index: n, dir: fallbackLog, fallback: true})
	}
	if run.Recorder != nil && attempts.MergeRecordsEnabled() {
		mergeAttemptRecords(run.Recorder, run, logs)
	}

	rec := attempts.ToRecord(outcomes, winner.Index)
	rec.Isolated = isolated
	rec.AppliedFiles = applied
	rec.AppliedFileCount = len(applied)
	rec.Fallback = fallbackReason
	if err := attempts.WriteRecord(run.Dir, rec); err != nil {
		_ = run.EmitEvent("attempts_record_failed", map[string]any{"error": err.Error()})
	}
	if run.Recorder != nil {
		if err := attempts.WriteRecord(run.Recorder.Dir(), rec); err != nil {
			_ = run.EmitEvent("attempts_record_failed", map[string]any{"error": err.Error()})
		}
	}
	_ = run.EmitEvent("attempts_finished", map[string]any{
		"attempts": n, "winner": winner.Index, "winner_state": winner.State,
		"applied_files": len(applied), "fallback": fb != nil,
	})
	_ = run.EmitStep(telemetry.ATIFStep{
		Role: "assistant",
		Content: fmt.Sprintf("Best-of-%d selection: attempt %d wins (%s, %d verified, %d errors; %s). Applied %d file(s) to the working directory.",
			n, winner.Index, winner.State, winner.Verified, winner.ToolErrors, winner.DiffStat, len(applied)),
	}, "attempts_selected")

	if fb != nil {
		summary := fb.Summary
		if summary == "" {
			summary = string(fb.State)
		}
		return finalize(fb.State, fb.Reason, summary+" [attempts fallback: "+fallbackReason+"]", fb.Question)
	}

	// The named outputs are re-checked on the real directory after the
	// promotion (the tree the run actually leaves behind). A winner whose
	// outputs are still missing does not finish as completed.
	winnerState := types.RunState(winner.State)
	pending := winner.Deliverable
	if isolated && len(reqs) > 0 && attempts.DeliverableEnabled() {
		pending = attempts.CheckDeliverables(reqs, attempts.WorkdirMap{}, src)
		if len(pending) > 0 {
			_ = run.EmitEvent("attempts_deliverable_missing", map[string]any{"pendings": capStrings(pending, 5)})
		}
	}
	if len(pending) > 0 && winnerState == types.RunCompleted {
		winnerState = types.RunCompletedPending
	}

	if ctx.Err() == context.DeadlineExceeded {
		return finalize(types.RunPaused, "timeout", "timeout expired; paused", "")
	}
	if ctx.Err() == context.Canceled {
		return finalize(types.RunCanceled, "signal", "canceled", "")
	}
	summary := winner.Summary
	if summary == "" {
		summary = string(winner.State)
	}
	if !strings.Contains(summary, "best of") {
		summary += fmt.Sprintf(" [best of %d attempts]", n)
	}
	// A bare paused winner (engine budget exhaustion) must carry an
	// honest reason instead of an empty one, or the outer final.json
	// ends as paused with no reason and the summary reads only
	// "paused [best of N attempts]".
	winnerReason := winner.Reason
	if winnerState == types.RunPaused && strings.TrimSpace(winnerReason) == "" {
		winnerReason = "budget"
	}
	if strings.TrimSpace(summary) == "paused" || strings.TrimSpace(summary) == "paused [best of "+strconv.Itoa(n)+" attempts]" {
		summary = "paused: budget exhausted [best of " + strconv.Itoa(n) + " attempts]"
	}
	return finalize(winnerState, winnerReason, summary, winner.Question)
}

// attemptBudgets returns the per-attempt step and USD caps. Each attempt
// gets the full parent budget by default (see attempts.StepsForAttempt);
// in time-only mode (W8) every attempt gets the safety caps so no attempt
// pauses before the shared wall edge.
func attemptBudgets(opts ExecOptions, profile ProfileName, n int) (int, *float64) {
	eff := EffectiveBudgetsFor(opts, profile)
	perStep := attempts.StepsForAttempt(opts.MaxSteps, n)
	perBudget := attempts.BudgetForAttempt(opts.BudgetUSD, n)
	if eff.TimeBudgetOnly && attempts.FullBudgetEnabled() {
		perStep = eff.MaxSteps
		if eff.CostCapUSD > 0 {
			c := eff.CostCapUSD
			perBudget = &c
		} else {
			perBudget = nil
		}
	}
	return perStep, perBudget
}

// runSubRun executes one normal engine run inside workDir with its own
// logs. Fan-out sub-runs never recurse into another fan-out.
func runSubRun(ctx context.Context, opts ExecOptions, workDir, logDir string, maxSteps int, budget *float64, mapping *attempts.WorkdirMap) (*ExecOutcome, error) {
	sub := opts
	sub.ProjectDir = workDir
	sub.LogsDir = logDir
	sub.RunDir = logDir
	sub.TaskID = ""
	sub.RunID = ""
	sub.EmitJSON = false
	sub.Emit = nil
	sub.MaxSteps = maxSteps
	sub.BudgetUSD = budget
	sub.DisableAttempts = true
	sub.JournalStore = nil
	sub.attemptMap = mapping
	return RunExec(ctx, sub)
}

// runOneAttempt runs the normal loop for one attempt in its workspace and
// reports the attempt's own evidence: its terminal state, its tool
// counts, the named outputs in its copy and the changes it made against
// the baseline.
func runOneAttempt(
	actx context.Context,
	opts ExecOptions,
	ws attempts.Workspace,
	logDir string,
	baseline attempts.Manifest,
	reqs []tasks.ArtifactRequirement,
	metrics *telemetry.TaskMetrics,
	perStep int,
	perBudget *float64,
	mapping *attempts.WorkdirMap,
) attempts.Outcome {
	start := time.Now()
	oc := attempts.Outcome{Index: ws.Index, WorkDir: ws.Dir, LogDir: logDir}
	subOut, subErr := runSubRun(actx, opts, ws.Dir, logDir, perStep, perBudget, mapping)
	if subErr != nil {
		oc.State = string(types.RunFailed)
		oc.Reason = "work"
		oc.Summary = fmt.Sprintf("failed: %v", subErr)
	} else {
		oc.State = string(subOut.State)
		oc.Summary = subOut.Summary
		oc.Reason = subOut.Reason
		oc.Question = subOut.Question
	}
	oc.DurationSecs = time.Since(start).Seconds()
	if snap, serr := readAttemptSnapshot(logDir); serr == nil {
		var calls int64
		for _, v := range snap.CallsByTool {
			calls += v
		}
		oc.ToolCalls = calls
		oc.GateAttempts = snap.GateAttempts
		if snap.WallSeconds > 0 {
			oc.DurationSecs = snap.WallSeconds
		}
		// Aggregate the attempt spend into the outer metrics so the trial
		// total reflects best-of-N, not the winner alone.
		metrics.MergeSnapshot(snap)
	}
	oc.ToolErrors = int64(countAttemptToolErrors(logDir))
	oc.Verified = oc.ToolCalls - oc.ToolErrors
	if oc.Verified < 0 {
		oc.Verified = 0
	}
	fillAttemptEvidence(ws, baseline, &oc)
	if len(reqs) > 0 && attempts.DeliverableEnabled() {
		oc.Deliverable = attempts.CheckDeliverables(reqs, ws.Map, ws.Dir)
	}
	return oc
}

// fillAttemptEvidence records the attempt's changes against the baseline
// of the real directory (or against an empty tree when there is none).
func fillAttemptEvidence(ws attempts.Workspace, baseline attempts.Manifest, oc *attempts.Outcome) {
	cur, err := attempts.Scan(ws.Dir)
	if err != nil {
		oc.DiffStat = "diff unavailable"
		return
	}
	ch := attempts.Diff(baseline, cur)
	oc.FilesChanged = capStrings(ch.Paths(), 50)
	oc.ChangedCount = ch.Files
	oc.DiffStat = ch.Summary()
}

// runFallbackOnly handles a fan-out that could not start: with the
// fallback switch on, one normal attempt runs in the real directory.
func runFallbackOnly(
	ctx context.Context,
	opts ExecOptions,
	run *ExecRun,
	metrics *telemetry.TaskMetrics,
	n int,
	reason string,
	finalize func(types.RunState, string, string, string) (*ExecOutcome, error),
) (*ExecOutcome, error) {
	if !attempts.FallbackEnabled() {
		return finalize(types.RunFailed, "work", "failed: "+reason, "")
	}
	_ = run.EmitEvent("attempts_fallback", map[string]any{"reason": reason})
	logDir := filepath.Join(run.Dir, "attempts", "fallback")
	fb, err := runSubRun(ctx, opts, opts.ProjectDir, logDir, opts.MaxSteps, opts.BudgetUSD, nil)
	mergeAttemptMetrics(metrics, logDir)
	if run.Recorder != nil && attempts.MergeRecordsEnabled() {
		mergeAttemptRecords(run.Recorder, run, []attemptLog{{index: n, dir: logDir, fallback: true}})
	}
	if err != nil {
		return finalize(types.RunFailed, "work", fmt.Sprintf("failed: %v", err), "")
	}
	summary := fb.Summary
	if summary == "" {
		summary = string(fb.State)
	}
	return finalize(fb.State, fb.Reason, summary+" [attempts fallback: "+reason+"]", fb.Question)
}

// mergeAttemptMetrics folds one sub-run's metrics snapshot into the outer
// metrics (the fallback's spend counts toward the trial total too).
func mergeAttemptMetrics(metrics *telemetry.TaskMetrics, logDir string) {
	if snap, err := readAttemptSnapshot(logDir); err == nil {
		metrics.MergeSnapshot(snap)
	}
}

// attemptPathRunner maps one attempt's tool-call paths and shell commands
// onto its own copy before the tool runs. The recording layers wrap it,
// so the records keep the arguments the model actually sent.
type attemptPathRunner struct {
	base engine.ToolRunner
	m    attempts.WorkdirMap
}

func (r attemptPathRunner) Definition(name string) (tools.Definition, error) {
	return r.base.Definition(name)
}

func (r attemptPathRunner) Execute(ctx context.Context, name string, args json.RawMessage, o tools.ExecuteOptions) (tools.Result, error) {
	return r.base.Execute(ctx, name, r.m.MapArgs(name, args), o)
}

// attemptArtifactStat resolves the engine's required-artifact checks
// through the attempt map, so the gate inside an attempt checks the
// attempt's copy. Nil keeps the default filesystem checks.
func attemptArtifactStat(m *attempts.WorkdirMap) tasks.StatFunc {
	if m == nil {
		return nil
	}
	mapping := *m
	return func(p string) (tasks.ArtifactFS, error) {
		return tasks.DefaultStat(mapping.Resolve(p))
	}
}

// attemptArtifactRead is the content counterpart of attemptArtifactStat.
func attemptArtifactRead(m *attempts.WorkdirMap) tasks.ReadFunc {
	if m == nil {
		return nil
	}
	mapping := *m
	return func(p string) (string, error) {
		return tasks.DefaultRead(mapping.Resolve(p))
	}
}

// readAttemptSnapshot loads one attempt metrics.json (runDir copy,
// which exists for every profile).
func readAttemptSnapshot(logDir string) (telemetry.Snapshot, error) {
	var snap telemetry.Snapshot
	for _, cand := range []string{
		filepath.Join(logDir, "metrics.json"),
		filepath.Join(logDir, "excalibur", "metrics.json"),
	} {
		raw, err := os.ReadFile(cand)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(raw, &snap); err != nil {
			continue
		}
		return snap, nil
	}
	return snap, fmt.Errorf("cli: no attempt metrics in %q", logDir)
}

// countAttemptToolErrors counts is_error tool calls in one attempt's
// eval record; non-eval attempts (no recorder) report zero.
func countAttemptToolErrors(logDir string) int {
	path := filepath.Join(logDir, "excalibur", "tool_calls.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var row struct {
			IsError bool `json:"is_error"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			continue
		}
		if row.IsError {
			count++
		}
	}
	return count
}

// pickWinnerWithModel runs the cheap model comparison of evidence over
// an exact deterministic tie: one short call comparing the tied
// attempts' summaries, file changes and error counts. It returns the
// winning attempt Index, or -1 to keep the deterministic winner.
func pickWinnerWithModel(ctx context.Context, settings models.ModelSettings, layerCfg config.Config, p providers.Provider, request string, tied []int, outcomes []OutcomeAlias) int {
	if len(tied) < 2 || p == nil {
		return -1
	}
	byIndex := map[int]attempts.Outcome{}
	for _, o := range outcomes {
		byIndex[o.Index] = o
	}
	var b strings.Builder
	b.WriteString("Pick the best of these independent attempts at the same task. Reply with only the attempt number (e.g. 0).\n\nTask:\n")
	b.WriteString(strings.TrimSpace(request) + "\n\n")
	for _, t := range tied {
		o := byIndex[t]
		b.WriteString(fmt.Sprintf("Attempt %d: state=%s verified=%d errors=%d tools=%d files=%s\nsummary: %s\n\n",
			o.Index, o.State, o.Verified, o.ToolErrors, o.ToolCalls, o.DiffStat, truncateEvidence(o.Summary, 400)))
	}
	gw := BuildGateway(settings, layerCfg, p)
	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: providers.RoleUser, Content: b.String()},
		},
		MaxTokens:       16,
		ReasoningEffort: "low",
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, _, err := gw.Chat(cctx, types.RoleVerifier, req)
	if err != nil {
		return -1
	}
	// Parse the first integer in the reply.
	text := strings.TrimSpace(resp.Message.Content)
	num := ""
	for _, r := range text {
		if r >= '0' && r <= '9' {
			num += string(r)
		} else if num != "" {
			break
		}
	}
	if num == "" {
		return -1
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return -1
	}
	for _, t := range tied {
		if t == n {
			return n
		}
	}
	return -1
}

// attemptLog names one attempt's log directory for the record merge.
// The fallback run is tagged with the index one past the last attempt
// and fallback=true.
type attemptLog struct {
	index    int
	dir      string
	fallback bool
}

// mergeAttemptRecords folds every attempt's model/tool calls (and gate
// checks) into the outer trial recorder tagged by attempt, and copies
// content-addressed blobs. It runs after the fan-out and the fallback
// join, so no locking beyond the recorder's own append is needed.
// Missing per-attempt files are skipped; a merge failure never fails the
// run (the per-attempt logs under <run>/attempts/attempt-i stay
// canonical).
func mergeAttemptRecords(outer *eval.TrialRecorder, run *ExecRun, logs []attemptLog) {
	if outer == nil {
		return
	}
	dstDir := outer.Dir()
	_ = os.MkdirAll(filepath.Join(dstDir, "blobs"), 0o755)
	modelTotal, toolTotal := 0, 0
	for _, lg := range logs {
		srcEx := filepath.Join(lg.dir, "excalibur")
		// Blobs first so merged tool rows always resolve.
		if entries, err := os.ReadDir(filepath.Join(srcEx, "blobs")); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				src := filepath.Join(srcEx, "blobs", e.Name())
				dst := filepath.Join(dstDir, "blobs", e.Name())
				if _, err := os.Stat(dst); err == nil {
					continue
				}
				if raw, err := os.ReadFile(src); err == nil {
					_ = os.WriteFile(dst, raw, 0o644)
				}
			}
		}
		// Model and tool calls tagged by attempt. Merged via generic
		// maps (not the typed structs) so attempt 0 stays explicit: the
		// typed structs use omitempty and would drop the zero tag. Rows
		// keep their attempt-local seq; the attempt tag disambiguates
		// across attempts for the audit. Blobs are copied above, so
		// hashes stay byte-identical.
		if raw, err := os.ReadFile(filepath.Join(srcEx, "model_calls.jsonl")); err == nil {
			for _, line := range splitJSONLLines(raw) {
				var m map[string]any
				if err := json.Unmarshal(line, &m); err != nil {
					continue
				}
				tagAttempt(m, lg)
				modelTotal++
				appendTaggedJSONL(filepath.Join(dstDir, "model_calls.jsonl"), m)
			}
		}
		if raw, err := os.ReadFile(filepath.Join(srcEx, "tool_calls.jsonl")); err == nil {
			for _, line := range splitJSONLLines(raw) {
				var t map[string]any
				if err := json.Unmarshal(line, &t); err != nil {
					continue
				}
				tagAttempt(t, lg)
				toolTotal++
				appendTaggedJSONL(filepath.Join(dstDir, "tool_calls.jsonl"), t)
			}
		}
		// Gate checks tagged by fanout attempt for the audit.
		if raw, err := os.ReadFile(filepath.Join(srcEx, "gate_attempts.jsonl")); err == nil {
			for _, line := range splitJSONLLines(raw) {
				var g map[string]any
				if err := json.Unmarshal(line, &g); err != nil {
					continue
				}
				g["fanout_attempt"] = lg.index
				if lg.fallback {
					g["fallback"] = true
				}
				appendTaggedJSONL(filepath.Join(dstDir, "gate_attempts.jsonl"), g)
			}
		}
	}
	// Advance the outer seq counters past the merged rows so any later
	// outer recording continues without seq reuse.
	if toolTotal > run.toolSeq {
		run.toolSeq = toolTotal
	}
	if modelTotal > run.modelSeq {
		run.modelSeq = modelTotal
	}
}

func tagAttempt(row map[string]any, lg attemptLog) {
	row["attempt"] = lg.index
	if lg.fallback {
		row["fallback"] = true
	}
}

// splitJSONLLines splits raw JSONL bytes into non-empty lines.
func splitJSONLLines(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range raw {
		if b == '\n' {
			if i > start {
				out = append(out, raw[start:i])
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		// Trailing line without newline.
		line := raw[start:]
		if len(line) > 0 {
			out = append(out, line)
		}
	}
	return out
}

// appendTaggedJSONL appends one value as a JSONL line (used for merged
// attempt rows where the recorder's blob re-hash must be skipped).
func appendTaggedJSONL(path string, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		return
	}
	buf = append(buf, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(buf)
	_ = f.Sync()
	_ = f.Close()
}

// OutcomeAlias keeps the model-compare helper free of a direct
// dependency on the outcomes slice element type name.
type OutcomeAlias = attempts.Outcome

func truncateEvidence(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// capStrings returns at most n leading entries of xs.
func capStrings(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return xs[:n]
}

// attemptPathRunnerFor wraps base with the attempt mapping. A nil map
// returns base unchanged (single runs and the fallback).
func attemptPathRunnerFor(base engine.ToolRunner, m *attempts.WorkdirMap) engine.ToolRunner {
	if m == nil {
		return base
	}
	return attemptPathRunner{base: base, m: *m}
}
