package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Automatic failure triage of evaluation trials (agent-quality
// workstream W2). For every trial with reward 0 the triage reads only
// the agent's own records (agent/excalibur/*.jsonl, final state, last
// tool outputs, the instruction from the trial trajectory) plus the
// pass/fail reward, and classifies the main cause into a fixed
// taxonomy. It never reads the verifier's tests, stdout or reference
// solutions: only verifier/reward.txt (or the reward field of
// result.json) is consulted, which carries pass/fail and nothing else.
//
// Method: deterministic signals first (exit codes, timeouts, repeated
// commands, error strings), then one cheap model call per trial only
// when the deterministic pass is ambiguous. The model fallback is
// gated by the EXCALIBUR_TRIAGE_MODEL switch (default ON).

// Triage causes: the fixed taxonomy. Knowledge-gap carries a domain
// in TriageResult.Domain (for example "R", "python", "sql").
const (
	TriageWrongDone     = "wrong-solution-declared-done"
	TriageIncomplete    = "incomplete-stopped-early"
	TriageTimeout       = "timeout"
	TriageEnvMissing    = "environment/tooling-missing"
	TriageToolMisuse    = "tool-misuse/harness-friction"
	TriageLoop          = "loop/no-progress"
	TriageContextLoss   = "context-loss"
	TriageMisunderstood = "misunderstood-instruction"
	TriageKnowledgeGap  = "knowledge-gap"
	TriageOther         = "other"
	// TriageInfra marks a failed trial that measured the infrastructure
	// (TrialInfraReason); TriageResult.Domain carries the infra reason.
	TriageInfra = "infra"
)

// triageCauses lists every valid cause for validation.
var triageCauses = map[string]bool{
	TriageWrongDone:     true,
	TriageIncomplete:    true,
	TriageTimeout:       true,
	TriageEnvMissing:    true,
	TriageToolMisuse:    true,
	TriageLoop:          true,
	TriageContextLoss:   true,
	TriageMisunderstood: true,
	TriageKnowledgeGap:  true,
	TriageOther:         true,
	TriageInfra:         true,
}

// TriageResult is one triaged failure: the main cause, a short
// evidence quote and the step where it went wrong.
type TriageResult struct {
	Task       string  `json:"task"`
	Trial      string  `json:"trial"`
	Reward     float64 `json:"reward"`
	Cause      string  `json:"cause"`
	Domain     string  `json:"domain,omitempty"`
	Evidence   string  `json:"evidence"`
	FailStep   int     `json:"fail_step"`
	ToolCalls  int     `json:"tool_calls"`
	ModelCalls int     `json:"model_calls"`
	FinalState string  `json:"final_state"`
	DecidedBy  string  `json:"decided_by"`
}

// TriageStats counts the triage run. Skipped trials have no readable
// reward (the verifier never scored them) and stay unclassified.
// JudgeCalls/JudgeErrors track the cheap model fallback: every error
// falls back to the deterministic verdict, so errors never lose a
// trial.
type TriageStats struct {
	Total       int `json:"total"`
	Passed      int `json:"passed"`
	Failed      int `json:"failed"`
	Skipped     int `json:"skipped"`
	Infra       int `json:"infra"`
	JudgeCalls  int `json:"judge_calls"`
	JudgeErrors int `json:"judge_errors"`
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// TriageOptions configures a triage run.
type TriageOptions struct {
	// ModelOverride forces the judge model; empty means the run's own
	// model (from the trial records), falling back to the credentials
	// file model.
	ModelOverride string
	// CredentialsFile is the operator credentials JSON; empty means
	// the default path (~/.config/excalibur/credentials/openai.json).
	CredentialsFile string
	// NoModel disables the cheap model fallback (deterministic only).
	// The EXCALIBUR_TRIAGE_MODEL environment switch does the same.
	NoModel bool
}

// TriageModelEnabled reports the EXCALIBUR_TRIAGE_MODEL switch
// (default ON): when off, triage is deterministic only and never
// calls a model.
func TriageModelEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_TRIAGE_MODEL")))
	switch v {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// useModel reports whether the model fallback may run.
func (o TriageOptions) useModel() bool {
	if o.NoModel {
		return false
	}
	return TriageModelEnabled()
}

// isTrialDir reports whether dir looks like a trial directory (as
// opposed to a job directory, which also holds a result.json): a
// trial carries the agent records or the verifier reward.
func isTrialDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "result.json")); err != nil {
		return false
	}
	if st, err := os.Stat(filepath.Join(dir, "agent")); err == nil && st.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "verifier", "reward.txt")); err == nil {
		return true
	}
	return false
}

// FindTrialDirs lists trial directories under runDir: a trial
// directory itself, a job directory of trials, or a run root holding
// timestamped job directories (job-level result.json files never
// shadow the trials beneath them).
func FindTrialDirs(runDir string) ([]string, error) {
	if isTrialDir(runDir) {
		return []string{runDir}, nil
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil, fmt.Errorf("eval: read run dir: %w", err)
	}
	var trials []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if sub := filepath.Join(runDir, e.Name()); isTrialDir(sub) {
			trials = append(trials, sub)
		}
	}
	if len(trials) > 0 {
		sort.Strings(trials)
		return trials, nil
	}
	// One level of job directories (run roots like tb21-full/).
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(runDir, e.Name())
		subs, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, s := range subs {
			if !s.IsDir() {
				continue
			}
			if trial := filepath.Join(sub, s.Name()); isTrialDir(trial) {
				trials = append(trials, trial)
			}
		}
	}
	if len(trials) == 0 {
		return nil, fmt.Errorf("eval: no trials under %s", runDir)
	}
	sort.Strings(trials)
	return trials, nil
}

// readTrialReward reads only the pass/fail signal: verifier/reward.txt
// first, then the rewards map of result.json. It never touches the
// verifier's tests, stdout or reference solutions.
func readTrialReward(trialDir string) (float64, bool) {
	if raw, err := os.ReadFile(filepath.Join(trialDir, "verifier", "reward.txt")); err == nil {
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%g", &f); err == nil {
			return f, true
		}
	}
	doc, err := readTrialResult(trialDir)
	if err != nil {
		return 0, false
	}
	for _, v := range doc.VerifierResult.Rewards {
		return v, true
	}
	return 0, false
}

// readTrialInstruction returns the task instruction from the trial's
// own trajectory (the first user step), truncated. It never reads the
// verifier directory.
func readTrialInstruction(trialDir string) string {
	for _, p := range []string{
		filepath.Join(trialDir, "agent", "trajectory.json"),
		filepath.Join(trialDir, "agent", "excalibur", "trajectory.json"),
	} {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var doc struct {
			Steps []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		for _, s := range doc.Steps {
			if strings.ToLower(s.Role) == "user" && strings.TrimSpace(s.Content) != "" {
				return truncateRunes(s.Content, 4000)
			}
		}
	}
	return ""
}

// triageSignals is the deterministic evidence for one failed trial.
type triageSignals struct {
	task        string
	trial       string
	reward      float64
	instruction string
	final       harnessFinal
	finalOK     bool
	exception   string
	tools       []ToolCall
	modelCalls  int
	loops       int
	loopKinds   []string
	gateTries   int
	gatePassed  bool
	compactions int
	transitions []Transition
}

// parsedTool is one tool call with its exec command extracted.
type parsedTool struct {
	seq      int
	tool     string
	command  string
	exitCode int
	isError  bool
	output   string
	full     string
	alive    *bool
}

// loadTriageSignals reads one trial's agent records. Verifier files
// besides reward.txt are never opened.
func loadTriageSignals(trialDir string) (*triageSignals, error) {
	sig := &triageSignals{trial: filepath.Base(trialDir)}
	doc, err := readTrialResult(trialDir)
	if err != nil {
		return nil, err
	}
	sig.task = doc.TaskName
	if sig.task == "" {
		sig.task = strings.SplitN(filepath.Base(trialDir), "__", 2)[0]
	}
	if sig.task == "" {
		sig.task = filepath.Base(trialDir)
	}
	if r, ok := readTrialReward(trialDir); ok {
		sig.reward = r
	}
	if doc.ExceptionInfo != nil {
		msg := doc.ExceptionInfo.Type + " " + doc.ExceptionInfo.Message
		if doc.ExceptionInfo.ExType != "" || doc.ExceptionInfo.ExMsg != "" {
			msg = doc.ExceptionInfo.ExType + " " + doc.ExceptionInfo.ExMsg
		}
		sig.exception = msg
	}
	exDir := filepath.Join(trialDir, "agent", "excalibur")
	sig.instruction = readTrialInstruction(trialDir)
	if fin, ok := readHarnessFinal(exDir, trialDir); ok {
		sig.final, sig.finalOK = fin, true
	}
	for _, line := range readJSONLLines(filepath.Join(exDir, "tool_calls.jsonl")) {
		var t ToolCall
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			continue
		}
		sig.tools = append(sig.tools, t)
	}
	sig.modelCalls = len(readJSONLLines(filepath.Join(exDir, "model_calls.jsonl")))
	for _, line := range readJSONLLines(filepath.Join(exDir, "loops.jsonl")) {
		var s LoopSignal
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		if s.Stage == "final" {
			continue
		}
		sig.loops++
		sig.loopKinds = append(sig.loopKinds, s.Kind)
	}
	for _, line := range readJSONLLines(filepath.Join(exDir, "gate_attempts.jsonl")) {
		var g GateAttempt
		if err := json.Unmarshal([]byte(line), &g); err != nil {
			continue
		}
		sig.gateTries++
		if g.Passed {
			sig.gatePassed = true
		}
	}
	for _, line := range readJSONLLines(filepath.Join(exDir, "compaction.jsonl")) {
		var e CompactEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Note == "final" || e.Note == "engine history length" {
			continue
		}
		if (e.FromSeq == 0 || e.FromSeq == e.ToSeq) && e.KeptBytes == 0 {
			continue
		}
		sig.compactions++
	}
	for _, line := range readJSONLLines(filepath.Join(exDir, "transitions.jsonl")) {
		var tr Transition
		if err := json.Unmarshal([]byte(line), &tr); err != nil {
			continue
		}
		sig.transitions = append(sig.transitions, tr)
	}
	// Attach full blob output for the last three tool calls only, so
	// evidence quotes come from what the agent actually saw last.
	for i := range sig.tools {
		if i < len(sig.tools)-3 {
			continue
		}
		t := &sig.tools[i]
		if t.OutputPath == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(exDir, t.OutputPath))
		if err != nil {
			continue
		}
		if len(raw) > 4000 {
			raw = raw[:4000]
		}
		t.StdoutInline = string(raw)
	}
	return sig, nil
}

// parseTools normalizes tool calls for pattern matching.
func parseTools(tools []ToolCall) []parsedTool {
	out := make([]parsedTool, 0, len(tools))
	for _, t := range tools {
		p := parsedTool{
			seq: t.Seq, tool: t.Tool, exitCode: t.ExitCode,
			isError: t.IsError, output: t.StdoutInline, full: t.StdoutInline,
		}
		p.command = execCommand(t.Arguments, t.Tool)
		if m := aliveRe.FindStringSubmatch(t.StdoutInline); m != nil {
			b := m[1] == "true"
			p.alive = &b
		}
		out = append(out, p)
	}
	return out
}

var aliveRe = regexp.MustCompile(`alive:\s*(true|false)`)

// execCommand extracts the shell command from an exec/call_tool
// arguments blob; other tools yield "".
func execCommand(args string, tool string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return ""
	}
	for _, k := range []string{"command", "cmd"} {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	if tool == "call_tool" {
		if inner, ok := m["arguments"]; ok {
			raw, _ := json.Marshal(inner)
			var m2 map[string]any
			if err := json.Unmarshal(raw, &m2); err == nil {
				for _, k := range []string{"command", "cmd"} {
					if v, ok := m2[k].(string); ok {
						return v
					}
				}
			}
			if s, ok := inner.(string); ok {
				return s
			}
		}
		if v, ok := m["input"].(string); ok {
			return v
		}
	}
	return ""
}

func lowerContains(hay string, needles ...string) string {
	lower := strings.ToLower(hay)
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return n
		}
	}
	return ""
}

// harnessMatch reports the harness-friction pattern in a tool call:
// strong patterns match on text alone, weak ones need an error flag.
func harnessMatch(p parsedTool) string {
	if w := lowerContains(p.output, strongHarnessWords...); w != "" {
		return w
	}
	if p.isError || p.exitCode != 0 {
		return lowerContains(p.output, weakHarnessWords...)
	}
	return ""
}

var (
	timeoutWords = []string{"timeout", "timed out", "deadline exceeded", "context deadline", "agent timeout", "harbor timeout"}
	// Strong environment patterns fire on the text alone: executor
	// sessions can report exit 0 while the command body clearly
	// failed (for example a Python traceback ending in
	// FileNotFoundError), so the exit code is not a reliable gate.
	strongEnvWords = []string{
		"command not found", "no such file or directory", "unable to locate package",
		"no packages found", "no module named", "modulenotfounderror",
		"package not found", "no such package",
	}
	// Weak environment patterns need an error flag as well, since the
	// agent may mention them while recovering (for example echoing
	// "not installed" before installing).
	weakEnvWords = []string{
		"cannot find", "not installed", "failed to install", "could not resolve",
		"temporary failure resolving", "network unreachable",
	}
	// Strong harness patterns fire on the text alone; the rest need
	// an error flag.
	strongHarnessWords = []string{
		"unknown target", "remote nodes land in", "not wired", "query is required",
	}
	weakHarnessWords = []string{
		"unknown tool", "invalid arguments", "missing required", "schema validation",
		"unexpected argument",
	}
	contextWords = []string{
		"context length", "token limit", "context window", "history truncated",
		"lost context", "what was i doing", "i don't remember", "no memory of",
	}
	langErrorWords = []string{
		"traceback (most recent call", "syntaxerror", "nameerror", "typeerror",
		"referenceerror", "error in ", "execution halted", "compilation failed",
		"undefined reference", "cannot compile", "build failed",
	}
)

// deterministicTriage applies the ordered signal rules. It returns the
// cause, domain, evidence, failing step and whether the model fallback
// should refine the verdict.
func deterministicTriage(sig *triageSignals) (cause, domain, evidence string, step int, needModel bool) {
	tools := parseTools(sig.tools)
	lastSeq := 0
	if len(tools) > 0 {
		lastSeq = tools[len(tools)-1].seq
	}
	window := tools
	if len(window) > 5 {
		window = window[len(window)-5:]
	}
	fullText := sig.exception + " " + sig.final.Reason + " " + sig.final.Summary
	for _, tr := range sig.transitions {
		fullText += " " + tr.Reason
	}
	if w := lowerContains(fullText, timeoutWords...); w != "" {
		return TriageTimeout, "", evidenceOf("harness reported "+w, quoteOf(fullText, w), lastSeq),
			lastSeq, false
	}
	// Environment/tooling missing in the last window: late missing
	// binaries or packages the agent never recovered from.
	for i := len(window) - 1; i >= 0; i-- {
		p := window[i]
		if w := lowerContains(p.output, strongEnvWords...); w != "" {
			return TriageEnvMissing, "", evidenceOf("tool output reports "+w, quoteOf(p.output, w), p.seq),
				p.seq, false
		}
		if (p.isError || p.exitCode != 0) && lowerContains(p.output, weakEnvWords...) != "" {
			w := lowerContains(p.output, weakEnvWords...)
			return TriageEnvMissing, "", evidenceOf("tool output reports "+w, quoteOf(p.output, w), p.seq),
				p.seq, false
		}
	}
	// Harness friction in the last window: unknown targets, unwired
	// tools, empty-search errors the agent kept hitting.
	harnessHits := 0
	var harnessAt parsedTool
	for _, p := range tools {
		if harnessMatch(p) != "" {
			harnessHits++
			harnessAt = p
		}
	}
	inWindow := false
	for _, p := range window {
		if harnessMatch(p) != "" {
			inWindow = true
			harnessAt = p
		}
	}
	lastIsHarness := false
	if len(tools) > 0 && harnessMatch(tools[len(tools)-1]) != "" {
		lastIsHarness = true
	}
	if inWindow && (harnessHits >= 2 || len(tools) <= 8 || lastIsHarness) {
		w := harnessMatch(harnessAt)
		return TriageToolMisuse, "", evidenceOf("harness rejected the call ("+w+")", quoteOf(harnessAt.output, w), harnessAt.seq),
			harnessAt.seq, false
	}
	// Loop / no progress: detector signals fire here. Bare repeated
	// commands are checked after the knowledge-gap rule, so retrying
	// the same failing command with language errors triages as a
	// knowledge gap (the likely root cause) rather than a bare loop.
	if sig.loops >= 2 {
		kind := ""
		if len(sig.loopKinds) > 0 {
			kind = sig.loopKinds[len(sig.loopKinds)-1]
		}
		return TriageLoop, "", evidenceOf("loop detector fired "+itoa(sig.loops)+" times"+suffixKind(kind), quoteOf(strings.Join(sig.loopKinds, ", "), kind), lastSeq),
			lastSeq, false
	}
	// Context loss: explicit strings, or compaction paired with repeats.
	for _, p := range window {
		if w := lowerContains(p.output, contextWords...); w != "" {
			return TriageContextLoss, "", evidenceOf("output reports "+w, quoteOf(p.output, w), p.seq),
				p.seq, false
		}
	}
	if sig.compactions >= 2 {
		if seq, cmd := repeatedCommand(tools); seq > 0 {
			return TriageContextLoss, "", evidenceOf("history compacted then the agent repeated earlier work", quoteOf(cmd, cmd), seq),
				seq, false
		}
	}
	// Misunderstood instruction: required artifact never touched.
	if sig.instruction != "" && len(tools) >= 3 {
		if missing := missingArtifact(sig.instruction, tools); missing != "" {
			return TriageMisunderstood, "", evidenceOf("instruction requires "+missing+" but no tool call touches it", quoteOf(sig.instruction, missing), lastSeq),
				lastSeq, false
		}
	}
	// Knowledge gap: repeated language errors with a clear domain.
	langHits := 0
	var langAt parsedTool
	var langWord string
	for _, p := range window {
		if w := lowerContains(p.output, langErrorWords...); w != "" {
			langHits++
			langAt, langWord = p, w
		}
	}
	if langHits >= 2 {
		dom := detectDomain(sig.instruction, tools)
		return TriageKnowledgeGap, dom, evidenceOf("code fails with "+langWord, quoteOf(langAt.output, langWord), langAt.seq),
			langAt.seq, false
	}
	// Loop / no progress from bare repetition: reached only when the
	// repeats carry no language errors (those triaged above as a
	// knowledge gap).
	if seq, cmd := repeatedCommand(tools); seq > 0 {
		return TriageLoop, "", evidenceOf("same command repeated without progress", quoteOf(cmd, cmd), seq),
			seq, false
	}
	// Incomplete: barely started, or left background work running.
	if len(tools) <= 4 && sig.finalOK && sig.final.State == "completed" {
		return TriageIncomplete, "", evidenceOf("only "+itoa(len(tools))+" tool call"+plural(len(tools))+" before declaring done", lastOutputQuote(tools), lastSeq),
			lastSeq, false
	}
	for i := len(tools) - 1; i >= 0 && i >= len(tools)-2; i-- {
		if tools[i].alive != nil && *tools[i].alive {
			return TriageIncomplete, "", evidenceOf("background session still running when the run declared done", quoteOf(tools[i].output, "alive: true"), tools[i].seq),
				tools[i].seq, false
		}
	}
	if killedAfterHang(tools) {
		return TriageIncomplete, "", evidenceOf("killed the hanging check then declared done without a passing result", lastOutputQuote(tools), lastSeq),
			lastSeq, false
	}
	if langHits == 1 {
		// Single language error: knowledge gap or wrong solution is
		// ambiguous; let the cheap model decide when allowed.
		dom := detectDomain(sig.instruction, tools)
		return TriageKnowledgeGap, dom, evidenceOf("code fails with "+langWord, quoteOf(langAt.output, langWord), langAt.seq),
			langAt.seq, true
	}
	if sig.instruction == "" || len(tools) >= 5 {
		// Ambiguous among wrong solution, misunderstood and other:
		// needs the model when available.
		return TriageWrongDone, "", evidenceOf("run completed but the verifier scored 0 after "+itoa(len(tools))+" tool calls", lastOutputQuote(tools), lastSeq),
			lastSeq, true
	}
	return TriageOther, "", evidenceOf("no dominant signal in the agent records", lastOutputQuote(tools), lastSeq),
		lastSeq, true
}

func suffixKind(k string) string {
	if k == "" {
		return ""
	}
	return " (" + k + ")"
}

// repeatedCommand finds a normalized exec command run 3+ times.
func repeatedCommand(tools []parsedTool) (int, string) {
	counts := map[string]int{}
	lastSeq := map[string]int{}
	norm := map[string]string{}
	for _, p := range tools {
		if p.tool != "exec" || p.command == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(p.command))
		if len(key) > 300 {
			key = key[:300]
		}
		counts[key]++
		lastSeq[key] = p.seq
		norm[key] = p.command
	}
	for k, n := range counts {
		if n >= 3 {
			cmd := norm[k]
			if len(cmd) > 200 {
				cmd = cmd[:200]
			}
			return lastSeq[k], cmd
		}
	}
	// Repeated identical error lines also signal no progress.
	errCounts := map[string]int{}
	errLast := map[string]int{}
	for _, p := range tools {
		if !p.isError {
			continue
		}
		line := firstLine(p.output)
		if len(line) < 20 {
			continue
		}
		if len(line) > 120 {
			line = line[:120]
		}
		key := strings.ToLower(line)
		errCounts[key]++
		errLast[key] = p.seq
	}
	for k, n := range errCounts {
		if n >= 3 {
			return errLast[k], k
		}
	}
	return 0, ""
}

// missingArtifact returns a required /app path or code file named in
// the instruction that no tool call ever mentions.
func missingArtifact(instruction string, tools []parsedTool) string {
	re := regexp.MustCompile(`/app/\S+|[\w\-./]+\.(?:R|r|py|js|ts|go|rs|c|cpp|java|sql|sh|md|json|yaml|yml|html)`)
	cands := re.FindAllString(instruction, -1)
	if len(cands) == 0 {
		return ""
	}
	var hay strings.Builder
	for _, p := range tools {
		hay.WriteString(" " + p.command + " " + p.output)
	}
	blob := hay.String()
	seen := map[string]bool{}
	for _, c := range cands {
		trimmed := strings.Trim(c, `"'.,;:()[]`)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		base := trimmed
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if base == "" {
			continue
		}
		if !strings.Contains(blob, trimmed) && !strings.Contains(blob, base) {
			// Ignore tiny extensions-only matches.
			if len(base) < 3 {
				continue
			}
			return trimmed
		}
	}
	return ""
}

// killedAfterHang reports a pkill/kill following an alive session.
func killedAfterHang(tools []parsedTool) bool {
	seenAlive := false
	for _, p := range tools {
		if p.alive != nil && *p.alive {
			seenAlive = true
		}
	}
	if !seenAlive || len(tools) == 0 {
		return false
	}
	last := tools[len(tools)-1]
	if last.tool == "exec" && lowerContains(last.command, "pkill", "kill ") != "" {
		return true
	}
	if len(tools) >= 2 {
		prev := tools[len(tools)-2]
		if prev.alive != nil && *prev.alive && last.tool == "exec" {
			if lowerContains(last.command, "pkill", "kill ", "ps ") != "" {
				return true
			}
		}
	}
	return false
}

// detectDomain guesses the knowledge-gap domain from the instruction
// and the commands the agent ran.
func detectDomain(instruction string, tools []parsedTool) string {
	blob := strings.ToLower(instruction)
	for _, p := range tools {
		blob += " " + strings.ToLower(p.command)
	}
	switch {
	case strings.Contains(blob, "rscript"), strings.Contains(blob, ".r "), strings.Contains(blob, ".r\n"), strings.Contains(blob, "/ars"), regexp.MustCompile(`\.r\b`).MatchString(blob):
		return "R"
	case strings.Contains(blob, "python"), strings.Contains(blob, ".py"):
		return "python"
	case strings.Contains(blob, "node"), strings.Contains(blob, ".js"), strings.Contains(blob, ".ts"):
		return "javascript"
	case strings.Contains(blob, "go build"), strings.Contains(blob, "go test"), strings.Contains(blob, ".go"):
		return "go"
	case strings.Contains(blob, "cargo"), strings.Contains(blob, ".rs"):
		return "rust"
	case strings.Contains(blob, "gcc"), strings.Contains(blob, ".c "), strings.Contains(blob, ".h"):
		return "c"
	case strings.Contains(blob, "javac"), strings.Contains(blob, ".java"):
		return "java"
	case strings.Contains(blob, ".sql"), strings.Contains(blob, "psql"), strings.Contains(blob, "sqlite"):
		return "sql"
	case strings.Contains(blob, "docker"):
		return "docker"
	case strings.Contains(blob, ".sh"), strings.Contains(blob, "bash"):
		return "shell"
	}
	return ""
}

// evidenceOf joins a finding with its quote into 1-2 sentences.
func evidenceOf(finding, quote string, _ int) string {
	finding = strings.TrimSpace(finding)
	if !strings.HasSuffix(finding, ".") {
		finding += "."
	}
	if quote == "" {
		return finding
	}
	return finding + " Quote: \"" + quote + "\""
}

// quoteOf extracts a short snippet around the matched word.
func quoteOf(hay, needle string) string {
	line := firstLine(hay)
	if needle != "" {
		lower := strings.ToLower(hay)
		idx := strings.Index(lower, strings.ToLower(needle))
		if idx >= 0 {
			start := idx - 60
			if start < 0 {
				start = 0
			}
			end := idx + len(needle) + 120
			if end > len(hay) {
				end = len(hay)
			}
			snip := strings.ReplaceAll(hay[start:end], "\n", " | ")
			snip = strings.Join(strings.Fields(snip), " ")
			if len(snip) > 220 {
				snip = snip[:220]
			}
			return strings.TrimSpace(snip)
		}
	}
	if len(line) > 220 {
		line = line[:220]
	}
	return strings.TrimSpace(line)
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func lastOutputQuote(tools []parsedTool) string {
	for i := len(tools) - 1; i >= 0; i-- {
		if q := firstLine(tools[i].output); q != "" {
			if len(q) > 220 {
				q = q[:220]
			}
			return q
		}
	}
	return "no tool output"
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// triageCreds is the operator credentials file shape for the judge
// call (keys travel by environment/header only, never into artifacts).
type triageCreds struct {
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	FallbackAPIKey string `json:"fallback_api_key"`
	Model          string `json:"model"`
}

// loadTriageCreds reads the credentials file, falling back to the
// environment when the file is absent.
func loadTriageCreds(path string) triageCreds {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "excalibur", "credentials", "openai.json")
	}
	var c triageCreds
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &c)
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://api.openai.com/v1"
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("EXCALIBUR_API_KEY")
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("OPENAI_API_KEY")
	}
	if c.FallbackAPIKey == "" {
		c.FallbackAPIKey = os.Getenv("EXCALIBUR_FALLBACK_API_KEY")
	}
	return c
}

// defaultJudgeModel resolves the judge model: explicit override, then
// the run's own model from its config, then the credentials default.
func defaultJudgeModel(runDir string, trials []string, override string, creds triageCreds) string {
	if override != "" {
		return override
	}
	for _, t := range trials {
		if raw, err := os.ReadFile(filepath.Join(t, "result.json")); err == nil {
			var doc struct {
				Config struct {
					Agent struct {
						ModelName string `json:"model_name"`
					} `json:"agent"`
				} `json:"config"`
			}
			if err := json.Unmarshal(raw, &doc); err == nil && doc.Config.Agent.ModelName != "" {
				name := doc.Config.Agent.ModelName
				if i := strings.Index(name, "/"); i >= 0 {
					name = name[i+1:]
				}
				return name
			}
			break
		}
	}
	// Run-level config fallback (Harbor job config.json).
	if raw, err := os.ReadFile(filepath.Join(runDir, "config.json")); err == nil {
		var doc struct {
			Agents []struct {
				ModelName string `json:"model_name"`
			} `json:"agents"`
		}
		if err := json.Unmarshal(raw, &doc); err == nil && len(doc.Agents) > 0 && doc.Agents[0].ModelName != "" {
			name := doc.Agents[0].ModelName
			if i := strings.Index(name, "/"); i >= 0 {
				name = name[i+1:]
			}
			return name
		}
	}
	return creds.Model
}

// judgeVerdict is the cheap model's classification.
type judgeVerdict struct {
	Cause    string `json:"cause"`
	Domain   string `json:"domain"`
	Evidence string `json:"evidence"`
	FailStep int    `json:"fail_step"`
}

// judgeWithModel makes one cheap low-effort call to classify an
// ambiguous trial. The prompt carries only agent records and the
// pass/fail reward: never verifier tests or reference solutions.
func judgeWithModel(ctx context.Context, creds triageCreds, model string, sig *triageSignals, guess string) (judgeVerdict, error) {
	var out judgeVerdict
	if model == "" || creds.APIKey == "" {
		return out, fmt.Errorf("eval: triage judge needs a model and an API key")
	}
	instr := truncateRunes(sig.instruction, 1200)
	tools := parseTools(sig.tools)
	tail := tools
	if len(tail) > 3 {
		tail = tail[len(tail)-3:]
	}
	var last []string
	for _, p := range tail {
		q := firstLine(p.output)
		if len(q) > 400 {
			q = q[:400]
		}
		last = append(last, fmt.Sprintf("seq=%d tool=%s exit=%d err=%v cmd=%.120s out=%.400s",
			p.seq, p.tool, p.exitCode, p.isError, p.command, q))
	}
	user := "You classify why an AI coding agent failed a terminal task. Reply with JSON only.\n" +
		"Taxonomy (exact cause strings): wrong-solution-declared-done, incomplete-stopped-early, " +
		"timeout, environment/tooling-missing, tool-misuse/harness-friction, loop/no-progress, " +
		"context-loss, misunderstood-instruction, knowledge-gap, other. " +
		"For knowledge-gap set domain to the technology (e.g. R, python, sql, docker, shell); else \"\".\n" +
		"Evidence must be 1-2 sentences quoting the records (never mention the initial reading). fail_step is the tool seq where it went wrong (0 when unclear).\n" +
		"Task: " + sig.task + " trial: " + sig.trial + "\n" +
		"Instruction excerpt: " + instr + "\n" +
		fmt.Sprintf("Final: state=%s reason=%.200s exit=%d; reward=0; tool_calls=%d model_calls=%d loops=%d gate_tries=%d compactions=%d initial_reading=%s exception=%.200s\n",
			sig.final.State, sig.final.Reason, sig.final.ExitCode, len(tools), sig.modelCalls, sig.loops, sig.gateTries, sig.compactions, guess, sig.exception) +
		"Last tool calls:\n" + strings.Join(last, "\n")
	system := "You are a failure-triage judge. You only see agent records and pass/fail, never hidden tests. Output JSON like {\"cause\": \"...\", \"domain\": \"\", \"evidence\": \"...\", \"fail_step\": 0}."
	raw, err := postChatCompletions(ctx, creds, model, system, user)
	if err != nil {
		return out, err
	}
	v, err := parseVerdict(raw)
	if err != nil {
		return out, err
	}
	if !triageCauses[v.Cause] {
		return out, fmt.Errorf("eval: triage judge returned unknown cause %q", v.Cause)
	}
	if v.Cause != TriageKnowledgeGap {
		v.Domain = ""
	}
	return v, nil
}

// postChatCompletions posts one Chat Completions call with low
// reasoning effort, retrying once with the fallback key on
// quota/rate-limit errors.
func postChatCompletions(ctx context.Context, creds triageCreds, model, system, user string) (string, error) {
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		// No temperature: reasoning models such as gpt-6-luna reject
		// anything but the default. No other tuning knobs either:
		// the call stays cheap via the small input and the low
		// reasoning effort plus the completion cap.
		"reasoning_effort":      "low",
		"max_completion_tokens": 400,
	}
	raw, status, err := doChat(ctx, creds.BaseURL, creds.APIKey, body)
	if err != nil {
		return "", err
	}
	if (status == 429 || status >= 500) && creds.FallbackAPIKey != "" {
		raw2, status2, err2 := doChat(ctx, creds.BaseURL, creds.FallbackAPIKey, body)
		if err2 == nil && status2 < 400 {
			return extractContent(raw2)
		}
	}
	if status >= 400 {
		return "", fmt.Errorf("eval: triage judge status %d", status)
	}
	return extractContent(raw)
}

func doChat(ctx context.Context, base, key string, body map[string]any) ([]byte, int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	url := strings.TrimRight(base, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	tmp := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(tmp)
		if n > 0 {
			out.Write(tmp[:n])
		}
		if rerr != nil {
			break
		}
		if out.Len() > 256*1024 {
			break
		}
	}
	return out.Bytes(), resp.StatusCode, nil
}

// extractContent pulls the assistant text from a Chat Completions body.
func extractContent(raw []byte) (string, error) {
	var doc struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("eval: triage judge decode: %w", err)
	}
	if doc.Error != nil && len(doc.Choices) == 0 {
		return "", fmt.Errorf("eval: triage judge: %s", doc.Error.Message)
	}
	if len(doc.Choices) == 0 {
		return "", fmt.Errorf("eval: triage judge: no choices")
	}
	return doc.Choices[0].Message.Content, nil
}

// parseVerdict extracts the JSON verdict, tolerating code fences.
func parseVerdict(raw string) (judgeVerdict, error) {
	var v judgeVerdict
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "\n"); j >= 0 {
			s = s[j+1:]
		}
		if k := strings.Index(s, "```"); k >= 0 {
			s = s[:k]
		}
	}
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return v, fmt.Errorf("eval: triage judge returned no JSON")
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &v); err != nil {
		return v, fmt.Errorf("eval: triage judge JSON: %w", err)
	}
	if strings.TrimSpace(v.Cause) == "" {
		return v, fmt.Errorf("eval: triage judge returned empty cause")
	}
	return v, nil
}

// TriageRun classifies every reward-0 trial under runDir. It returns
// the per-trial results (sorted by trial) and the run stats.
func TriageRun(ctx context.Context, runDir string, opts TriageOptions) ([]TriageResult, TriageStats, error) {
	trials, err := FindTrialDirs(runDir)
	if err != nil {
		return nil, TriageStats{}, err
	}
	creds := loadTriageCreds(opts.CredentialsFile)
	model := defaultJudgeModel(runDir, trials, opts.ModelOverride, creds)
	var out []TriageResult
	stats := TriageStats{Total: len(trials)}
	for _, dir := range trials {
		reward, ok := readTrialReward(dir)
		if reason := TrialInfraReason(dir, reward); reason != "" {
			// Infrastructure failures are not agent failures: they keep their
			// own cause and are excluded from the failed count.
			stats.Infra++
			out = append(out, TriageResult{
				Task: stringBefore(filepath.Base(dir), "__"), Trial: filepath.Base(dir),
				Reward: reward, Cause: TriageInfra, Domain: reason,
				Evidence:   "infrastructure: " + reason + " (see the trial's harness and verifier records)",
				FinalState: "unknown", DecidedBy: "infra-rule",
			})
			continue
		}
		if !ok {
			stats.Skipped++
			continue
		}
		if reward > 0 {
			stats.Passed++
			continue
		}
		stats.Failed++
		sig, err := loadTriageSignals(dir)
		if err != nil {
			out = append(out, TriageResult{
				Task: stringBefore(filepath.Base(dir), "__"), Trial: filepath.Base(dir),
				Reward: reward, Cause: TriageOther,
				Evidence:   "agent records unreadable; classified by absence.",
				FinalState: "unknown", DecidedBy: "deterministic",
			})
			continue
		}
		cause, domain, evidence, step, needModel := deterministicTriage(sig)
		decided := "deterministic"
		if needModel && opts.useModel() && model != "" && creds.APIKey != "" {
			stats.JudgeCalls++
			if v, err := judgeWithModel(ctx, creds, model, sig, cause); err == nil {
				cause, domain, evidence, decided = v.Cause, v.Domain, v.Evidence, "model"
				if v.FailStep > 0 {
					step = v.FailStep
				}
			} else {
				stats.JudgeErrors++
			}
		}
		if cause == TriageKnowledgeGap && domain == "" {
			domain = detectDomain(sig.instruction, parseTools(sig.tools))
			if domain == "" {
				domain = "unknown"
			}
		}
		if cause != TriageKnowledgeGap {
			domain = ""
		}
		final := ""
		if sig.finalOK {
			final = sig.final.State
		}
		out = append(out, TriageResult{
			Task: sig.task, Trial: sig.trial, Reward: reward,
			Cause: cause, Domain: domain, Evidence: evidence,
			FailStep: step, ToolCalls: len(sig.tools),
			ModelCalls: sig.modelCalls, FinalState: final, DecidedBy: decided,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Trial < out[j].Trial })
	return out, stats, nil
}

func stringBefore(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i]
	}
	return s
}

// WriteTriageJSONL writes one result per line.
func WriteTriageJSONL(path string, results []TriageResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("eval: mkdir triage: %w", err)
	}
	var buf bytes.Buffer
	for _, r := range results {
		raw, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("eval: marshal triage: %w", err)
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("eval: write triage: %w", err)
	}
	return nil
}

// RenderTriageMarkdown aggregates counts per cause and per domain
// with examples.
func RenderTriageMarkdown(results []TriageResult, stats TriageStats, runDir string) string {
	byCause := map[string]int{}
	byDomain := map[string]int{}
	byDecider := map[string]int{}
	examples := map[string][]TriageResult{}
	for _, r := range results {
		byCause[r.Cause]++
		byDecider[r.DecidedBy]++
		if r.Cause == TriageKnowledgeGap {
			dom := r.Domain
			if dom == "" {
				dom = "unknown"
			}
			byDomain[dom]++
		}
		if len(examples[r.Cause]) < 3 {
			examples[r.Cause] = append(examples[r.Cause], r)
		}
	}
	var b strings.Builder
	b.WriteString("# Bench triage\n\n")
	fmt.Fprintf(&b, "Run: %s\n\n", runDir)
	fmt.Fprintf(&b, "Trials: %d passed: %d failed (triaged): %d skipped (no reward): %d\n\n",
		stats.Total, stats.Passed, stats.Failed, stats.Skipped)
	b.WriteString("Decided by: ")
	fmt.Fprintf(&b, "deterministic=%d model=%d\n\n", byDecider["deterministic"], byDecider["model"])
	fmt.Fprintf(&b, "Judge model calls: %d errors (fell back to deterministic): %d\n\n",
		stats.JudgeCalls, stats.JudgeErrors)
	b.WriteString("## Counts per cause\n\n| cause | count |\n|---|---|\n")
	causes := make([]string, 0, len(byCause))
	for c := range byCause {
		causes = append(causes, c)
	}
	sort.Strings(causes)
	for _, c := range causes {
		fmt.Fprintf(&b, "| %s | %d |\n", c, byCause[c])
	}
	b.WriteString("\n## Counts per domain (knowledge-gap only)\n\n| domain | count |\n|---|---|\n")
	if len(byDomain) == 0 {
		b.WriteString("| (none) | 0 |\n")
	} else {
		doms := make([]string, 0, len(byDomain))
		for d := range byDomain {
			doms = append(doms, d)
		}
		sort.Strings(doms)
		for _, d := range doms {
			fmt.Fprintf(&b, "| %s | %d |\n", d, byDomain[d])
		}
	}
	b.WriteString("\n## Examples\n\n| cause | trial | step | evidence |\n|---|---|---|---|\n")
	for _, c := range causes {
		for _, r := range examples[c] {
			ev := strings.ReplaceAll(r.Evidence, "|", "/")
			ev = strings.ReplaceAll(ev, "\n", " ")
			if len(ev) > 240 {
				ev = ev[:240]
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", c, r.Trial, r.FailStep, ev)
		}
	}
	return b.String()
}
