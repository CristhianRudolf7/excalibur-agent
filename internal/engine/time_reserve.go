package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TimeReserveExplicitOnlySwitch keeps assumed exec/eval budgets advisory.
const TimeReserveExplicitOnlySwitch = "EXCALIBUR_TIME_RESERVE_EXPLICIT_ONLY"

func TimeReserveExplicitOnlyEnabled() bool { return switchOn(TimeReserveExplicitOnlySwitch) }

// HardWallSeconds preserves the guidance estimate but gives an unknown outer
// deadline four times that estimate as an internal safety bound.
func HardWallSeconds(seconds int64, source string) int64 {
	if source == "default" && TimeReserveExplicitOnlyEnabled() {
		return seconds * 4
	}
	return seconds
}

// Callers hold run.mu. Other engine clients retain their configured limits.
func (run *Run) assumedWallLocked() bool {
	return run.Profile.WallSource == "default" && TimeReserveExplicitOnlyEnabled()
}

func (e *Engine) reserveMayAccept(run *Run, now time.Time, percent int) bool {
	run.mu.Lock()
	assumed := run.assumedWallLocked()
	run.mu.Unlock()
	return !assumed && e.inTimeReserve(run, now, percent)
}

// A live build/test is a completion precondition, separate from the bounded
// refusal checklist. Repeating finish cannot turn a running job into evidence.
// Only an explicit reserve may override it; services keep their finish policy.
func (e *Engine) pendingCommandReason(run *Run, now time.Time) string {
	if !TimeReserveExplicitOnlyEnabled() || !e.explicitFinishRequired(run) || e.reserveMayAccept(run, now, 85) {
		return ""
	}
	history := e.currentSessionHistory(workHistory(run))
	live := map[string]bool{}
	for _, id := range tasks.LiveSessions(history) {
		live[id] = true
	}
	if e.deps.ActiveSessions != nil {
		for _, s := range e.deps.ActiveSessions() {
			if !live[string(s.ID)] || !s.Alive {
				continue
			}
			if s.Build || buildOrTestCommand(s.Command) {
				return "finish refused: session " + string(s.ID) + " is still running a build or test; wait for it to exit with exec_io before calling finish"
			}
		}
	}
	for id := range live {
		if buildOrTestCommand(sessionStartCommand(history, id)) {
			return "finish refused: session " + id + " is still running a build or test; wait for it to exit with exec_io before calling finish"
		}
	}
	return ""
}

func buildOrTestCommand(command string) bool {
	for _, words := range commandWords(command) {
		switch words[0] {
		case "apt-get", "apt", "dpkg", "pip", "pip3", "conda", "gem",
			"make", "cmake", "ninja", "gcc", "g++", "clang", "clang++", "cc", "rustc", "javac",
			"pytest", "pytest-3", "ctest", "bats", "jest", "vitest", "mocha":
			return true
		case "go", "dotnet", "bun", "deno", "npm", "yarn", "pnpm", "cargo", "mvn", "gradle":
			for _, word := range words[1:] {
				if word == "test" || word == "build" || word == "install" || word == "check" || word == "compile" || word == "ci" {
					return true
				}
			}
		case "python", "python3":
			for i, word := range words[1:] {
				if word == "-m" && i+2 < len(words) && (words[i+2] == "pytest" || words[i+2] == "unittest") {
					return true
				}
			}
		}
	}
	return false
}

const timeReserveNote = "time reserve: finish now with what works; refusals are advisory from here"

// Only autonomous explicit exec/eval finishes spend a delivery reserve.
// In particular, this uses wall time even when all step budgets are disabled.
func (e *Engine) inTimeReserve(run *Run, now time.Time, percent int) bool {
	if !switchOn("EXCALIBUR_FINISH_TIME_RESERVE") || !e.explicitFinishRequired(run) {
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	total := time.Duration(run.Tracker.Limit.MaxSeconds) * time.Second
	return total > 0 && !run.StartedAt.IsZero() && now.Sub(run.StartedAt) >= total*time.Duration(percent)/100
}

func (e *Engine) announceTimeReserve(run *Run, now time.Time) {
	if !e.inTimeReserve(run, now, 70) {
		return
	}
	run.mu.Lock()
	noted := run.fin.reserveNoted
	run.fin.reserveNoted = true
	run.mu.Unlock()
	if !noted {
		run.mu.Lock()
		assumed := run.assumedWallLocked()
		left := run.Tracker.Limit.MaxSeconds - int64(now.Sub(run.StartedAt)/time.Second)
		run.mu.Unlock()
		if assumed {
			if left < 0 {
				left = 0
			}
			run.InjectNote(fmt.Sprintf("time reserve estimate: the time limit is not known; assume about %d s are left; wait for running builds and tests before finishing", left))
		} else {
			run.InjectNote(timeReserveNote)
		}
	}
}

func (e *Engine) completeReservedFinish(ctx context.Context, run *Run, args FinishArgs, callIDs ...string) error {
	callID := ""
	if len(callIDs) > 0 {
		callID = callIDs[0]
	}
	run.auditFinish(e.deps.Clock(), callID, "time_reserve", "reserve_acceptance", "accepted finish at wall reserve")
	run.storeFinish(args, "")
	run.clearRefusal()
	if args.Status == "blocked" {
		return e.completeBlocked(ctx, run, args)
	}
	run.mu.Lock()
	from := run.state
	err := run.transition(types.RunVerifying)
	run.mu.Unlock()
	if err != nil {
		return err
	}
	if pending := run.deliveryPendingReason(); pending != "" {
		return e.completeWithPending(ctx, run, []string{pending})
	}
	const reason = "time reserve: accepted finish at 85% of wall budget"
	e.recordState(ctx, run, from, types.RunVerifying, reason)
	return e.complete(ctx, run, types.RunCompleted, reason)
}

func (run *Run) wallStatus(elapsed time.Duration) tasks.Status {
	run.mu.Lock()
	defer run.mu.Unlock()
	limit := run.Tracker.Limit
	if run.assumedWallLocked() {
		limit.MaxSeconds = HardWallSeconds(limit.MaxSeconds, "default")
	}
	return timeOnlyStatus(limit, elapsed)
}
