// Crash suite, first row of 17.8: kill -9 at every journal point.
//
// A real kill -9 cannot run inside the test binary, so each case
// writes the journal up to the crash point, drops all in-memory
// state (the "dead process"), reopens the same store and resumes
// with a fresh engine:
//
//   - read-only reruns;
//   - idempotent resolves by postcondition (done when the effect
//     already happened, rerun when it did not);
//   - keyed resends with the same key (one effect in the service);
//   - non-idempotent never repeats: blocked(uncertain_effect);
//   - model_request without a response repeats the call;
//   - the recorded response is never recalled: the resumed run
//     makes exactly one model call for its next step.
package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// probeFunc adapts a closure to journal.Prober.
type probeFunc func(ctx context.Context, tool string, args json.RawMessage) (bool, error)

func (f probeFunc) Probe(ctx context.Context, tool string, args json.RawMessage) (bool, error) {
	return f(ctx, tool, args)
}

// crashWorld holds one interrupted run and its journal.
type crashWorld struct {
	js   *journal.MemStore
	rec  *journal.Recorder
	run  *Run
	e    *Engine
	tool *fakeRunner
}

func newCrashWorld(tool *fakeRunner) *crashWorld {
	js := journal.NewMemStore()
	e := New(Deps{
		Models:  &fakeEndpoint{},
		Context: &fakeBuilder{},
		Tools:   tool,
		Gate:    passingGate(),
	})
	run := testRun(Profile{})
	e.AttachJournal(run.ID, js)
	return &crashWorld{js: js, rec: e.recorder(run.ID), run: run, e: e, tool: tool}
}

func (w *crashWorld) requestResponse(t *testing.T, callID, name, args string) {
	t.Helper()
	ctx := context.Background()
	if _, err := w.rec.ModelRequest(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.rec.ModelResponse(ctx, journal.ModelResponsePayload{
		Hash: "h1", Tools: []string{name},
		Calls: []journal.RecordedCall{{CallID: callID, Tool: name, Args: json.RawMessage(args)}},
	}); err != nil {
		t.Fatal(err)
	}
}

func (w *crashWorld) intent(t *testing.T, callID, name, args, key string, effect types.EffectClass, idem types.IdempotencyClass) {
	t.Helper()
	if _, err := w.rec.ToolIntent(context.Background(), journal.ToolIntentPayload{
		CallID: callID, Tool: name, Args: json.RawMessage(args),
		IdempotencyKey: key, Effect: effect, Idempotency: idem,
	}); err != nil {
		t.Fatal(err)
	}
}

// resume reopens the world after the kill: fresh engine and run
// sharing the same journal bytes, then recover, apply, replay and
// one finishing step.
func (w *crashWorld) resume(t *testing.T, prober journal.Prober, finalText string) (RecoveryReport, *fakeEndpoint, *fakeRunner) {
	t.Helper()
	ctx := context.Background()
	entries, err := w.js.List(ctx, w.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(finalText)}}
	tool := w.tool
	e2 := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: tool, Gate: passingGate()})
	e2.AttachJournal(w.run.ID, w.js)
	run2 := testRun(Profile{})
	run2.ID = w.run.ID
	rep := e2.Recover(ctx, run2, entries, prober, false)
	if err := e2.ApplyRecovery(ctx, run2, rep); err != nil {
		t.Fatal(err)
	}
	if err := e2.ReplayPending(ctx, run2, rep); err != nil {
		t.Fatal(err)
	}
	if rep.Decision == RecoverResume {
		if err := e2.Start(ctx, run2); err != nil {
			t.Fatal(err)
		}
		if err := e2.Run(ctx, run2); err != nil {
			t.Fatal(err)
		}
	}
	w.e = e2
	w.run = run2
	return rep, model, tool
}

func TestCrashBeforeIntentReplaysOnce(t *testing.T) {
	tool := scriptedRunner()
	tool.add("write_file", types.EffectWriteLocal, types.IdemIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "wrote"})
	w := newCrashWorld(tool)
	w.requestResponse(t, "c1", "write_file", `{"path":"/f"}`)
	// Kill: intent never recorded, effect never ran.
	rep, model, runner := w.resume(t, nil, "done")
	if rep.Decision != RecoverResume {
		t.Fatalf("decision = %s", rep.Decision)
	}
	if len(rep.Rerun) != 1 {
		t.Fatalf("rerun = %d, want 1 synthesized from the response", len(rep.Rerun))
	}
	if runner.ncalls() != 1 {
		t.Fatalf("tool ran %d times, want exactly 1", runner.ncalls())
	}
	if model.calls() != 1 {
		t.Fatalf("model called %d times, want 1 (recorded response reused)", model.calls())
	}
	mustState(t, w.run, types.RunCompleted)
}

func TestCrashAfterIntentPureReruns(t *testing.T) {
	tool := scriptedRunner()
	tool.add("read_file", types.EffectReadOnly, types.IdemPure,
		tools.Result{Status: tools.StatusOK, Output: "data"})
	w := newCrashWorld(tool)
	w.requestResponse(t, "c1", "read_file", `{"path":"/f"}`)
	w.intent(t, "c1", "read_file", `{"path":"/f"}`, "", types.EffectReadOnly, types.IdemPure)
	rep, _, runner := w.resume(t, nil, "done")
	if rep.Decision != RecoverResume || len(rep.Rerun) != 1 {
		t.Fatalf("rep: %+v", rep)
	}
	if runner.ncalls() != 1 {
		t.Fatalf("pure tool ran %d times, want 1", runner.ncalls())
	}
}

func TestCrashIdempotentProbeDone(t *testing.T) {
	tool := scriptedRunner()
	tool.add("write_file", types.EffectWriteLocal, types.IdemIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "wrote"})
	w := newCrashWorld(tool)
	w.requestResponse(t, "c1", "write_file", `{"path":"/f"}`)
	w.intent(t, "c1", "write_file", `{"path":"/f"}`, "", types.EffectWriteLocal, types.IdemIdempotent)
	// The effect landed before the kill, but the result did not.
	prober := probeFunc(func(context.Context, string, json.RawMessage) (bool, error) { return true, nil })
	rep, _, runner := w.resume(t, prober, "done")
	if len(rep.Done) != 1 || len(rep.Rerun) != 0 {
		t.Fatalf("rep: %+v", rep)
	}
	if runner.ncalls() != 0 {
		t.Fatalf("postcondition held: tool must not rerun, ran %d", runner.ncalls())
	}
	mustState(t, w.run, types.RunCompleted)
}

// keyedService deduplicates by idempotency key like an external
// service honoring the key (17.2): one effect per key.
type keyedService struct {
	mu      sync.Mutex
	def     tools.Definition
	seen    map[string]bool
	effects int
	calls   int
}

func (k *keyedService) Definition() tools.Definition { return k.def }
func (k *keyedService) Execute(_ context.Context, _ string, _ json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls++
	if !k.seen[opts.IdempotencyKey] {
		k.seen[opts.IdempotencyKey] = true
		k.effects++
	}
	return tools.Result{Status: tools.StatusOK, Output: "sent"}, nil
}

func TestCrashKeyedSingleEffect(t *testing.T) {
	svc := &keyedService{seen: map[string]bool{}, def: tools.Definition{
		Name: "send_msg", Description: "keyed send",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Effect:      types.EffectCommunication, Idempotency: types.IdemKeyed, Cancel: types.CancelImmediate,
	}}
	runner := &keyedRunner{svc: svc}
	js := journal.NewMemStore()
	e := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: runner, Gate: passingGate()})
	run := testRun(Profile{})
	e.AttachJournal(run.ID, js)
	rec := e.recorder(run.ID)
	ctx := context.Background()
	_, _ = rec.ModelRequest(ctx, "h1")
	_, _ = rec.ModelResponse(ctx, journal.ModelResponsePayload{Hash: "h1", Tools: []string{"send_msg"},
		Calls: []journal.RecordedCall{{CallID: "c1", Tool: "send_msg", Args: json.RawMessage(`{}`)}}})
	_, _ = rec.ToolIntent(ctx, journal.ToolIntentPayload{
		CallID: "c1", Tool: "send_msg", Args: json.RawMessage(`{}`),
		IdempotencyKey: "key-1", Effect: types.EffectCommunication, Idempotency: types.IdemKeyed,
	})
	// Kill after the effect, before the result; then deliver the
	// same key twice more (retry + replay): still one effect.
	entries, _ := js.List(ctx, run.ID)
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
	e2 := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: runner, Gate: passingGate()})
	e2.AttachJournal(run.ID, js)
	run2 := testRun(Profile{})
	run2.ID = run.ID
	rep := e2.Recover(ctx, run2, entries, nil, false)
	if len(rep.Rerun) != 1 {
		t.Fatalf("rep: %+v", rep)
	}
	if err := e2.ApplyRecovery(ctx, run2, rep); err != nil {
		t.Fatal(err)
	}
	if err := e2.ReplayPending(ctx, run2, rep); err != nil {
		t.Fatal(err)
	}
	if svc.effects != 1 {
		t.Fatalf("effects = %d, want 1", svc.effects)
	}
	// A duplicate delivery of the same key is a no-op.
	_, _ = runner.Execute(ctx, "send_msg", json.RawMessage(`{}`), tools.ExecuteOptions{IdempotencyKey: "key-1"})
	if svc.effects != 1 {
		t.Fatalf("key resend duplicated the effect: %d", svc.effects)
	}
}

type keyedRunner struct {
	svc *keyedService
}

func (k *keyedRunner) Execute(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	return k.svc.Execute(ctx, name, args, opts)
}
func (k *keyedRunner) Definition(name string) (tools.Definition, error) { return k.svc.def, nil }

func TestCrashNonIdempotentBlockedWithoutRepeat(t *testing.T) {
	tool := scriptedRunner()
	tool.add("publish", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "published"})
	w := newCrashWorld(tool)
	w.requestResponse(t, "c1", "publish", `{}`)
	w.intent(t, "c1", "publish", `{}`, "", types.EffectExec, types.IdemNonIdempotent)
	rep, _, runner := w.resume(t, nil, "done")
	if rep.Decision != RecoverBlocked {
		t.Fatalf("decision = %s, want blocked", rep.Decision)
	}
	if len(rep.Uncertain) != 1 {
		t.Fatalf("rep: %+v", rep)
	}
	if runner.ncalls() != 0 {
		t.Fatalf("non-idempotent effect repeated %d times", runner.ncalls())
	}
	mustState(t, w.run, types.RunBlocked)
}

func TestCrashRequestWithoutResponseRepeatsCall(t *testing.T) {
	js := journal.NewMemStore()
	e := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
	run := testRun(Profile{})
	e.AttachJournal(run.ID, js)
	if _, err := e.recorder(run.ID).ModelRequest(context.Background(), "h9"); err != nil {
		t.Fatal(err)
	}
	// Kill before any response: the call simply repeats (with cost).
	rep, model, _ := (&crashWorld{js: js, e: e, run: run, tool: scriptedRunner()}).resume(t, nil, "done")
	if rep.ModelReplay.HasResponse {
		t.Fatal("no response recorded")
	}
	if model.calls() != 1 {
		t.Fatalf("model calls = %d, want 1 repeat", model.calls())
	}
}

func TestCrashDuringCompactionAndCheckpoint(t *testing.T) {
	tool := scriptedRunner()
	tool.add("read_file", types.EffectReadOnly, types.IdemPure,
		tools.Result{Status: tools.StatusOK, Output: "data"})
	w := newCrashWorld(tool)
	ctx := context.Background()
	if _, err := w.rec.StateChange(ctx, types.RunCreated, types.RunRunning, ""); err != nil {
		t.Fatal(err)
	}
	w.requestResponse(t, "c1", "read_file", `{"path":"/f"}`)
	if _, err := w.rec.Checkpoint(ctx, 20, json.RawMessage(`{"step":20}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.rec.Record(ctx, journal.TypeCompaction, journal.MarshalPayload(map[string]string{"span": "0-100"}), "", "", ""); err != nil {
		t.Fatal(err)
	}
	rep, _, runner := w.resume(t, nil, "done")
	if rep.Decision != RecoverResume || rep.StepsPassed != 1 {
		t.Fatalf("rep: %+v", rep)
	}
	if runner.ncalls() != 1 {
		t.Fatalf("tool ran %d times", runner.ncalls())
	}
}

// TestCrashReopenFileDB kills the porter mid-run: the file keeps
// every confirmed entry and the resumed run repeats no effect.
func TestCrashReopenFileDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "crash.db")
	open := func() *store.DB {
		opts := store.DefaultOptions()
		opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
		db, err := store.Open(ctx, path, opts, migrations.Main())
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := open()
	js := &journal.SQLStore{W: db, R: db}
	tool := scriptedRunner()
	tool.add("write_file", types.EffectWriteLocal, types.IdemIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "wrote"})
	e := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: tool, Gate: passingGate()})
	run := testRun(Profile{})
	e.AttachJournal(run.ID, js)
	rec := e.recorder(run.ID)
	if _, err := rec.ModelRequest(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.ModelResponse(ctx, journal.ModelResponsePayload{Hash: "h1",
		Calls: []journal.RecordedCall{{CallID: "c1", Tool: "write_file", Args: json.RawMessage(`{}`)}}}); err != nil {
		t.Fatal(err)
	}
	// kill -9: close without checkpointing or flushing anything else.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := open()
	defer db2.Close()
	js2 := &journal.SQLStore{W: db2, R: db2}
	entries, err := js2.List(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("confirmed entries = %d, want 2", len(entries))
	}
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
	e2 := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: tool, Gate: passingGate()})
	e2.AttachJournal(run.ID, js2)
	run2 := testRun(Profile{})
	run2.ID = run.ID
	rep := e2.Recover(ctx, run2, entries, nil, false)
	if err := e2.ApplyRecovery(ctx, run2, rep); err != nil {
		t.Fatal(err)
	}
	if err := e2.ReplayPending(ctx, run2, rep); err != nil {
		t.Fatal(err)
	}
	if tool.ncalls() != 1 {
		t.Fatalf("tool ran %d times, want 1", tool.ncalls())
	}
	if err := e2.Start(ctx, run2); err != nil {
		t.Fatal(err)
	}
	if err := e2.Run(ctx, run2); err != nil {
		t.Fatal(err)
	}
	mustState(t, run2, types.RunCompleted)
	if model.calls() != 1 {
		t.Fatalf("model calls = %d, want 1", model.calls())
	}
}

func TestRecoverWaitingUserRestoresWait(t *testing.T) {
	tool := scriptedRunner()
	w := newCrashWorld(tool)
	ctx := context.Background()
	_, _ = w.rec.StateChange(ctx, types.RunCreated, types.RunRunning, "")
	_, _ = w.rec.StateChange(ctx, types.RunRunning, types.RunWaiting, "user")
	entries, _ := w.js.List(ctx, w.run.ID)
	run2 := testRun(Profile{})
	run2.ID = w.run.ID
	rep := w.e.Recover(ctx, run2, entries, nil, true)
	if rep.Decision != RecoverAskUser {
		t.Fatalf("decision = %s", rep.Decision)
	}
	if err := w.e.ApplyRecovery(ctx, run2, rep); err != nil {
		t.Fatal(err)
	}
	mustState(t, run2, types.RunWaiting)
}

func TestRecoverLongOutageParks(t *testing.T) {
	js := journal.NewMemStore()
	rec := &journal.Recorder{Store: js, RunID: "old"}
	ctx := context.Background()
	if _, err := rec.ModelRequest(ctx, "h"); err != nil {
		t.Fatal(err)
	}
	entries, _ := js.List(ctx, "old")
	// Backdate the entry past the 6h auto-resume window.
	entries[0].At = entries[0].At.Add(-7 * time.Hour)
	eng := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate()})
	run := testRun(Profile{})
	run.ID = "old"
	rep := eng.Recover(ctx, run, entries, nil, false)
	if rep.Decision != RecoverPaused {
		t.Fatalf("decision = %s, want paused after >6h", rep.Decision)
	}
	if err := eng.ApplyRecovery(ctx, run, rep); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunPaused)
	if _, reason, _, _ := run.Reasons(); reason != types.PauseRecovery {
		t.Fatalf("reason = %s", reason)
	}
}
