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
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
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

// runAttemptsFanOut executes N isolated attempts of the same request,
// selects the attempt whose own executable checks pass, applies only
// the winner's changes to the real working directory and finalizes
// the outer run with the winner's state. Attempt budgets share the
// task budget: steps and explicit USD split evenly, the wall clock is
// shared via the parent context.
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
	cfg := attempts.ConfigFromEnv()
	n := cfg.N
	if n < 2 {
		return nil, fmt.Errorf("cli: attempts fan-out needs N>=2")
	}
	src := opts.ProjectDir
	_ = run.EmitEvent("attempts_started", map[string]any{"attempts": n})

	workDirs, cleanup, err := attempts.PrepareCopies(src, n, "")
	if err != nil {
		return finalize(types.RunFailed, "work", fmt.Sprintf("failed: prepare attempts: %v", err), "")
	}
	defer cleanup()

	logDirs := make([]string, n)
	for i := range logDirs {
		logDirs[i] = filepath.Join(run.Dir, "attempts", fmt.Sprintf("attempt-%d", i))
	}

	perStep := attempts.DivideSteps(opts.MaxSteps, n)
	perBudget := attempts.DivideBudget(opts.BudgetUSD, n)

	outcomes := attempts.RunConcurrent(ctx, cfg.MaxConcurrency, workDirs, logDirs,
		func(actx context.Context, idx int, workDir, logDir string) (attempts.Outcome, error) {
			start := time.Now()
			sub := opts
			sub.ProjectDir = workDir
			sub.LogsDir = logDir
			sub.RunDir = logDir
			sub.TaskID = ""
			sub.RunID = ""
			sub.EmitJSON = false
			sub.Emit = nil
			sub.MaxSteps = perStep
			sub.BudgetUSD = perBudget
			sub.DisableAttempts = true
			sub.JournalStore = nil
			subOut, subErr := RunExec(actx, sub)
			oc := attempts.Outcome{Index: idx, WorkDir: workDir, LogDir: logDir}
			if subErr != nil {
				oc.State = string(types.RunFailed)
				oc.Reason = "work"
				oc.Summary = fmt.Sprintf("failed: %v", subErr)
				oc.DurationSecs = time.Since(start).Seconds()
				fillAttemptEvidence(src, &oc)
				return oc, nil
			}
			oc.State = string(subOut.State)
			oc.Summary = subOut.Summary
			oc.Reason = subOut.Reason
			oc.Question = subOut.Question
			if snap, serr := readAttemptSnapshot(logDir); serr == nil {
				var calls int64
				for _, v := range snap.CallsByTool {
					calls += v
				}
				oc.ToolCalls = calls
				oc.GateAttempts = snap.GateAttempts
				if snap.WallSeconds > 0 {
					oc.DurationSecs = snap.WallSeconds
				} else {
					oc.DurationSecs = time.Since(start).Seconds()
				}
				// Aggregate the attempt spend into the outer metrics
				// so the trial total reflects best-of-N, not the
				// winner alone.
				metrics.MergeSnapshot(snap)
			} else {
				oc.DurationSecs = time.Since(start).Seconds()
			}
			oc.ToolErrors = int64(countAttemptToolErrors(logDir))
			if oc.ToolCalls-oc.ToolErrors < 0 {
				oc.Verified = 0
			} else {
				oc.Verified = oc.ToolCalls - oc.ToolErrors
			}
			fillAttemptEvidence(src, &oc)
			return oc, nil
		})

	// Self-selection: executable checks first, then more verified
	// checks, then fewer errors. An exact tie goes to the cheap model
	// comparison of evidence; without a gateway it stays launch order.
	winnerPos := attempts.SelectBest(outcomes)
	if tied := attempts.Tied(outcomes, winnerPos); len(tied) > 1 && opts.Provider != nil {
		if pick := pickWinnerWithModel(ctx, settings, layerCfg, opts.Provider, opts.Request, tied, outcomes); pick >= 0 {
			for _, t := range tied {
				if outcomes[t].Index == pick {
					for i, o := range outcomes {
						if o.Index == pick {
							winnerPos = i
							break
						}
					}
					break
				}
			}
		}
	}
	winner := outcomes[winnerPos]

	// Apply only the winner's changes to the real directory.
	var applied []string
	if strings.TrimSpace(src) != "" {
		var aerr error
		applied, aerr = attempts.ApplyWinner(winner.WorkDir, src)
		if aerr != nil {
			_ = run.EmitEvent("attempts_apply_failed", map[string]any{"error": aerr.Error()})
			return finalize(types.RunFailed, "work", fmt.Sprintf("failed: apply winner: %v", aerr), "")
		}
	}

	rec := attempts.ToRecord(outcomes, winner.Index)
	_ = attempts.WriteRecord(run.Dir, rec)
	if run.Recorder != nil {
		_ = attempts.WriteRecord(run.Recorder.Dir(), rec)
	}
	_ = run.EmitEvent("attempts_finished", map[string]any{
		"attempts": n, "winner": winner.Index, "winner_state": winner.State,
		"applied_files": len(applied),
	})
	summary := winner.Summary
	if summary == "" {
		summary = string(winner.State)
	}
	if n > 1 && !strings.Contains(summary, "best of") {
		summary += fmt.Sprintf(" [best of %d attempts]", n)
	}
	_ = run.EmitStep(telemetry.ATIFStep{
		Role: "assistant",
		Content: fmt.Sprintf("Best-of-%d selection: attempt %d wins (%s, %d verified, %d errors; %s). Applied %d file(s) to the working directory.",
			n, winner.Index, winner.State, winner.Verified, winner.ToolErrors, winner.DiffStat, len(applied)),
	}, "attempts_selected")

	if ctx.Err() == context.DeadlineExceeded {
		return finalize(types.RunPaused, "timeout", "timeout expired; paused", "")
	}
	if ctx.Err() == context.Canceled {
		return finalize(types.RunCanceled, "signal", "canceled", "")
	}
	return finalize(types.RunState(winner.State), winner.Reason, summary, winner.Question)
}

// fillAttemptEvidence computes the file diff of one attempt against
// the original directory for the selection record.
func fillAttemptEvidence(src string, oc *attempts.Outcome) {
	if strings.TrimSpace(src) == "" {
		files := listWorkFiles(oc.WorkDir, 50)
		oc.FilesChanged = files
		if len(files) == 0 {
			oc.DiffStat = "no changes"
		} else {
			oc.DiffStat = fmt.Sprintf("%d files in isolated attempt dir", len(files))
		}
		return
	}
	changed, added, deleted, err := attempts.DiffDirs(src, oc.WorkDir)
	if err != nil {
		oc.DiffStat = "diff unavailable"
		return
	}
	all := append(append(append([]string{}, changed...), added...), deleted...)
	if len(all) > 50 {
		all = all[:50]
	}
	oc.FilesChanged = all
	oc.DiffStat = attempts.DiffStatLine(changed, added, deleted)
}

func listWorkFiles(root string, cap int) []string {
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(out) >= cap {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == ".excalibur" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	return out
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
		for _, pos := range outcomes {
			if pos.Index == t {
				o = pos
				break
			}
		}
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
	text := strings.TrimSpace(resp.Message.Content)
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool { return r < '0' || r > '9' }) {
		_ = tok
	}
	// Parse the first integer in the reply.
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
