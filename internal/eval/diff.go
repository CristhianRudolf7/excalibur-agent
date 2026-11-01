package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Arm diff with flip triage (measurement workstream, report side only).
//
// `bench diff` pairs two finished run directories per task like
// `bench compare` (Section 23.7, paired bootstrap over tasks) and
// adds what the arm loop needs to act on: the per-task pass-rate
// table is already in CompareReport, but diff additionally reports
// the flipped tasks in both directions with the triage cause and
// evidence quote for every failed trial on the failing side, plus
// the mean step / tool-call / time / token differences. Triage rows
// are read from each run's triage.jsonl when present and computed
// lazily (deterministic only, never a paid model call) otherwise.
//
// The Harbor invocation, the model gateway wire protocol, providers,
// API, contracts, integrations, MCP/ACP and the bench run path are
// untouched; this file only reads finished run directories.

// DiffTriageSwitch is the config switch for the lazy flip triage
// (default ON). OFF skips both the triage.jsonl read and the lazy
// deterministic pass, leaving flip rows without cause/evidence.
const DiffTriageSwitch = "EXCALIBUR_DIFF_TRIAGE"

// DiffTriageEnabled reports whether flip triage runs. Accepts
// 1/true/yes/on and 0/false/no/off; unset means ON.
func DiffTriageEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(DiffTriageSwitch)))
	switch v {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// DiffOptions configures DiffRuns.
type DiffOptions struct {
	K int
	// Reps/Seed drive the paired bootstrap (defaults 10000/7, the
	// Section 23.7 seed convention shared with Aggregate/Compare).
	Reps int
	Seed int64
	// Bar is the practical acceptance bar (default DefaultPracticalBar).
	Bar float64
	// BasePath optionally points at a baseline file
	// {"baseline_rate": {task: rate}} included per task for context.
	// Empty means no baseline column.
	BasePath string
	// NoTriage disables the flip triage even when DiffTriageEnabled
	// is on (mirrors --no-triage for one invocation).
	NoTriage bool
	// TriageTimeout bounds the lazy deterministic pass.
	TriageTimeout time.Duration
}

// FlipTrial is one failed trial on the failing side of a flipped task.
type FlipTrial struct {
	Task     string  `json:"task"`
	Trial    string  `json:"trial"`
	Side     string  `json:"side"`
	Reward   float64 `json:"reward"`
	Cause    string  `json:"cause,omitempty"`
	Evidence string  `json:"evidence,omitempty"`
	Decided  string  `json:"decided_by,omitempty"`
}

// FlipDetail is one flipped task: A passed and B failed (Regressed)
// or A failed and B passed (Improved), using the all-or-nothing flip
// convention of CompareReport (FlippedToKO / FlippedToOK) so the two
// commands never disagree on what "flipped" means.
type FlipDetail struct {
	Task      string      `json:"task"`
	Direction string      `json:"direction"`
	SuccessA  int         `json:"success_a"`
	TrialsA   int         `json:"trials_a"`
	SuccessB  int         `json:"success_b"`
	TrialsB   int         `json:"trials_b"`
	RateA     float64     `json:"rate_a"`
	RateB     float64     `json:"rate_b"`
	Failed    []FlipTrial `json:"failed_trials"`
}

// Flip directions.
const (
	FlipRegressed = "regressed"
	FlipImproved  = "improved"
)

// ResourceDelta is one mean resource comparison over the paired
// trials: model calls (steps), tool calls, wall seconds and tokens.
type ResourceDelta struct {
	Metric string  `json:"metric"`
	MeanA  float64 `json:"mean_a"`
	MeanB  float64 `json:"mean_b"`
	Diff   float64 `json:"diff_b_minus_a"`
	N      int     `json:"n"`
}

// DiffReport is the arm diff: the paired Section 23.7 statistics
// plus flips with triage and mean resource differences.
type DiffReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	DirA        string    `json:"dir_a"`
	DirB        string    `json:"dir_b"`
	BasePath    string    `json:"base_path,omitempty"`
	K           int       `json:"k"`
	Reps        int       `json:"bootstrap_reps"`
	Seed        int64     `json:"seed"`

	ArmA ArmSummary `json:"arm_a"`
	ArmB ArmSummary `json:"arm_b"`

	SwitchDiff map[string][2]string `json:"switch_diff,omitempty"`

	PairedTasks []string `json:"paired_tasks"`
	UnpairedA   []string `json:"unpaired_a,omitempty"`
	UnpairedB   []string `json:"unpaired_b,omitempty"`

	SuccessA MeanCI `json:"success_a"`
	SuccessB MeanCI `json:"success_b"`
	Diff     MeanCI `json:"paired_diff_b_minus_a"`

	PassAtKA    float64 `json:"pass_at_k_a"`
	PassAtKB    float64 `json:"pass_at_k_b"`
	PassPowKA   float64 `json:"pass_pow_k_a"`
	PassPowKB   float64 `json:"pass_pow_k_b"`
	PassAtKDiff float64 `json:"pass_at_k_diff"`
	PassPowDiff float64 `json:"pass_pow_k_diff"`

	ByTask []TaskDelta `json:"by_task"`

	// Baseline carries the optional per-task reference rates from
	// BasePath for context; it never enters the paired statistics.
	Baseline map[string]float64 `json:"baseline_rate,omitempty"`

	Flips []FlipDetail `json:"flips"`

	Resources []ResourceDelta `json:"resources"`

	PracticalBar float64 `json:"practical_bar"`
	Accepted     bool    `json:"accepted"`
	Verdict      string  `json:"verdict"`
}

// LoadBaselineFile reads {"baseline_rate": {task: rate}} and returns
// the per-task reference rates. Rates outside [0,1] are an error.
func LoadBaselineFile(path string) (map[string]float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: read baseline: %w", err)
	}
	var doc struct {
		BaselineRate map[string]float64 `json:"baseline_rate"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("eval: parse baseline: %w", err)
	}
	if len(doc.BaselineRate) == 0 {
		return nil, fmt.Errorf("eval: baseline %s carries no baseline_rate entries", path)
	}
	for task, r := range doc.BaselineRate {
		if r < 0 || r > 1 {
			return nil, fmt.Errorf("eval: baseline rate for %q out of range: %v", task, r)
		}
	}
	return doc.BaselineRate, nil
}

// readTriageFile loads triage.jsonl rows keyed by trial basename.
func readTriageFile(path string) map[string]TriageResult {
	out := map[string]TriageResult{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r TriageResult
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		out[r.Trial] = r
	}
	return out
}

// lazyTriage runs the deterministic triage over runDir when no
// triage.jsonl exists there. It never calls a paid model
// (NoModel=true): flip triage must stay offline and deterministic so
// `bench diff` works on any machine without credentials. Trials that
// still classify as skipped/unreadable simply yield no rows.
func lazyTriage(runDir string, timeout time.Duration) map[string]TriageResult {
	out := map[string]TriageResult{}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	results, _, err := TriageRun(ctx, runDir, TriageOptions{NoModel: true})
	if err != nil {
		return out
	}
	for _, r := range results {
		out[r.Trial] = r
	}
	return out
}

// triageForRun returns trial-keyed triage rows for runDir: the
// checked-in triage.jsonl when present, else the lazy deterministic
// pass (unless disabled by option or switch).
func triageForRun(runDir string, opts DiffOptions) map[string]TriageResult {
	if opts.NoTriage || !DiffTriageEnabled() {
		return map[string]TriageResult{}
	}
	got := readTriageFile(filepath.Join(runDir, "triage.jsonl"))
	if len(got) > 0 {
		return got
	}
	return lazyTriage(runDir, opts.TriageTimeout)
}

// trialKey matches triage rows (trial basenames) to TrialOutcome
// rows (TrialID, which for bench jobs is the trial name and for
// quick dirs the task name).
func flipTrialsForTask(trials []TrialOutcome, task, side string, triage map[string]TriageResult) []FlipTrial {
	var out []FlipTrial
	for _, t := range trials {
		if t.TaskID != task || t.Passed() {
			continue
		}
		ft := FlipTrial{Task: task, Trial: t.TrialID, Side: side, Reward: t.Reward}
		if r, ok := triage[t.TrialID]; ok {
			ft.Cause, ft.Evidence, ft.Decided = r.Cause, r.Evidence, r.DecidedBy
		} else if r, ok := triage[filepath.Base(t.TrialID)]; ok {
			ft.Cause, ft.Evidence, ft.Decided = r.Cause, r.Evidence, r.DecidedBy
		}
		out = append(out, ft)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Trial < out[j].Trial })
	return out
}

func trialTokens(t TrialOutcome) float64 {
	return float64(t.InputTokens+t.CacheRead+t.CacheWrite) + float64(t.OutputTokens)
}

// meanResourceDeltas averages ModelCalls (steps), ToolCalls, wall
// seconds and tokens over the paired trials of each arm.
func meanResourceDeltas(paired []string, byA, byB map[string][]TrialOutcome) []ResourceDelta {
	var stepsA, stepsB, toolsA, toolsB, wallA, wallB, tokA, tokB []float64
	for _, task := range paired {
		for _, t := range byA[task] {
			stepsA = append(stepsA, float64(t.ModelCalls))
			toolsA = append(toolsA, float64(t.ToolCalls))
			wallA = append(wallA, t.WallSeconds)
			tokA = append(tokA, trialTokens(t))
		}
		for _, t := range byB[task] {
			stepsB = append(stepsB, float64(t.ModelCalls))
			toolsB = append(toolsB, float64(t.ToolCalls))
			wallB = append(wallB, t.WallSeconds)
			tokB = append(tokB, trialTokens(t))
		}
	}
	mk := func(name string, a, b []float64) ResourceDelta {
		return ResourceDelta{Metric: name, MeanA: meanOf(a), MeanB: meanOf(b), Diff: meanOf(b) - meanOf(a), N: len(a) + len(b)}
	}
	return []ResourceDelta{
		mk("steps", stepsA, stepsB),
		mk("tool_calls", toolsA, toolsB),
		mk("wall_seconds", wallA, wallB),
		mk("tokens", tokA, tokB),
	}
}

func meanOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// DiffRuns pairs two finished runs task by task with the Section 23.7
// statistics (reusing BootstrapCI/PairedDiffCI/AcceptImprovement) and
// adds flip details with triage plus mean resource differences.
func DiffRuns(dirA, dirB string, opts DiffOptions) (DiffReport, error) {
	k, reps, seed, bar := opts.K, opts.Reps, opts.Seed, opts.Bar
	if k <= 0 {
		k = 5
	}
	if reps <= 0 {
		reps = 10000
	}
	if bar <= 0 {
		bar = DefaultPracticalBar
	}
	if seed == 0 {
		seed = 7
	}
	trialsA, err := LoadRunTrials(dirA)
	if err != nil {
		return DiffReport{}, fmt.Errorf("eval: load run A: %w", err)
	}
	trialsB, err := LoadRunTrials(dirB)
	if err != nil {
		return DiffReport{}, fmt.Errorf("eval: load run B: %w", err)
	}
	if len(trialsA) == 0 || len(trialsB) == 0 {
		return DiffReport{}, fmt.Errorf("eval: need trials in both runs (A=%d B=%d)", len(trialsA), len(trialsB))
	}
	byA, byB := groupByTask(trialsA), groupByTask(trialsB)
	var paired, unpairedA, unpairedB []string
	for task := range byA {
		if _, ok := byB[task]; ok {
			paired = append(paired, task)
		} else {
			unpairedA = append(unpairedA, task)
		}
	}
	for task := range byB {
		if _, ok := byA[task]; !ok {
			unpairedB = append(unpairedB, task)
		}
	}
	sort.Strings(paired)
	sort.Strings(unpairedA)
	sort.Strings(unpairedB)
	if len(paired) == 0 {
		return DiffReport{}, fmt.Errorf("eval: no shared tasks between %s and %s", dirA, dirB)
	}
	ratesA := make([]float64, 0, len(paired))
	ratesB := make([]float64, 0, len(paired))
	var rows []TaskDelta
	var passAtA, passAtB, powA, powB float64
	for _, task := range paired {
		ta, tb := byA[task], byB[task]
		ca, cb := countPassed(ta), countPassed(tb)
		ra, rb := float64(ca)/float64(len(ta)), float64(cb)/float64(len(tb))
		ratesA = append(ratesA, ra)
		ratesB = append(ratesB, rb)
		pka, err := PassAtK(len(ta), ca, k)
		if err != nil {
			return DiffReport{}, err
		}
		pkb, err := PassAtK(len(tb), cb, k)
		if err != nil {
			return DiffReport{}, err
		}
		pwa, err := PassPowK(len(ta), ca, k)
		if err != nil {
			return DiffReport{}, err
		}
		pwb, err := PassPowK(len(tb), cb, k)
		if err != nil {
			return DiffReport{}, err
		}
		passAtA += pka
		passAtB += pkb
		powA += pwa
		powB += pwb
		rows = append(rows, TaskDelta{
			Task:    task,
			TrialsA: len(ta), SuccessA: ca, TrialsB: len(tb), SuccessB: cb,
			RateA: ra, RateB: rb, Delta: rb - ra,
			PassAtKA: pka, PassAtKB: pkb, PassPowKA: pwa, PassPowKB: pwb,
			Improved:    rb > ra,
			Regressed:   rb < ra,
			FlippedToOK: ca == 0 && cb > 0,
			FlippedToKO: ca > 0 && cb == 0,
		})
	}
	n := float64(len(paired))
	ciA, err := BootstrapCI(ratesA, reps, seed)
	if err != nil {
		return DiffReport{}, err
	}
	ciB, err := BootstrapCI(ratesB, reps, seed)
	if err != nil {
		return DiffReport{}, err
	}
	diffCI, err := PairedDiffCI(ratesA, ratesB, reps, seed)
	if err != nil {
		return DiffReport{}, err
	}
	var baseline map[string]float64
	if opts.BasePath != "" {
		baseline, err = LoadBaselineFile(opts.BasePath)
		if err != nil {
			return DiffReport{}, err
		}
	}
	// Flip details: triage maps are loaded once per run (file first,
	// lazy deterministic pass when missing and enabled).
	triageA := triageForRun(dirA, opts)
	triageB := triageForRun(dirB, opts)
	byTaskA := map[string][]TrialOutcome{}
	byTaskB := map[string][]TrialOutcome{}
	for _, t := range trialsA {
		byTaskA[t.TaskID] = append(byTaskA[t.TaskID], t)
	}
	for _, t := range trialsB {
		byTaskB[t.TaskID] = append(byTaskB[t.TaskID], t)
	}
	var flips []FlipDetail
	for _, d := range rows {
		switch {
		case d.FlippedToKO:
			flips = append(flips, FlipDetail{
				Task: d.Task, Direction: FlipRegressed,
				SuccessA: d.SuccessA, TrialsA: d.TrialsA,
				SuccessB: d.SuccessB, TrialsB: d.TrialsB,
				RateA: d.RateA, RateB: d.RateB,
				Failed: flipTrialsForTask(byTaskB[d.Task], d.Task, "B", triageB),
			})
		case d.FlippedToOK:
			flips = append(flips, FlipDetail{
				Task: d.Task, Direction: FlipImproved,
				SuccessA: d.SuccessA, TrialsA: d.TrialsA,
				SuccessB: d.SuccessB, TrialsB: d.TrialsB,
				RateA: d.RateA, RateB: d.RateB,
				Failed: flipTrialsForTask(byTaskA[d.Task], d.Task, "A", triageA),
			})
		}
	}
	sort.Slice(flips, func(i, j int) bool { return flips[i].Task < flips[j].Task })
	manA, manB := LoadRunManifest(dirA), LoadRunManifest(dirB)
	accepted := AcceptImprovement(diffCI, bar)
	verdict := "REJECT"
	if accepted {
		verdict = "ACCEPT"
	}
	return DiffReport{
		GeneratedAt: time.Now().UTC(),
		DirA:        dirA, DirB: dirB, BasePath: opts.BasePath,
		K: k, Reps: reps, Seed: seed,
		ArmA:        summarizeArm(dirA, manA, trialsA, paired),
		ArmB:        summarizeArm(dirB, manB, trialsB, paired),
		SwitchDiff:  diffSwitches(manA, manB),
		PairedTasks: paired, UnpairedA: unpairedA, UnpairedB: unpairedB,
		SuccessA: ciA, SuccessB: ciB, Diff: diffCI,
		PassAtKA: passAtA / n, PassAtKB: passAtB / n,
		PassPowKA: powA / n, PassPowKB: powB / n,
		PassAtKDiff: (passAtB - passAtA) / n, PassPowDiff: (powB - powA) / n,
		ByTask:       rows,
		Baseline:     baseline,
		Flips:        flips,
		Resources:    meanResourceDeltas(paired, byA, byB),
		PracticalBar: bar, Accepted: accepted, Verdict: verdict,
	}, nil
}

// WriteDiffReport writes diff.json into dir.
func WriteDiffReport(dir string, rep DiffReport) error {
	buf, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal diff: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: mkdir diff: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "diff.json"), append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: write diff: %w", err)
	}
	return nil
}

// RenderDiffMarkdown renders the per-task pass-rate table first, the
// flipped tasks with triage evidence second, then the resource means
// and the Section 23.7 verdict.
func RenderDiffMarkdown(rep DiffReport) string {
	var b strings.Builder
	b.WriteString("# Bench diff\n\n")
	fmt.Fprintf(&b, "A: %s (arm %s)\n\n", rep.DirA, displayArm(rep.ArmA.Arm))
	fmt.Fprintf(&b, "B: %s (arm %s)\n\n", rep.DirB, displayArm(rep.ArmB.Arm))
	if rep.BasePath != "" {
		fmt.Fprintf(&b, "Baseline: %s\n\n", rep.BasePath)
	}
	if len(rep.SwitchDiff) > 0 {
		b.WriteString("## Configuration diff\n\n| key | A | B |\n|---|---|---|\n")
		keys := make([]string, 0, len(rep.SwitchDiff))
		for k := range rep.SwitchDiff {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := rep.SwitchDiff[k]
			fmt.Fprintf(&b, "| %s | %s | %s |\n", k, v[0], v[1])
		}
		b.WriteString("\n")
	}
	b.WriteString("## Per-task pass rates\n\n| task | A | B | delta | baseline |\n|---|---|---|---|---|\n")
	for _, d := range rep.ByTask {
		base := "-"
		if rep.Baseline != nil {
			if r, ok := rep.Baseline[d.Task]; ok {
				base = fmt.Sprintf("%.3f", r)
			} else {
				base = "n/a"
			}
		}
		fmt.Fprintf(&b, "| %s | %d/%d (%.3f) | %d/%d (%.3f) | %+.3f | %s |\n",
			d.Task, d.SuccessA, d.TrialsA, d.RateA, d.SuccessB, d.TrialsB, d.RateB, d.Delta, base)
	}
	b.WriteString("\n## Flipped tasks\n\n")
	if len(rep.Flips) == 0 {
		b.WriteString("No all-or-nothing flips (every task passes at least once on both sides or fails on both sides).\n\n")
	} else {
		for _, f := range rep.Flips {
			fmt.Fprintf(&b, "### %s (%s: A %d/%d B %d/%d)\n\n", f.Task, f.Direction,
				f.SuccessA, f.TrialsA, f.SuccessB, f.TrialsB)
			if len(f.Failed) == 0 {
				b.WriteString("No failed trials with records on the failing side.\n\n")
				continue
			}
			b.WriteString("| trial | side | cause | evidence |\n|---|---|---|---|\n")
			for _, ft := range f.Failed {
				cause := ft.Cause
				if cause == "" {
					cause = "(no triage)"
				}
				ev := strings.ReplaceAll(ft.Evidence, "|", "/")
				ev = strings.ReplaceAll(ev, "\n", " ")
				if len(ev) > 240 {
					ev = ev[:240]
				}
				if ev == "" {
					ev = "-"
				}
				fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", ft.Trial, ft.Side, cause, ev)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("## Mean resource differences (paired trials)\n\n| metric | A | B | diff |\n|---|---|---|---|\n")
	for _, r := range rep.Resources {
		fmt.Fprintf(&b, "| %s | %.2f | %.2f | %+.2f |\n", r.Metric, r.MeanA, r.MeanB, r.Diff)
	}
	fmt.Fprintf(&b, "\n## Verdict\n\n%s: B-A success %+.4f 95%%CI [%+.4f, %+.4f] (bar %.2f points, %d paired tasks, k=%d)\n\n",
		rep.Verdict, rep.Diff.Mean, rep.Diff.Low, rep.Diff.High, rep.PracticalBar*100, len(rep.PairedTasks), rep.K)
	fmt.Fprintf(&b, "Success A %.4f [%.4f, %.4f] vs B %.4f [%.4f, %.4f]\n\n",
		rep.SuccessA.Mean, rep.SuccessA.Low, rep.SuccessA.High,
		rep.SuccessB.Mean, rep.SuccessB.Low, rep.SuccessB.High)
	if len(rep.UnpairedA) > 0 || len(rep.UnpairedB) > 0 {
		b.WriteString("Unpaired (excluded from paired statistics):\n")
		if len(rep.UnpairedA) > 0 {
			fmt.Fprintf(&b, "\n- only in A: %s\n", strings.Join(rep.UnpairedA, ", "))
		}
		if len(rep.UnpairedB) > 0 {
			fmt.Fprintf(&b, "\n- only in B: %s\n", strings.Join(rep.UnpairedB, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}
