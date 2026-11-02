// Deliverable checks for the finish gate (agent quality, work package
// D): the deterministic rules that decide whether named outputs and the
// agent's own verification really support `completed`, the soft warnings
// shown in the confirmation checklist, and the switches for each part.
//
// Hard checks (D3) refuse a completed finish with the concrete evidence.
// Soft checks (D4, D9) never refuse by themselves: they are listed in
// the confirmation checklist so the agent can address them before the
// run ends. Everything here is general: it reads the instruction, the
// run history and the files of the run workdir. It never inspects any
// benchmark material and carries no task-shaped keyword lists.
package engine

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// switchOn reads a default-on EXCALIBUR_* switch: off, 0, false or no
// disables it; anything else (including unset) keeps it on.
func switchOn(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "off", "0", "false", "no":
		return false
	default:
		return true
	}
}

// FinishVerdictResultEnabled controls D1 and the no-change refusal of
// D2: the finish tool result carries the verdict text.
func FinishVerdictResultEnabled() bool { return switchOn("EXCALIBUR_FINISH_VERDICT_RESULT") }

// DeliverableChecksEnabled controls the deliverable hard checks (D3),
// the deliverable soft checks (D4b, D4c, D9) and the strict deliverable
// path rules of internal/tasks (D3a) through tasks.DeliverableChecksEnabled.
func DeliverableChecksEnabled() bool { return switchOn("EXCALIBUR_DELIVERABLE_CHECKS") }

// InputSnapshotEnabled controls the provided-input snapshot (D4a).
func InputSnapshotEnabled() bool { return switchOn("EXCALIBUR_INPUT_SNAPSHOT") }

// FinishChecklistEnabled controls the confirmation checklist (D5) and
// the display of soft warnings in it.
func FinishChecklistEnabled() bool { return switchOn("EXCALIBUR_FINISH_CHECKLIST") }

// SoftVerifierEnabled controls the once-per-run refusal budget of the
// self-verify LLM gate (D6): later verdicts are advisory.
func SoftVerifierEnabled() bool { return switchOn("EXCALIBUR_SOFT_VERIFIER") }

// deliverable is one output the run must leave on disk: named by the
// instruction or written by a write tool in the history.
type deliverable struct {
	// Path is resolved on the run filesystem (absolute).
	Path string
	// Base is the file name; commands name files by it.
	Base string
	// Program is true for a script or program (extension or shebang).
	Program bool
	// Preexisting is true when the file existed at run start.
	Preexisting bool
}

// deliverableWriteTools are the history tools that create or edit files.
var deliverableWriteTools = map[string]bool{"write_file": true, "edit_file": true}

// programRunners maps script extensions to the interpreter that runs
// them. General engineering knowledge, not task material.
var programRunners = map[string]string{
	".py": "python3", ".sh": "bash", ".bash": "bash", ".js": "node", ".mjs": "node",
	".rb": "ruby", ".pl": "perl", ".php": "php", ".r": "Rscript", ".jl": "julia",
}

// finishRunView copies the run fields the deliverable checks need.
func (r *Run) finishRunView() (snap *tasks.InputSnapshot, started time.Time, workdir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fin.snapshot, r.StartedAt, r.Workdir
}

// preexistingPath reports whether path was provided before the run. The
// input snapshot answers precisely; without one the file's mtime is
// compared with the run start.
func preexistingPath(path string, snap *tasks.InputSnapshot, started time.Time) bool {
	if snap != nil {
		return snap.Has(path)
	}
	fi, err := os.Stat(path)
	return err == nil && fi.ModTime().Before(started)
}

// modifyVerbRe marks sentences that ask to change existing files.
var modifyVerbRe = regexp.MustCompile(`(?i)\b(modify|update|fix|edit|patch|change|correct|repair|overwrite|replace|rewrite|append|extend|amend|revise)\b`)

// asksToModify reports whether the instruction asks to change path: a
// sentence names it (full path or file name) together with a modify verb.
func asksToModify(request, path string) bool {
	base := filepath.Base(path)
	for _, s := range requestSentences(request) {
		if (strings.Contains(s, path) || strings.Contains(s, base)) && modifyVerbRe.MatchString(s) {
			return true
		}
	}
	return false
}

// requestSentences splits the instruction into sentences on lines and
// sentence ends. It is deterministic and never calls a model.
func requestSentences(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		for _, part := range strings.Split(line, ". ") {
			if t := strings.TrimSpace(part); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// instructionRunsPath recognizes an output explicitly named as something
// to execute, including extensionless programs.
func instructionRunsPath(request, path string) bool {
	for _, sentence := range requestSentences(request) {
		if !mentionsFile(sentence, filepath.Base(path)) {
			continue
		}
		if regexp.MustCompile(`(?i)\b(run|execute|invoke)\b`).MatchString(sentence) && !runNotWantedRe.MatchString(sentence) {
			return true
		}
	}
	return false
}

// isProgramPath reports a script or program: a known script extension,
// or a shebang on the first line.
func isProgramPath(path string) bool {
	if tasks.TypedArtifactsEnabled() {
		if info, err := os.Stat(path); err == nil && !info.Mode().IsRegular() {
			return false
		}
	}
	if _, ok := programRunners[strings.ToLower(filepath.Ext(path))]; ok {
		return true
	}

	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0 {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 2)
	n, _ := f.Read(buf)
	return n == 2 && string(buf) == "#!"
}

// writtenPath returns the path argument of a write or edit call.
func writtenPath(args string) string {
	var a map[string]any
	if json.Unmarshal([]byte(args), &a) != nil {
		return ""
	}
	for _, k := range []string{"path", "file_path", "file", "filename"} {
		if s, ok := a[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// execCommand returns the command of an exec call's arguments.
func execCommand(args string) string {
	var a struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(args), &a) != nil {
		return ""
	}
	return a.Command
}

// deliverables collects the run's deliverables: the named outputs of the
// instruction (absolute paths under the known roots, or under the workdir,
// that the instruction asks to create). Agent-created scratch files do
// not become requirements. A file that existed at run start is a deliverable only when
// the instruction asks to modify it (D3a, items 77 and 100).
func (e *Engine) deliverables(run *Run, request string, history []tasks.HistoryTool) []deliverable {
	snap, started, workdir := run.finishRunView()
	cfg := e.artifactConfig()
	seen := map[string]bool{}
	var out []deliverable
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, deliverable{
			Path: path, Base: filepath.Base(path),
			Program:     isProgramPath(path) || instructionRunsPath(request, path),
			Preexisting: preexistingPath(path, snap, started),
		})
	}
	for _, r := range tasks.ExtractArtifactRequirementsIn(request, cfg.MaxArtifacts, workdir) {
		if !tasks.HardArtifactRequirement(r) {
			continue
		}
		switch r.Kind {
		case tasks.ArtifactOutput, tasks.ArtifactPath, tasks.ArtifactExecutable:
		default:
			continue
		}
		resolved := resolveFinishPath(workdir, r.Path)
		if preexistingPath(resolved, snap, started) && !asksToModify(request, r.Path) {
			continue
		}
		if tasks.TypedArtifactsEnabled() {
			if info, err := os.Stat(resolved); err == nil && !info.Mode().IsRegular() {
				continue
			}
		}
		add(resolved)
	}
	// Compatibility switch for the former write-history inference.
	if switchOn("EXCALIBUR_INSTRUCTION_DELIVERABLES") {
		return out
	}
	for _, h := range history {
		if !deliverableWriteTools[strings.ToLower(h.Name)] {
			continue
		}
		p := writtenPath(h.Args)
		if p == "" {
			continue
		}
		resolved := resolveFinishPath(workdir, p)
		if preexistingPath(resolved, snap, started) && !asksToModify(request, p) {
			continue
		}
		add(resolved)
	}
	return out
}

// mentionsFile reports whether text names the file base as a token.
func mentionsFile(text, base string) bool {
	if base == "" || !strings.Contains(text, base) {
		return false
	}
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + regexp.QuoteMeta(base) + `($|[^A-Za-z0-9_.-])`)
	return re.MatchString(text)
}

// lastEditOf returns the history index of the last write or edit of f
// (a write call naming it, or a mutating exec naming it). -1 when none.
func lastEditOf(history []tasks.HistoryTool, f deliverable) int {
	last := -1
	for i, h := range history {
		name := strings.ToLower(h.Name)
		if deliverableWriteTools[name] && mentionsFile(h.Args, f.Base) {
			last = i
			continue
		}
		if name == "exec" && mentionsFile(execCommand(h.Args), f.Base) &&
			tasks.LastArtifactChange([]tasks.HistoryTool{h}) == 0 && !tasks.EntryVerifiesChanges(h) {
			last = i
		}
	}
	return last
}

// shellPartRe splits a shell command into its sequential parts.
var shellPartRe = regexp.MustCompile(`&&|\|\||;|\||\n`)

// probePrefixes start commands that only observe state (existence,
// sizes, text). Probes never count as verification or execution.
var probePrefixes = []string{
	"test ", "[ ", "ls", "stat ", "wc ", "file ", "which ", "command -v ",
	"echo ", "printf ", "cd ", "true", "pwd", "du ", "df ", "id", "head ",
	"tail ", "cat ", "grep -q", "grep -l", "readlink ", "realpath ",
}

// isProbeCommand reports a command made only of probe parts.
func isProbeCommand(cmd string) bool {
	seen := false
	for _, part := range shellPartRe.Split(cmd, -1) {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		seen = true
		probe := false
		for _, pre := range probePrefixes {
			if p == strings.TrimSpace(pre) || strings.HasPrefix(p, pre) {
				probe = true
				break
			}
		}
		if !probe {
			return false
		}
	}
	return seen
}

// execFailed reports whether one finished exec result failed: a non-zero
// exit, a tool error, or a failure marker in its output.
func execFailed(h tasks.HistoryTool) (failed bool, detail string) {
	code, hasCode := exitCodeOfOutput(h.Output)
	switch {
	case hasCode && code != 0:
		return true, fmt.Sprintf("exit %d", code)
	case !hasCode && h.IsError:
		return true, "tool error"
	}
	marker := failureMarker(h.Output)
	if switchOn("EXCALIBUR_PRECISE_FAILURE_MARKERS") && marker == "" && knownDiagnosticTool(execCommand(h.Args)) {
		if loc := toolErrorLineRe.FindStringIndex(h.Output); loc != nil {
			marker = h.Output[loc[0]:loc[1]]
		}
	}
	if marker != "" {
		return true, "failure marker " + marker
	}
	return false, ""
}

// failureMarkerRe matches the output markers of a failed check.
var failureMarkerRe = regexp.MustCompile(`\bFAIL(?:ED|URE)?\b|AssertionError|Traceback \(most recent call last\)|(?m:^Error:)|(?i:\bassert\b[^\n]*\bfailed\b)|(?m:^False$)`)

// Tool diagnostics use a recognizable tool origin. Numeric error metrics are
// measurements even when their label is capitalized.
var explicitFailureRe = regexp.MustCompile(`\bFAIL(?:ED|URE)?\b|AssertionError|Traceback \(most recent call last\)|(?i:\b[1-9][0-9]* failed\b|\bassert\b[^\n]*\bfailed\b)`)
var toolErrorLineRe = regexp.MustCompile(`(?im)^error:\s*[^\s0-9.+-][^\n]*`)

func knownDiagnosticTool(command string) bool {
	for _, part := range shellPartRe.Split(command, -1) {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		switch filepath.Base(fields[0]) {
		case "gcc", "g++", "clang", "clang++", "cc", "c++", "rustc", "cargo", "go", "make", "cmake", "ninja", "javac", "pytest", "ctest", "npm", "mvn", "gradle", "dotnet":
			return true
		}
	}
	return false
}

// zeroCountRe excludes a summary such as "FAIL: 0".
var zeroCountRe = regexp.MustCompile(`^\s*[:=]\s*0\b`)

// failureMarker returns the first failure marker in output, or "".
func failureMarker(out string) string {
	re := failureMarkerRe
	if switchOn("EXCALIBUR_PRECISE_FAILURE_MARKERS") {
		re = explicitFailureRe
	}
	for _, loc := range re.FindAllStringIndex(out, -1) {
		m := out[loc[0]:loc[1]]
		if m == "FAIL" && zeroCountRe.MatchString(out[loc[1]:]) {
			continue
		}
		return m
	}
	return ""
}

// testRunnerRe names test runners; checkScriptRe names check scripts.
var (
	testRunnerRe  = regexp.MustCompile(`(?i)\b(pytest|py\.test|unittest|nosetests|go test|npm (?:run )?test|yarn test|pnpm test|cargo test|make (?:test|check)|ctest|rspec|jest|mocha|tox)\b`)
	checkScriptRe = regexp.MustCompile(`(?i)[\w./-]*(?:check|verify|validate|test)[\w.-]*\.(?:py|sh|bash|js|rb|pl|r|jl)\b`)
)

// isVerificationCommand reports an exec that ran a deliverable, a test
// runner or a check script.
func isVerificationCommand(cmd string, files []deliverable) bool {
	for _, f := range files {
		if mentionsFile(cmd, f.Base) {
			return true
		}
	}
	return testRunnerRe.MatchString(cmd) || checkScriptRe.MatchString(cmd)
}

// verificationFailureReason is D3b: when the most recent verification
// exec after the last edit failed (non-zero exit or a failure marker),
// the finish is refused with the excerpt.
func verificationFailureReason(history []tasks.HistoryTool, files []deliverable) string {
	lastChange := tasks.LastArtifactChange(history)
	for i := len(history) - 1; i >= 0; i-- {
		if tasks.DependencyFreshnessEnabled() {
			if !tasks.EvidenceFresh(history, i, nil) {
				continue
			}
		} else if i <= lastChange {
			break
		}
		h := history[i]
		if strings.ToLower(h.Name) != "exec" {
			continue
		}
		cmd := execCommand(h.Args)
		if cmd == "" || isProbeCommand(cmd) || !isVerificationCommand(cmd, files) {
			continue
		}
		if tasks.HistoryRunning(h) {
			return ""
		}
		failed, _ := execFailed(h)
		if !failed {
			return ""
		}
		return verificationEntryReason(h)
	}
	return ""
}

// runNotWantedRe marks instructions that forbid running the deliverable.
var runNotWantedRe = regexp.MustCompile(`(?i)\b(?:do not|don't|dont|never|without|must not|should not|not)\s+(?:\w+\s+){0,2}?(?:run|running|execute|executing|invoke|invoking)\b`)

// programRunner returns the command that runs a deliverable program.
func programRunner(f deliverable) string {
	if r, ok := programRunners[strings.ToLower(filepath.Ext(f.Path))]; ok {
		return r + " " + f.Base
	}
	return "./" + f.Base
}

// unexecutedProgramReasons is D3c: a program deliverable must have run
// with exit 0 after its last edit, unless the instruction says not to run it.
func unexecutedProgramReasons(request string, history []tasks.HistoryTool, files []deliverable) []string {
	if runNotWantedRe.MatchString(request) {
		return nil
	}
	var out []string
	for _, f := range files {
		if !f.Program {
			continue
		}
		last := lastEditOf(history, f)
		if last < 0 && f.Preexisting {
			continue
		}
		ran := tasks.ServiceExercised(history[last+1:], f.Base)
		for i := last + 1; i < len(history) && !ran; i++ {
			h := history[i]
			if tasks.DependencyFreshnessEnabled() && !tasks.EvidenceFresh(history, i, []string{f.Path}) {
				continue
			}
			if strings.ToLower(h.Name) != "exec" {
				continue
			}
			cmd := execCommand(h.Args)
			// A run counts only when it exercises the program on real
			// input (not --help, a compile or an import): the shared
			// predicate of the strong verifier.
			if !mentionsFile(cmd, f.Base) || isProbeCommand(cmd) || !tasks.ExercisesDeliverable(cmd) || tasks.HistoryRunning(h) {
				continue
			}
			if code, ok := exitCodeOfOutput(h.Output); (ok && code == 0) || (!ok && !h.IsError) {
				ran = true
			}
		}
		if !ran {
			out = append(out, "deliverable `"+f.Path+"` is a program and has not run successfully since its last edit: run it on the task input and confirm exit 0 (for example `"+programRunner(f)+"`)")
		}
	}
	return out
}

// packageOrBuildRe names package managers and build tools.
var packageOrBuildRe = regexp.MustCompile(`(?i)\b(apt-get|apt|dpkg|pip3?|uv pip|npm|yarn|pnpm|cargo|go (?:build|install|get)|make|cmake|gcc|clang|mvn|gradle|conda|gem)\b`)

// serviceCommandRe names commands that keep serving on purpose.
var serviceCommandRe = regexp.MustCompile(`(?i)\b(serve|server|listen|daemon|http|port|localhost|uvicorn|gunicorn|nginx|redis-server|sshd)\b`)

// deliverableMTime reads the modification time of a deliverable; a
// variable so tests can pin it.
var deliverableMTime = func(path string) (time.Time, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

// sessionStartCommand returns the command of the exec that started the
// session id (the exec whose header names it).
func sessionStartCommand(history []tasks.HistoryTool, id string) string {
	needle := "session: " + id + " "
	for _, h := range history {
		if strings.ToLower(h.Name) == "exec" && strings.Contains(h.Output, needle) {
			return execCommand(h.Args)
		}
	}
	return ""
}

// runningWorkReasons is D3d: refuse while a live session of this run is
// running a package manager or a build, or is a non-service process that
// wrote a deliverable in the last 10 seconds.
func runningWorkReasons(history []tasks.HistoryTool, files []deliverable, now time.Time, sessions ...[]tools.SessionInfo) []string {
	var out []string
	for _, id := range tasks.LiveSessions(history) {
		cmd := sessionStartCommand(history, id)
		if cmd == "" {
			continue
		}
		build := packageOrBuildRe.MatchString(cmd)
		if switchOn("EXCALIBUR_COMMAND_POSITION_CHECK") {
			build = packageOrBuildCommand(cmd)
			for _, infos := range sessions {
				for _, info := range infos {
					if string(info.ID) == id {
						if !info.Alive {
							cmd = ""
						}
						build = info.Alive && (info.PackageManager || info.Build)
					}
				}
			}
		}
		if cmd == "" {
			continue
		}
		if build {
			out = append(out, "session "+id+" is still running a package manager or build (`"+truncOneLine(cmd, 120)+"`): wait for it to finish (exec_io read or wait_for) before calling finish")
			continue
		}
		verifiedService := false
		for _, f := range files {
			if mentionsFile(cmd, f.Base) && tasks.ServiceExercised(history[lastEditOf(history, f)+1:], f.Base) {
				verifiedService = true
				break
			}
		}
		if verifiedService || serviceCommandRe.MatchString(cmd) {
			continue
		}
		for _, f := range files {
			if mt, ok := deliverableMTime(f.Path); ok && now.Sub(mt) >= 0 && now.Sub(mt) < 10*time.Second {
				out = append(out, "session "+id+" (`"+truncOneLine(cmd, 120)+"`) wrote "+f.Path+" in the last 10 s: wait until it exits before calling finish")
				break
			}
		}
	}
	return out
}

// proofExts are proof-assistant sources; proofEscapeRe names the tokens
// that leave a proof incomplete.
var (
	proofExts     = map[string]bool{".v": true, ".lean": true, ".thy": true, ".agda": true}
	proofEscapeRe = regexp.MustCompile(`\b(Admitted|admit|Abort|sorry|oops)\b`)
)

// proofEscapeReasons is D3e: a delivered proof file containing an
// escape token (Admitted, admit, Abort, sorry, oops) is refused.
func proofEscapeReasons(files []deliverable) []string {
	var out []string
	for _, f := range files {
		if !proofExts[strings.ToLower(filepath.Ext(f.Path))] {
			continue
		}
		content, err := readCapped(f.Path, 1<<20)
		if err != nil {
			continue
		}
		if loc := proofEscapeRe.FindStringIndex(content); loc != nil {
			line := 1 + strings.Count(content[:loc[0]], "\n")
			out = append(out, "deliverable "+f.Path+" contains `"+content[loc[0]:loc[1]]+"` at line "+strconv.Itoa(line)+": the proof is incomplete; finish it without escapes")
		}
	}
	return out
}

// serviceSentenceRe marks instruction sentences that require a service.
var serviceSentenceRe = regexp.MustCompile(`(?i)\b(serve|serves|serving|listen|listening|server|running|respond|responds|endpoint|http|bind|accessible|available)\b`)

// portProbe opens a fresh localhost connection; a variable so tests can
// replace it without binding ports.
var portProbe = func(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// declaredServiceReasons is D3f: a port the instruction says must serve
// must accept a fresh connection at finish.
func declaredServiceReasons(request, workdir string, maxArtifacts int) []string {
	var out []string
	for _, r := range tasks.ExtractArtifactRequirementsIn(request, maxArtifacts, workdir) {
		if !serviceSentenceRe.MatchString(r.Source) {
			continue
		}
		port := r.Port
		if r.Kind == tasks.ArtifactURL {
			u, err := url.Parse(r.URL)
			if err != nil {
				continue
			}
			host := u.Hostname()
			if host != "localhost" && host != "127.0.0.1" && host != "::1" {
				continue
			}
			port, _ = strconv.Atoi(u.Port())
			if port == 0 {
				if u.Scheme == "https" {
					port = 443
				} else {
					port = 80
				}
			}
		} else if r.Kind != tasks.ArtifactPort {
			continue
		}
		if !portProbe(port) {
			out = append(out, "declared service on port "+strconv.Itoa(port)+" accepts no connection on localhost: start it, keep it running, and confirm it answers before calling finish")
		}
	}
	return out
}

// readCapped reads at most limit bytes of a file.
func readCapped(path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if n == 0 && err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

// deliverableHardReasons runs the D3 checks for one completed finish. An
// empty result means the deliverable checks pass.
func (e *Engine) deliverableHardReasons(run *Run, request string, history []tasks.HistoryTool) []string {
	if !DeliverableChecksEnabled() || !e.explicitFinishRequired(run) {
		return nil
	}
	history = e.currentSessionHistory(history)
	_, _, workdir := run.finishRunView()
	var sessions []tools.SessionInfo
	if e.deps.ActiveSessions != nil {
		sessions = e.deps.ActiveSessions()
		// Refresh stopped sessions before treating historical client evidence as
		// proof that a service is still alive. Keep the actual history immutable.
		history = append([]tasks.HistoryTool(nil), history...)
		for _, s := range sessions {
			if !s.Alive {
				history = append(history, tasks.HistoryTool{Name: "exec_io", Output: "session: " + string(s.ID) + " alive: false"})
			}
		}
	}
	files := e.deliverables(run, request, history)
	var out []string
	if reason := verificationFailureReason(history, files); reason != "" {
		out = append(out, reason)
	}
	out = append(out, unexecutedProgramReasons(request, history, files)...)
	out = append(out, runningWorkReasons(history, files, e.deps.Clock(), sessions)...)
	out = append(out, proofEscapeReasons(files)...)
	out = append(out, declaredServiceReasons(request, workdir, e.artifactConfig().MaxArtifacts)...)
	return out
}

// referenceNameRe names files that are answers or references, not inputs.
var referenceNameRe = regexp.MustCompile(`(?i)(expected|reference|answer|solution|golden|ground[_-]?truth)`)

// timingPatchRe matches reassignments of timing functions or patches of
// them (a measurement that does not use the real clock).
var timingPatchRe = regexp.MustCompile(`\b(?:time\.(?:perf_counter|time|monotonic|process_time)|timeit\.default_timer|Date\.now|performance\.now)\s*=[^=]|mock\.patch\(\s*["'][\w.]*(?:perf_counter|time\.time|monotonic|timeit)`)

// weakenRe matches disabled or weakened checks.
var weakenRe = regexp.MustCompile(`strict\s*=\s*False|@(?:pytest\.mark\.)?(?:skip|xfail)\b|pytest\.(?:skip|xfail)\(|unittest\.skip|\bskipIf\(|\bit\.skip\(|\bxit\(|\bdescribe\.skip\(|\bassume\(False\)`)

// toleranceRe captures a literal tolerance assignment.
var toleranceRe = regexp.MustCompile(`(?i)\b(atol|rtol|tol|tolerance|epsilon|eps)\s*=\s*([0-9]*\.?[0-9]+(?:[eE][-+]?\d+)?)`)

// softFinishWarnings collects the D4 and D9 warnings for one finish.
// They are shown in the confirmation checklist and never refuse.
func (e *Engine) softFinishWarnings(run *Run, request string, history []tasks.HistoryTool) []string {
	snap, _, _ := run.finishRunView()
	var out []string
	for _, call := range history {
		if call.Name == "exec" {
			if warning := tools.GitHistoryRewriteWarning(execCommand(call.Args), request); warning != "" {
				out = append(out, warning)
				break
			}
		}
	}
	if InputSnapshotEnabled() && snap != nil {
		for _, c := range snap.Changes(func(p string) bool { return asksToModify(request, p) }) {
			warning := "provided file " + c.Path + " was " + c.Kind + " and the instruction does not ask to change it: restore it or confirm the change is intended"
			if copy := snap.Entries[c.Path].OriginalCopy; copy != "" {
				warning += "; original copy: " + copy + " (restore with cp -a " + shellRestorePath(copy) + " " + shellRestorePath(c.Path) + ")"
			}
			out = append(out, warning)
		}
	}
	if DeliverableChecksEnabled() {
		files := e.deliverables(run, request, history)
		refs := referenceBases(snap, history)
		for _, f := range files {
			content, err := readCapped(f.Path, 256<<10)
			if err != nil {
				continue
			}
			out = append(out, deliverableContentWarnings(f, content, refs)...)
			if f.Program {
				out = append(out, exampleValueWarnings(f, content, request)...)
			}
		}
		out = append(out, baselineOverwriteWarnings(snap, files, history)...)
	}
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// referenceBases lists shipped reference files (by name) that the
// history reads or runs.
func referenceBases(snap *tasks.InputSnapshot, history []tasks.HistoryTool) []string {
	if snap == nil {
		return nil
	}
	var bases []string
	seen := map[string]bool{}
	for _, p := range sortedKeys(snap) {
		base := filepath.Base(p)
		if !referenceNameRe.MatchString(base) {
			continue
		}
		if !seen[base] {
			seen[base] = true
			bases = append(bases, base)
		}
	}
	return bases
}

// sortedKeys returns the snapshot paths in order.
func sortedKeys(snap *tasks.InputSnapshot) []string {
	paths := make([]string, 0, len(snap.Entries))
	for p := range snap.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// deliverableContentWarnings is D4b for one deliverable's content.
func deliverableContentWarnings(f deliverable, content string, refs []string) []string {
	var out []string
	for _, base := range refs {
		if mentionsFile(content, base) {
			out = append(out, "deliverable "+f.Path+" reads "+base+", a reference file shipped with the task")
		}
	}
	if m := timingPatchRe.FindString(content); m != "" {
		out = append(out, "deliverable "+f.Path+" reassigns a timing function (`"+truncOneLine(m, 60)+"`): measurements must come from the real clock")
	}
	if m := weakenRe.FindString(content); m != "" {
		out = append(out, "deliverable "+f.Path+" weakens or skips a check (`"+truncOneLine(m, 60)+"`): keep checks strict and unskipped")
	}
	if isTestLikeName(f.Base) {
		for _, m := range toleranceRe.FindAllStringSubmatch(content, -1) {
			if v, err := strconv.ParseFloat(m[2], 64); err == nil && v >= 0.01 {
				out = append(out, "deliverable "+f.Path+" widens a tolerance ("+m[1]+" = "+m[2]+"): justify it or tighten it")
				break
			}
		}
	}
	return out
}

// isTestLikeName reports a test or check file by its name.
func isTestLikeName(base string) bool {
	lower := strings.ToLower(base)
	return strings.Contains(lower, "test") || strings.Contains(lower, "check") ||
		strings.Contains(lower, "verify") || strings.Contains(lower, "spec")
}

// exampleSentenceRe marks instruction sentences that show an example.
var exampleSentenceRe = regexp.MustCompile(`(?i)\b(example|e\.g\.|for instance|such as|sample)\b`)

// exampleLiteralRe captures quoted spans and long numbers in an example.
var exampleLiteralRe = regexp.MustCompile("`([^`\\n]{4,80})`|\"([^\"\\n]{4,80})\"|'([^'\\n]{4,80})'|\\b(\\d{4,}(?:\\.\\d+)?)\\b")

// exampleValueWarnings is D4c: an example value from the instruction
// embedded verbatim in a program deliverable.
func exampleValueWarnings(f deliverable, content, request string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range requestSentences(request) {
		if !exampleSentenceRe.MatchString(s) {
			continue
		}
		for _, m := range exampleLiteralRe.FindAllStringSubmatch(s, -1) {
			v := firstNonEmptyStr(m[1], m[2], m[3], m[4])
			if v == "" || seen[v] || strings.Contains(v, "/") {
				continue
			}
			seen[v] = true
			if strings.Contains(content, v) {
				out = append(out, "deliverable "+f.Path+" embeds the instruction's example value "+strconv.Quote(truncOneLine(v, 40))+" verbatim: compute it from the input instead of hard-coding the example")
				if len(out) >= 2 {
					return out
				}
			}
		}
	}
	return out
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// baselineOverwriteWarnings is D9: a script shipped with the task that
// runs after a deliverable's last edit and mentions it may overwrite it.
func baselineOverwriteWarnings(snap *tasks.InputSnapshot, files []deliverable, history []tasks.HistoryTool) []string {
	if snap == nil {
		return nil
	}
	var out []string
	for _, p := range sortedKeys(snap) {
		if _, ok := programRunners[strings.ToLower(filepath.Ext(p))]; !ok {
			continue
		}
		script := filepath.Base(p)
		body, _ := readCapped(p, 256<<10)
		for _, f := range files {
			last := lastEditOf(history, f)
			if last < 0 {
				continue
			}
			for i := last + 1; i < len(history); i++ {
				cmd := execCommand(history[i].Args)
				if !mentionsFile(cmd, script) {
					continue
				}
				if mentionsFile(body, f.Base) || mentionsFile(cmd, f.Base) {
					out = append(out, "shipped script "+script+" ran after the last edit of "+f.Path+" and may overwrite it: regenerate the deliverable with your final program and do not run baseline writers into deliverable paths")
					break
				}
			}
		}
	}
	return out
}

// startInputSnapshot records the provided files of the run workdir
// before the first model action (D4a). It runs once per run.
func (e *Engine) startInputSnapshot(run *Run) {
	if !InputSnapshotEnabled() {
		return
	}
	run.mu.Lock()
	if run.fin.snapshotTaken || run.Workdir == "" {
		run.mu.Unlock()
		return
	}
	run.fin.snapshotTaken = true
	workdir := run.Workdir
	copyDir := run.fin.inputCopyDir
	run.mu.Unlock()
	if !switchOn("EXCALIBUR_INPUT_COPIES") {
		copyDir = ""
	}
	snap, err := tasks.TakeInputSnapshotWithCopies(workdir, tasks.InputSnapshotHashBudget, copyDir)
	if err != nil || snap == nil {
		return
	}
	run.mu.Lock()
	run.fin.snapshot = snap
	run.mu.Unlock()
}

// finishRequirements returns the artifact requirements the finish gate
// checks: extracted under the workdir, minus withdrawn items and inputs
// provided before the run unless the instruction asks to change them
// (D3a, item 100).
func (e *Engine) finishRequirements(run *Run, request string) []tasks.ArtifactRequirement {
	snap, started, workdir := run.finishRunView()
	reqs := tasks.ExtractArtifactRequirementsIn(request, e.artifactConfig().MaxArtifacts, workdir)
	kept := make([]tasks.ArtifactRequirement, 0, len(reqs))
	for _, a := range reqs {
		if !tasks.HardArtifactRequirement(a) {
			e.noteAdvisory(run, "artifact mention (advisory): "+a.Path)
			continue
		}
		if run.requirementDropped(a.ID) {
			continue
		}
		// Only output requirements are dropped: a provided file named as
		// an output is an input (item 100). Existence and execute-bit
		// checks on provided executables still apply.
		if a.Kind == tasks.ArtifactOutput && a.Path != "" && DeliverableChecksEnabled() {
			resolved := resolveFinishPath(workdir, a.Path)
			if preexistingPath(resolved, snap, started) && !asksToModify(request, a.Path) {
				continue
			}
		}
		kept = append(kept, a)
	}
	return kept
}

// verificationEntryReason formats command, status and output from one entry.
func verificationEntryReason(h tasks.HistoryTool) string {
	_, detail := execFailed(h)
	return "the last verification failed: `" + truncOneLine(execCommand(h.Args), 120) + "` (" + detail + "; output: " + lastOutputLines(h.Output, 3, 300) + "). Fix the cause, re-run the check, and only then call finish."
}

// SetInputCopyDir sets the private run-state destination before Start. Exec
// wiring supplies it; other engine callers retain metadata-only snapshots
// unless they explicitly supply their own persistent state directory.
func (r *Run) SetInputCopyDir(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fin.inputCopyDir = dir
}

// shellRestorePath keeps simple paths readable and quotes all shell syntax.
func shellRestorePath(path string) string {
	for _, c := range path {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/._-", c)) {
			return "'" + strings.ReplaceAll(path, "'", `'"'"'`) + "'"
		}
	}
	return path
}
