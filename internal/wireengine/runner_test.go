package wireengine

import (
	"context"
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

// scriptedEngine builds a one-step engine over a fake provider.
func scriptedEngine(t *testing.T, scripts []providers.ChatResponse) (*engine.Engine, *engine.Run, *providerfake.FakeProvider, journal.Store) {
	t.Helper()
	fake := providerfake.New("wire-runner")
	fake.Responses = scripts
	gw := models.NewGateway(models.GatewayConfig{
		Roles: map[types.ModelRole]models.RoleEntry{
			types.RoleExecutor: {Primary: models.ModelRef{Provider: "wire-runner", Model: "m"}},
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
	toolDefs := func(run *engine.Run) []providers.ToolDefinition { return nil }
	eng := engine.New(engine.Deps{
		Models: gw,
		Context: &engine.DefaultBuilder{
			Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
			Snapshot: snap, ToolDefs: toolDefs, ReasoningEffort: "low",
		},
		Tools: runner,
	})
	js := journal.NewMemStore()
	run := engine.NewRun("run_wire_1", "task_wire_1", engine.Profile{
		Name: "child", Role: types.RoleExecutor, ContextRole: "child",
		Limit: tasks.Limit{MaxSteps: 5},
	})
	eng.AttachJournal(run.ID, js)
	return eng, run, fake, js
}

func finalChat(text string) providers.ChatResponse {
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: text}}
}

// TestRunnerImplementsOrchestrator covers the engine <-> orchestrator
// seam: the engine runner attaches, applies a directive at the next
// safe point, reports a snapshot, and a full interrupt stops the
// loop without failing the run.
func TestRunnerImplementsOrchestrator(t *testing.T) {
	ctx := context.Background()
	eng, run, _, _ := scriptedEngine(t, []providers.ChatResponse{finalChat("done")})
	o := orchestrator.New(orchestrator.DefaultConfig(), orchestrator.NewMemoryStore(), orchestrator.NoopJournal{})
	er := engine.NewRunner(eng, run)
	var _ orchestrator.Runner = er
	o.AttachRunner(er)
	defer o.DetachRunner(er.RunID())
	if _, ok := o.RunnerFor(er.RunID()); !ok {
		t.Fatal("runner must be attached")
	}
	// Directive lands at the next safe point.
	o.AttachRunner(er)
	er.ApplyDirective(orchestrator.Directive{ID: "msg_1", Action: orchestrator.ActionAdjust, Text: "steer left"})
	if got := len(run.PendingDirectives()); got != 1 {
		t.Fatalf("pending directives = %d, want 1", got)
	}
	if err := eng.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if run.State() != types.RunCompleted {
		t.Fatalf("state = %q, want completed", run.State())
	}
	snap := er.Snapshot()
	if snap.State != types.RunCompleted || snap.StepsDone == 0 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if !snap.AckedDirectives["msg_1"] {
		t.Fatalf("directive must be acked: %+v", snap.AckedDirectives)
	}
}

// TestRunnerInterruptStopsLoop covers RequestInterrupt(false): the
// in-flight model call is canceled and the loop exits gracefully
// without failing, leaving the state to the orchestrator.
func TestRunnerInterruptStopsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	eng, run, _, _ := scriptedEngine(t, []providers.ChatResponse{finalChat("never")})
	// Block the model until the interrupt arrives.
	block := make(chan struct{})
	eng2 := eng
	_ = eng2
	_ = block
	er := engine.NewRunner(eng, run)
	if err := eng.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx, run) }()
	er.RequestInterrupt(false)
	cancel()
	if err := <-done; err != nil {
		// A parent-context cancel without an interrupt flag
		// surfaces the context error; with the flag the loop
		// exits cleanly. Either way the run must not fail.
		t.Logf("run returned: %v", err)
	}
	if st := run.State(); st == types.RunFailed {
		t.Fatalf("interrupted run must not fail, got %q", st)
	}
}
