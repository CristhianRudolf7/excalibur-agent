package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// meteredEndpoint serves scripted responses and reports a fixed cost and
// input-token usage per call. onCall runs before each call, which lets a
// test advance a fake clock.
type meteredEndpoint struct {
	responses []providers.ChatResponse
	cost      float64
	input     int64
	onCall    func()
	calls     int
}

func (m *meteredEndpoint) Chat(_ context.Context, _ types.ModelRole, _ providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	m.calls++
	if m.onCall != nil {
		m.onCall()
	}
	resp := finalResp("done")
	if len(m.responses) > 0 {
		resp = m.responses[0]
		m.responses = m.responses[1:]
	}
	resp.Usage.Input = m.input
	return resp, models.CallMeta{Provider: "fake", Model: "m", CostUSD: m.cost}, nil
}

// repeatCalls returns n responses, each one call of tool with args.
func repeatCalls(n int, tool, args string) []providers.ChatResponse {
	out := make([]providers.ChatResponse, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, toolResp(toolCall(fmt.Sprintf("c%d", i), tool, args)))
	}
	return out
}

// constantRunner answers every call of tool with the same result, so the
// observation repeats exactly.
func constantRunner(tool string, effect types.EffectClass, idem types.IdempotencyClass, out string) *fakeRunner {
	r := scriptedRunner()
	results := make([]tools.Result, 32)
	for i := range results {
		results[i] = tools.Result{Status: tools.StatusOK, Output: out}
	}
	r.add(tool, effect, idem, results...)
	return r
}

// historyContents returns the content of every tool result in history.
func historyContents(run *Run) []string {
	run.mu.Lock()
	defer run.mu.Unlock()
	var out []string
	for _, ev := range run.History {
		if ev.ToolResult != nil {
			out = append(out, ev.Content)
		}
	}
	return out
}

func countContaining(items []string, sub string) int {
	n := 0
	for _, it := range items {
		if strings.Contains(it, sub) {
			n++
		}
	}
	return n
}

// Item 15: the sixth identical call opens a five-turn refusal of the exact
// call. The refused calls never run, the run goes on, and the loop never
// blocks it (backlog 99).
func TestExactCallWindowRefusesThenResumes(t *testing.T) {
	ctx := context.Background()
	responses := repeatCalls(14, "read_file", `{"path":"a.txt"}`)
	model := &fakeEndpoint{responses: append(responses, finalResp("done"))}
	runner := constantRunner("read_file", types.EffectReadOnly, types.IdemPure, "contents")
	e, js := testEngine(model, runner, passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	// Calls 1-6 run, 7-11 are refused, 12-14 run again after the window.
	if got := runner.ncalls(); got != 9 {
		t.Fatalf("executions = %d, want 9", got)
	}
	history := historyContents(run)
	if got := countContaining(history, "refused: you ran this exact call 6 times"); got != 5 {
		t.Fatalf("refused results = %d, want 5", got)
	}
	notes := run.VolatileNotes()
	if countContaining(notes, tasks.ExactCallNote(3)) != 2 {
		t.Fatalf("the third occurrence must note twice (before the window and after it): %v", notes)
	}
}

// Item 106: a session that is still running is progress, never a loop.
func TestLiveSessionOutputIsNotCounted(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: append(repeatCalls(8, "exec", `{"command":"make -j2"}`), finalResp("done"))}
	live := "exit: running  duration: 12.0s  session: sess-3  alive: true  next_offset: 40\n"
	runner := constantRunner("exec", types.EffectExec, types.IdemNonIdempotent, live)
	e, js := testEngine(model, runner, passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if got := runner.ncalls(); got != 8 {
		t.Fatalf("live session polls must all run, executions = %d", got)
	}
	if countContaining(historyContents(run), "refused:") != 0 || countContaining(run.VolatileNotes(), "you ran this exact call") != 0 {
		t.Fatalf("live sessions must never be counted: %v", run.VolatileNotes())
	}
	if len(run.Loop.Counts()) != 0 {
		t.Fatalf("running sessions triggered legacy loop rules: %v", run.Loop.Counts())
	}
	// Control: the same polls once the session has finished are counted.
	done := "exit: 0  duration: 12.0s  session: sess-3  alive: false\n"
	model2 := &fakeEndpoint{responses: append(repeatCalls(3, "exec", `{"command":"make -j2"}`), finalResp("done"))}
	e2, js2 := testEngine(model2, constantRunner("exec", types.EffectExec, types.IdemNonIdempotent, done), passingGate())
	run2 := testRun(Profile{})
	startRun(t, e2, run2, js2)
	if err := e2.Run(ctx, run2); err != nil {
		t.Fatal(err)
	}
	if countContaining(run2.VolatileNotes(), tasks.ExactCallNote(3)) != 1 {
		t.Fatalf("a finished session repeated three times must note: %v", run2.VolatileNotes())
	}
}

// Item 15: a workspace change restarts the count. The control run without
// the write shows the same reads do note at the third occurrence.
func TestStateChangeRestartsExactCount(t *testing.T) {
	ctx := context.Background()
	reads := repeatCalls(2, "read_file", `{"path":"a.txt"}`)
	write := toolResp(toolCall("w", "write_file", `{"path":"a.txt","content":"new"}`))
	responses := append(append(append([]providers.ChatResponse{}, reads...), write), repeatCalls(2, "read_file", `{"path":"a.txt"}`)...)
	model := &fakeEndpoint{responses: append(responses, finalResp("done"))}
	runner := constantRunner("read_file", types.EffectReadOnly, types.IdemPure, "old")
	runner.add("write_file", types.EffectWriteLocal, types.IdemIdempotent, tools.Result{Status: tools.StatusOK, Output: "written"})
	e, js := testEngine(model, runner, passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if countContaining(run.VolatileNotes(), "you ran this exact call") != 0 {
		t.Fatalf("a write between identical reads must restart the count: %v", run.VolatileNotes())
	}

	// Control: the same reads with no write note at the third occurrence.
	control := &fakeEndpoint{responses: append(repeatCalls(3, "read_file", `{"path":"a.txt"}`), finalResp("done"))}
	runner2 := constantRunner("read_file", types.EffectReadOnly, types.IdemPure, "old")
	e2, js2 := testEngine(control, runner2, passingGate())
	run2 := testRun(Profile{})
	startRun(t, e2, run2, js2)
	if err := e2.Run(ctx, run2); err != nil {
		t.Fatal(err)
	}
	if countContaining(run2.VolatileNotes(), tasks.ExactCallNote(3)) != 1 {
		t.Fatalf("control run must note the third identical read: %v", run2.VolatileNotes())
	}
}

// Pure echo, printf, true and sleep commands count by their output: the
// same sleep with a different duration header is the same observation.
func TestPureCommandsCountByOutput(t *testing.T) {
	ctx := context.Background()
	responses := []providers.ChatResponse{
		toolResp(toolCall("a", "exec", `{"command":"sleep 30"}`)),
		toolResp(toolCall("b", "exec", `{"command":"sleep 31"}`)),
		toolResp(toolCall("c", "exec", `{"command":"echo hi"}`)),
		finalResp("done"),
	}
	model := &fakeEndpoint{responses: responses}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 30.0s  session: sess-1  alive: false\n"},
		tools.Result{Status: tools.StatusOK, Output: "exit: 1  duration: 31.2s  session: sess-2  alive: false\n"},
		tools.Result{Status: tools.StatusOK, Output: "exit: 2  duration: 0.0s  session: sess-3  alive: false\n"},
	)
	e, js := testEngine(model, runner, passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if countContaining(run.VolatileNotes(), tasks.ExactCallNote(3)) != 0 {
		t.Fatal("three calls with different outputs must not note")
	}
	// Same output after normalization: the third pure call notes.
	responses2 := []providers.ChatResponse{
		toolResp(toolCall("a", "exec", `{"command":"sleep 30"}`)),
		toolResp(toolCall("b", "exec", `{"command":"sleep 31"}`)),
		toolResp(toolCall("c", "exec", `{"command":"true"}`)),
		finalResp("done"),
	}
	model2 := &fakeEndpoint{responses: responses2}
	// The same exit with different durations and sessions is one
	// observation: pure commands with a constant output count as identical.
	runner2 := scriptedRunner()
	runner2.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 30.0s  session: sess-1  alive: false\n"},
		tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 31.2s  session: sess-2  alive: false\n"},
		tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 9.9s  session: sess-9  alive: false\n"},
	)
	e2, js2 := testEngine(model2, runner2, passingGate())
	run2 := testRun(Profile{})
	startRun(t, e2, run2, js2)
	if err := e2.Run(ctx, run2); err != nil {
		t.Fatal(err)
	}
	if countContaining(run2.VolatileNotes(), tasks.ExactCallNote(3)) != 1 {
		t.Fatalf("pure commands with a constant output must count as identical: %v", run2.VolatileNotes())
	}
}

// Finish repeats count toward the loop key: an unchanged refusal of
// finish notes at its third occurrence, and the run ends at its step cap.
func TestFinishRepeatsCountTowardLoopKey(t *testing.T) {
	// Exercise the legacy repeat-loop policy independently of the refusal cap.
	t.Setenv("EXCALIBUR_FINISH_REFUSAL_CAP", "0")
	ctx := context.Background()
	finish := func(id string) providers.ChatResponse {
		return toolResp(toolCall(id, "finish", `{"status":"completed","summary":"done","evidence":"ran it"}`))
	}
	model := &fakeEndpoint{responses: []providers.ChatResponse{finish("f1"), finish("f2"), finish("f3"), finish("f4")}}
	gate := GateFunc(func(*Run) tasks.GateReport {
		return tasks.GateReport{OK: false, Pendings: []string{"the output file is missing"}}
	})
	e, js := testEngine(model, scriptedRunner(), gate)
	run := testRun(Profile{ExplicitFinish: true, Limit: tasks.Limit{MaxSteps: 4}})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if run.FinishRefusals < 3 {
		t.Fatalf("finish refusals = %d, want at least 3", run.FinishRefusals)
	}
	if countContaining(run.VolatileNotes(), tasks.ExactCallNote(3)) != 1 {
		t.Fatalf("repeated finish refusals must note at the third: %v", run.VolatileNotes())
	}
}

// meteredRun returns a time-only run with a wall budget and the given
// caps, and the endpoint serving n tool calls of one cost or input.
func meteredRun(t *testing.T, limit tasks.Limit, n int, cost float64, input int64) (*Engine, *Run, *meteredEndpoint, *fakeRunner) {
	t.Helper()
	responses := repeatCalls(n, "read_file", `{"path":"a.txt"}`)
	model := &meteredEndpoint{responses: responses, cost: cost, input: input}
	runner := scriptedRunner()
	runner.add("read_file", types.EffectReadOnly, types.IdemPure, tools.Result{Status: tools.StatusOK, Output: "contents"})
	js := journal.NewMemStore()
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: runner, Gate: passingGate()})
	run := NewRun("run_test", "task_test", Profile{Role: types.RoleExecutor, TimeBudgetOnly: true, Limit: limit})
	startRun(t, e, run, js)
	return e, run, model, runner
}

// Item 55 and 90: at the closing share of the cost cap the agent is asked
// to finish; at the stop share the run pauses with the budget motive.
func TestCostCapClosingNoteThenPausedStop(t *testing.T) {
	t.Setenv(SpendCapSwitch, "")
	ctx := context.Background()
	e, run, model, _ := meteredRun(t, tasks.Limit{MaxCostUSD: 1.0}, 6, 0.5, 0)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunPaused)
	if run.pauseReason != types.PauseBudget {
		t.Fatalf("pause reason = %s, want budget", run.pauseReason)
	}
	// Step 3 starts at exactly 100 percent; step 4 starts at 150 percent
	// and stops before another model call.
	if model.calls != 3 {
		t.Fatalf("model calls = %d, want 3", model.calls)
	}
	notes := run.VolatileNotes()
	if countContaining(notes, SpendCloseNote) != 1 {
		t.Fatalf("closing note must fire once: %v", notes)
	}
	if countContaining(notes, SafetyCapNote) != 0 {
		t.Fatalf("the cost cap is a spend cap, not a safety cap: %v", notes)
	}
	if countContaining(notes, "budget exhausted: cost") != 1 {
		t.Fatalf("the paused report must name the cost cap: %v", notes)
	}
}

// Input-token cap: the same two thresholds, measured in input tokens.
func TestInputTokenCapClosingNoteThenPausedStop(t *testing.T) {
	t.Setenv(SpendCapSwitch, "")
	t.Setenv(InputTokenCapSwitch, "1000")
	ctx := context.Background()
	e, run, model, _ := meteredRun(t, tasks.Limit{}, 6, 0, 600)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunPaused)
	if model.calls != 3 {
		t.Fatalf("model calls = %d, want 3 (1200 tokens closes, 1800 stops)", model.calls)
	}
	if countContaining(run.VolatileNotes(), "budget exhausted: input_tokens") != 1 {
		t.Fatalf("the paused report must name the token cap: %v", run.VolatileNotes())
	}
}

// EXCALIBUR_SPEND_CAP=0 leaves only the wall clock binding.
func TestSpendCapSwitchOffNeverStops(t *testing.T) {
	t.Setenv(SpendCapSwitch, "0")
	t.Setenv(InputTokenCapSwitch, "1000")
	ctx := context.Background()
	e, run, _, _ := meteredRun(t, tasks.Limit{MaxCostUSD: 1.0}, 6, 0.5, 600)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if countContaining(run.VolatileNotes(), SpendCloseNote) != 0 {
		t.Fatal("the closing note must stay silent with the spend caps off")
	}
}

// fakeClockEngine returns an engine whose clock is now, and a model that
// advances the clock by step before every model call.
func fakeClockEngine(t *testing.T, base time.Time, step time.Duration, responses []providers.ChatResponse) (*Engine, *Run, *journal.MemStore) {
	t.Helper()
	now := base
	model := &meteredEndpoint{responses: responses, onCall: func() { now = now.Add(step) }}
	runner := constantRunner("read_file", types.EffectReadOnly, types.IdemPure, "contents")
	js := journal.NewMemStore()
	clock := func() time.Time { return now }
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: runner, Gate: passingGate(), Clock: clock})
	run := NewRun("run_test", "task_test", Profile{Role: types.RoleExecutor, TimeBudgetOnly: true, Limit: tasks.Limit{MaxSeconds: 900}})
	run.StartedAt = base
	startRun(t, e, run, js)
	return e, run, js
}

var footerPattern = regexp.MustCompile(`\[time: [0-9hms]+ used, about [0-9hms]+ left\]`)

// Item 14: every tool result of a time-only run ends with the footer, and
// the pacing advice rides on the result that crosses each threshold.
func TestTimeFooterOnEveryToolResult(t *testing.T) {
	t.Setenv(TimeFooterSwitch, "")
	ctx := context.Background()
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	responses := append(repeatCalls(5, "read_file", `{"path":"a.txt"}`), finalResp("done"))
	e, run, _ := fakeClockEngine(t, base, 200*time.Second, responses)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	history := historyContents(run)
	if len(history) != 5 {
		t.Fatalf("tool results = %d", len(history))
	}
	for i, content := range history {
		lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
		last := lines[len(lines)-1]
		if !footerPattern.MatchString(last) {
			t.Fatalf("result %d must end with the footer: %q", i+1, content)
		}
	}
	// Elapsed time at tool result k is 200k seconds.
	if !strings.HasSuffix(history[0], "[time: 3m20s used, about 11m40s left]") {
		t.Fatalf("first footer = %q", history[0])
	}
	// Thresholds: 50% (450s) on result 3, 75% (675s) on result 4, 90% (810s)
	// on result 5. Each advice appears exactly once.
	for i, want := range map[int]string{2: "Pacing 50%", 3: "Pacing 75%", 4: "Pacing 90%"} {
		if !strings.Contains(history[i], want) {
			t.Fatalf("result %d must carry %q: %q", i+1, want, history[i])
		}
	}
	if countContaining(history, "Pacing ") != 3 {
		t.Fatalf("pacing advice must appear three times: %v", history)
	}
	if countContaining(run.VolatileNotes(), "Task deadline") != 0 {
		t.Fatal("the footer carries the pacing; the volatile note must stay silent")
	}
}

// Runs outside time-only mode keep their tool results unchanged.
func TestTimeFooterAbsentForLegacyRun(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{toolResp(toolCall("c", "read_file", `{"path":"a.txt"}`)), finalResp("done")}}
	runner := constantRunner("read_file", types.EffectReadOnly, types.IdemPure, "contents")
	e, js := testEngine(model, runner, passingGate())
	run := testRun(Profile{Limit: tasks.Limit{MaxSeconds: 900}})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	for _, content := range historyContents(run) {
		if strings.Contains(content, "[time:") {
			t.Fatalf("legacy run must not carry the footer: %q", content)
		}
	}
}

// The footer never changes what the loop guard observes: the same output
// with a different footer is the same result.
func TestFooterDoesNotChangeTheLoopKey(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	responses := append(repeatCalls(3, "read_file", `{"path":"a.txt"}`), finalResp("done"))
	e, run, _ := fakeClockEngine(t, base, 300*time.Second, responses)
	if err := e.Run(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if countContaining(run.VolatileNotes(), tasks.ExactCallNote(3)) != 1 {
		t.Fatalf("identical outputs under different footers must still count: %v", run.VolatileNotes())
	}
}

// Switching the loop guard off restores the legacy blocker: the second
// identical call is rejected once and the guard adds no notes.
func TestLoopGuardSwitchOffRestoresLegacyBlocker(t *testing.T) {
	t.Setenv(LoopGuardV2Switch, "0")
	ctx := context.Background()
	model := &fakeEndpoint{responses: append(repeatCalls(3, "read_file", `{"path":"a.txt"}`), finalResp("done"))}
	runner := scriptedRunner()
	runner.add("read_file", types.EffectReadOnly, types.IdemPure, tools.Result{Status: tools.StatusOK, Output: "contents"})
	e, js := testEngine(model, runner, passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if countContaining(historyContents(run), "blocked: exact repeat") != 1 {
		t.Fatal("the legacy blocker must reject the second identical call")
	}
	if countContaining(run.VolatileNotes(), "you ran this exact call") != 0 {
		t.Fatal("the guard must stay silent with the switch off")
	}
}

func TestFinishGuardUsesArgumentsAndBoundedWindow(t *testing.T) {
	r := testRun(Profile{})
	tc := toolCall("f", "finish", `{"status":"completed","summary":"done"}`)
	for i := 1; i <= 6; i++ {
		r.Steps = i
		note := r.observeFinish(tc, "missing evidence", true)
		if i == 3 && !strings.Contains(note, tasks.ExactCallNote(3)) {
			t.Fatalf("third finish: %q", note)
		}
	}
	for i := 7; i <= 11; i++ {
		r.Steps = i
		if _, refused := r.guardRefusal(tc); !refused {
			t.Fatalf("turn %d must refuse", i)
		}
	}
	other := toolCall("other", "finish", `{"status":"completed","summary":"verified"}`)
	if _, refused := r.guardRefusal(other); refused {
		t.Fatal("different arguments refused")
	}
	r.Steps = 12
	if _, refused := r.guardRefusal(tc); refused {
		t.Fatal("window did not expire")
	}
	if note := r.observeFinish(tc, "missing evidence", false); note != "" {
		t.Fatal(note)
	}
}

func TestCostPrimarySpendDoesNotPausePricedInput(t *testing.T) {
	t.Setenv("EXCALIBUR_COST_PRIMARY_SPEND", "on")
	t.Setenv(InputTokenCapSwitch, "1000")
	e, run, _, _ := meteredRun(t, tasks.Limit{MaxCostUSD: 100}, 6, 0.01, 600)
	if err := e.Run(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if countContaining(run.VolatileNotes(), "budget exhausted: input_tokens") != 0 {
		t.Fatal(run.VolatileNotes())
	}
}

func TestInputTokenFallbackForUnknownCostAndUnavailableCap(t *testing.T) {
	t.Setenv("EXCALIBUR_COST_PRIMARY_SPEND", "on")
	for _, tc := range []struct {
		name      string
		cost, cap float64
		unpriced  bool
		want      int
	}{
		{"priced", 1, 100, false, 0},
		{"unknown", 0, 100, false, 2},
		{"partially priced", 1, 100, true, 2},
		{"no cap", 1, 0, false, 2},
		{"over cost cap", 130, 100, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRun("r", "t", Profile{TimeBudgetOnly: true})
			r.inputTokenCap = 1000
			r.Tracker.Limit.MaxCostUSD = tc.cap
			r.observeUsageLocked(tasks.Usage{InputTokens: 1500, CostUSD: tc.cost})
			if tc.unpriced {
				r.observeUsageLocked(tasks.Usage{InputTokens: 1})
			}
			if got, _ := r.spendLevel(); got != tc.want {
				t.Fatalf("level=%d want=%d", got, tc.want)
			}
		})
	}
	t.Setenv("EXCALIBUR_COST_PRIMARY_SPEND", "off")
	r := NewRun("r", "t", Profile{TimeBudgetOnly: true})
	r.inputTokenCap = 1000
	r.Tracker.Limit.MaxCostUSD = 100
	r.observeUsageLocked(tasks.Usage{InputTokens: 1500, CostUSD: 1})
	if level, _ := r.spendLevel(); level != 2 {
		t.Fatal("off must restore independent caps")
	}
	t.Setenv(InputTokenCapSwitch, "")
	if InputTokenCap() != 30_000_000 {
		t.Fatal("default input cap must be 30 million")
	}
}
