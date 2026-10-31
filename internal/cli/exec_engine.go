package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// runEnginePath drives the real embedded engine loop with the full
// wired catalog (Sections 7.3, 13.3, 13.4): leases, shared history,
// LSP diagnostics, deferred tools through search_tools/call_tool,
// skill tools, vault redaction and the journal write-ahead rule. The
// scripted fake provider in tests (or the live provider in
// production) answers model calls; no step is deterministic. Every
// model turn and tool result is appended to the ATIF trajectory so
// the Appendix F records show the real execution.
func runEnginePath(ctx context.Context, opts ExecOptions, run *ExecRun) (types.RunState, error) {
	profile := opts.Profile
	if profile == "" {
		profile = ProfileDefault
	}
	store := opts.JournalStore
	if store == nil {
		store = journal.NewMemStore()
	}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{
		RunID: string(run.RunID), Project: opts.ProjectDir, Vault: opts.Vault,
		Delegated: profile == ProfileDelegate,
	})
	if err != nil {
		return "", fmt.Errorf("cli: wire embedded catalog: %w", err)
	}
	gw := models.NewGateway(models.GatewayConfig{
		Roles: map[types.ModelRole]models.RoleEntry{
			types.RoleExecutor: {Primary: models.ModelRef{Provider: opts.Provider.Name(), Model: "embedded"}},
		},
		Prices:       map[string]models.Price{"embedded": {}},
		PriceVersion: "test",
		MaxAttempts:  2,
		BaseDelay:    1,
	})
	gw.Register(opts.Provider)
	runner := engine.NewRegistryRunner(emb.Registry, emb.Exec)
	snap := func(ctx context.Context, erun *engine.Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{
			Role: "child", HarnessVersion: Version,
			Volatile: ctxcompiler.Volatile{Now: time.Now().UTC()},
		}, nil
	}
	// Only the fixed core prefix rides the model request in order;
	// deferred tools resolve through search_tools from the next step
	// so adding them never breaks the input cache (11.6, 13.3).
	toolDefs := func(erun *engine.Run) []providers.ToolDefinition {
		var out []providers.ToolDefinition
		for _, name := range emb.Catalog.CoreNames() {
			tool, err := emb.Registry.Get(name)
			if err != nil {
				continue
			}
			def := tool.Definition()
			out = append(out, providers.ToolDefinition{Name: def.Name, Description: def.Description, Schema: def.InputSchema})
		}
		return out
	}
	eng := engine.New(engine.Deps{
		Models: gw,
		Context: &engine.DefaultBuilder{
			Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
			Snapshot: snap, ToolDefs: toolDefs, ReasoningEffort: "low",
		},
		Tools:          runner,
		LeaseHeartbeat: wireengine.LeaseHeartbeatHook(emb.Leases),
		LeaseRelease:   wireengine.LeaseReleaseHook(emb.Leases),
	})
	erun := engine.NewRun(string(run.RunID), string(run.TaskID), engine.Profile{
		Name: "child", Role: types.RoleExecutor, ContextRole: "child",
		Limit: tasks.Limit{MaxSteps: 25},
	})
	if opts.MaxSteps > 0 {
		erun.Profile.Limit.MaxSteps = opts.MaxSteps
		erun.Tracker.Limit.MaxSteps = opts.MaxSteps
	}
	rec := eng.AttachJournal(erun.ID, store)
	// Prove the orchestrator contract in embedded mode: the engine
	// runner attaches, serves a snapshot, and detaches. Control
	// orders travel the same path as the daemon's (Sections 15.8).
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchestrator.NewMemoryStore(), orchestrator.NoopJournal{})
	er := engine.NewRunner(eng, erun)
	orch.AttachRunner(er)
	defer orch.DetachRunner(er.RunID())
	if err := eng.Admit(ctx, erun); err != nil {
		return "", err
	}
	if _, err := rec.RunStarted(ctx, map[string]string{"task": opts.Request}); err != nil {
		return "", err
	}
	if err := eng.Start(ctx, erun); err != nil {
		return "", err
	}
	if err := eng.Run(ctx, erun); err != nil {
		// An interrupt cancels the context without failing: the
		// orchestrator owns the resulting state.
		if ctx.Err() == nil {
			return "", err
		}
	}
	rep := eng.Report(erun)
	// Mirror the real trajectory into ATIF: one assistant step per
	// model turn is already journaled; here the tool history and the
	// final report land as steps so the trajectory is complete.
	for _, ev := range erun.History {
		if ev.ToolResult == nil {
			continue
		}
		if err := run.EmitStep(telemetry.ATIFStep{
			Role: "assistant", Content: "tool call " + ev.ToolResult.Name,
			ToolCalls: []telemetry.ATIFToolCall{{
				ID: ev.ToolResult.CallID, Name: ev.ToolResult.Name,
				Arguments: string(ev.ToolResult.CallID),
			}},
		}, "tool_call"); err != nil {
			return "", err
		}
		if err := run.EmitStep(telemetry.ATIFStep{
			Role:       "tool",
			ToolResult: &telemetry.ATIFToolResult{CallID: ev.ToolResult.CallID, Name: ev.ToolResult.Name, Output: ev.Content},
		}, "tool_result"); err != nil {
			return "", err
		}
	}
	summary := rep.Summary
	if summary == "" {
		summary = "completed: " + shortSummary(opts.Request)
	}
	if err := run.EmitStep(telemetry.ATIFStep{Role: "assistant", Content: summary}, "message"); err != nil {
		return "", err
	}
	return erun.State(), nil
}
