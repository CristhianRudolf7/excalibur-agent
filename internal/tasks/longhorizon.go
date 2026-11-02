// Long-horizon persistence for small models (Sections 7.3, 18.4, P1-P10).
//
// Small models stop early: on hour-scale tasks they average 11-23 tool
// calls and declare completed while the result is wrong. The existing
// self-verification and artifact gates block completions without fresh
// executable evidence, but they pass vacuously when no criterion was
// ever extracted and they give the model no sense of how much budget
// is left or what remains to be done. This file adds four
// engine-independent mechanisms that keep the run working longer and
// deeper:
//
//  1. Time- and evidence-aware continuation: when the agent tries to
//     finish while significant budget remains and any extracted draft
//     lacks fresh executable evidence (or the deliverable was never
//     exercised end to end), the finish path continues with a concise
//     "what is still unproven" note. Returns stay bounded; after the
//     bound the run enters closing mode.
//  2. Milestone plan for large tasks: an explicit ordered plan kept in
//     task state, with progress checks every N steps and a "next
//     milestone" nudge.
//  3. Budget pacing: a compact elapsed/remaining line for the dynamic
//     (L5) context layer so the model can pace itself.
//  4. Long-command discipline: detect commands that will outlive the
//     yield and steer the model to background them and poll instead of
//     ending the run.
//
// Every mechanism honors LongHorizonConfig (EXCALIBUR_LONG_HORIZON
// master plus per-mechanism switches), all default ON for the ablation
// protocol. Nothing here looks at any benchmark's tasks, tests or
// solutions: all signals are general harness behaviour (tool history,
// drafts extracted from the literal instruction, budgets).
package tasks

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// LongHorizonConfig gates each long-horizon persistence mechanism. All
// default ON so each can be measured on and off through the environment.
type LongHorizonConfig struct {
	// Enable is the master switch. Off restores the pre-long-horizon
	// behavior (self-verification and artifact gates only, legacy
	// budget line, no milestones, no background nudges).
	Enable bool
	// EnableContinuation runs the time- and evidence-aware
	// continuation before completed.
	EnableContinuation bool
	// EnableMilestones builds the explicit milestone plan for large
	// tasks and nudges the next milestone every N steps.
	EnableMilestones bool
	// EnablePacing renders the budget-pacing line in L5.
	EnablePacing bool
	// EnableBackgroundNudge steers long commands to background+poll.
	EnableBackgroundNudge bool
	// MaxContinuations bounds continuation returns to work before
	// closing mode. Mirrors GateAttempts semantics.
	MaxContinuations int
	// MilestoneEvery sets the progress-check period in steps.
	MilestoneEvery int
	// SignificantFrac is the fraction of the time budget that must
	// remain for a finish to count as "significant time left".
	SignificantFrac float64
	// MinStepsLeft is the steps-remaining threshold when no wall
	// budget is capped.
	MinStepsLeft int
	// LargeRequestChars marks a request large by length.
	LargeRequestChars int
	// LargeDrafts marks a request large by extracted draft count.
	LargeDrafts int
}

// DefaultLongHorizonConfig returns the ablation defaults, overridden
// by the environment when set.
func DefaultLongHorizonConfig() LongHorizonConfig {
	cfg := LongHorizonConfig{
		Enable:                true,
		EnableContinuation:    true,
		EnableMilestones:      true,
		EnablePacing:          true,
		EnableBackgroundNudge: true,
		MaxContinuations:      3,
		MilestoneEvery:        8,
		SignificantFrac:       0.30,
		MinStepsLeft:          5,
		LargeRequestChars:     400,
		LargeDrafts:           3,
	}
	cfg.Enable = longHorizonBool("EXCALIBUR_LONG_HORIZON", cfg.Enable)
	cfg.EnableContinuation = longHorizonBool("EXCALIBUR_LONG_HORIZON_CONTINUATION", cfg.EnableContinuation)
	cfg.EnableMilestones = longHorizonBool("EXCALIBUR_LONG_HORIZON_MILESTONES", cfg.EnableMilestones)
	cfg.EnablePacing = longHorizonBool("EXCALIBUR_LONG_HORIZON_PACING", cfg.EnablePacing)
	cfg.EnableBackgroundNudge = longHorizonBool("EXCALIBUR_LONG_HORIZON_BACKGROUND", cfg.EnableBackgroundNudge)
	cfg.MaxContinuations = longHorizonInt("EXCALIBUR_LONG_HORIZON_MAX_CONT", cfg.MaxContinuations)
	cfg.MilestoneEvery = longHorizonInt("EXCALIBUR_LONG_HORIZON_EVERY", cfg.MilestoneEvery)
	if v := longHorizonFloat("EXCALIBUR_LONG_HORIZON_SIGNIFICANT_FRAC", cfg.SignificantFrac); v > 0 && v < 1 {
		cfg.SignificantFrac = v
	}
	cfg.MinStepsLeft = longHorizonInt("EXCALIBUR_LONG_HORIZON_MIN_STEPS", cfg.MinStepsLeft)
	if cfg.MaxContinuations < 0 {
		cfg.MaxContinuations = 0
	}
	if cfg.MilestoneEvery < 1 {
		cfg.MilestoneEvery = 8
	}
	if cfg.MinStepsLeft < 0 {
		cfg.MinStepsLeft = 0
	}
	return cfg
}

func longHorizonBool(name string, def bool) bool {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func longHorizonInt(name string, def int) int {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return n
}

func longHorizonFloat(name string, def float64) float64 {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return def
	}
	return f
}

// LHMilestone is one explicit milestone of the long-horizon plan: an
// ordered, checkable phase kept in task state. Titles are general
// engineering phases or draft-derived checks; they never carry task
// answers.
type LHMilestone struct {
	// ID is the stable code (M1, M2, ...) in plan order.
	ID string
	// Title is the checkable phase (explore, implement, verify...).
	Title string
	// Key is the artifact token the milestone tracks (a draft key or
	// phase word, lowercased). Empty matches any progress.
	Key string
}

// EstimateLarge reports whether the task counts as large: long literal
// instructions or enough extracted drafts to deserve an explicit plan.
// The heuristic is deliberately cheap and deterministic so the plan
// decision is stable inside a run.
func EstimateLarge(cfg LongHorizonConfig, request string, drafts int) bool {
	if len(strings.TrimSpace(request)) >= cfg.LargeRequestChars {
		return true
	}
	return drafts >= cfg.LargeDrafts
}

// BuildMilestones compiles the explicit plan for a large task: explore
// first, one implement milestone per draft (capped), then verify and
// finish. With no drafts the plan falls back to the generic
// explore/implement/verify/finish phases. All titles are general
// engineering; no benchmark content is used.
func BuildMilestones(request string, drafts []DraftCriterion) []LHMilestone {
	var out []LHMilestone
	add := func(title, key string) {
		out = append(out, LHMilestone{
			ID:    "M" + strconv.Itoa(len(out)+1),
			Title: title, Key: strings.ToLower(strings.TrimSpace(key)),
		})
	}
	add("Explore the workdir and read the relevant files before changing anything", "explore")
	capped := drafts
	if len(capped) > 4 {
		capped = capped[:4]
	}
	for _, d := range capped {
		key := strings.TrimSpace(d.Key)
		if key == "" {
			key = strings.ToLower(trimLen(d.Statement, 40))
		}
		title := "Implement and check: " + trimLen(d.Statement, 90)
		add(title, key)
	}
	if len(capped) == 0 {
		add("Implement the requested change in small steps", "implement")
	}
	_ = request
	add("Run the checks end to end and confirm the deliverable behaves as requested", "verify")
	add("Report what was verified and how", "report")
	return out
}

// milestoneEvidenceCorpus joins successful tool history into one
// lowercased body for milestone completion checks.
func milestoneEvidenceCorpus(history []HistoryTool) string {
	var b strings.Builder
	for _, h := range history {
		if h.IsError {
			continue
		}
		b.WriteString(strings.ToLower(h.Args))
		b.WriteString("\n")
		b.WriteString(strings.ToLower(h.Output))
		b.WriteString("\n")
	}
	return b.String()
}

// MilestoneDone reports whether one milestone shows evidence in the
// tool history. Explore counts on any read-only probe; implement
// milestones count when their key appears in an exercising call;
// verify counts on any exercising call; report never auto-completes
// (the final answer closes it).
func MilestoneDone(m LHMilestone, history []HistoryTool) bool {
	key := strings.ToLower(strings.TrimSpace(m.Key))
	corpus := milestoneEvidenceCorpus(history)
	switch key {
	case "explore":
		for _, h := range history {
			if h.IsError {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(h.Name))
			if name == "exec" || name == "read_file" || name == "call_tool" {
				return true
			}
		}
		return false
	case "verify":
		for _, h := range history {
			if IsExercisingCall(h) {
				return true
			}
		}
		return false
	case "report", "implement":
		return false
	default:
		if key == "" {
			return false
		}
		for _, h := range history {
			if !IsExercisingCall(h) {
				continue
			}
			if strings.Contains(strings.ToLower(h.Args)+"\n"+strings.ToLower(h.Output), key) {
				return true
			}
			if base := baseToken(key); base != "" && base != key && strings.Contains(corpus, base) {
				return true
			}
		}
		return false
	}
}

// NextPending returns the first milestone without evidence, or nil when
// every milestone shows progress. The report phase never counts as
// pending on its own: once verify is done the plan is done.
func NextPending(plan []LHMilestone, history []HistoryTool) *LHMilestone {
	for i := range plan {
		key := strings.ToLower(strings.TrimSpace(plan[i].Key))
		if key == "report" {
			continue
		}
		if !MilestoneDone(plan[i], history) {
			cp := plan[i]
			return &cp
		}
	}
	return nil
}

// MilestoneProgress counts milestones with evidence (report excluded).
func MilestoneProgress(plan []LHMilestone, history []HistoryTool) (done, total int) {
	total = 0
	for _, m := range plan {
		if strings.ToLower(strings.TrimSpace(m.Key)) == "report" {
			continue
		}
		total++
		if MilestoneDone(m, history) {
			done++
		}
	}
	return done, total
}

// NextMilestoneNote renders the progress-check nudge injected every N
// steps: the completed count plus the next checkable milestone.
func NextMilestoneNote(plan []LHMilestone, history []HistoryTool) string {
	if len(plan) == 0 {
		return ""
	}
	done, total := MilestoneProgress(plan, history)
	next := NextPending(plan, history)
	if next == nil {
		return fmt.Sprintf("milestone plan %d/%d done: all checkable milestones show evidence; run the final end-to-end verification and report.", done, total)
	}
	return fmt.Sprintf("milestone plan %d/%d done; next milestone %s: %s. Finish that milestone with an executable check before moving on.", done, total, next.ID, next.Title)
}

// ContinuationGaps lists what is still unproven for the
// time- and evidence-aware continuation: fresh verification gaps,
// exercise-the-deliverable gaps, and the never-exercised end-to-end
// hole. Empty means the finish may proceed.
func ContinuationGaps(history []HistoryTool, drafts []DraftCriterion, minChecks int) []string {
	if minChecks < 1 {
		minChecks = 1
	}
	if len(history) == 0 {
		// Trivial answers with no checkable drafts may finish without
		// tool work; checkable work must still run before finishing.
		if len(drafts) == 0 {
			return nil
		}
		return []string{"no tool work ran yet; explore, implement and exercise the deliverable before finishing"}
	}
	var gaps []string
	gaps = append(gaps, CheckFreshVerification(history, minChecks)...)
	if len(drafts) > 0 {
		gaps = append(gaps, CheckStrongExercise(history, drafts, minChecks)...)
	} else {
		// Draft-free tasks (no extractable criteria): the freshness
		// check above already enforces a verification pass after the
		// last change. Only when a change exists but nothing verified
		// it does the end-to-end hole fire, so read-only probes and
		// trivial write+read tasks still finish while truly
		// unverified work continues.
		if lastChangeIndex(history) < 0 {
			return gaps
		}
		verified := false
		for _, h := range history {
			if !h.IsError && !HistoryRunning(h) && isVerification(h) {
				verified = true
				break
			}
		}
		if !verified {
			gaps = append(gaps, "deliverable never exercised end to end; run it as the instruction describes (program/CLI with inputs, import and call, query the service, parse the output) and confirm the output before finishing")
		}
	}
	return gaps
}

// SignificantBudgetLeft reports whether the run still holds significant
// budget: enough wall time (when capped) or enough steps (when
// uncapped) to justify continuing instead of finishing thin.
func SignificantBudgetLeft(cfg LongHorizonConfig, maxSeconds int64, elapsed time.Duration, stepsUsed, stepsMax int) bool {
	if maxSeconds > 0 {
		total := time.Duration(maxSeconds) * time.Second
		left := total - elapsed
		if left <= 0 {
			return false
		}
		return float64(left) >= cfg.SignificantFrac*float64(total)
	}
	if stepsMax > 0 {
		return stepsMax-stepsUsed >= cfg.MinStepsLeft
	}
	return false
}

// UnprovenNote renders the concise continuation message: what is still
// unproven and how to prove it, capped so the context stays small.
func UnprovenNote(gaps []string) string {
	const maxLines = 3
	lines := append([]string(nil), gaps...)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "…and "+strconv.Itoa(len(gaps)-maxLines)+" more")
	}
	return "long-horizon continuation: significant budget remains but the result is still unproven — " + strings.Join(lines, "; ") + ". Keep working; do not finish without executable evidence."
}

// FormatPacing renders the budget-pacing line for L5: elapsed plus the
// remaining steps/time and milestone progress. Every segment without a
// cap is omitted except elapsed, which always renders so uncapped runs
// still pace themselves.
func FormatPacing(elapsed time.Duration, maxSeconds int64, stepsUsed, stepsMax int, msDone, msTotal int) string {
	var parts []string
	parts = append(parts, "elapsed "+formatPacingSpan(elapsed))
	if stepsMax > 0 {
		left := stepsMax - stepsUsed
		if left < 0 {
			left = 0
		}
		parts = append(parts, fmt.Sprintf("steps %d/%d (%d left)", stepsUsed, stepsMax, left))
	}
	if maxSeconds > 0 {
		total := time.Duration(maxSeconds) * time.Second
		left := total - elapsed
		if left < 0 {
			parts = append(parts, fmt.Sprintf("time overdue %s of %s", formatPacingSpan(-left), formatPacingSpan(total)))
		} else {
			pct := 0
			if total > 0 {
				pct = int(float64(left) * 100 / float64(total))
			}
			parts = append(parts, fmt.Sprintf("time %s left of %s (%d%% left)", formatPacingSpan(left), formatPacingSpan(total), pct))
		}
	}
	if msTotal > 0 {
		parts = append(parts, fmt.Sprintf("milestones %d/%d", msDone, msTotal))
	}
	return "pacing: " + strings.Join(parts, ", ")
}

func formatPacingSpan(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int64(d / time.Second)
	switch {
	case secs >= 3600:
		return fmt.Sprintf("%dh%dm", secs/3600, (secs%3600)/60)
	case secs >= 120:
		return fmt.Sprintf("%dm", secs/60)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// longCommands are substrings of an exec command showing it will run
// long: builds, test suites, servers, installers, training and sleeps.
// The list is general engineering knowledge; it names no benchmark,
// product or task.
var longCommands = []string{
	"make", "cmake", "cargo build", "cargo test", "go test", "go build",
	"pytest", "npm test", "npm run", "yarn", "pip install", "apt-get",
	"sleep ", "server", "serve ", "listen", "train", "pytest -",
	"./", ".sh", "build", "test ",
}

// IsLongCommand reports whether an exec command looks long-running and
// was launched without backgrounding: a long pattern with a generous
// yield (or an alive session) that should have been backgrounded and
// polled instead of blocking the run or ending it.
func IsLongCommand(command, args string, background bool) bool {
	if background {
		return false
	}
	lower := strings.ToLower(command + "\n" + args)
	for _, m := range longCommands {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// BackgroundHint suggests the background+poll discipline for a long
// command.
func BackgroundHint(command string) string {
	short := trimLen(strings.TrimSpace(command), 80)
	if short == "" {
		short = "the long command"
	}
	return "long command detected (" + short + "): rerun with background:true and poll with exec_io wait_for/read instead of busy-polling or ending the run; never finish while the session is still alive and unverified"
}
