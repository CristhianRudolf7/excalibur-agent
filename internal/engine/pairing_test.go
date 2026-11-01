package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// scriptEndpoint serves scripted errors then scripted responses so a
// test can stage a strict-wire rejection followed by a recovery.
type scriptEndpoint struct {
	mu        sync.Mutex
	errs      []error
	responses []providers.ChatResponse
	calls     int
	requests  []providers.ChatRequest
	sites     []string
	callHook  func(int)
}

func (s *scriptEndpoint) Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.sites = append(s.sites, ModelCallSite(ctx))
	if s.callHook != nil {
		s.callHook(s.calls)
	}
	s.requests = append(s.requests, req)
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		if err != nil {
			return providers.ChatResponse{}, models.CallMeta{}, err
		}
	}
	if len(s.responses) == 0 {
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "done"}}, models.CallMeta{}, nil
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	return resp, models.CallMeta{Provider: "fake", Model: "m"}, nil
}

func (s *scriptEndpoint) ncalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *scriptEndpoint) Close() error { return nil }

// orphan400 stages the Responses rejection of an orphan
// function_call_output.
func orphan400(callID string) error {
	return fmt.Errorf("providers: openai/muse-spark-1.3 input: http 400: No function call found for function call output with call_id '%s'", callID)
}

func transient500() error {
	return fmt.Errorf("providers: openai/muse-spark-1.3 transient: http 500: server error")
}

// seedOrphanHistory leaves one orphan result in the run history.
func seedOrphanHistory(run *Run) {
	run.mu.Lock()
	defer run.mu.Unlock()
	run.History = []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "go",
			ToolCalls: []providers.ToolCall{{ID: "ok1", Name: "exec", Arguments: []byte(`{}`)}}},
		{Seq: 1, Role: providers.RoleTool, Content: "r1",
			ToolResult: &providers.ToolResult{CallID: "ok1", Name: "exec", Output: "r1"}},
		{Seq: 2, Role: providers.RoleTool, Content: "orphan-out",
			ToolResult: &providers.ToolResult{CallID: "lost1", Name: "exec", Output: "orphan-out"}},
	}
}

func runForPairTest(name string) *Run {
	return NewRun(name, "task_"+name, Profile{
		Name: "child", ContextRole: "child", Limit: tasks.Limit{MaxSteps: 10},
	})
}

// The pre-request check repairs an orphan history into a valid
// request and records the repair.
func TestBuilderRepairsOrphanRequest(t *testing.T) {
	run := runForPairTest("pair_build_1")
	seedOrphanHistory(run)
	var notes []string
	b := &DefaultBuilder{
		Config:  ctxcompiler.DefaultConfig(),
		Counter: ctxcompiler.NewEstimator(0),
		Snapshot: func(ctx context.Context, r *Run) (ctxcompiler.Snapshot, error) {
			return ctxcompiler.Snapshot{
				Role: "child", HarnessVersion: "test",
				Task:     ctxcompiler.TaskView{OriginalRequest: "pair check"},
				Volatile: ctxcompiler.Volatile{Now: time.Now().UTC()},
			}, nil
		},
		OnPairRepair: func(repaired int, note string) { notes = append(notes, note) },
	}
	req, _, err := b.BuildRequest(context.Background(), run, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep := ctxcompiler.ValidateMessages(req.Messages); !rep.Valid() {
		t.Fatalf("built request must validate: %+v", rep)
	}
	if run.PairRepairs != 1 {
		t.Fatalf("run must count the repair, got %d", run.PairRepairs)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "lost1") {
		t.Fatalf("repair must be recorded with the orphan id: %v", notes)
	}
	// The run history itself is untouched: only the 400 path rewrites
	// history, the pre-request check repairs the request copy.
	if rep := ctxcompiler.ValidateActivity(run.History); rep.Valid() {
		t.Fatal("pre-request check must not rewrite run history")
	}
}

// The check switch restores the legacy bytes for ablations.
func TestBuilderPairCheckOff(t *testing.T) {
	run := runForPairTest("pair_build_2")
	seedOrphanHistory(run)
	off := false
	b := &DefaultBuilder{
		Config:  ctxcompiler.DefaultConfig(),
		Counter: ctxcompiler.NewEstimator(0),
		Snapshot: func(ctx context.Context, r *Run) (ctxcompiler.Snapshot, error) {
			return ctxcompiler.Snapshot{Role: "child", HarnessVersion: "test"}, nil
		},
		PairCheck: &off,
	}
	req, _, err := b.BuildRequest(context.Background(), run, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep := ctxcompiler.ValidateMessages(req.Messages); rep.Valid() {
		t.Fatal("disabled check must leave the orphan in place")
	}
	if run.PairRepairs != 0 {
		t.Fatalf("disabled check must not count repairs, got %d", run.PairRepairs)
	}
}

// A strict-wire orphan rejection repairs the history and retries the
// model call exactly once, then the run completes.
func TestStepRetriesOrphan400Once(t *testing.T) {
	ctx := context.Background()
	model := &scriptEndpoint{errs: []error{orphan400("lost1")}, responses: []providers.ChatResponse{finalResp("done")}}
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
	var retries []string
	e.deps.OnPairRetry = func(repaired int, firstErr string) {
		retries = append(retries, firstErr)
	}
	run := runForPairTest("pair_retry_1")
	seedOrphanHistory(run)
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if got := model.ncalls(); got != 2 {
		t.Fatalf("model calls = %d, want 2 (rejection + retry)", got)
	}
	if run.PairRetries != 1 {
		t.Fatalf("run must count one retry, got %d", run.PairRetries)
	}
	if run.PairRepairs == 0 {
		t.Fatal("run must count the history repair")
	}
	if len(retries) != 1 || !strings.Contains(retries[0], "No function call found") {
		t.Fatalf("retry must be observed with the first error: %v", retries)
	}
	if rep := ctxcompiler.ValidateActivity(run.History); !rep.Valid() {
		t.Fatalf("history must be paired after repair: %+v", rep)
	}
	if run.State() != types.RunCompleted {
		t.Fatalf("run must complete after the retry, state=%s", run.State())
	}
}

// A second rejection fails the run: exactly one retry, no loop.
func TestStepOrphan400FailsAfterOneRetry(t *testing.T) {
	ctx := context.Background()
	model := &scriptEndpoint{errs: []error{orphan400("lost1"), orphan400("lost1")}}
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
	run := runForPairTest("pair_retry_2")
	seedOrphanHistory(run)
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	err := e.Run(ctx, run)
	if err == nil || !strings.Contains(err.Error(), "model call:") {
		t.Fatalf("run must fail with the model-call cause, got %v", err)
	}
	if got := model.ncalls(); got != 2 {
		t.Fatalf("model calls = %d, want exactly 2 (no retry loop)", got)
	}
	if run.PairRetries != 1 {
		t.Fatalf("run must count exactly one retry, got %d", run.PairRetries)
	}
}

// Non-orphan failures never retry.
func TestStepNoRetryOnOtherErrors(t *testing.T) {
	ctx := context.Background()
	model := &scriptEndpoint{errs: []error{transient500()}}
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
	run := runForPairTest("pair_retry_3")
	seedOrphanHistory(run)
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(ctx, run); err == nil {
		t.Fatal("transient failure must fail the run")
	}
	if got := model.ncalls(); got != 1 {
		t.Fatalf("transient error must not retry, calls=%d", got)
	}
	if run.PairRetries != 0 || run.PairRepairs != 0 {
		t.Fatalf("no pairing counters on other errors: retries=%d repairs=%d", run.PairRetries, run.PairRepairs)
	}
	// History untouched when nothing pairs to repair.
	if rep := ctxcompiler.ValidateActivity(run.History); rep.Valid() {
		t.Fatal("failed run without repair must keep its history")
	}
}

// The retry switch restores the legacy fail-fast for ablations.
func TestStepNoRetryWhenDisabled(t *testing.T) {
	ctx := context.Background()
	model := &scriptEndpoint{errs: []error{orphan400("lost1")}}
	off := false
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate(), PairRetry: &off})
	run := runForPairTest("pair_retry_4")
	seedOrphanHistory(run)
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(ctx, run); err == nil {
		t.Fatal("disabled retry must fail the run on the orphan 400")
	}
	if got := model.ncalls(); got != 1 {
		t.Fatalf("disabled retry must not call again, calls=%d", got)
	}
	if run.PairRetries != 0 {
		t.Fatalf("disabled retry must not count, got %d", run.PairRetries)
	}
}

// An orphan rejection with nothing to repair fails without a wasted
// extra call.
func TestStepNoRetryWithoutOrphans(t *testing.T) {
	ctx := context.Background()
	model := &scriptEndpoint{errs: []error{orphan400("ghost")}}
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
	run := runForPairTest("pair_retry_5")
	// Paired history: the rejection names a call that is not ours.
	run.mu.Lock()
	run.History = []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "go",
			ToolCalls: []providers.ToolCall{{ID: "ok1", Name: "exec", Arguments: []byte(`{}`)}}},
		{Seq: 1, Role: providers.RoleTool, Content: "r1",
			ToolResult: &providers.ToolResult{CallID: "ok1", Name: "exec", Output: "r1"}},
	}
	run.mu.Unlock()
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(ctx, run); err == nil {
		t.Fatal("unrepairable orphan rejection must fail the run")
	}
	if got := model.ncalls(); got != 1 {
		t.Fatalf("nothing to repair means no retry, calls=%d", got)
	}
}

// Snapshot-owned exchanges are not necessarily in Run.History. A provider
// rejection must still produce a text-only single retry in that case.
func TestRetryPairsFromSnapshot(t *testing.T) {
	model := &scriptEndpoint{}
	b := &DefaultBuilder{
		Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
		Snapshot: func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
			return ctxcompiler.Snapshot{Role: "child", Activity: []ctxcompiler.ActivityEvent{
				{Seq: 1, Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "snap", Name: "exec", Arguments: []byte("{}")}}},
				{Seq: 2, Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "snap", Name: "exec", Output: "ok"}},
			}}, nil
		},
	}
	e := New(Deps{Models: model, Context: b})
	run := runForPairTest("snapshot_retry")
	if _, ok := e.retryOrphanPairs(context.Background(), run, false, orphan400("snap")); !ok {
		t.Fatal("snapshot exchange must get a repaired retry")
	}
	for _, m := range model.requests[0].Messages {
		if len(m.ToolCalls) != 0 || m.ToolResult != nil || m.Role == providers.RoleTool {
			t.Fatal("retry must replace both halves with plain text")
		}
	}
	if model.ncalls() != 1 || run.PairRetries != 1 || run.PairRepairs != 2 {
		t.Fatalf("calls=%d retries=%d repairs=%d", model.ncalls(), run.PairRetries, run.PairRepairs)
	}
}

func TestInterruptDuringPairRetry(t *testing.T) {
	for _, toolOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(toolOnly), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			run := runForPairTest("retry_interrupt")
			seedOrphanHistory(run)
			model := &scriptEndpoint{errs: []error{orphan400("lost1"), context.Canceled}}
			model.callHook = func(n int) {
				if n == 2 {
					run.RequestInterruptFlags(toolOnly)
					cancel()
				}
			}
			e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
			if err := e.Admit(ctx, run); err != nil {
				t.Fatal(err)
			}
			if err := e.Start(ctx, run); err != nil {
				t.Fatal(err)
			}
			done, err := e.step(ctx, run)
			if err != nil || done == toolOnly || run.State() == types.RunFailed {
				t.Fatalf("done=%t state=%s err=%v", done, run.State(), err)
			}
			if run.interruptedNow() {
				t.Fatal("interrupt was not consumed")
			}
		})
	}
}

// Recovery uses the same deadline and audit identity as the initial call.
func TestDeadlineDuringPairRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	run := runForPairTest("retry_deadline")
	seedOrphanHistory(run)
	model := &scriptEndpoint{errs: []error{orphan400("lost1"), context.DeadlineExceeded}}
	model.callHook = func(n int) {
		if n == 2 {
			<-ctx.Done()
		}
	}
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	done, err := e.step(ctx, run)
	if err != nil || !done || run.State() != types.RunPaused || run.pauseReason != types.PauseBudget {
		t.Fatalf("done=%t state=%s pause=%s err=%v", done, run.State(), run.pauseReason, err)
	}
	if model.ncalls() != 2 || run.PairRetries != 1 {
		t.Fatalf("calls=%d retries=%d", model.ncalls(), run.PairRetries)
	}
	for _, site := range model.sites {
		if site != "engine.step" {
			t.Fatalf("missing audit site: %q", site)
		}
	}
}

func TestQuotaDuringPairRetry(t *testing.T) {
	ctx := context.Background()
	run := runForPairTest("retry_quota")
	seedOrphanHistory(run)
	model := &scriptEndpoint{errs: []error{orphan400("lost1"), models.ErrProviderQuotaExhausted}}
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	done, err := e.step(ctx, run)
	if !done || !errors.Is(err, models.ErrProviderQuotaExhausted) || run.State() != types.RunWaiting {
		t.Fatalf("done=%t state=%s err=%v", done, run.State(), err)
	}
	if model.ncalls() != 2 {
		t.Fatalf("calls=%d", model.ncalls())
	}
}
