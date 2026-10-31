package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
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

// recordingModelEndpoint wraps the gateway and logs one JSONL line
// per model call (role, model, effort, layer hashes/sizes, tokens,
// latency, finish reason, provider error class, retries).
type recordingModelEndpoint struct {
	base     engine.ModelEndpoint
	run      *ExecRun
	settings models.ModelSettings
}

func (m *recordingModelEndpoint) Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	start := time.Now()
	resp, meta, err := m.base.Chat(ctx, role, req)
	latency := time.Since(start)
	if m.run.Recorder == nil {
		return resp, meta, err
	}
	m.run.modelSeq++
	layerHashes := map[string]string{}
	layerSizes := map[string]int{}
	for i, msg := range req.Messages {
		h := sha256.Sum256([]byte(string(msg.Role) + "\n" + msg.Content))
		key := fmt.Sprintf("msg_%d_%s", i, msg.Role)
		layerHashes[key] = hex.EncodeToString(h[:])
		layerSizes[key] = len(msg.Content)
	}
	for i, td := range req.Tools {
		h := sha256.Sum256([]byte(td.Name + td.Description + string(td.Schema)))
		key := fmt.Sprintf("tooldef_%d_%s", i, td.Name)
		layerHashes[key] = hex.EncodeToString(h[:])
		layerSizes[key] = len(td.Description) + len(td.Schema)
	}
	providerErr := ""
	retries := 0
	if meta.Attempts > 1 {
		retries = meta.Attempts - 1
	}
	if err != nil {
		var perr *providers.ProviderError
		if e2, ok := err.(*providers.ProviderError); ok {
			perr = e2
		} else {
			// Unwrap once for gateway-wrapped errors.
			type unwrapper interface{ Unwrap() error }
			if uw, ok := err.(unwrapper); ok {
				if inner, ok := uw.Unwrap().(*providers.ProviderError); ok {
					perr = inner
				}
			}
		}
		if perr != nil {
			providerErr = string(perr.Kind)
		} else {
			providerErr = "unknown"
		}
	}
	model := req.Model
	if model == "" {
		model = m.settings.Model
	}
	effort := req.ReasoningEffort
	if effort == "" {
		effort = m.settings.ReasoningEffort
	}
	_ = m.run.Recorder.RecordModelCall(eval.ModelCall{
		Seq: m.run.modelSeq, Role: string(role), Model: model, Effort: effort,
		LayerHashes: layerHashes, LayerSizes: layerSizes,
		InputTokens: resp.Usage.Input, CacheRead: resp.Usage.CacheRead, CacheWrite: resp.Usage.CacheWrite,
		OutputTokens: resp.Usage.Output, ReasoningTok: resp.Usage.Reasoning,
		LatencySecs: latency.Seconds(), ProviderError: providerErr, Retries: retries,
	})
	// Budget consumption over time (incremental).
	_ = m.run.Recorder.RecordBudget(eval.BudgetPoint{TokensUsed: resp.Usage.Input + resp.Usage.Output, CostUSD: 0, Note: "model_call"})
	return resp, meta, err
}

// recordingToolRunner wraps the gated runner and logs one JSONL line
// per tool call with truncated inline output and a content-hashed blob.
type recordingToolRunner struct {
	base engine.ToolRunner
	run  *ExecRun
}

func (r *recordingToolRunner) Definition(name string) (tools.Definition, error) {
	return r.base.Definition(name)
}

func (r *recordingToolRunner) Execute(ctx context.Context, name string, args json.RawMessage, o tools.ExecuteOptions) (tools.Result, error) {
	start := time.Now()
	res, err := r.base.Execute(ctx, name, args, o)
	dur := time.Since(start)
	if r.run.Recorder == nil {
		return res, err
	}
	r.run.toolSeq++
	exitCode := 0
	if res.Status != tools.StatusOK {
		exitCode = 1
	}
	if err != nil {
		exitCode = 1
	}
	_ = r.run.Recorder.RecordToolCall(eval.ToolCall{
		Seq: r.run.toolSeq, Tool: name, Arguments: string(args),
		ExitCode: exitCode, DurationSec: dur.Seconds(),
		Truncated: res.Truncated, IsError: err != nil || res.Status != tools.StatusOK,
	}, res.Output)
	// Masking/compaction signals piggyback on tool history depth: the
	// engine emits them separately; here we only note large outputs.
	if res.Truncated {
		_ = r.run.Recorder.RecordMask(0, len(res.Output), "truncated tool output: "+name)
	}
	return res, err
}

// runEnginePath drives the real embedded engine loop with the full
// wired catalog (Sections 7.3, 13.3, 13.4): leases, shared history,
// LSP diagnostics, deferred tools through search_tools/call_tool,
// skill tools, vault redaction and the journal write-ahead rule. The
// scripted fake provider in tests (or the registry-built live
// provider in production, registered on gw with the Section 19.1
// role table) answers model calls; no step is deterministic. Every
// model turn and tool result is appended to the ATIF trajectory so
// the Appendix F records show the real execution. It returns the
// engine token usage for the metrics record.
func runEnginePath(ctx context.Context, opts ExecOptions, run *ExecRun, settings models.ModelSettings, gw *models.Gateway) (types.RunState, providers.Usage, error) {
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
		return "", providers.Usage{}, fmt.Errorf("cli: wire embedded catalog: %w", err)
	}
	if gw == nil {
		return "", providers.Usage{}, fmt.Errorf("cli: engine path needs a gateway")
	}
	baseRunner := engine.NewRegistryRunner(emb.Registry, emb.Exec)
	runner := newProfileGatedRunner(profile, baseRunner)
	if profile == ProfileEval {
		pruneDeferredToProfile(emb.Catalog, runner.allowed)
	}
	// The L3 task layer carries the request: without it the model
	// calls never see what to do (Section 6.5, 11.1). The fixed
	// instructions pin the executable-verification rule (P3) for
	// the embedded run.
	projectContext := ""
	if opts.ProjectDir != "" {
		projectContext = "project directory: " + opts.ProjectDir + "\n" +
			"Do the work in that directory (terminal workdir or absolute paths); " +
			"verify files there."
	}
	snap := func(ctx context.Context, erun *engine.Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{
			Role: "child", HarnessVersion: Version,
			ProjectContext: projectContext,
			Task: ctxcompiler.TaskView{
				OriginalRequest: opts.Request,
				FixedInstructions: []string{
					"Complete the request with the available tools.",
					"Verify the result by executing it (tests, compilers, scripts) before finishing.",
				},
			},
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
	effort := settings.ReasoningEffort
	if effort == "" {
		effort = models.DefaultDecisionsEffort
	}
	var modelEndpoint engine.ModelEndpoint = gw
	var toolRunner engine.ToolRunner = runner
	if run.Recorder != nil {
		modelEndpoint = &recordingModelEndpoint{base: gw, run: run, settings: settings}
		toolRunner = &recordingToolRunner{base: runner, run: run}
		_ = run.Recorder.RecordTransition("created", "executing", "engine admitted")
	}
	eng := engine.New(engine.Deps{
		Models: modelEndpoint,
		Context: &engine.DefaultBuilder{
			Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
			Snapshot: snap, ToolDefs: toolDefs, ReasoningEffort: effort,
		},
		Tools:          toolRunner,
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
	// Resource sampler: harness RSS/CPU/processes/OOM every 5 s,
	// incremental into resources.jsonl (Appendix F).
	stopSampler := make(chan struct{})
	if run.Recorder != nil {
		sampler := newResourceSampler()
		pid := os.Getpid()
		safego.Go("bench-sampler", nil, func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopSampler:
					return
				case <-ticker.C:
					sample, err := sampler.Sample(pid)
					if err != nil {
						continue
					}
					_ = run.Recorder.RecordResource(eval.ResourceSample{
						HarnessRSS: sample.RSSBytes, CPUSeconds: sample.CPUSeconds,
						Processes: sample.Processes, OOMEvents: sample.OOMKills,
					})
				}
			}
		})
	}
	if err := eng.Admit(ctx, erun); err != nil {
		close(stopSampler)
		return "", providers.Usage{}, err
	}
	if _, err := rec.RunStarted(ctx, map[string]string{"task": opts.Request}); err != nil {
		close(stopSampler)
		return "", providers.Usage{}, err
	}
	if err := eng.Start(ctx, erun); err != nil {
		close(stopSampler)
		return "", providers.Usage{}, err
	}
	if err := eng.Run(ctx, erun); err != nil {
		// An interrupt cancels the context without failing: the
		// orchestrator owns the resulting state.
		if ctx.Err() == nil {
			close(stopSampler)
			return "", providers.Usage{}, err
		}
	}
	close(stopSampler)
	usage := providers.Usage{
		Input:  int64(erun.Tracker.Used.InputTokens),
		Output: int64(erun.Tracker.Used.OutputTokens),
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
			return "", providers.Usage{}, err
		}
		if err := run.EmitStep(telemetry.ATIFStep{
			Role:       "tool",
			ToolResult: &telemetry.ATIFToolResult{CallID: ev.ToolResult.CallID, Name: ev.ToolResult.Name, Output: ev.Content},
		}, "tool_result"); err != nil {
			return "", providers.Usage{}, err
		}
	}
	if run.Recorder != nil {
		// Completion-gate attempts with criteria and evidence.
		for i := 0; i < erun.GateAttempts; i++ {
			_ = run.Recorder.RecordGate(eval.GateAttempt{Attempt: i + 1, Passed: erun.State() == types.RunCompleted, Note: rep.Summary})
		}
		if erun.GateAttempts == 0 {
			_ = run.Recorder.RecordGate(eval.GateAttempt{Attempt: 1, Passed: erun.State() == types.RunCompleted, Note: rep.Summary})
		}
		// Loop-detector signals.
		if erun.Loop != nil {
			for rule, n := range erun.Loop.Counts() {
				_ = run.Recorder.RecordLoop("observed", string(rule), fmt.Sprintf("count=%d", n))
			}
		}
		// Masking and compaction counters from the tracker-adjacent
		// engine state: at least one event each keeps killed-trial
		// analysis honest about whether they ever fired.
		_ = run.Recorder.RecordCompaction(0, len(erun.History), 0, "engine history length")
		// Final resource sample with runtime memory.
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		sampler := newResourceSampler()
		if sample, err := sampler.Sample(os.Getpid()); err == nil {
			_ = run.Recorder.RecordResource(eval.ResourceSample{
				HarnessRSS: sample.RSSBytes, CPUSeconds: sample.CPUSeconds,
				Processes: sample.Processes, OOMEvents: sample.OOMKills,
				ChildPeakRSS: 0, Note: "final",
			})
		} else {
			_ = run.Recorder.RecordResource(eval.ResourceSample{HarnessRSS: mem.Alloc, Note: "final-fallback"})
		}
		_ = run.Recorder.RecordTransition("executing", string(erun.State()), rep.Summary)
	}
	summary := rep.Summary
	if summary == "" {
		summary = "completed: " + shortSummary(opts.Request)
	}
	if err := run.EmitStep(telemetry.ATIFStep{Role: "assistant", Content: summary}, "message"); err != nil {
		return "", providers.Usage{}, err
	}
	return erun.State(), usage, nil
}
