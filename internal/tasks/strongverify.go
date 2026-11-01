// Strong verification before completion: exercise the deliverable
// and ask an independent verifier (Sections 7.5, 8.7, 18.1).
//
// The W1 self-verification phase (selfverify.go) blocks completion
// without fresh executable evidence, but any passing tool result
// counts: `ls -l` after a write satisfies it without ever running
// the deliverable. Trial triage showed the failure mode this leaves
// open: the agent declares success on wrong results (for example an
// illegal move written to a file, or generated artifacts that were
// never validated) because nothing forces it to exercise the
// deliverable the way the instruction describes it.
//
// This file extends that phase; it does not duplicate it:
//
//  1. Exercise check (deterministic): after the last change, the
//     history must hold passing checks that actually exercise each
//     produced artifact — run the program or CLI with representative
//     inputs, import the module and call it, query the service, or
//     parse the output file and check the stated format — instead of
//     merely observing it (`ls`, `test -e`, bare `cat`, `read_file`).
//     Changes made by exec itself (redirections, builds, downloads)
//     count as changes too, closing the hole where runs without
//     write_file markers passed vacuously.
//  2. Independent verifier (one cheap verifier-role model call per
//     finish attempt, wired from the engine closing hook): a fresh
//     small context that carries ONLY the literal instruction, the
//     drafts, short evidence lines, the change diff and the final
//     answer — never executor reasoning — asks for the verifier's
//     own small checks plus a per-draft pass/fail verdict with
//     reasons. A FAIL verdict returns the agent to work with the
//     reasons, bounded by the same gate attempts as every other
//     pending.
//
// The verifier never looks for the benchmark's own tests: the prompt
// forbids it explicitly, and the deterministic layer only re-checks
// the agent's own checks against the instruction text.
//
// Every mechanism honors SelfVerifyConfig.EnableStrong (master,
// EXCALIBUR_STRONG_VERIFY) with per-mechanism switches
// (EXCALIBUR_STRONG_VERIFY_EXERCISE, EXCALIBUR_STRONG_VERIFY_VERIFIER),
// all default ON.
package tasks

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// exerciseMarkers are lowercase substrings of an exec/call_tool
// argument showing the call ran, queried or parsed something instead
// of merely observing it. The list is general engineering knowledge
// (interpreters, runners, test suites, query tools, parsers); it
// names no benchmark, product or task.
var exerciseMarkers = []string{
	// Run the deliverable: interpreters, runners, test suites,
	// builds and direct execution.
	"python", "node", "deno", "bun", "rscript",
	"ruby", "php", "perl", "julia",
	"java ", "javac", "gcc", "g++", "clang", "rustc",
	"go run", "go test", "cargo test", "cargo run",
	"pytest", "npm test", "npm run", "npx ",
	"make", "cmake", "gradle", "mvn ", "dotnet",
	"ctest", "bats", "./", ".sh", "bash ",
	"import ",
	// Query a service or store.
	"curl", "wget", "sqlite3", "psql", "mysql",
	"redis-cli", "pg_isready",
	// Parse or validate an output: format parsers, text
	// filters with an assertion shape, file identification,
	// checksums and explicit check/verify/assert runs.
	"jq", "yq", "xmllint", "json.tool",
	"grep", "awk", "sed", "wc ", "diff", "cmp ",
	"md5sum", "sha256sum", "file ",
	"check", "verify", "assert",
}

// mutationMarkers are lowercase substrings of an exec argument
// showing the call changed artifacts (move/copy, build, install,
// download, output redirection via redirectRe). Pure queries
// (`curl` without output redirection, `sqlite3` selects) are
// deliberately absent: they verify rather than change.
var mutationMarkers = []string{
	"tee", "-o ", "--output",
	"wget", "curl -o", "curl --output",
	"mv ", "cp ", "install ", "touch ", "mkdir ", "rm ",
	"cargo build", "go build",
	"make", "pip install", "apt-get", "apk add",
	"npm install", "npm run build",
	"unzip", "tar ", "patch ", "git apply",
}

// formatWords are the output-format tokens whose drafts need a
// parsing or validating check (not just a mention in prose) before
// the run may complete.
var formatWords = map[string]bool{
	"json": true, "csv": true, "tsv": true, "pdf": true,
	"html": true, "xml": true, "yaml": true, "yml": true,
	"parquet": true, "png": true, "jpg": true, "jpeg": true,
	"markdown": true, "md": true, "txt": true, "log": true,
}

// IsExercisingCall reports whether one history entry can serve as
// executable verification of the deliverable: a passing exec or
// call_tool that ran, queried or parsed something with observable
// output. Observations (`read_file`, bare `cat`), writes and bare
// existence probes (`ls`, `test -e`) never count, however
// successful: they show the artifact exists, not that it behaves as
// the instruction describes.
func IsExercisingCall(h HistoryTool) bool {
	if h.IsError || strings.TrimSpace(h.Output) == "" {
		return false
	}
	if isStillRunning(h.Output) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(h.Name)) {
	case "exec", "call_tool":
	default:
		return false
	}
	args := strings.ToLower(h.Args)
	for _, m := range exerciseMarkers {
		if strings.Contains(args, m) {
			return true
		}
	}
	return false
}

// sinkRe strips output redirections that discard rather than
// change artifacts (null sink, stderr merges) before the mutation
// scan, so a validating check like `json.tool x > /dev/null` is
// not mistaken for an artifact change.
var sinkRe = regexp.MustCompile(`(?i)\s*2?>\s*/dev/null|\s*2>&1`)

// redirectRe recognizes shell output redirection into a file: a
// `>` (or `>>`, `&>`, `2>`) preceded by a command boundary and
// followed by a target. A bare `>` substring is not enough — it
// also matches format strings (`printf '<%s>'`) and comparisons —
// so the boundary and target are required. Trade-off: a redirect
// written entirely inside quotes (`echo "a > b"`) still counts as a
// change; missing a real change is the worse failure (it lets
// unverified work complete), while a spurious one only asks for one
// more check.
var redirectRe = regexp.MustCompile(`(^|[\s;&|(\d])\d?&?>+&?\s*\S`)

// lastChangeIndex returns the index of the last history entry that
// changed artifacts: a write tool, or an exec whose arguments show
// a mutation (redirection into a file, move/copy, build, install,
// download). It returns -1 when nothing marks a change.
func lastChangeIndex(history []HistoryTool) int {
	last := -1
	for i, h := range history {
		if writeTools[strings.ToLower(h.Name)] {
			last = i
			continue
		}
		if strings.ToLower(strings.TrimSpace(h.Name)) != "exec" {
			continue
		}
		args := sinkRe.ReplaceAllString(strings.ToLower(h.Args), "")
		if redirectRe.MatchString(args) {
			last = i
			continue
		}
		for _, m := range mutationMarkers {
			if strings.Contains(args, m) {
				last = i
				break
			}
		}
	}
	return last
}

// splitStrongDrafts divides drafts into the two classes the
// deterministic exercise layer enforces: command drafts naming a
// produced artifact (path or file key), and format drafts naming a
// stated output format. Count and constraint drafts keep the
// inspection semantics of Section 18.1: the contrast check and the
// independent verifier judge them, since no deterministic rule can
// tell a real semantic check (for example that a move is legal)
// from a coincidental keyword mention.
func splitStrongDrafts(drafts []DraftCriterion) (cmd, fmt []DraftCriterion) {
	for _, d := range drafts {
		key := strings.ToLower(strings.TrimSpace(d.Key))
		if key == "" {
			continue
		}
		switch d.Method {
		case types.CriterionCommand, types.CriterionTest, types.CriterionMetric:
			cmd = append(cmd, d)
		default:
			if formatWords[key] {
				fmt = append(fmt, d)
			}
		}
	}
	return cmd, fmt
}

// draftMentioned reports whether an exercising-evidence corpus names
// a draft artifact: full key, basename, or module stem as a word
// (so `import filter` covers `/app/filter.py`). Matching stays
// case-insensitive, like DraftCoverage.
func draftMentioned(d DraftCriterion, corpus string) bool {
	key := strings.ToLower(strings.TrimSpace(d.Key))
	if key == "" {
		return true
	}
	if strings.Contains(corpus, key) {
		return true
	}
	base := key
	if i := strings.LastIndex(key, "/"); i >= 0 {
		base = key[i+1:]
	}
	if base != "" && base != key && strings.Contains(corpus, base) {
		return true
	}
	stem := base
	if i := strings.LastIndex(stem, "."); i > 0 {
		stem = stem[:i]
	}
	if stem != "" && stem != base && isWordPresent(corpus, stem) {
		return true
	}
	return false
}

var wordCharRe = regexp.MustCompile(`[a-z0-9_]`)

func isWordPresent(corpus, word string) bool {
	start := 0
	for start <= len(corpus) {
		rel := strings.Index(corpus[start:], word)
		if rel < 0 {
			return false
		}
		i := start + rel
		before := true
		if i > 0 {
			before = !wordCharRe.MatchString(corpus[i-1 : i])
		}
		after := true
		if j := i + len(word); j < len(corpus) {
			after = !wordCharRe.MatchString(corpus[j : j+1])
		}
		if before && after {
			return true
		}
		start = i + 1
	}
	return false
}

// CheckStrongExercise enforces the exercise-the-deliverable rule
// over the tool history: at least minChecks passing exercising
// calls after the last change, every command draft's artifact named
// by at least one of them, and every stated output format parsed or
// validated by at least one of them. It returns one concise pending
// per gap, most blocking first, capped so the injected note stays
// small.
func CheckStrongExercise(history []HistoryTool, drafts []DraftCriterion, minChecks int) []string {
	if minChecks < 1 {
		minChecks = 1
	}
	cmd, fmt := splitStrongDrafts(drafts)
	if len(cmd) == 0 && len(fmt) == 0 {
		return nil
	}
	if len(history) == 0 {
		return []string{"no executable check ran yet; " + strongHint(cmd, fmt, nil) + ", and only then finish"}
	}
	window := history
	if last := lastChangeIndex(history); last >= 0 {
		if last+1 >= len(history) {
			window = nil
		} else {
			window = history[last+1:]
		}
	}
	var exercising []HistoryTool
	for _, h := range window {
		if IsExercisingCall(h) {
			exercising = append(exercising, h)
		}
	}
	var pendings []string
	if len(exercising) < minChecks {
		pendings = append(pendings, "changes since the last check are unverified; "+strongHint(cmd, fmt, history)+", confirm they pass on the current artifacts, and only then finish")
	}
	var corpus strings.Builder
	for _, h := range exercising {
		corpus.WriteString(strings.ToLower(h.Args))
		corpus.WriteString("\n")
		corpus.WriteString(strings.ToLower(h.Output))
		corpus.WriteString("\n")
	}
	text := corpus.String()
	for _, d := range cmd {
		if draftMentioned(d, text) {
			continue
		}
		pendings = append(pendings, "deliverable not exercised: no passing run, import, query or parse of "+strconv.Quote(d.Key)+" after the last change; run "+d.Check+" with representative inputs and check the output, and only then finish")
		if len(pendings) >= 3 {
			return pendings
		}
	}
	for _, d := range fmt {
		if strings.Contains(text, strings.ToLower(d.Key)) {
			continue
		}
		pendings = append(pendings, "stated "+d.Key+" format not validated: parse the produced output and check the format constraints (not just its existence), and only then finish")
		if len(pendings) >= 3 {
			return pendings
		}
	}
	return pendings
}

// strongHint suggests what to run: the suggested checks of the
// drafts that still lack exercising evidence, capped so the note
// stays small and stable.
func strongHint(cmd, fmt []DraftCriterion, history []HistoryTool) string {
	var parts []string
	for _, d := range cmd {
		if strings.TrimSpace(d.Check) != "" {
			parts = append(parts, d.Check)
		}
		if len(parts) >= 2 {
			break
		}
	}
	if len(parts) == 0 && len(fmt) > 0 {
		parts = append(parts, "parse the "+fmt[0].Key+" output and check its format")
	}
	if len(parts) == 0 {
		for i := len(history) - 1; i >= 0 && len(parts) == 0; i-- {
			h := history[i]
			if writeTools[strings.ToLower(h.Name)] {
				continue
			}
			if strings.ToLower(h.Name) == "exec" && strings.TrimSpace(h.Args) != "" {
				parts = append(parts, "re-run "+trimLen(h.Args, 80))
			}
		}
	}
	if len(parts) == 0 {
		return "run the deliverable as the instruction describes (program/CLI with inputs, import and call, query the service, parse the output)"
	}
	return "exercise the deliverable (" + strings.Join(parts, "; ") + ")"
}

// StrongVerifierPrompt builds the single independent-verifier pass:
// the literal instruction, the checkable drafts, the commands run,
// short evidence lines, the change diff and the final answer
// excerpt. It never carries executor reasoning: there is no field
// for it, and the small context is rebuilt here from the same
// inputs as BuildVerifierContext. The prompt forbids looking for
// any hidden test suite: the verdict judges ONLY the instruction
// text above.
func StrongVerifierPrompt(small VerifierSmallContext, history []HistoryTool, finalText string) string {
	return StrongVerifierPromptWithConfig(DefaultSelfVerifyConfig(), small, history, finalText)
}

// StrongVerifierPromptWithConfig uses the same run-level ablation as the gate.
func StrongVerifierPromptWithConfig(cfg SelfVerifyConfig, small VerifierSmallContext, history []HistoryTool, finalText string) string {
	var b strings.Builder
	b.WriteString("You are the independent verifier for a task just executed by another agent. ")
	if cfg.EnableGeneralizeChecks {
		b.WriteString("You review recorded tool calls and results; you cannot execute tools in this pass. Request missing checks from the executor. ")
	} else {
		b.WriteString("You have read-only access plus the ability to run small checks. ")
	}
	b.WriteString("Judge ONLY the instruction text below against the produced artifacts and their evidence.\n\n")
	b.WriteString("HARD RULES:\n")
	b.WriteString("- Do NOT look for, name, run, or assume any hidden test suite, grading script, or reference solution. They do not exist for you.\n")
	b.WriteString("- Prior evidence lines are claims, not proof. A draft passes only when the evidence shows the deliverable was EXERCISED as the instruction describes: the program/CLI run with representative inputs, the module imported and called, the service queried, the output file parsed with its stated format/constraints checked, with fresh evidence after the last change.\n")
	b.WriteString("- Mere existence checks (ls, test -e, cat without validation) never pass a draft.\n\n")
	if cfg.EnableGeneralizeChecks {
		b.WriteString(generalizeInstructions)
	}
	b.WriteString("instruction:\n" + strings.TrimSpace(small.Request) + "\n\n")
	b.WriteString("checkable items:\n")
	for _, d := range small.Drafts {
		b.WriteString("- " + d + "\n")
	}
	b.WriteString("\ncommands run (newest first, at most 10):\n")
	if cfg.EnableGeneralizeChecks {
		b.WriteString("Last mutation history index: " + strconv.Itoa(lastChangeIndex(history)) + ". Only checks with a higher index count as fresh evidence.\n")
	}
	shown := 0
	for i := len(history) - 1; i >= 0 && shown < 10; i-- {
		h := history[i]
		name := strings.ToLower(strings.TrimSpace(h.Name))
		if name != "exec" && name != "call_tool" {
			continue
		}
		if h.IsError || isStillRunning(h.Output) {
			continue
		}
		if args := strings.TrimSpace(h.Args); args != "" {
			limit := 160
			if cfg.EnableGeneralizeChecks {
				limit = 2400
			}
			label := name
			if cfg.EnableGeneralizeChecks {
				label = "history[" + strconv.Itoa(i) + "] " + name
			}
			b.WriteString("- " + label + ": " + trimLen(oneLine(args), limit) + "\n")
			if cfg.EnableGeneralizeChecks {
				b.WriteString("  result: " + trimLen(oneLine(h.Output), 1200) + "\n")
			}
			shown++
		}
	}
	b.WriteString("\nevidence (one line per verification call, most recent last):\n")
	for _, e := range small.Evidence {
		b.WriteString("- " + e + "\n")
	}
	b.WriteString("\nchanged: " + strings.TrimSpace(small.Diff) + "\n")
	if t := strings.TrimSpace(finalText); t != "" {
		b.WriteString("\nfinal answer (excerpt):\n" + trimLen(t, 800) + "\n")
	}
	b.WriteString("\nReply in EXACTLY this shape (one draft line per checkable item ID):\n")
	b.WriteString("CHECKS:\n- <shell command you would run to verify>\n- ...\n")
	if cfg.EnableGeneralizeChecks {
		b.WriteString("FRESH_INPUTS: PASS or FAIL or NA - <executed command, generated input and independent oracle; NA only if no method applied to inputs>\n")
		b.WriteString("PROPERTY: PASS or FAIL - <one explicit instruction property and its executed assertion; repeat for EVERY property>\n")
		b.WriteString("OUTPUT_CONTRACT: PASS or FAIL or NA - <all explicit output properties tested; NA only if none specified>\n")
	}
	b.WriteString("D1: PASS - <one-line reason citing exercised evidence>\n")
	b.WriteString("D2: FAIL - <one-line reason naming what was not exercised>\n")
	b.WriteString("VERDICT: PASS or FAIL\n")
	b.WriteString("REASONS: <semicolon-separated reasons, empty when PASS>\n")
	return b.String()
}

// StrongVerdict is the parsed independent-verdict: overall pass,
// whether a verdict was actually present, the failure reasons and
// the verifier's own checks.
type StrongVerdict struct {
	// Pass is true when the verifier explicitly passed every draft
	// it judged and the overall verdict is PASS.
	Pass bool
	// Explicit is false when no verdict line was found at all, so
	// the caller falls back to the deterministic outcome instead
	// of blocking on an unparseable reply.
	Explicit bool
	// Reasons carries the failure reasons (and, on failure without
	// a REASONS line, the failed draft lines).
	Reasons []string
	// Checks holds the verifier's own small checks.
	Checks []string
	// FailedDrafts lists the draft IDs judged FAIL.
	FailedDrafts []string
}

var (
	strongDraftLineRe = regexp.MustCompile(`(?im)^\s*(D\d+)\s*:\s*(PASS|FAIL)\b\s*[-–:.]?\s*(.*)$`)
	strongVerdictRe   = regexp.MustCompile(`(?im)^\s*VERDICT\s*:\s*(PASS|FAIL)\b`)
	strongReasonsRe   = regexp.MustCompile(`(?im)^\s*REASONS\s*:\s*(.*)$`)
)

// ParseStrongVerdict parses the independent-verifier reply
// leniently: unparseable lines are skipped, and a reply with no
// verdict or draft line at all parses as non-explicit (the caller
// falls back to the deterministic outcome). Any FAIL draft or FAIL
// verdict fails overall.
func ParseStrongVerdict(text string, drafts []DraftCriterion) StrongVerdict {
	var out StrongVerdict
	verdictFail := false
	inChecks := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		upper := strings.ToUpper(t)
		if strings.HasPrefix(upper, "CHECKS") {
			inChecks = true
			continue
		}
		if m := strongVerdictRe.FindStringSubmatch(t); m != nil {
			out.Explicit = true
			verdictFail = strings.ToUpper(m[1]) != "PASS"
			inChecks = false
			continue
		}
		if m := strongReasonsRe.FindStringSubmatch(t); m != nil {
			for _, r := range strings.Split(m[1], ";") {
				if r = strings.TrimSpace(r); r != "" && !strings.EqualFold(r, "none") {
					out.Reasons = append(out.Reasons, r)
				}
			}
			inChecks = false
			continue
		}
		if m := strongDraftLineRe.FindStringSubmatch(t); m != nil {
			out.Explicit = true
			inChecks = false
			if strings.ToUpper(m[2]) == "FAIL" {
				out.FailedDrafts = append(out.FailedDrafts, strings.ToUpper(m[1]))
				if reason := strings.TrimSpace(m[3]); reason != "" {
					out.Reasons = append(out.Reasons, m[1]+": "+reason)
				} else {
					out.Reasons = append(out.Reasons, m[1]+" failed")
				}
			}
			continue
		}
		if inChecks && strings.HasPrefix(t, "-") {
			if check := strings.TrimSpace(strings.TrimPrefix(t, "-")); check != "" {
				out.Checks = append(out.Checks, check)
			}
		}
	}
	_ = drafts
	out.Pass = out.Explicit && !verdictFail && len(out.FailedDrafts) == 0
	if !out.Pass && out.Explicit && len(out.Reasons) == 0 {
		if len(out.FailedDrafts) > 0 {
			out.Reasons = append(out.Reasons, "independent verifier disagrees with "+strings.Join(out.FailedDrafts, ", "))
		} else {
			out.Reasons = append(out.Reasons, "independent verifier returned FAIL")
		}
	}
	return out
}

// The verifier judges semantics; keyword matching cannot establish whether an
// input is new or an oracle independent. Missing structured evidence fails
// closed only for this optional extension, within the existing gate budget.
const generalizeInstructions = `GENERALISATION CHECKLIST (mandatory):
- First list EVERY explicit property in the literal instruction as a PROPERTY checklist entry, including properties omitted from the draft items. Test each one separately against recorded executable evidence.
- For a method/program applied to inputs (function, CLI, extractor, converter, model, solver), require at least one executed check on fresh inputs generated by the agent: different seeds, sizes, random instances or edge cases. Cite the input, command and asserted result. Use an independent oracle, invariant or independently computed expected result.
- Visible examples or visible reference values MUST NOT be the only evidence. Comparing copied/extracted values to the same visible source does not establish that the method generalises. A proposed command is not an executed check.
- Check EVERY explicit output-format statement: types, shapes, file formats, field names, ordering and other stated constraints. Parsing alone does not prove those properties. Record one PROPERTY line per explicit property with its actual assertion and result.
- Return FAIL and name the missing checks whenever fresh-input evidence or any explicit property check is absent, failed, stale after the last change, or too abbreviated to assess. Return the executor to work to run those checks. Never read or run hidden benchmark tests.

`

// GeneralizationPendings validates the verifier's explicit coverage report.
// It does not treat suggested checks or the executor's final prose as evidence.
func GeneralizationPendings(reply string) []string {
	var fresh, contract, contractPassed, property bool
	var pending []string
	for _, line := range strings.Split(reply, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		status := strings.ToUpper(fields[0])
		accepted := status == "PASS" || status == "NA"
		switch strings.ToUpper(key) {
		case "FRESH_INPUTS":
			fresh = true
			if !accepted {
				pending = append(pending, "run a fresh generated-input check with an independent oracle")
			}
		case "OUTPUT_CONTRACT":
			contract = true
			contractPassed = contractPassed || status == "PASS"
			if !accepted {
				pending = append(pending, "test every explicit output-format property")
			}
		case "PROPERTY":
			property = true
			if status != "PASS" {
				pending = append(pending, "explicit property not verified: "+value)
			}
		}
	}
	if !fresh {
		pending = append(pending, "verifier must assess fresh generated-input evidence (FRESH_INPUTS)")
	}
	if !contract {
		pending = append(pending, "verifier must assess every explicit output property (OUTPUT_CONTRACT)")
	}
	if contractPassed && !property {
		pending = append(pending, "verifier must list explicit properties as a PROPERTY checklist")
	}
	return pending
}

// HasGeneralizableInstruction supplies a conservative backstop when draft
// extraction found no artifact. Applicability is still judged by the verifier;
// these general task terms do not establish that any check has passed.
func HasGeneralizableInstruction(request string) bool {
	return generalizableInstructionRe.MatchString(request)
}

var generalizableInstructionRe = regexp.MustCompile(`(?i)\b(functions?|programs?|scripts?|clis?|extractors?|converters?|models?|solvers?|algorithms?|implement|inputs?|outputs?|arrays?|lists?|matrices|tensors?|sort|convert|extract|solve|parse|transform)\b`)

// LastArtifactChange identifies the latest tool-recorded mutation for advisory
// verification reuse detection. It shares the completion gate's mutation rules.
func LastArtifactChange(history []HistoryTool) int { return lastChangeIndex(history) }
