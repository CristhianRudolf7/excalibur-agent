// Self-verification for the completion path (Sections 7.5, 8.7, 18.1).
//
// Small, cheap models declare success on wrong results: the gate in
// gate.go is deterministic and strict, but in the non-interactive and
// embedded paths no criteria are ever recorded, so the gate passes
// vacuously on the first final answer. This file closes that hole
// with three engine-independent mechanisms:
//
//  1. Requirement extraction: a cheap deterministic pass over the
//     literal instruction pulls out checkable items (exact paths,
//     file names, formats, outputs, commands, constraints) as draft
//     criteria with a suggested executable check each. A single model
//     pass can refine the drafts; the deterministic drafts always
//     stand alone so the gate never depends on a model call.
//  2. Mandatory verification phase: before completed, the hook
//     confirms every draft against fresh executable evidence from
//     the tool history (a passing check after the last change, a
//     last result that is neither failed nor still running) and
//     re-reads the instruction against the produced artifacts
//     (contrast coverage). Anything unverified returns the agent to
//     work with a concise list of what is missing; returns stay
//     bounded by the gate attempts of Section 7.3.
//  3. Independent verifier input for big tasks: a fresh small
//     context (request, drafts, evidence summaries, change diff)
//     that never carries the executor reasoning, for the Section
//     8.7 verifier profile. Spawning stays with the orchestrator;
//     this package builds the input and infers when it applies.
//
// Every mechanism has a config switch (SelfVerifyConfig, environment
// overrides) so each can be measured on and off; the switches are
// listed in docs/development/agent-quality.md.
//
// The verifier never looks for the benchmark's own tests: it only
// re-runs the checks the agent wrote and the executable suggestions
// below, plus the agent's own recorded evidence.
package tasks

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// SelfVerifyConfig gates each self-verification mechanism. All
// mechanisms default ON: each helps small models and each can be
// ablated off through the environment.
type SelfVerifyConfig struct {
	// Enable is the master switch. Off restores the legacy gate-only
	// behavior.
	Enable bool
	// EnableExtraction runs the deterministic draft extraction (and
	// the single model refine pass when Refine is also on).
	EnableExtraction bool
	// EnableVerification runs the mandatory verification phase that
	// blocks completed without fresh executable evidence.
	EnableVerification bool
	// EnableContrast re-reads the instruction against the produced
	// artifacts and reports uncovered parts as omissions.
	EnableContrast bool
	// EnableIndependent requires the independent verifier verdict on
	// big tasks (daemon path; the embedded path has no spawner and
	// only records the need).
	EnableIndependent bool
	// EnableRefine allows the single model pass that refines the
	// deterministic drafts. The deterministic drafts always apply;
	// refine only improves their wording.
	EnableRefine bool
	// EnableStrong is the master switch for strong verification
	// (strongverify.go): the exercise-the-deliverable check and the
	// independent verifier pass. Off restores the W1 self-verification
	// behavior exactly.
	EnableStrong bool
	// EnableStrongExercise requires passing checks after the last
	// change that actually exercise each produced artifact (run,
	// import, query, parse and validate) instead of merely
	// observing it.
	EnableStrongExercise bool
	// EnableStrongVerifier runs the independent verifier pass (one
	// cheap verifier-role call per finish attempt) when the
	// deterministic checks would otherwise let the run complete.
	EnableStrongVerifier bool
	// EnableGeneralizeChecks requires fresh-input and explicit-property evidence.
	EnableGeneralizeChecks bool
	// MinFreshChecks is how many successful verification calls must
	// follow the last change before the run may complete.
	MinFreshChecks int
	// MaxReturns bounds verification returns to work. It mirrors
	// GateAttempts of Section 7.3.
	MaxReturns int
	// MaxDrafts caps the deterministic extraction so the injected
	// note stays small and stable.
	MaxDrafts int
	// BigTaskSteps marks an embedded run big when it ran this many
	// steps with enough drafts, standing in for the requirement
	// count the daemon path has.
	BigTaskSteps int
}

// DefaultSelfVerifyConfig returns the ablation defaults, overridden
// by the environment when set (1/0, true/false for booleans,
// integers for counts).
func DefaultSelfVerifyConfig() SelfVerifyConfig {
	cfg := SelfVerifyConfig{
		Enable:                 true,
		EnableExtraction:       true,
		EnableVerification:     true,
		EnableContrast:         true,
		EnableIndependent:      true,
		EnableRefine:           true,
		EnableStrong:           true,
		EnableStrongExercise:   true,
		EnableStrongVerifier:   true,
		EnableGeneralizeChecks: true,
		MinFreshChecks:         1,
		MaxReturns:             GateAttempts,
		MaxDrafts:              6,
		BigTaskSteps:           20,
	}
	cfg.Enable = envBool("EXCALIBUR_SELFVERIFY", cfg.Enable)
	cfg.EnableExtraction = envBool("EXCALIBUR_SELFVERIFY_EXTRACTION", cfg.EnableExtraction)
	cfg.EnableVerification = envBool("EXCALIBUR_SELFVERIFY_VERIFICATION", cfg.EnableVerification)
	cfg.EnableContrast = envBool("EXCALIBUR_SELFVERIFY_CONTRAST", cfg.EnableContrast)
	cfg.EnableIndependent = envBool("EXCALIBUR_SELFVERIFY_INDEPENDENT", cfg.EnableIndependent)
	cfg.EnableRefine = envBool("EXCALIBUR_SELFVERIFY_REFINE", cfg.EnableRefine)
	cfg.EnableStrong = envBool("EXCALIBUR_STRONG_VERIFY", cfg.EnableStrong)
	cfg.EnableStrongExercise = envBool("EXCALIBUR_STRONG_VERIFY_EXERCISE", cfg.EnableStrongExercise)
	cfg.EnableStrongVerifier = envBool("EXCALIBUR_STRONG_VERIFY_VERIFIER", cfg.EnableStrongVerifier)
	cfg.EnableGeneralizeChecks = envBool("EXCALIBUR_GENERALIZE_CHECKS", cfg.EnableGeneralizeChecks)
	cfg.MinFreshChecks = envInt("EXCALIBUR_SELFVERIFY_MIN_CHECKS", cfg.MinFreshChecks)
	cfg.MaxReturns = envInt("EXCALIBUR_SELFVERIFY_MAX_RETURNS", cfg.MaxReturns)
	cfg.MaxDrafts = envInt("EXCALIBUR_SELFVERIFY_MAX_DRAFTS", cfg.MaxDrafts)
	cfg.BigTaskSteps = envInt("EXCALIBUR_SELFVERIFY_BIG_STEPS", cfg.BigTaskSteps)
	if cfg.MinFreshChecks < 1 {
		cfg.MinFreshChecks = 1
	}
	if cfg.MaxDrafts < 1 {
		cfg.MaxDrafts = 1
	}
	return cfg
}

func envBool(name string, def bool) bool {
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

func envInt(name string, def int) int {
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

// DraftCriterion is one checkable item extracted from the literal
// instruction: an observable statement with a concrete executable
// check the agent can run. Drafts are advisory for the note and
// decisive for contrast; they never replace recorded criteria.
type DraftCriterion struct {
	// ID is the short stable code (D1, D2, ...) in extraction order.
	ID string
	// Statement is observable and checkable: exact paths, file
	// names, formats, outputs, commands, constraints.
	Statement string
	// Method suggests how to verify it (command by default;
	// inspection only when no executable check exists).
	Method types.CriterionMethod
	// Check is the concrete command the agent can run.
	Check string
	// Key is the artifact token contrast looks for in the history
	// and the final answer (a path or file name, lowercased).
	Key string
	// Source is the request phrase the draft came from.
	Source string
}

var (
	absPathRe  = regexp.MustCompile(`/[A-Za-z0-9_.\-]+(?:/[A-Za-z0-9_.\-]+)+`)
	relPathRe  = regexp.MustCompile("(?:^|[\\s(\"'])(\\./[^\\s\"'`,;]+)")
	quotedRe   = regexp.MustCompile("`([^`\\n]+)`|\"([^\"\\n]+)\"|'([^'\\n]+)'")
	bareFileRe = regexp.MustCompile(`\b[\w\-]+\.(py|js|ts|jsx|tsx|go|rs|java|c|h|hpp|cpp|cc|rb|php|R|r|jl|sh|bash|sql|json|yaml|yml|toml|ini|cfg|csv|tsv|html|css|xml|pdf|md|txt|log|png|jpg|jpeg|wasm|so|o|a|bin|exe|jar|m|stan|pm|pl|tex|bib|Rmd|ipynb)\b`)
	formatRe   = regexp.MustCompile(`(?i)\b(json|csv|tsv|pdf|html|xml|yaml|yml|parquet|png|jpg|jpeg|markdown|md|txt|log)\b`)
	commandRe  = regexp.MustCompile(`(?i)\b(run|runs|execute|executes|build|builds|compile|compiles|test|tests|pytest|npm test|go test|cargo test|make|rscript|python3?)\b`)
	countRe    = regexp.MustCompile(`(?i)\b(\d+)\s+(points?|samples?|tests?|files?|rows?|columns?|cases?|examples?|iterations?|epochs?|steps?|lines?|words?|characters?|keys?|fields?)\b`)
)

// ExtractDraftCriteria runs the cheap deterministic pass over the
// literal instruction: exact paths, file names, formats, outputs,
// commands and constraints become checkable drafts in request order,
// deduplicated and capped at maxDrafts. It never calls a model and
// never inspects any benchmark material.
func ExtractDraftCriteria(request string, maxDrafts int) []DraftCriterion {
	if maxDrafts < 1 {
		maxDrafts = 6
	}
	seen := map[string]bool{}
	var out []DraftCriterion
	add := func(statement string, method types.CriterionMethod, check, key, source string) {
		norm := strings.ToLower(strings.TrimSpace(statement))
		if norm == "" || seen[norm] {
			return
		}
		if len(out) >= maxDrafts {
			return
		}
		seen[norm] = true
		out = append(out, DraftCriterion{
			ID:        "D" + strconv.Itoa(len(out)+1),
			Statement: statement,
			Method:    method,
			Check:     check,
			Key:       strings.ToLower(key),
			Source:    source,
		})
	}
	text := strings.TrimSpace(request)
	if text == "" {
		return nil
	}
	sentences := splitSentences(text)

	// Exact absolute and ./-relative paths first: they are the
	// strongest, most checkable items.
	for _, p := range absPathRe.FindAllString(text, -1) {
		clean := strings.TrimRight(p, ".,;:")
		add("path "+clean+" exists with the expected content",
			types.CriterionCommand, exerciseCheckFor(clean),
			clean, "path "+clean)
	}
	for _, m := range relPathRe.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		p := strings.TrimRight(m[1], ".,;:")
		add("path "+p+" exists with the expected content",
			types.CriterionCommand, exerciseCheckFor(p),
			p, "path "+p)
	}
	// Quoted spans that name a file or command.
	for _, m := range quotedRe.FindAllStringSubmatch(text, -1) {
		span := firstNonEmpty(m[1], m[2], m[3])
		span = strings.TrimSpace(span)
		if span == "" || len(span) > 120 {
			continue
		}
		if strings.Contains(span, " ") && !looksLikeCommand(span) {
			continue
		}
		if strings.ContainsAny(span, "/.") || looksLikeCommand(span) {
			add("quoted item "+span+" is honored exactly",
				types.CriterionCommand, span,
				span, "quoted "+span)
		}
	}
	// Bare file names with known extensions.
	for _, f := range bareFileRe.FindAllString(text, -1) {
		add("file "+f+" exists with the expected content",
			types.CriterionCommand, exerciseCheckFor(f),
			f, "file "+f)
	}
	// Output formats tied to an output verb in the same sentence.
	for _, s := range sentences {
		lower := strings.ToLower(s)
		if !strings.Contains(lower, "output") && !strings.Contains(lower, "format") &&
			!strings.Contains(lower, "export") && !strings.Contains(lower, "write") &&
			!strings.Contains(lower, "produce") && !strings.Contains(lower, "generate") &&
			!strings.Contains(lower, "return") {
			continue
		}
		for _, m := range formatRe.FindAllStringSubmatch(s, -1) {
			fmt := strings.ToLower(m[1])
			add("output is produced in "+fmt+" format",
				types.CriterionInspection, "inspect the produced "+fmt+" output against the request",
				fmt, trimLen(s, 120))
		}
	}
	// Counts with units: the agent must produce that many items.
	for _, m := range countRe.FindAllStringSubmatch(text, -1) {
		if len(m) < 3 {
			continue
		}
		add("produces "+m[1]+" "+m[2]+" as requested",
			types.CriterionInspection, "count the produced "+m[2]+" and compare with "+m[1],
			m[1]+" "+m[2], trimLen(m[0], 80))
	}
	// Hard constraints (must/never/only/exactly): keep the
	// strongest ones as inspection drafts so contrast can check
	// the final answer mentions them.
	for _, s := range sentences {
		lower := strings.ToLower(s)
		if !strings.Contains(lower, "must") && !strings.Contains(lower, "never") &&
			!strings.Contains(lower, "only") && !strings.Contains(lower, "exactly") &&
			!strings.Contains(lower, "required") && !strings.Contains(lower, "at least") {
			continue
		}
		t := trimLen(strings.TrimSpace(s), 160)
		if t == "" {
			continue
		}
		key := constraintKey(s)
		add("constraint holds: "+t, types.CriterionInspection,
			"re-read the instruction and confirm the constraint holds on the artifacts",
			key, t)
	}
	_ = commandRe
	return out
}

func looksLikeCommand(s string) bool {
	lower := strings.ToLower(s)
	for _, w := range []string{"test", "run", "build", "make", "go ", "npm", "pytest", "python", "rscript", "ls", "cat", "grep", "./", ".sh", "-"} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			return p
		}
	}
	return ""
}

func splitSentences(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		for _, part := range strings.Split(line, ". ") {
			t := strings.TrimSpace(part)
			if t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

func trimLen(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func constraintKey(sentence string) string {
	words := strings.Fields(strings.ToLower(sentence))
	stop := map[string]bool{"the": true, "a": true, "an": true, "must": true, "should": true,
		"never": true, "only": true, "and": true, "or": true, "to": true, "of": true,
		"is": true, "are": true, "be": true, "it": true, "that": true, "with": true}
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()\"'")
		if len(w) > 3 && !stop[w] {
			return w
		}
	}
	return trimLen(strings.ToLower(sentence), 24)
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`).MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// scriptRunners maps artifact extensions to the interpreter that
// exercises them. The mapping is general engineering knowledge (no
// task, product or benchmark); unknown artifacts keep the existence
// probe so the suggestion is always runnable.
var scriptRunners = []struct{ ext, runner string }{
	{".py", "python3"},
	{".js", "node"},
	{".mjs", "node"},
	{".cjs", "node"},
	{".ts", "node"},
	{".sh", "bash"},
	{".bash", "bash"},
	{".r", "Rscript"},
	{".rb", "ruby"},
	{".php", "php"},
	{".pl", "perl"},
	{".jl", "julia"},
}

// exerciseCheckFor suggests how to exercise an artifact path: run
// script files with their interpreter (with representative inputs to
// be filled in by the agent), otherwise probe existence. Running the
// deliverable outranks merely observing it (Section 18.1).
func exerciseCheckFor(path string) string {
	lower := strings.ToLower(path)
	for _, sr := range scriptRunners {
		if strings.HasSuffix(lower, sr.ext) {
			return sr.runner + " " + shellQuote(path)
		}
	}
	return "test -s " + shellQuote(path) + " && ls -l " + shellQuote(path)
}

// RefinePrompt builds the single model pass over the deterministic
// drafts: the verifier-role model improves wording, drops duplicates
// and assigns the right method, without seeing any executor
// reasoning. The prompt carries only the instruction and the drafts.
func RefinePrompt(request string, drafts []DraftCriterion) string {
	var b strings.Builder
	b.WriteString("Rewrite these checkable acceptance criteria for the task below. ")
	b.WriteString("Keep only items directly checkable from the task text; drop the rest. ")
	b.WriteString("One per line as: METHOD | statement | check command. ")
	b.WriteString("METHOD is command when a shell command verifies it, else inspection.\n\n")
	b.WriteString("task:\n" + strings.TrimSpace(request) + "\n\ncriteria:\n")
	for _, d := range drafts {
		b.WriteString("- " + string(d.Method) + " | " + d.Statement + " | " + d.Check + "\n")
	}
	return b.String()
}

// ParseRefinedCriteria parses the single model refine pass back into
// drafts, leniently: unparseable lines are skipped and the caller
// falls back to the deterministic drafts when nothing parses.
func ParseRefinedCriteria(text string, maxDrafts int) []DraftCriterion {
	var out []DraftCriterion
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			continue
		}
		method := types.CriterionMethod(strings.TrimSpace(strings.ToLower(parts[0])))
		if method != types.CriterionCommand && method != types.CriterionTest &&
			method != types.CriterionMetric && method != types.CriterionInspection {
			continue
		}
		statement := strings.TrimSpace(parts[1])
		check := ""
		if len(parts) == 3 {
			check = strings.TrimSpace(parts[2])
		}
		if statement == "" || seen[strings.ToLower(statement)] {
			continue
		}
		if len(out) >= maxDrafts {
			break
		}
		seen[strings.ToLower(statement)] = true
		key := ""
		if m := bareFileRe.FindString(statement); m != "" {
			key = strings.ToLower(m)
		} else if m := absPathRe.FindString(statement); m != "" {
			key = strings.ToLower(m)
		}
		out = append(out, DraftCriterion{
			ID:        "D" + strconv.Itoa(len(out)+1),
			Statement: statement, Method: method, Check: check,
			Key: key, Source: "refined",
		})
	}
	return out
}

// HistoryTool is the engine-independent view of one tool result the
// verification phase needs: what ran, with what output, and whether
// it failed. The engine maps its history into this shape.
type HistoryTool struct {
	// Name is the tool name (exec, read_file, write_file, ...).
	Name string
	// Args carries the raw arguments (for path mentions).
	Args string
	// Output is the tool output text.
	Output string
	// IsError is true when the tool reported failure.
	IsError bool
}

// writeTools are history entries that change artifacts; everything
// else after them can serve as fresh verification.
var writeTools = map[string]bool{"write_file": true, "edit_file": true}

// CheckFreshVerification enforces the mandatory verification phase
// over the tool history: the last result must not be a failure or a
// still-running session, and at least minChecks successful
// verification calls must follow the last change. It returns one
// concise pending per problem, most blocking first.
func CheckFreshVerification(history []HistoryTool, minChecks int) []string {
	if minChecks < 1 {
		minChecks = 1
	}
	if len(history) == 0 {
		return nil
	}
	last := history[len(history)-1]
	if last.IsError {
		return []string{"last " + toolName(last) + " failed; fix the cause, re-run the check, and only then finish"}
	}
	if isStillRunning(last.Output) && !verifiedServicePoll(history, last) {
		return []string{toolName(last) + " is still running; wait for it (status/get_tool_output), confirm success, and only then finish"}
	}
	lastWrite := -1
	for i, h := range history {
		if writeTools[strings.ToLower(h.Name)] {
			lastWrite = i
		}
	}
	if lastWrite < 0 {
		return nil
	}
	fresh := 0
	for _, h := range history[lastWrite+1:] {
		if h.IsError || isStillRunning(h.Output) {
			continue
		}
		if isVerification(h) {
			fresh++
		}
	}
	if fresh < minChecks {
		return []string{"changes since the last check are unverified; run the checks (" +
			suggestChecks(history) + "), confirm they pass on the current artifacts, and only then finish"}
	}
	return nil
}

func toolName(h HistoryTool) string {
	if h.Name == "" {
		return "tool"
	}
	return h.Name
}

// isStillRunning reports the embedded terminal marker for a session
// that has not settled: nothing is verified yet while it shows.
func isStillRunning(output string) bool {
	return strings.Contains(output, "alive: true")
}

// isVerification reports whether a successful tool result can serve
// as executable verification: it executed something and returned
// observable output, instead of only recording a write.
func isVerification(h HistoryTool) bool {
	switch strings.ToLower(h.Name) {
	case "exec", "read_file", "call_tool":
		return strings.TrimSpace(h.Output) != ""
	default:
		return false
	}
}

func suggestChecks(history []HistoryTool) string {
	for i := len(history) - 1; i >= 0; i-- {
		h := history[i]
		if writeTools[strings.ToLower(h.Name)] {
			continue
		}
		if strings.ToLower(h.Name) == "exec" && strings.TrimSpace(h.Args) != "" {
			return "re-run " + rerunCommandHint(h.Args)
		}
	}
	return "re-run the task checks"
}

// DraftCoverage re-reads the instruction against the produced
// artifacts: every draft whose key artifact appears nowhere in the
// tool history or the final answer is reported as an omission. Keys
// match case-insensitively on basenames and full tokens so
// paraphrases of the surrounding text do not hide a missing file.
func DraftCoverage(drafts []DraftCriterion, finalText string, history []HistoryTool) (covered bool, omissions []string) {
	if len(drafts) == 0 {
		return true, nil
	}
	corpus := strings.ToLower(finalText) + "\n"
	for _, h := range history {
		corpus += strings.ToLower(h.Args) + "\n" + strings.ToLower(h.Output) + "\n"
	}
	for _, d := range drafts {
		key := strings.TrimSpace(d.Key)
		if key == "" {
			continue
		}
		if strings.Contains(corpus, key) {
			continue
		}
		if base := baseToken(key); base != "" && base != key && strings.Contains(corpus, base) {
			continue
		}
		omissions = append(omissions, d.ID+" "+d.Statement)
	}
	return len(omissions) == 0, omissions
}

func baseToken(key string) string {
	key = strings.Trim(key, "\"'`")
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return ""
}

// InferBigTask reports whether the independent verifier input should
// be built: the daemon rule (N2 with enough requirements, any N3
// milestone) plus the embedded stand-in (enough drafts or steps with
// real tool work behind them).
func InferBigTask(level types.TaskLevel, requirements, steps int, drafts int) bool {
	if level == types.TaskN3 {
		return true
	}
	if level == types.TaskN2 && requirements >= VerifierRequirements {
		return true
	}
	return drafts >= VerifierRequirements || steps >= 20 && drafts >= 3
}

// VerifierSmallContext builds the fresh small context for the
// independent verifier pass: the literal request, the drafts, short
// evidence summaries and the change diff. It never carries executor
// reasoning by construction: there is no field for it.
type VerifierSmallContext struct {
	// Request is the literal original instruction.
	Request string
	// Drafts are the checkable items extracted from it.
	Drafts []string
	// Evidence holds one short line per verification call.
	Evidence []string
	// Diff summarizes the changed artifacts.
	Diff string
}

// BuildVerifierContext compiles the small context from the request,
// the drafts and the tool history: at most 20 evidence lines,
// truncated outputs, no reasoning.
func BuildVerifierContext(request string, drafts []DraftCriterion, history []HistoryTool, maxLines int) VerifierSmallContext {
	if maxLines < 1 {
		maxLines = 20
	}
	ctx := VerifierSmallContext{Request: strings.TrimSpace(request)}
	for _, d := range drafts {
		ctx.Drafts = append(ctx.Drafts, d.ID+" "+d.Statement+" ["+string(d.Method)+"]")
	}
	var changed []string
	seenChanged := map[string]bool{}
	for _, h := range history {
		if !writeTools[strings.ToLower(h.Name)] {
			continue
		}
		path := artifactOf(h)
		if path != "" && !seenChanged[path] {
			seenChanged[path] = true
			changed = append(changed, path)
		}
	}
	ctx.Diff = "changed: " + strings.Join(changed, ", ")
	for _, h := range history {
		if len(ctx.Evidence) >= maxLines {
			break
		}
		if !isVerification(h) || h.IsError {
			continue
		}
		line := toolName(h) + ": " + trimLen(oneLine(h.Output), 160)
		ctx.Evidence = append(ctx.Evidence, line)
	}
	return ctx
}

func artifactOf(h HistoryTool) string {
	for _, tok := range strings.Fields(h.Args) {
		tok = strings.Trim(tok, "\"',;")
		if absPathRe.MatchString(tok) || bareFileRe.MatchString(tok) {
			return tok
		}
	}
	return ""
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " | ")
	return strings.TrimSpace(s)
}

// VerificationNote renders the compact note the engine injects when
// verification fails: what is missing and how to fix it, capped so
// the context stays small and stable.
func VerificationNote(pendings []string) string {
	const maxLines = 5
	lines := append([]string(nil), pendings...)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "…and "+strconv.Itoa(len(pendings)-maxLines)+" more")
	}
	return "self-verify before finishing: " + strings.Join(lines, "; ")
}

// ExtractRequestFromTaskLayer recovers the literal instruction from
// a rendered L3 task layer (request:\n...\nfixed instructions:).
// The engine stashes it once per run so verification and contrast
// need no driver changes.
func ExtractRequestFromTaskLayer(layer string) string {
	const prefix = "request:\n"
	if !strings.HasPrefix(layer, prefix) {
		return ""
	}
	rest := layer[len(prefix):]
	if i := strings.Index(rest, "\nfixed instructions:\n"); i >= 0 {
		return strings.TrimSpace(rest[:i])
	}
	return strings.TrimSpace(rest)
}

// SelfVerifyRequest is everything the verification phase needs for
// one finish attempt.
type SelfVerifyRequest struct {
	// Request is the literal original instruction ("", when unknown).
	Request string
	// Drafts are the extracted checkable items (computed when empty
	// and extraction is on).
	Drafts []DraftCriterion
	// History carries the tool results in order.
	History []HistoryTool
	// FinalText is the model's final answer text.
	FinalText string
	// Steps counts engine steps so far (big-task inference).
	Steps int
	// Level and Requirements feed the daemon big-task rule when
	// known; zero values fall back to drafts/steps.
	Level        types.TaskLevel
	Requirements int
}

// ApplySelfVerify merges the mandatory verification phase into the
// gate outcome: the deterministic gate result stands, and
// verification appends its own concise pendings. With an empty
// request, no drafts and a criteria-bearing gate input, only the
// history freshness check applies; with no request, no drafts and
// no criteria, it returns the gate report untouched (legacy runs,
// including every existing unit test).
func ApplySelfVerify(cfg SelfVerifyConfig, gate GateReport, in GateInput, req SelfVerifyRequest) (GateReport, []DraftCriterion) {
	if !cfg.Enable {
		return gate, req.Drafts
	}
	drafts := req.Drafts
	if len(drafts) == 0 && cfg.EnableExtraction && strings.TrimSpace(req.Request) != "" {
		drafts = ExtractDraftCriteria(req.Request, cfg.MaxDrafts)
	}
	out := gate
	if cfg.EnableVerification {
		out.Pendings = append(out.Pendings, BehaviouralEvidencePending(req.Request, req.History)...)
	}
	if cfg.EnableVerification && needsVerification(in, req, drafts) {
		out.Pendings = append(out.Pendings, CheckFreshVerification(req.History, cfg.MinFreshChecks)...)
		if cfg.EnableContrast && len(drafts) > 0 {
			if _, omissions := DraftCoverage(drafts, req.FinalText, req.History); len(omissions) > 0 {
				capped := omissions
				if len(capped) > 3 {
					capped = capped[:3]
				}
				out.Pendings = append(out.Pendings,
					"request parts with no produced artifact: "+strings.Join(capped, "; "))
			}
		}
	}
	// Strong verification extends (never replaces) the phase above:
	// the deliverable must be exercised, not merely observed, after
	// the last change. The independent verifier pass itself runs in
	// the engine closing hook (verify_hooks.go), which has the model
	// endpoint; the gate stays tool-free.
	if cfg.EnableStrong && cfg.EnableStrongExercise && needsVerification(in, req, drafts) {
		out.Pendings = append(out.Pendings, CheckStrongExercise(req.History, drafts, cfg.MinFreshChecks)...)
	}
	out.OK = len(out.Pendings) == 0
	if len(drafts) > 0 {
		sort.Slice(drafts, func(i, j int) bool { return drafts[i].ID < drafts[j].ID })
	}
	return out, drafts
}

// needsVerification decides whether the phase applies at all: with
// criteria (or checklist/contrast/verifier duties) the gate already
// speaks, and the history freshness check joins it; on the bare
// embedded path the request or its drafts trigger the backstop.
// Empty request, no drafts, no criteria: legacy behavior.
func needsVerification(in GateInput, req SelfVerifyRequest, drafts []DraftCriterion) bool {
	if len(in.Criteria) > 0 || in.ChecklistRequired || in.ContrastRequired || in.VerifierRequired {
		return true
	}
	if strings.TrimSpace(req.Request) != "" || len(drafts) > 0 {
		return true
	}
	return false
}

// rerunCommandHint renders executable text rather than the tool's JSON envelope.
// Keep refusal guidance short, including for multi-line shell programs.
func rerunCommandHint(args string) string {
	if !envBool("EXCALIBUR_PLAIN_CHECK_HINTS", true) {
		return trimLen(args, 80)
	}
	command, ok := historyCommand(args)
	if !ok {
		return "the task checks"
	}
	command = strings.TrimSpace(command)
	line, _, multiline := strings.Cut(command, "\n")
	line = strings.TrimSuffix(line, "\r")
	chars := []rune(line)
	if len(chars) > 160 {
		return string(chars[:160]) + "…"
	}
	if multiline {
		return line + "…"
	}
	return line
}
