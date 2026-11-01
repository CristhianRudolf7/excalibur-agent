package engine

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// Strict blocked policy (director's W8 design, non-interactive profiles),
// reworked in work package D (D7).
//
// `finish status=blocked` is refused while time remains unless the claim
// survives the general rules below. Every rule is a property of the run
// (what was attempted, what the instruction names, how much wall budget
// is left, whether the deliverable checks pass). None is a vocabulary of
// a particular task:
//
//	closing  less than 10% of the wall budget is left: never blocked.
//	(a)      no edit, write or install was attempted: the claim is untested.
//	(b)      a named deliverable can still be created and no call touched it.
//	(e)      the run itself stopped the service the reason says is gone.
//	(c)      more than half of the wall budget remains and the reason does
//	         not quote a failing command, its output, or the external marker
//	         that a failing output of this run shows.
//	(d)      the deliverable checks pass: the work is done, finish completed.
//
// Each refusal is one numbered reason in the finish tool result (D1).

// strictInstallMarks name install commands and provisioner reports.
var strictInstallMarks = []string{
	"apt-get", "apt ", "pip install", "uv pip", "npm install",
	"yarn add", "pnpm add", "gem install", "cargo add", "go get",
	"install.packages", "cpan", "planinstall", "provision:",
}

func containsAnyLower(hay string, marks []string) string {
	for _, m := range marks {
		if strings.Contains(hay, m) {
			return m
		}
	}
	return ""
}

// timeRemains reports whether the run still has wall-clock (or step)
// budget left: closing has not been reached. Uncapped runs count as
// time remaining so validation failures never block by default.
func (e *Engine) timeRemains(run *Run) bool {
	run.mu.Lock()
	limit := run.Tracker.Limit
	used := run.Tracker.Used
	elapsed := run.HeartbeatAt.Sub(run.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	run.mu.Unlock()
	if limit.MaxSeconds > 0 {
		total := int64(limit.MaxSeconds)
		left := total - int64(elapsed/time.Second)
		// Closing starts below 10% remainder: anything above that is
		// time remaining.
		threshold := int64(tasks.ClosingFraction * float64(total))
		if threshold < 1 {
			threshold = 1
		}
		return left > threshold
	}
	if limit.MaxSteps > 0 {
		left := limit.MaxSteps - used.Steps
		th := int(tasks.ClosingFraction * float64(limit.MaxSteps))
		if th < 1 {
			th = 1
		}
		return left > th
	}
	return true
}

// wallRemaining returns the share of the wall budget still unused and
// whether the run has a wall budget at all. Without one there is no
// remaining share to compare, so rule (c) does not apply.
func (e *Engine) wallRemaining(run *Run) (float64, bool) {
	run.mu.Lock()
	limit := run.Tracker.Limit
	elapsed := run.HeartbeatAt.Sub(run.StartedAt)
	run.mu.Unlock()
	if limit.MaxSeconds <= 0 {
		return 0, false
	}
	total := time.Duration(limit.MaxSeconds) * time.Second
	left := total - elapsed
	if left <= 0 {
		return 0, true
	}
	return float64(left) / float64(total), true
}

// changeAttempted reports whether the run wrote, edited or installed
// anything: a write or edit call, a mutating exec, or an install command.
func changeAttempted(history []tasks.HistoryTool) bool {
	if tasks.LastArtifactChange(history) >= 0 {
		return true
	}
	for _, h := range history {
		if strings.ToLower(h.Name) != "exec" {
			continue
		}
		if containsAnyLower(strings.ToLower(h.Args), strictInstallMarks) != "" ||
			containsAnyLower(strings.ToLower(h.Output), strictInstallMarks) != "" {
			return true
		}
	}
	return false
}

// inKnownRoot reports whether an absolute path is under a deliverable
// root or under the run workdir.
func inKnownRoot(p, workdir string) bool {
	if w := strings.TrimRight(workdir, "/"); w != "" && (p == w || strings.HasPrefix(p, w+"/")) {
		return true
	}
	for _, root := range tasks.DeliverableRoots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return true
		}
	}
	return false
}

// untouchedDeliverable returns the first named output that does not exist
// yet, can still be created (its parent exists or it lies under a known
// root or the workdir), and that no call has touched. "" when none.
func (e *Engine) untouchedDeliverable(run *Run, request string, history []tasks.HistoryTool) string {
	_, _, workdir := run.finishRunView()
	stat := e.deps.ArtifactStat
	if stat == nil {
		stat = tasks.DefaultStat
	}
	for _, r := range tasks.ExtractArtifactRequirementsIn(request, e.artifactConfig().MaxArtifacts, workdir) {
		if r.Kind != tasks.ArtifactOutput || r.Path == "" {
			continue
		}
		resolved := resolveFinishPath(workdir, r.Path)
		if fs, err := stat(resolved); err == nil && fs.Exists {
			continue
		}
		parentFS, err := stat(filepath.Dir(resolved))
		creatable := (err == nil && parentFS.IsDir) || inKnownRoot(resolved, workdir)
		if !creatable || touched(history, resolved) {
			continue
		}
		return resolved
	}
	return ""
}

// touched reports whether any call named the path (full or base name).
func touched(history []tasks.HistoryTool, path string) bool {
	base := filepath.Base(path)
	for _, h := range history {
		if strings.Contains(h.Args, path) || mentionsFile(h.Args, base) || mentionsFile(h.Output, base) {
			return true
		}
	}
	return false
}

// reasonQuotesFailure reports whether the blocked reason quotes a failing
// command together with an output line from the same failed call.
func reasonQuotesFailure(reason string, history []tasks.HistoryTool) bool {
	rl := strings.ToLower(reason)
	for _, h := range history {
		failed := h.IsError
		if !failed && strings.ToLower(h.Name) == "exec" {
			failed, _ = execFailed(h)
		}
		if !failed {
			continue
		}
		outLower := strings.ToLower(h.Output)
		cmd := strings.ToLower(execCommand(h.Args))
		if cmd == "" || !strings.Contains(rl, cmd) {
			continue
		}
		for _, line := range strings.Split(outLower, "\n") {
			if t := strings.TrimSpace(line); len(t) >= 12 && strings.Contains(rl, t) {
				return true
			}
		}

	}
	return false
}

// deliverablesPass reports whether the deliverables exist and the finish
// checks pass for them (D3 hard checks and the artifact gate). It is the
// "the work is done" test of rule (d).
func (e *Engine) deliverablesPass(run *Run, request string, history []tasks.HistoryTool) bool {
	if !DeliverableChecksEnabled() {
		return false
	}
	if len(e.deliverables(run, request, history)) == 0 {
		return false
	}
	if len(e.deliverableHardReasons(run, request, history)) > 0 {
		return false
	}
	_, _, workdir := run.finishRunView()
	stat := e.deps.ArtifactStat
	if stat == nil {
		stat = tasks.DefaultStat
	}
	for _, d := range e.deliverables(run, request, history) {
		fs, err := stat(d.Path)
		if err != nil || !fs.Exists || (!fs.IsDir && fs.Size == 0) {
			return false
		}
	}
	reqs := e.finishRequirements(run, request)
	return len(tasks.CheckArtifactFiles(reqs, history, workdir, stat, e.deps.ArtifactRead)) == 0
}

// isSelfCausedUnavailability reports whether the blocked reason claims a
// service is unavailable after the run itself stopped, killed or closed
// it: the history shows a kill, signal or close, and the reason names the
// resulting unavailability. Killing a process the task requires is a
// failure, not an external impossibility.
func isSelfCausedUnavailability(reason string, history []tasks.HistoryTool) bool {
	if len(history) == 0 {
		return false
	}
	rl := strings.ToLower(reason)
	unavail := false
	for _, m := range []string{"stopped", "killed", "not available", "unavailable", "no longer", "not serving", "timed out", "timeout", "did not reach", "could not complete", "unmet"} {
		if strings.Contains(rl, m) {
			unavail = true
			break
		}
	}
	if !unavail {
		return false
	}
	for _, h := range history {
		hl := strings.ToLower(h.Args + "\n" + h.Output)
		if strings.Contains(hl, "kill ") || strings.Contains(hl, "kill\t") ||
			strings.Contains(strings.ToLower(h.Args), "\"signal\"") ||
			strings.Contains(strings.ToLower(h.Args), "\"close\"") ||
			strings.Contains(hl, "pkill") || strings.Contains(hl, "killall") {
			return true
		}
	}
	// A reason that admits the run stopped the service is self-caused
	// even when the kill is not in the history.
	for _, m := range []string{"was stopped", "i stopped", "we stopped"} {
		if strings.Contains(rl, m) {
			return true
		}
	}
	return false
}

// workHistory returns the run history without the finish control calls:
// they are never work.
func workHistory(run *Run) []tasks.HistoryTool {
	var out []tasks.HistoryTool
	for _, h := range historyTools(run.historyCopy()) {
		if h.Name != FinishToolName {
			out = append(out, h)
		}
	}
	return out
}

// strictBlockedRefusal applies the D7 rules to a blocked claim. Empty
// means the claim may be accepted. Non-interactive runs only; the switch
// off restores acceptance.
func (e *Engine) strictBlockedRefusal(run *Run, reason string) string {
	if !StrictBlockedEnabled() {
		return ""
	}
	// Only non-interactive runs enforce the strict policy; interactive
	// daemon runs keep the legacy path-exists-only check.
	if !e.explicitFinishRequired(run) {
		return ""
	}
	// Closing: time is almost exhausted. Never end blocked.
	if !e.timeRemains(run) {
		return "strict blocked: closing mode, less than 10% of the wall budget is left; do not declare blocked. Leave the best deliverable in place, verify it quickly, and call finish with status completed."
	}
	run.mu.Lock()
	request := run.Request
	run.mu.Unlock()
	history := workHistory(run)
	if !changeAttempted(history) {
		return "strict blocked: no edit, write or install was attempted, so the claim is untested; inspect the named inputs and try the task before declaring blocked."
	}
	if path := e.untouchedDeliverable(run, request, history); path != "" {
		return "strict blocked: the named deliverable " + path + " can still be created and no call has touched it; create it as stated before declaring blocked."
	}
	if isSelfCausedUnavailability(reason, history) {
		return "strict blocked: the run itself stopped or killed the service the reason calls unavailable. That is a failure, not an external impossibility: restart it, verify it serves, and keep working."
	}
	if share, known := e.wallRemaining(run); known && share > 0.5 && !reasonQuotesFailure(reason, history) {
		return "strict blocked: more than half of the wall budget remains and the reason does not quote a failing command or its output; show the failing output that proves the impossibility, or keep working."
	}
	if e.deliverablesPass(run, request, history) {
		return "strict blocked: the deliverable checks pass, so the work is done; finish with status completed and the evidence."
	}
	return ""
}

// blockedReasons returns the refusal reasons for a blocked claim: a
// named path that exists refutes it, then the strict blocked rules apply.
// Empty means the claim may be accepted.
func (e *Engine) blockedReasons(run *Run, args FinishArgs) []string {
	if existing := e.blockedPathsExist(run, args.Reason); existing != "" {
		return []string{"blocked claim refused: path " + existing + " exists. Re-read the filesystem result and continue working instead of declaring blocked."}
	}
	if refusal := e.strictBlockedRefusal(run, args.Reason); refusal != "" {
		// Count strict refusals separately for the trial records; the
		// generic FinishRefusals counter is bumped by the caller.
		run.mu.Lock()
		run.StrictBlockedRefusals++
		run.mu.Unlock()
		return []string{refusal}
	}
	return nil
}
