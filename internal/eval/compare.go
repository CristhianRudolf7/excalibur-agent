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

// Paired arm comparison (Section 23.7, Appendix F): two finished run
// directories — bench jobs or eval quick dirs — are aligned per task
// and compared with paired bootstrap over tasks. Only tasks present
// in BOTH runs enter the paired statistics; tasks present in one run
// are listed as unpaired so silently different task sets can never
// pass as an arm effect.

// Outcome converts an index row to the canonical trial outcome used
// by Aggregate and CompareRuns (single implementation shared by the
// bench report and compare paths).
func (r BenchTrialIndex) Outcome() TrialOutcome {
	return TrialOutcome{
		TrialID: r.Trial, TaskID: r.Task, Reward: r.Reward,
		Failed:       r.FailCategory == FailHarness || r.FailCategory == FailCrash,
		FailCategory: r.FailCategory,
		InfraReason:  r.InfraReason,
		InputTokens:  r.InputTokens, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite,
		OutputTokens: r.OutputTokens, CostUSD: r.CostUSD,
		WallSeconds: r.WallSecs, ModelSeconds: r.ModelSecs, ToolSeconds: r.ToolSecs,
		ChildPeakBytes: r.PeakRSS, HarnessPeakBytes: r.HarnessPeakRSS,
		CPUSeconds:   r.CPUSeconds,
		ModelRetries: r.ModelRetries, ToolRetries: r.ToolRetries,
		ModelCalls: r.ModelCalls, ToolCalls: r.ToolCallSum,
		Subagents: r.Subagents, Compactions: r.Compactions,
		MaskBatches: r.MaskBatches, LoopsDetected: r.LoopsCaught,
		GateAttempts: r.GateAttempts, Interruptions: r.Interruptions,
	}
}

// LoadRunTrials reads one finished run directory: trials.jsonl when
// present (bench jobs), otherwise the trial.json files inside (eval
// report shape, used by the quick suite and smoke runs).
func LoadRunTrials(runDir string) ([]TrialOutcome, error) {
	indexPath := filepath.Join(runDir, "trials.jsonl")
	if raw, err := os.ReadFile(indexPath); err == nil {
		var out []TrialOutcome
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var row BenchTrialIndex
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return nil, fmt.Errorf("eval: parse %s: %w", indexPath, err)
			}
			out = append(out, row.Outcome())
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return LoadTrials(runDir)
}

// LoadRunManifest reads manifest.json when present; a missing or
// corrupt manifest is not an error (quick dirs and old runs may lack
// one) — the comparison then reports the arm as unknown.
func LoadRunManifest(runDir string) RunManifest {
	var m RunManifest
	raw, err := os.ReadFile(filepath.Join(runDir, "manifest.json"))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(raw, &m)
	return m
}

// ArmSummary describes one side of a comparison for ablation
// reporting: which configuration ran, with which switches.
type ArmSummary struct {
	Dir            string            `json:"dir"`
	Arm            string            `json:"arm"`
	Switches       map[string]string `json:"switches,omitempty"`
	ProfileHash    string            `json:"profile_hash,omitempty"`
	ArmProfileHash string            `json:"arm_profile_hash,omitempty"`
	Model          string            `json:"model"`
	Provider       string            `json:"provider"`
	Effort         string            `json:"effort"`
	K              int               `json:"k"`
	Trials         int               `json:"trials"`
	Tasks          int               `json:"tasks"`
}

// TaskDelta is one paired task row: per-arm trials and successes,
// success rates, the paired delta and pass@k/pass^k per arm.
type TaskDelta struct {
	Task        string  `json:"task"`
	TrialsA     int     `json:"trials_a"`
	SuccessA    int     `json:"success_a"`
	TrialsB     int     `json:"trials_b"`
	SuccessB    int     `json:"success_b"`
	RateA       float64 `json:"rate_a"`
	RateB       float64 `json:"rate_b"`
	Delta       float64 `json:"delta"`
	PassAtKA    float64 `json:"pass_at_k_a"`
	PassAtKB    float64 `json:"pass_at_k_b"`
	PassPowKA   float64 `json:"pass_pow_k_a"`
	PassPowKB   float64 `json:"pass_pow_k_b"`
	Improved    bool    `json:"improved"`
	Regressed   bool    `json:"regressed"`
	FlippedToOK bool    `json:"flipped_to_ok"`
	FlippedToKO bool    `json:"flipped_to_ko"`
}

// CompareReport is the ablation-ready comparison of two arms.
type CompareReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	DirA        string    `json:"dir_a"`
	DirB        string    `json:"dir_b"`
	K           int       `json:"k"`
	Reps        int       `json:"bootstrap_reps"`
	Seed        int64     `json:"seed"`

	ArmA ArmSummary `json:"arm_a"`
	ArmB ArmSummary `json:"arm_b"`

	// SwitchDiff lists switches that differ between the arms
	// (key: "a-value" -> "b-value", "<absent>" when one side lacks
	// it) so the measured effect is attributable to a declared
	// configuration change.
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

	TokensPerSuccessA float64 `json:"tokens_per_success_a"`
	TokensPerSuccessB float64 `json:"tokens_per_success_b"`
	CostPerSuccessA   float64 `json:"cost_per_success_a"`
	CostPerSuccessB   float64 `json:"cost_per_success_b"`

	FailuresA map[string]int `json:"failures_a"`
	FailuresB map[string]int `json:"failures_b"`

	ByTask []TaskDelta `json:"by_task"`

	PracticalBar float64 `json:"practical_bar"`
	Accepted     bool    `json:"accepted"`
	Verdict      string  `json:"verdict"`
}

// DefaultPracticalBar is the Section 23.7 acceptance bar on
// Terminal-Bench: differences under 3 points may be harness noise
// (Anthropic's recommendation recorded in Section 23.1).
const DefaultPracticalBar = 0.03

// CompareRuns pairs two finished runs task by task and compares them
// with the Section 23.7 statistics. k is the pass@k horizon (default
// 5); reps/seed drive the paired bootstrap (defaults 10000/7, the
// same seed as Aggregate); bar is the practical acceptance bar
// (default DefaultPracticalBar).
func CompareRuns(dirA, dirB string, k, reps int, seed int64, bar float64) (CompareReport, error) {
	if k <= 0 {
		k = 5
	}
	if reps <= 0 {
		reps = 10000
	}
	if bar <= 0 {
		bar = DefaultPracticalBar
	}
	trialsA, err := LoadRunTrials(dirA)
	if err != nil {
		return CompareReport{}, fmt.Errorf("eval: load run A: %w", err)
	}
	trialsB, err := LoadRunTrials(dirB)
	if err != nil {
		return CompareReport{}, fmt.Errorf("eval: load run B: %w", err)
	}
	if len(trialsA) == 0 || len(trialsB) == 0 {
		return CompareReport{}, fmt.Errorf("eval: need trials in both runs (A=%d B=%d)", len(trialsA), len(trialsB))
	}
	byA := groupByTask(trialsA)
	byB := groupByTask(trialsB)
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
		return CompareReport{}, fmt.Errorf("eval: no shared tasks between %s and %s", dirA, dirB)
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
			return CompareReport{}, err
		}
		pkb, err := PassAtK(len(tb), cb, k)
		if err != nil {
			return CompareReport{}, err
		}
		pwa, err := PassPowK(len(ta), ca, k)
		if err != nil {
			return CompareReport{}, err
		}
		pwb, err := PassPowK(len(tb), cb, k)
		if err != nil {
			return CompareReport{}, err
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
		return CompareReport{}, err
	}
	ciB, err := BootstrapCI(ratesB, reps, seed)
	if err != nil {
		return CompareReport{}, err
	}
	diff, err := PairedDiffCI(ratesA, ratesB, reps, seed)
	if err != nil {
		return CompareReport{}, err
	}
	// Per-success cost/tokens over the PAIRED tasks only, so arm size
	// differences outside the overlap cannot skew the comparison.
	costA, tokA := perSuccess(filterTasks(trialsA, paired))
	costB, tokB := perSuccess(filterTasks(trialsB, paired))
	failA := countFailures(filterTasks(trialsA, paired))
	failB := countFailures(filterTasks(trialsB, paired))
	manA, manB := LoadRunManifest(dirA), LoadRunManifest(dirB)
	accepted := AcceptImprovement(diff, bar)
	verdict := "REJECT"
	if accepted {
		verdict = "ACCEPT"
	}
	return CompareReport{
		GeneratedAt: time.Now().UTC(),
		DirA:        dirA, DirB: dirB, K: k, Reps: reps, Seed: seed,
		ArmA:        summarizeArm(dirA, manA, trialsA, paired),
		ArmB:        summarizeArm(dirB, manB, trialsB, paired),
		SwitchDiff:  diffSwitches(manA, manB),
		PairedTasks: paired, UnpairedA: unpairedA, UnpairedB: unpairedB,
		SuccessA: ciA, SuccessB: ciB, Diff: diff,
		PassAtKA: passAtA / n, PassAtKB: passAtB / n,
		PassPowKA: powA / n, PassPowKB: powB / n,
		PassAtKDiff: (passAtB - passAtA) / n, PassPowDiff: (powB - powA) / n,
		TokensPerSuccessA: tokA, TokensPerSuccessB: tokB,
		CostPerSuccessA: costA, CostPerSuccessB: costB,
		FailuresA: failA, FailuresB: failB,
		ByTask:       rows,
		PracticalBar: bar, Accepted: accepted, Verdict: verdict,
	}, nil
}

func summarizeArm(dir string, m RunManifest, trials []TrialOutcome, paired []string) ArmSummary {
	keep := map[string]bool{}
	for _, t := range paired {
		keep[t] = true
	}
	tasks := map[string]bool{}
	for _, t := range trials {
		if keep[t.TaskID] {
			tasks[t.TaskID] = true
		}
	}
	return ArmSummary{
		Dir: dir, Arm: m.Arm, Switches: m.Switches,
		ProfileHash: m.ProfileHash, ArmProfileHash: m.ArmProfileHash,
		Model: m.Model, Provider: m.Provider, Effort: m.Effort, K: m.K,
		Trials: len(trials), Tasks: len(tasks),
	}
}

// diffSwitches reports arm, profile and switch differences between two
// manifests: the arm names first, then every switch key whose value
// differs (or exists on one side only). Identical configurations yield
// an empty map, which is itself the ablation warning: a comparison
// with no declared difference measures noise.
func diffSwitches(a, b RunManifest) map[string][2]string {
	out := map[string][2]string{}
	if a.Arm != b.Arm {
		out["arm"] = [2]string{emptyAsAbsent(a.Arm), emptyAsAbsent(b.Arm)}
	}
	if a.ProfileHash != "" && b.ProfileHash != "" && a.ProfileHash != b.ProfileHash {
		out["profile_hash"] = [2]string{a.ProfileHash, b.ProfileHash}
	}
	keys := map[string]bool{}
	for k := range a.Switches {
		keys[k] = true
	}
	for k := range b.Switches {
		keys[k] = true
	}
	for k := range keys {
		va, oka := a.Switches[k]
		vb, okb := b.Switches[k]
		if !oka {
			va = "<absent>"
		}
		if !okb {
			vb = "<absent>"
		}
		if va != vb {
			out[k] = [2]string{va, vb}
		}
	}
	return out
}

func emptyAsAbsent(s string) string {
	if s == "" {
		return "<absent>"
	}
	return s
}

func groupByTask(trials []TrialOutcome) map[string][]TrialOutcome {
	out := map[string][]TrialOutcome{}
	for _, t := range trials {
		out[t.TaskID] = append(out[t.TaskID], t)
	}
	return out
}

func countPassed(trials []TrialOutcome) int {
	var n int
	for _, t := range trials {
		if t.Passed() {
			n++
		}
	}
	return n
}

func filterTasks(trials []TrialOutcome, keep []string) []TrialOutcome {
	set := map[string]bool{}
	for _, t := range keep {
		set[t] = true
	}
	var out []TrialOutcome
	for _, t := range trials {
		if set[t.TaskID] {
			out = append(out, t)
		}
	}
	return out
}

func perSuccess(trials []TrialOutcome) (cost, tokens float64) {
	var successes int
	var totalCost, totalTok float64
	for _, t := range trials {
		if t.Passed() {
			successes++
			totalCost += t.CostUSD
			totalTok += float64(t.InputTokens+t.CacheRead+t.CacheWrite) + float64(t.OutputTokens)
		}
	}
	if successes == 0 {
		return 0, 0
	}
	return totalCost / float64(successes), totalTok / float64(successes)
}

func countFailures(trials []TrialOutcome) map[string]int {
	out := map[string]int{}
	for _, t := range trials {
		if !t.Passed() {
			out[Category(t.FailCategory)]++
		}
	}
	return out
}

// WriteCompareReport writes compare.json into dir.
func WriteCompareReport(dir string, rep CompareReport) error {
	buf, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal compare: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: mkdir compare: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compare.json"), append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: write compare: %w", err)
	}
	return nil
}

// RenderCompareMarkdown renders the ablation-ready comparison: arms
// and their switch diff first (what changed), the Section 23.7
// verdict second (whether it mattered), then the per-task and
// failure-category evidence.
func RenderCompareMarkdown(rep CompareReport) string {
	var b strings.Builder
	b.WriteString("# Bench compare\n\n")
	fmt.Fprintf(&b, "A: %s (arm %s)\n\n", rep.DirA, displayArm(rep.ArmA.Arm))
	fmt.Fprintf(&b, "B: %s (arm %s)\n\n", rep.DirB, displayArm(rep.ArmB.Arm))
	b.WriteString("## Configuration diff\n\n")
	if len(rep.SwitchDiff) == 0 {
		b.WriteString("No declared difference: both runs carry the same arm and switches. Any measured delta is noise.\n\n")
	} else {
		b.WriteString("| key | A | B |\n|---|---|---|\n")
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
	b.WriteString("## Verdict\n\n")
	fmt.Fprintf(&b, "%s: B-A success %+.4f 95%%CI [%+.4f, %+.4f] (bar %.2f points, %d paired tasks, k=%d)\n\n",
		rep.Verdict, rep.Diff.Mean, rep.Diff.Low, rep.Diff.High, rep.PracticalBar*100, len(rep.PairedTasks), rep.K)
	fmt.Fprintf(&b, "Success A %.4f [%.4f, %.4f] vs B %.4f [%.4f, %.4f]\n\n",
		rep.SuccessA.Mean, rep.SuccessA.Low, rep.SuccessA.High,
		rep.SuccessB.Mean, rep.SuccessB.Low, rep.SuccessB.High)
	fmt.Fprintf(&b, "pass@%d: A %.4f B %.4f (diff %+.4f) pass^%d: A %.4f B %.4f (diff %+.4f)\n\n",
		rep.K, rep.PassAtKA, rep.PassAtKB, rep.PassAtKDiff,
		rep.K, rep.PassPowKA, rep.PassPowKB, rep.PassPowDiff)
	fmt.Fprintf(&b, "Tokens/success: A %.1f B %.1f cost/success: A $%.4f B $%.4f\n\n",
		rep.TokensPerSuccessA, rep.TokensPerSuccessB, rep.CostPerSuccessA, rep.CostPerSuccessB)
	b.WriteString("## Failures\n\n| category | A | B |\n|---|---|---|\n")
	cats := map[string]bool{}
	for c := range rep.FailuresA {
		cats[c] = true
	}
	for c := range rep.FailuresB {
		cats[c] = true
	}
	sorted := make([]string, 0, len(cats))
	for c := range cats {
		sorted = append(sorted, c)
	}
	sort.Strings(sorted)
	for _, c := range sorted {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", c, rep.FailuresA[c], rep.FailuresB[c])
	}
	b.WriteString("\n## Per task\n\n| task | A | B | delta | pass@k A/B | pass^k A/B |\n|---|---|---|---|---|---|\n")
	for _, d := range rep.ByTask {
		fmt.Fprintf(&b, "| %s | %d/%d | %d/%d | %+.3f | %.3f/%.3f | %.3f/%.3f |\n",
			d.Task, d.SuccessA, d.TrialsA, d.SuccessB, d.TrialsB, d.Delta,
			d.PassAtKA, d.PassAtKB, d.PassPowKA, d.PassPowKB)
	}
	if len(rep.UnpairedA) > 0 || len(rep.UnpairedB) > 0 {
		b.WriteString("\nUnpaired (excluded from paired statistics):\n")
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

func displayArm(arm string) string {
	if arm == "" {
		return "default"
	}
	return arm
}
