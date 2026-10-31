package wireengine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TestResumeReplaysWithoutDuplicatingEffects covers Section 17.3: a
// crash between a tool intent and its result replays from the journal
// without calling the model again and without repeating a keyed
// effect, and the orchestrator adopts the recovery decision.
func TestResumeReplaysWithoutDuplicatingEffects(t *testing.T) {
	ctx := context.Background()
	fake := providerfake.New("wire-resume")
	fake.Responses = []providers.ChatResponse{finalChat("done")}
	gw := models.NewGateway(models.GatewayConfig{
		Roles: map[types.ModelRole]models.RoleEntry{
			types.RoleExecutor: {Primary: models.ModelRef{Provider: "wire-resume", Model: "m"}},
		},
		Prices:       map[string]models.Price{"m": {}},
		PriceVersion: "test",
		MaxAttempts:  2,
		BaseDelay:    1,
	})
	gw.Register(fake)
	reg := tools.NewMapRegistry()
	if err := tools.RegisterBuiltins(reg, tools.DefaultBuiltinDeps()); err != nil {
		t.Fatal(err)
	}
	exec := tools.NewExecutor(reg)
	runner := engine.NewRegistryRunner(reg, exec)
	snap := func(ctx context.Context, run *engine.Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{Role: "child", HarnessVersion: "test",
			Volatile: ctxcompiler.Volatile{Now: time.Now().UTC()}}, nil
	}
	eng := engine.New(engine.Deps{
		Models: gw,
		Context: &engine.DefaultBuilder{
			Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
			Snapshot: snap, ToolDefs: func(*engine.Run) []providers.ToolDefinition { return nil },
			ReasoningEffort: "low",
		},
		Tools: runner,
	})
	js := journal.NewMemStore()
	run := engine.NewRun("run_resume_1", "task_resume_1", engine.Profile{
		Name: "child", Role: types.RoleExecutor, ContextRole: "child",
		Limit: tasks.Limit{MaxSteps: 5},
	})
	jrec := eng.AttachJournal(run.ID, js)
	// Simulate a crash: run_started plus a read_only intent with no
	// result. Read-only intents rerun safely (17.2).
	if _, err := jrec.RunStarted(ctx, map[string]string{"task": "resume me"}); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "note.txt"})
	if _, err := jrec.ToolIntent(ctx, journal.ToolIntentPayload{
		CallID: "c1", Tool: "read_file", Args: args,
		IdempotencyKey: "key-1", Effect: types.EffectReadOnly, Idempotency: types.IdemPure,
	}); err != nil {
		t.Fatal(err)
	}
	callsBefore := len(fake.ChatRequests)
	out, err := ResumeReplays(ctx, eng, run, js, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Reran != 1 {
		t.Fatalf("reran = %d, want 1 (read-only intent replays)", out.Reran)
	}
	if len(fake.ChatRequests) != callsBefore {
		t.Fatalf("replay must not call the model: %d -> %d", callsBefore, len(fake.ChatRequests))
	}
	if run.State() != types.RunQueued {
		t.Fatalf("reconciled run = %q, want queued (resume)", run.State())
	}
	// The orchestrator adopts the same decision on its row.
	o := orchestrator.New(orchestrator.DefaultConfig(), orchestrator.NewMemoryStore(), orchestrator.NoopJournal{})
	master, err := o.RegisterMaster("Master", types.GeneralScope())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptIntoOrchestrator(o, "run_missing", out.Report); err == nil {
		t.Fatal("adoption of an unknown run must fail")
	}
	// A spawned child run adopts a resume decision through
	// recovering back to queued without repeating effects.
	child, orec, err := o.Spawn(master.ID, orchestrator.DelegationCard{
		Name: "Kid", Description: "does the thing", Goal: "do T",
		Deliverable: "done T", Scope: types.Scope{Kind: types.ScopeProject, ID: "p1"},
		Done: []string{"gate passes"}, Report: "summary",
		TaskID: types.TaskID("tsk_000000000000000000000001"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = child
	st, err := AdoptIntoOrchestrator(o, orec.Run.ID, engine.RecoveryReport{Decision: engine.RecoverResume})
	if err != nil {
		t.Fatal(err)
	}
	if st != types.RunQueued {
		t.Fatalf("adopted = %q, want queued", st)
	}
}
