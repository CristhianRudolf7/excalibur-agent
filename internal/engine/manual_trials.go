package engine

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

const manualTrialsSwitch = "EXCALIBUR_MANUAL_TRIALS"
const manualTrialsNote = "repeated manual trials: write a small automated search script to try candidates with the checker and apply the best result"

type manualTrialState struct {
	command         string
	count, edits    int
	invalid, hinted bool
	best            checkMeasurement
}

// Compare complete bounded file contents, not tool names: a claimed edit that
// did not change a file cannot establish a manual trial. A small edit changes
// at most 1024 bytes after removing the common prefix and suffix.
func smallTrialEdit(before, after trialFiles) (changed bool, small bool) {
	paths := map[string]bool{}
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	count := 0
	for path := range paths {
		a, existed := before[path]
		b, exists := after[path]
		if existed && exists && a == b {
			continue
		}
		count++
		if !existed || !exists || count > 1 {
			return true, false
		}
		start := 0
		for start < len(a) && start < len(b) && a[start] == b[start] {
			start++
		}
		tail := 0
		for tail < len(a)-start && tail < len(b)-start && a[len(a)-1-tail] == b[len(b)-1-tail] {
			tail++
		}
		if len(a)+len(b)-2*(start+tail) > 1024 {
			return true, false
		}
	}
	return count == 1, count == 1
}

func comparableTrialChecks(a, b checkMeasurement) bool {
	if len(a.counts) == 0 && len(b.counts) == 0 {
		return a.fingerprint == b.fingerprint
	}
	if len(a.counts) != len(b.counts) {
		return false
	}
	for key := range a.counts {
		if _, ok := b.counts[key]; !ok {
			return false
		}
	}
	return true
}

// This advisory detector shares result normalization and executable-check
// classification with existing loop/evidence rules. It deliberately does not
// escalate, refuse calls, or change the run's completion/budget state.
func (e *Engine) observeManualTrials(r *Run, calls []providers.ToolCall, results []tools.Result, before trialObservation) {
	if !r.trialAdviceEnabled(manualTrialsSwitch) || len(calls) == 0 {
		return
	}
	r.mu.Lock()
	dir := r.Workdir
	r.mu.Unlock()
	var paths []string
	for _, tc := range calls {
		paths = append(paths, trialOperands(tc, dir)...)
	}
	after := canonicalTrialFiles(paths)
	changed, small := smallTrialEdit(before.files, after)
	r.mu.Lock()
	defer r.mu.Unlock()
	state := &r.manualTrials
	if state.hinted {
		return
	}
	if changed {
		if small {
			state.edits++
		} else {
			state.invalid = true
		}
	}
	for i, tc := range calls {
		command, check := trialCheck(tc, results[i])
		if changed && tc.Name == "exec" && len(tasks.SplitShellSegments(execCommand(string(tc.Arguments)))) < 2 {
			check = false
		}
		if !check {
			// Unknown writes cannot stand in for an observed small edit. Read-only
			// inspection between the edit and check leaves the trial intact.
			writes := deliverableWriteTools[tc.Name] || tc.Name == "apply_patch" || (tc.Name == "exec" && tasks.MayWriteCommand(execCommand(string(tc.Arguments))))
			if writes && !changed {
				state.invalid = true
			}
			continue
		}
		measure := measureTrialCheck(tc, results[i])
		reset := state.command != command || state.edits != 1 || state.invalid || measure.passed || measure.improves(state.best) || !comparableTrialChecks(measure, state.best)
		if reset {
			state.command, state.count, state.best = command, 1, measure
		} else {
			state.count++
		}
		state.edits, state.invalid = 0, false
		if state.count >= 5 && !measure.passed {
			state.hinted = true
			r.controlNoteLocked("loop.manual_trials", manualTrialsNote)
			return
		}
	}
}
