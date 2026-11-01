package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// daemonTurnRun is the run shape a daemon step engine drives.
func daemonTurnRun(id string) *engine.Run {
	return engine.NewRun(id, "task_"+id, engine.Profile{
		Name: "child", Role: types.RoleExecutor, ContextRole: "child",
		Limit: tasks.Limit{MaxSteps: 10},
	})
}

// fakeGateway routes the executor role to one scripted provider.
func fakeGateway(t *testing.T, fake *providerfake.FakeProvider, model string) *models.Gateway {
	t.Helper()
	gw := models.NewGateway(models.GatewayConfig{
		Roles: map[types.ModelRole]models.RoleEntry{
			types.RoleExecutor: {Primary: models.ModelRef{Provider: fake.Name(), Model: model}},
		},
		Prices:       map[string]models.Price{model: {Input: 1, Output: 4}},
		PriceVersion: "test",
		MaxAttempts:  2,
		BaseDelay:    1,
	})
	gw.Register(fake)
	return gw
}

// TestDaemonEngineDrivesOneTurnWithFakeProvider covers the Section 7.3
// step through the daemon engine: the configured gateway answers a
// tool call, the tool runs through the daemon executor, the scripted
// provider finishes the turn, and the run completes.
func TestDaemonEngineDrivesOneTurnWithFakeProvider(t *testing.T) {
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "hello.txt")
	args, _ := json.Marshal(map[string]string{"path": target, "content": "hello"})
	fake := providerfake.New("daemon-fake")
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{
			{ID: "c1", Name: "write_file", Arguments: args},
		}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "wrote hello.txt"}},
	}
	svc, err := Assemble(AssembleOptions{
		Models:  fakeGateway(t, fake, "daemon-model"),
		ModelID: "daemon-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	run := daemonTurnRun("run_daemon_turn")
	svc.Engine.AttachJournal(run.ID, journal.NewMemStore())
	if err := svc.Engine.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := svc.Engine.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := svc.Engine.Run(ctx, run); err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.State() != types.RunCompleted {
		t.Fatalf("state = %v, want completed", run.State())
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "hello" {
		t.Fatalf("tool did not run through the daemon executor: %q, %v", data, err)
	}
	if run.Steps < 2 {
		t.Fatalf("steps = %d, want the tool step and the final step", run.Steps)
	}
}

// TestDaemonEngineWithoutModelFailsRunCleanly covers the unconfigured
// daemon: the engine must not dereference a missing endpoint; the run
// ends with the configuration error instead.
func TestDaemonEngineWithoutModelFailsRunCleanly(t *testing.T) {
	ctx := context.Background()
	svc, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	run := daemonTurnRun("run_no_model")
	svc.Engine.AttachJournal(run.ID, journal.NewMemStore())
	if err := svc.Engine.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := svc.Engine.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	_ = svc.Engine.Run(ctx, run)
	if run.State() != types.RunFailed {
		t.Fatalf("state = %v, want failed on the configuration error", run.State())
	}
}
