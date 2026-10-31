package cli

import (
	"context"
	"encoding/json"
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
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// profileGatedRunner clamps the engine's tool surface to the profile
// table (Sections 3.6, 23.2): the model only sees the profile's core
// tools, and calls outside the surface — including disabled task_db
// operations — are rejected before execution. Assistant tools hidden
// in eval therefore never run even if the model hallucinates them.
type profileGatedRunner struct {
	profile ProfileName
	allowed map[string]bool
	base    engine.ToolRunner
}

func newProfileGatedRunner(profile ProfileName, base engine.ToolRunner) *profileGatedRunner {
	core, deferred := EffectiveTools(profile)
	allowed := make(map[string]bool, len(core)+len(deferred))
	for _, name := range core {
		allowed[name] = true
	}
	for _, name := range deferred {
		allowed[name] = true
	}
	return &profileGatedRunner{profile: profile, allowed: allowed, base: base}
}

// check rejects tools and operations outside the eval surface. Other
// profiles pass everything through: only eval has a frozen table.
func (g *profileGatedRunner) check(name string, args json.RawMessage) error {
	if g.profile != ProfileEval {
		return nil
	}
	if !g.allowed[name] {
		if err := EvalToolAllowed(name, ""); err != nil {
			return err
		}
		// EvalToolAllowed only knows the hidden list; anything not
		// in the profile table is still outside the surface.
		return fmt.Errorf("cli: tool %q is not in the eval profile surface", name)
	}
	if name == "task_db" {
		for _, op := range taskDBOps(args) {
			if err := EvalToolAllowed(name, op); err != nil {
				return err
			}
		}
	}
	return nil
}

// Definition implements engine.ToolRunner. Definitions pass through
// ungated: batch planning needs them for any name the model utters,
// and the model only ever sees the profile's core tools in its
// prefix. Execution is where the surface is enforced.
func (g *profileGatedRunner) Definition(name string) (tools.Definition, error) {
	return g.base.Definition(name)
}

// Execute implements engine.ToolRunner.
func (g *profileGatedRunner) Execute(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	if err := g.check(name, args); err != nil {
		return tools.Result{}, err
	}
	return g.base.Execute(ctx, name, args, opts)
}

// pruneDeferredToProfile drops every deferred mark outside the
// profile surface before the first model call (Section 23.2).
// search_tools only indexes deferred entries, and call_tool refuses
// anything not marked deferred, so unmarked tools are unreachable
// through both. Direct calls stay blocked by the runner gate.
func pruneDeferredToProfile(cat *tools.DeferredCatalog, allowed map[string]bool) {
	for _, name := range cat.DeferredNames() {
		if !allowed[name] {
			cat.UnmarkDeferred(name)
		}
	}
}

// taskDBOps extracts the op names of a task_db batch call so the eval
// gate can reject project-management operations (Section 23.2). An
// undecodable batch yields no ops here; the tool itself still
// validates and rejects it.
func taskDBOps(args json.RawMessage) []string {
	if len(args) == 0 {
		return nil
	}
	var body struct {
		Ops []struct {
			Op string `json:"op"`
		} `json:"ops"`
	}
	if err := json.Unmarshal(args, &body); err != nil {
		return nil
	}
	var out []string
	for _, op := range body.Ops {
		if op.Op != "" {
			out = append(out, op.Op)
		}
	}
	return out
}

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
	baseRunner := engine.NewRegistryRunner(emb.Registry, emb.Exec)
	runner := newProfileGatedRunner(profile, baseRunner)
	if profile == ProfileEval {
		pruneDeferredToProfile(emb.Catalog, runner.allowed)
	}
	snap := func(ctx context.Context, erun *engine.Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{
			Role: "child", HarnessVersion: Version,
			Volatile: ctxcompiler.Volatile{Now: time.Now().UTC()},
		}, nil
	}
	// Only the profile's core tools ride the model request in order;
	// deferred tools resolve through search_tools from the next step
	// so adding them never breaks the input cache (11.6, 13.3).
	// Under eval the surface is the frozen Section 23.2 table: the
	// gate rejects anything else even if the model asks for it.
	allowed := runner.allowed
	toolDefs := func(erun *engine.Run) []providers.ToolDefinition {
		var out []providers.ToolDefinition
		for _, name := range emb.Catalog.CoreNames() {
			if profile == ProfileEval && !allowed[name] {
				continue
			}
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
