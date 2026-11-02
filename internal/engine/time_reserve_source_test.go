package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestReserveBudgetSource(t *testing.T) {
	for _, source := range []string{"default", "explicit"} {
		for _, enabled := range []string{"1", "0"} {
			t.Run(source+enabled, func(t *testing.T) {
				t.Setenv(TimeReserveExplicitOnlySwitch, enabled)
				e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
				r := explicitRun()
				r.Workdir = t.TempDir()
				r.Profile.WallSource = source
				r.Tracker.Limit.MaxSeconds = 100
				startRun(t, e, r, js)
				r.armConfirm(FinishArgs{Status: "completed", Summary: "done", Evidence: "checked"})
				for _, percent := range []int{85, 96} {
					done, err := e.completePendingConfirmation(finishTestContext(t), r, r.StartedAt.Add(time.Duration(percent)*time.Second))
					want := source == "explicit" || enabled == "0"
					if err != nil || done != want {
						t.Fatalf("%d%% done=%v want=%v err=%v", percent, done, want, err)
					}
					if done {
						break
					}
				}
			})
		}
	}
}

func TestAssumedReserveNoticeAndTwoStrikeCap(t *testing.T) {
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	r.Profile.WallSource = "default"
	r.Tracker.Limit.MaxSeconds = 100
	startRun(t, e, r, js)
	e.deps.Clock = func() time.Time { return r.StartedAt.Add(70 * time.Second) }
	reasons := []string{"required artifact missing: /app/result.txt"}
	for i := 0; i < 3; i++ {
		hard := e.hardFinishReasons(r, reasons)
		if (len(hard) > 0) != (i < 2) {
			t.Fatalf("attempt %d: %v", i, hard)
		}
		e.countBlockingReasons(r, reasons)
	}
	if countContaining(r.VolatileNotes(), "time limit is not known; assume about 30 s are left") != 1 {
		t.Fatal(r.VolatileNotes())
	}
}

func TestRunningBuildAndTestCannotFinish(t *testing.T) {
	for _, source := range []string{"default", "explicit"} {
		for _, percent := range []int{80, 86, 96} {
			for _, command := range []string{"make -j2", "go test ./...", "python3 -m pytest -q"} {
				t.Run(fmt.Sprintf("%s-%d-%s", source, percent, command), func(t *testing.T) {
					runner := scriptedRunner()
					runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: "exit: running  session: sess-1  alive: true"})
					e, js := testEngine(&fakeEndpoint{}, runner, passingGate())
					e.deps.ActiveSessions = func() []tools.SessionInfo { return []tools.SessionInfo{{ID: "sess-1", Alive: true, Command: command}} }
					r := explicitRun()
					r.Profile.WallSource = source
					r.Tracker.Limit.MaxSeconds = 100
					startRun(t, e, r, js)
					args := fmt.Sprintf(`{"command":%q}`, command)
					if _, err := e.execBatch(finishTestContext(t), r, []providers.ToolCall{toolCall("build", "exec", args)}); err != nil {
						t.Fatal(err)
					}
					e.deps.Clock = func() time.Time { return r.StartedAt.Add(time.Duration(percent) * time.Second) }
					for i := 0; i < 4; i++ {
						done, err := e.execFinishBatch(finishTestContext(t), r, toolCall(fmt.Sprint(i), "finish", finishArgs("done", "checked")), "")
						want := source == "explicit" && percent >= 85
						if err != nil || done != want {
							t.Fatalf("attempt %d done=%v want=%v err=%v", i, done, want, err)
						}
						if done {
							break
						}
					}
					if source == "default" && !strings.Contains(e.pendingCommandReason(r, e.deps.Clock()), "still running") {
						t.Fatal("missing live-work precondition")
					}
				})
			}
		}
	}
}

func TestAssumedWallDoesNotPauseRunningCommand(t *testing.T) {
	for _, timeOnly := range []bool{true, false} {
		t.Run(fmt.Sprint(timeOnly), func(t *testing.T) {
			runner := scriptedRunner()
			runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: "exit: running  session: sess-1  alive: true"})
			endpoint := &fakeEndpoint{responses: []providers.ChatResponse{toolResp(toolCall("build", "exec", `{"command":"make"}`)), toolResp(toolCall("poll", "exec_io", `{"session_id":"sess-1","action":"read"}`))}}
			runner.add("exec_io", types.EffectReadOnly, types.IdemPure, tools.Result{Status: tools.StatusOK, Output: "building\nsession: sess-1 alive: true"})
			e, js := testEngine(endpoint, runner, passingGate())
			e.deps.ActiveSessions = func() []tools.SessionInfo { return []tools.SessionInfo{{ID: "sess-1", Alive: true, Build: true}} }
			r := explicitRun()
			r.Profile.WallSource = "default"
			r.Profile.TimeBudgetOnly = timeOnly
			r.Tracker.Limit = tasks.Limit{MaxSeconds: 1, MaxSteps: 20}
			startRun(t, e, r, js)
			if stop, err := e.step(finishTestContext(t), r); stop || err != nil {
				t.Fatalf("start: %v %v", stop, err)
			}
			e.deps.Clock = func() time.Time { return r.StartedAt.Add(1500 * time.Millisecond) }
			if stop, err := e.step(finishTestContext(t), r); stop || err != nil {
				t.Fatalf("estimate stopped command: %v %v", stop, err)
			}
			mustState(t, r, types.RunRunning)
			if endpoint.calls() != 2 {
				t.Fatal("no polling turn after estimate")
			}
			e.deps.Clock = func() time.Time { return r.StartedAt.Add(4 * time.Second) }
			if stop, err := e.step(finishTestContext(t), r); !stop || err != nil {
				t.Fatalf("safety cap: %v %v", stop, err)
			}
			mustState(t, r, types.RunPaused)
		})
	}
}

func TestAssumedWallGuidanceIsAnEstimate(t *testing.T) {
	r := explicitRun()
	r.Profile.TimeBudgetOnly = true
	r.Profile.WallSource = "default"
	r.Tracker.Limit.MaxSeconds = 100
	r.StartedAt = time.Unix(100, 0)
	now := r.StartedAt.Add(96 * time.Second)
	if text := r.timeFooter(now); !strings.Contains(text, "time limit unknown") || strings.Contains(text, "finish now") {
		t.Fatal(text)
	}
	if text := r.BudgetLine(now); !strings.Contains(text, "estimated time (limit unknown)") {
		t.Fatal(text)
	}
	r.Profile.TimeBudgetOnly = false
	r.DeadlinePacing(now)
	for _, note := range r.VolatileNotes() {
		if strings.Contains(note, "Task deadline") {
			t.Fatal(note)
		}
	}
}

func TestRunningCommandGuardUsesProcessFactsAndReleasesExitedSessions(t *testing.T) {
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: "exit: running  session: sess-1  alive: true"})
	e, js := testEngine(&fakeEndpoint{}, runner, passingGate())
	alive := true
	e.deps.ActiveSessions = func() []tools.SessionInfo {
		return []tools.SessionInfo{{ID: "sess-1", Alive: alive, Build: alive, Command: "sh ./compile.sh"}}
	}
	r := explicitRun()
	r.Profile.WallSource = "default"
	startRun(t, e, r, js)
	if _, err := e.execBatch(finishTestContext(t), r, []providers.ToolCall{toolCall("build", "exec", `{"command":"sh ./compile.sh","background":true}`)}); err != nil {
		t.Fatal(err)
	}
	if e.pendingCommandReason(r, e.deps.Clock()) == "" {
		t.Fatal("process-tree build missed")
	}
	alive = false
	if reason := e.pendingCommandReason(r, e.deps.Clock()); reason != "" {
		t.Fatal(reason)
	}
	// An unrelated service session must never prevent this run from finishing.
	e.deps.ActiveSessions = func() []tools.SessionInfo { return []tools.SessionInfo{{ID: "sess-other", Alive: true, Build: true}} }
	if reason := e.pendingCommandReason(r, e.deps.Clock()); reason != "" {
		t.Fatal(reason)
	}
}

func TestRunningCommandGuardKeepsServiceLaunchers(t *testing.T) {
	for _, command := range []string{"npm start", "npm run dev", "yarn serve", "cargo run", "python3 service.py", "echo 'pytest'"} {
		if buildOrTestCommand(command) {
			t.Errorf("service or display command classified as finite build/test: %q", command)
		}
	}
	for _, command := range []string{"npm test", "npm run build", "cargo test", "go test ./...", "make", "mvn compile", "gradle test"} {
		if !buildOrTestCommand(command) {
			t.Errorf("build/test missed: %q", command)
		}
	}
}

func TestAssumedWallRetainsSoftRefusalCapPastEstimate(t *testing.T) {
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), GateFunc(func(*Run) tasks.GateReport {
		return tasks.GateReport{Pendings: []string{"independent verifier: another check is needed"}}
	}))
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	r := explicitRun()
	r.Profile.WallSource = "default"
	r.Tracker.Limit.MaxSeconds = 100
	startRun(t, e, r, js)
	e.deps.Clock = func() time.Time { return r.StartedAt.Add(101 * time.Second) }
	r.HeartbeatAt = e.deps.Clock()
	if !e.timeRemains(r) {
		t.Fatal("estimate treated as binding deadline")
	}
	if e.safetyCapExhausted(r, 101*time.Second, false) {
		t.Fatal("estimate treated as exhausted safety cap")
	}
	args := FinishArgs{Status: "completed", Summary: "done", Evidence: "checked"}
	for i := 0; i < 3; i++ {
		reason := e.validateFinish(finishTestContext(t), r, args, args.Evidence)
		if (reason != "") != (i < 2) {
			t.Fatalf("attempt %d: %s", i, reason)
		}
		e.countBlockingReasons(r, []string{"independent verifier: another check is needed"})
	}
}
