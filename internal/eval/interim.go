package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Interim paired estimate on partial runs (measurement workstream,
// report side only).
//
// `bench interim` answers "should this run continue?" from the tasks
// completed so far: it pairs the current run's per-task success rates
// against a reference (a finished run directory or a baseline file
// {"baseline_rate": {task: rate}}) and reuses the Section 23.7 paired
// bootstrap. The early-stop verdict is STOP when the paired
// difference CI upper bound sits below the reference by more than the
// practical bar (even the optimistic end of the interval is worse by
// more than bar), and CONTINUE otherwise. A partial run that is still
// executing is fine: only trials already completed (reward readable)
// enter the estimate.

// InterimBarSwitch optionally overrides the practical bar from the
// environment; the CLI --bar flag wins over it.
const InterimBarSwitch = "EXCALIBUR_INTERIM_BAR"

// InterimBar resolves the practical bar: CLI flag first, then the
// EXCALIBUR_INTERIM_BAR environment override, then the Section 23.7
// default.
func InterimBar(flag float64) float64 {
	if flag > 0 {
		return flag
	}
	if v := strings.TrimSpace(os.Getenv(InterimBarSwitch)); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil && f > 0 {
			return f
		}
	}
	return DefaultPracticalBar
}

// InterimOptions configures InterimEstimate.
type InterimOptions struct {
	// Against is the reference: a run directory or a baseline JSON
	// file ({"baseline_rate": {task: rate}}).
	Against string
	Reps    int
	Seed    int64
	Bar     float64
}

// InterimTaskRow is one completed task in the interim estimate.
type InterimTaskRow struct {
	Task      string  `json:"task"`
	Trials    int     `json:"trials"`
	Successes int     `json:"successes"`
	Rate      float64 `json:"rate"`
	RefRate   float64 `json:"ref_rate"`
	Diff      float64 `json:"diff"`
}

// InterimReport is the paired estimate on tasks completed so far.
type InterimReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	RunDir      string    `json:"run_dir"`
	Against     string    `json:"against"`
	AgainstKind string    `json:"against_kind"`
	Reps        int       `json:"bootstrap_reps"`
	Seed        int64     `json:"seed"`

	CompletedTasks []string         `json:"completed_tasks"`
	ByTask         []InterimTaskRow `json:"by_task"`

	Current   MeanCI  `json:"current"`
	Reference float64 `json:"reference_mean"`
	Diff      MeanCI  `json:"paired_diff_vs_ref"`

	PracticalBar float64 `json:"practical_bar"`
	Verdict      string  `json:"verdict"`
}

// Interim verdicts.
const (
	InterimStop     = "STOP"
	InterimContinue = "CONTINUE"
)

// isRunDir reports whether path looks like a run directory: it holds
// trials.jsonl, trial.json files, or Harbor trial subdirectories with
// result.json.
func isRunDir(path string) bool {
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "trials.jsonl")); err == nil {
		return true
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			if e.Name() == "trial.json" || strings.HasSuffix(e.Name(), ".json") {
				return true
			}
			continue
		}
		if _, err := os.Stat(filepath.Join(path, e.Name(), "result.json")); err == nil {
			return true
		}
		if _, err := os.Stat(filepath.Join(path, e.Name(), "trial.json")); err == nil {
			return true
		}
	}
	return false
}

// LoadPartialRunTrials loads whatever trials are completed so far: the
// run-level trials.jsonl when present (finished runs and any
// incrementally written index), falling back to per-trial Harbor
// result.json directories (partial Harbor jobs still executing, where
// finalize has not written the index yet), and finally to trial.json
// files (quick/smoke partial dirs).
func LoadPartialRunTrials(runDir string) ([]TrialOutcome, error) {
	if raw, err := os.ReadFile(filepath.Join(runDir, "trials.jsonl")); err == nil {
		var out []TrialOutcome
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var row BenchTrialIndex
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				continue
			}
			out = append(out, row.Outcome())
		}
		if len(out) > 0 {
			// Merge any Harbor trial dirs not yet in the index
			// (a concurrently executing run may have finished
			// trials after the index snapshot was read).
			out = append(out, scanHarborTrials(runDir, trialKeys(out))...)
			return out, nil
		}
	}
	if harbor := scanHarborTrials(runDir, nil); len(harbor) > 0 {
		return harbor, nil
	}
	trials, err := LoadTrials(runDir)
	if err != nil {
		return nil, err
	}
	if len(trials) == 0 {
		return nil, fmt.Errorf("eval: no completed trials in %s yet", runDir)
	}
	return trials, nil
}

func trialKeys(trials []TrialOutcome) map[string]bool {
	seen := map[string]bool{}
	for _, t := range trials {
		seen[t.TrialID+"\x00"+t.TaskID] = true
	}
	return seen
}

// scanHarborTrials indexes every trial subdirectory holding
// result.json, skipping rows already seen and trial dirs without a
// readable reward (still executing). Index errors on a single trial
// never fail the partial estimate.
func scanHarborTrials(runDir string, seen map[string]bool) []TrialOutcome {
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil
	}
	var out []TrialOutcome
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		trialDir := filepath.Join(runDir, e.Name())
		if _, err := os.Stat(filepath.Join(trialDir, "result.json")); err != nil {
			continue
		}
		row, err := IndexTrial(trialDir)
		if err != nil {
			continue
		}
		oc := row.Outcome()
		if seen != nil && seen[oc.TrialID+"\x00"+oc.TaskID] {
			continue
		}
		out = append(out, oc)
	}
	return out
}

// InterimEstimate pairs the tasks completed so far in runDir against
// the reference and returns the paired bootstrap estimate with the
// early-stop verdict.
func InterimEstimate(runDir string, opts InterimOptions) (InterimReport, error) {
	reps := opts.Reps
	if reps <= 0 {
		reps = 10000
	}
	seed := opts.Seed
	if seed == 0 {
		seed = 7
	}
	bar := InterimBar(opts.Bar)
	if opts.Against == "" {
		return InterimReport{}, fmt.Errorf("eval: interim needs --against <run-dir|baseline.json>")
	}
	trials, err := LoadPartialRunTrials(runDir)
	if err != nil {
		return InterimReport{}, err
	}
	if len(trials) == 0 {
		return InterimReport{}, fmt.Errorf("eval: no completed trials in %s yet", runDir)
	}
	byTask := groupByTask(trials)
	tasks := make([]string, 0, len(byTask))
	for task := range byTask {
		tasks = append(tasks, task)
	}
	sort.Strings(tasks)

	refRates := map[string]float64{}
	kind := ""
	if isRunDir(opts.Against) {
		kind = "run"
		rt, err := LoadPartialRunTrials(opts.Against)
		if err != nil {
			return InterimReport{}, fmt.Errorf("eval: load reference run: %w", err)
		}
		for task, ts := range groupByTask(rt) {
			refRates[task] = float64(countPassed(ts)) / float64(len(ts))
		}
	} else {
		kind = "baseline"
		refRates, err = LoadBaselineFile(opts.Against)
		if err != nil {
			return InterimReport{}, err
		}
	}
	// Only completed tasks present on both sides enter the paired
	// estimate; reference-only tasks have no signal yet and
	// run-only tasks without a baseline rate cannot be scored.
	var paired []string
	for _, task := range tasks {
		if _, ok := refRates[task]; ok {
			paired = append(paired, task)
		}
	}
	if len(paired) == 0 {
		return InterimReport{}, fmt.Errorf("eval: no completed tasks overlap the reference (run=%d ref=%d)", len(tasks), len(refRates))
	}
	rates, refs, diffs := make([]float64, 0, len(paired)), make([]float64, 0, len(paired)), make([]float64, 0, len(paired))
	var rows []InterimTaskRow
	var refSum float64
	for _, task := range paired {
		ts := byTask[task]
		c := countPassed(ts)
		rate := float64(c) / float64(len(ts))
		ref := refRates[task]
		rates = append(rates, rate)
		refs = append(refs, ref)
		diffs = append(diffs, rate-ref)
		refSum += ref
		rows = append(rows, InterimTaskRow{
			Task: task, Trials: len(ts), Successes: c,
			Rate: rate, RefRate: ref, Diff: rate - ref,
		})
	}
	_ = refs
	current, err := BootstrapCI(rates, reps, seed)
	if err != nil {
		return InterimReport{}, err
	}
	diffCI, err := BootstrapCI(diffs, reps, seed)
	if err != nil {
		return InterimReport{}, err
	}
	verdict := InterimContinue
	if diffCI.High < -bar {
		verdict = InterimStop
	}
	return InterimReport{
		GeneratedAt:    time.Now().UTC(),
		RunDir:         runDir,
		Against:        opts.Against,
		AgainstKind:    kind,
		Reps:           reps,
		Seed:           seed,
		CompletedTasks: paired,
		ByTask:         rows,
		Current:        current,
		Reference:      refSum / float64(len(paired)),
		Diff:           diffCI,
		PracticalBar:   bar,
		Verdict:        verdict,
	}, nil
}

// WriteInterimReport writes interim.json into dir.
func WriteInterimReport(dir string, rep InterimReport) error {
	buf, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal interim: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: mkdir interim: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "interim.json"), append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: write interim: %w", err)
	}
	return nil
}

// RenderInterimMarkdown renders the paired estimate on completed
// tasks, the per-task table and the STOP/CONTINUE verdict.
func RenderInterimMarkdown(rep InterimReport) string {
	var b strings.Builder
	b.WriteString("# Bench interim\n\n")
	fmt.Fprintf(&b, "Run: %s\n\n", rep.RunDir)
	fmt.Fprintf(&b, "Against: %s (%s)\n\n", rep.Against, rep.AgainstKind)
	fmt.Fprintf(&b, "Completed paired tasks: %d\n\n", len(rep.CompletedTasks))
	fmt.Fprintf(&b, "Current success %.4f 95%%CI [%.4f, %.4f] vs reference %.4f\n\n",
		rep.Current.Mean, rep.Current.Low, rep.Current.High, rep.Reference)
	fmt.Fprintf(&b, "Paired diff %+.4f 95%%CI [%+.4f, %+.4f] (bar %.2f points)\n\n",
		rep.Diff.Mean, rep.Diff.Low, rep.Diff.High, rep.PracticalBar*100)
	fmt.Fprintf(&b, "## Verdict: %s\n\n", rep.Verdict)
	if rep.Verdict == InterimStop {
		b.WriteString("STOP: even the optimistic end of the interval sits more than the practical bar below the reference.\n\n")
	} else {
		b.WriteString("CONTINUE: the interval does not rule out reaching the reference; keep running.\n\n")
	}
	b.WriteString("## Per-task estimate\n\n| task | trials | rate | ref | diff |\n|---|---|---|---|---|\n")
	for _, r := range rep.ByTask {
		fmt.Fprintf(&b, "| %s | %d | %d/%d (%.3f) | %.3f | %+.3f |\n",
			r.Task, r.Trials, r.Successes, r.Trials, r.Rate, r.RefRate, r.Diff)
	}
	return b.String()
}
