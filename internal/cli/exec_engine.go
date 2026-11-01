package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/envsnap"
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/repoexplore"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
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

// compactSkillsCatalog reports whether exec renders the L1 skill
// catalog in compact (names-only) form: EXCALIBUR_SKILLS_COMPACT=1
// or =true. The default full form matches the Section 12.3 grouped
// catalog contract.
func compactSkillsCatalog() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_SKILLS_COMPACT")))
	return v == "1" || v == "true"
}

// ctxConfigForRun builds the compiler config for one embedded run
// from the layered config (owner decision 0051): the shipped
// defaults carry the full-window table (reference row plus
// conservative fallback), the layered [context] values overlay it,
// per-model rows overlay by lowercased id, and
// EXCALIBUR_CONTEXT_CAP wins when positive.
func ctxConfigForRun(layer config.Config) ctxcompiler.Config {
	cfg := ctxcompiler.DefaultConfig()
	lc := layer.Context
	if lc.CompactionCeilingTokens != 0 {
		cfg.CompactionCeilingTokens = lc.CompactionCeilingTokens
	}
	if lc.DefaultContextWindow > 0 {
		cfg.DefaultContextWindow = lc.DefaultContextWindow
	}
	if lc.DefaultMaxInput > 0 {
		cfg.DefaultMaxInput = lc.DefaultMaxInput
	}
	if lc.DefaultMaxOutput > 0 {
		cfg.DefaultMaxOutput = lc.DefaultMaxOutput
	}
	if lc.CompactionReserveTokens > 0 {
		cfg.CompactionReserveTokens = lc.CompactionReserveTokens
	}
	if lc.PostCompactionLiteralTokens > 0 {
		cfg.PostCompactionLiteralTokens = lc.PostCompactionLiteralTokens
	}
	if lc.LiteralObservations > 0 {
		cfg.LiteralObservations = lc.LiteralObservations
	}
	if lc.MaskTriggerTokens > 0 {
		cfg.MaskTriggerTokens = lc.MaskTriggerTokens
	}
	if lc.MaskTargetTokens > 0 {
		cfg.MaskTargetTokens = lc.MaskTargetTokens
	}
	if lc.MasterHistoryTokens > 0 {
		cfg.MasterHistoryTokens = lc.MasterHistoryTokens
	}
	if lc.MasterTargetMin > 0 {
		cfg.MasterTargetMin = lc.MasterTargetMin
	}
	if lc.MasterTargetMax > 0 {
		cfg.MasterTargetMax = lc.MasterTargetMax
	}
	if lc.ChildTargetMax > 0 {
		cfg.ChildTargetMax = lc.ChildTargetMax
	}
	if lc.ParentTargetMax > 0 {
		cfg.ParentTargetMax = lc.ParentTargetMax
	}
	if len(lc.ModelLimits) > 0 {
		if cfg.ModelLimits == nil {
			cfg.ModelLimits = map[string]ctxcompiler.ModelLimits{}
		}
		for k, v := range lc.ModelLimits {
			key := strings.ToLower(strings.TrimSpace(k))
			if key == "" {
				continue
			}
			cfg.ModelLimits[key] = ctxcompiler.ModelLimits{
				ContextWindow: v.ContextWindow,
				MaxInput:      v.MaxInput,
				MaxOutput:     v.MaxOutput,
			}
		}
	}
	if envCap := ctxcompiler.ContextCapFromEnv(nil); envCap > 0 {
		cfg.CompactionCeilingTokens = envCap
	}
	return cfg
}

// skillNames projects suggested skills to their names for events.
func skillNames(sugs []ctxcompiler.SkillView) []string {
	out := make([]string, 0, len(sugs))
	for _, s := range sugs {
		out = append(out, s.Name)
	}
	return out
}

// writeSkillsRecord persists the skills-library ablation record for
// the run (mode, catalog hash, suggestions) next to the other trial
// artifacts. A missing dir skips the write; failures never fail the
// run.
func writeSkillsRecord(dir string, mode skills.LibraryMode, compact bool, ix *skills.Index, sugs []ctxcompiler.SkillView) {
	if dir == "" || ix == nil {
		return
	}
	type suggestion struct {
		Name string `json:"name"`
		Why  string `json:"why"`
	}
	rec := map[string]any{
		"mode": string(mode), "compact": compact,
		"catalog_hash": ix.CatalogHash(), "catalog_skills": len(ix.Names()),
	}
	list := make([]suggestion, 0, len(sugs))
	for _, s := range sugs {
		list = append(list, suggestion{Name: s.Name, Why: s.Why})
	}
	rec["suggested"] = list
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(dir+"/skills.json", append(buf, '\n'), 0o644)
}

// writeExploreRecord persists the exploration ablation record for
// the run (switch, workdir, brief lines, truncation) next to the
// other trial artifacts. A missing dir skips the write; failures
// never fail the run.
func writeExploreRecord(dir string, cfg repoexplore.Config, brief repoexplore.Brief) {
	if dir == "" {
		return
	}
	rec := map[string]any{
		"enabled": true, "workdir": brief.Workdir,
		"lines": len(brief.Lines), "truncated": brief.Truncated, "empty": brief.Empty,
		"timeout_secs": cfg.Timeout.Seconds(), "max_files": cfg.MaxFiles,
		"max_depth": cfg.MaxDepth, "max_lines": cfg.MaxBriefLines,
		"brief": brief.Lines,
	}
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(dir+"/explore.json", append(buf, '\n'), 0o644)
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
	// Appendix F accounting lives here (not in the post-hoc aggregate
	// in RunExec): every model call, successful or failed, lands in
	// the task metrics with its real latency, and every gateway retry
	// lands in the retry counter. The post-hoc path skips recorded
	// runs so nothing is double-counted.
	model := req.Model
	if model == "" {
		model = m.settings.Model
	}
	if m.run.Metrics != nil {
		m.run.Metrics.ObserveModel(role, model, resp.Usage, telemetry.PriceTable{}, latency)
		for i := 1; i < meta.Attempts; i++ {
			m.run.Metrics.ObserveEfficiency(false, false, false, true, false, 0)
		}
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

// historyToolRunner appends one summary per tool call to the
// embedded run-local history behind history_search, so the model can
// retrieve earlier calls without a memory service (single-run scope).
type historyToolRunner struct {
	base engine.ToolRunner
	log  *wireengine.HistoryLog
}

func (r *historyToolRunner) Definition(name string) (tools.Definition, error) {
	return r.base.Definition(name)
}

func (r *historyToolRunner) Execute(ctx context.Context, name string, args json.RawMessage, o tools.ExecuteOptions) (tools.Result, error) {
	res, err := r.base.Execute(ctx, name, args, o)
	if r.log != nil {
		out := res.Output
		if err != nil {
			out = "error: " + err.Error()
		}
		r.log.Append(name, string(args), out)
	}
	return res, err
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
	// Appendix F time split and per-tool call counts: the engine has
	// no tool-level retry, so every execution is a first attempt.
	if r.run.Metrics != nil {
		r.run.Metrics.ObserveTool(name, dur, false)
	}
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
func runEnginePath(ctx context.Context, opts ExecOptions, run *ExecRun, settings models.ModelSettings, gw *models.Gateway, layerCfg config.Config) (types.RunState, providers.Usage, error) {
	profile := opts.Profile
	if profile == "" {
		profile = ProfileDefault
	}
	store := opts.JournalStore
	if store == nil {
		store = journal.NewMemStore()
	}
	// Skills library (Section 12, internal/skills owns the
	// catalog, ranking and the EXCALIBUR_SKILLS switch): off keeps
	// the historical behavior (no index, skill-tool stubs unwired),
	// on wires the L1 catalog with working search_skills/load_skill
	// but no injected suggestions, and auto (default) adds the
	// task-start auto-suggest from the instruction plus
	// project/PATH environment signals. Everything lands in the
	// records (skills.json, skills_wired event) for the ablation.
	skillsMode := skills.LibraryModeFromEnv(nil)
	skillsCompact := compactSkillsCatalog()
	ctxCfg := ctxConfigForRun(layerCfg)
	effWindow := ctxcompiler.LimitsForModel(settings.Model, ctxCfg).ContextWindow
	if effWindow <= 0 {
		effWindow = 200000
	}
	var skillsIndex *skills.Index
	var skillCatalog string
	var suggestedSkills []ctxcompiler.SkillView
	if skillsMode.CatalogEnabled() {
		skillsIndex = skills.NewBuiltinIndex()
		if err := skillsIndex.Scan(); err != nil {
			return "", providers.Usage{}, fmt.Errorf("cli: scan skills catalog: %w", err)
		}
		skillCatalog = skillsIndex.RenderCatalogForMode(skills.CatalogConfig{
			WindowTokens: effWindow, Pertinent: skills.DefaultPertinentForRole("programming"), Compact: skillsCompact,
		}, skillsMode)
		env := skills.CollectEnvSignals(opts.ProjectDir, exec.LookPath)
		for _, s := range skillsIndex.SuggestWithEnvForMode(opts.Request, env, settings.Model, ctxCfg.SuggestedSkillsMax, skillsMode) {
			suggestedSkills = append(suggestedSkills, ctxcompiler.SkillView{Name: s.Name, Why: s.Why})
		}
		writeSkillsRecord(run.Dir, skillsMode, skillsCompact, skillsIndex, suggestedSkills)
		_ = run.EmitEvent("skills_wired", map[string]any{
			"mode": string(skillsMode), "compact": skillsCompact,
			"catalog_hash": skillsIndex.CatalogHash(), "suggested": skillNames(suggestedSkills),
		})
	}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{
		RunID: string(run.RunID), Project: opts.ProjectDir, Vault: opts.Vault,
		Delegated: profile == ProfileDelegate,
		Skills:    skillsIndex,
	})
	if err != nil {
		return "", providers.Usage{}, fmt.Errorf("cli: wire embedded catalog: %w", err)
	}
	if gw == nil {
		return "", providers.Usage{}, fmt.Errorf("cli: engine path needs a gateway")
	}
	baseRunner := engine.NewRegistryRunner(emb.Registry, emb.Exec)
	var toolBase engine.ToolRunner = baseRunner
	if emb.Surface != nil && emb.Surface.History != nil {
		toolBase = &historyToolRunner{base: toolBase, log: emb.Surface.History}
	}
	runner := newProfileGatedRunner(profile, toolBase)
	if profile == ProfileEval {
		pruneDeferredToProfile(emb.Catalog, runner.allowed)
	}
	// The L3 task layer carries the request: without it the model
	// calls never see what to do (Section 6.5, 11.1). The fixed
	// instructions pin the executable-verification rule (P3) for the
	// embedded run.
	projectContext := ""
	if opts.ProjectDir != "" {
		projectContext = "project directory: " + opts.ProjectDir + "\n" +
			"Do the work in that directory (terminal workdir or absolute paths); " +
			"verify files there."
	}
	// Proactive environment snapshot (Section 13.5, Meta-Harness):
	// collected once per run and rendered into the stable L2 scope
	// layer, so the model stops spending tool calls discovering the
	// environment. Disabled by EXCALIBUR_ENV_SNAPSHOT=0.
	envCfg := envsnap.ConfigFromEnv(envsnap.Config{
		Workdir: opts.ProjectDir, ProbeHost: probeHost(settings.BaseURL),
	})
	envProvider := envsnap.NewCached(envCfg)
	if envCfg.Enabled {
		_ = envProvider.Snapshot(ctx)
		if rendered := envProvider.RenderCompact(); rendered != "" {
			if projectContext != "" {
				projectContext = rendered + "\n" + projectContext
			} else {
				projectContext = rendered
			}
		}
	}
	// Exploration before acting (agent quality, Sections 7.3, 11.1,
	// 15.2 explorer profile): a lightweight read-only pass over the
	// workdir inspects layout, build files, entry points, tests,
	// configs, versions and data files relevant to the instruction
	// and pins a compact brief (<=60 lines) into the L3 working
	// notes. Collected once per run so the L0-L2 prefix never
	// churns; the brief carries no timestamps. Default off until
	// measured (EXCALIBUR_EXPLORE=on to enable).
	exploreCfg := repoexplore.ConfigFromEnv(repoexplore.Config{Workdir: opts.ProjectDir})
	var exploreNote string
	var exploreBrief repoexplore.Brief
	if exploreCfg.Enabled && opts.ProjectDir != "" {
		collector := repoexplore.NewCollector(exploreCfg)
		exploreBrief = collector.Collect(ctx, opts.ProjectDir, opts.Request)
		exploreNote = exploreBrief.Render()
		writeExploreRecord(run.Dir, exploreCfg, exploreBrief)
		_ = run.EmitEvent("explore_brief", map[string]any{
			"enabled": true, "workdir": opts.ProjectDir,
			"lines": len(exploreBrief.Lines), "truncated": exploreBrief.Truncated,
		})
	}
	fixedInstructions := []string{
		"Complete the request with the available tools.",
		"Verify the result by executing it (tests, compilers, scripts) before finishing.",
	}
	// Time and session discipline (Sections 7.3, 13.5): the hook
	// below shows the remaining budget on every call; these lines
	// teach the model to spend it well. Gated by
	// EXCALIBUR_TIME_BUDGET with the rest of the time plumbing.
	timeBudget := engine.TimeBudgetEnabled()
	if timeBudget {
		fixedInstructions = append(fixedInstructions,
			"Budget your time: every step shows the remaining budget; when little remains, stop starting new work, verify what exists and report.",
			"Run long builds, servers and watchers in background terminal sessions and poll them with exec_io wait_for instead of busy-polling; never wait on interactive prompts: prefer non-interactive flags (-y, --no-input, --yes) and avoid pagers or editors.",
		)
	}
	snap := func(ctx context.Context, erun *engine.Run) (ctxcompiler.Snapshot, error) {
		var workingNotes []string
		if exploreNote != "" {
			// One note slot carries the whole brief (embedded
			// newlines): the WorkingNotes cap counts notes, not
			// lines, so the brief survives intact and stays
			// byte-identical across steps (stable L3 task layer).
			workingNotes = []string{exploreNote}
		}
		return ctxcompiler.Snapshot{
			Role: "child", HarnessVersion: Version,
			ProjectContext: projectContext,
			SkillCatalog:   skillCatalog,
			Task: ctxcompiler.TaskView{
				OriginalRequest:   opts.Request,
				FixedInstructions: fixedInstructions,
				SuggestedSkills:   suggestedSkills,
				WorkingNotes:      workingNotes,
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
			// Idle-tool pruning (agent quality): ack_directive
			// while no directive is pending and clock under eval
			// stay out of the prefix. Both remain executable, and
			// the switches restore the legacy prefix.
			if pruneFromPrefix(profile, emb, name) {
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
			Config: ctxCfg, Counter: ctxcompiler.NewEstimator(0),
			Snapshot: snap, ToolDefs: toolDefs, ReasoningEffort: effort,
			ModelID: settings.Model,
			// Remaining-budget line in L5 (Section 18.4): compact
			// steps/tools/time/cost left, every call. Empty when
			// nothing is capped, so uncapped runs add no bytes.
			Budget: func(erun *engine.Run) string {
				if !engine.TimeBudgetEnabled() {
					return ""
				}
				return erun.BudgetLine(time.Now().UTC())
			},
			// Full-window context control (11.3-11.4, owner
			// decision 0051): masking and compaction run inside
			// BuildRequest on the embedded path. Pins come from
			// the run-local store; batches and compactions land
			// in masking.jsonl / compaction.jsonl.
			Pins: func() []string {
				if emb.Surface == nil || emb.Surface.Pins == nil {
					return nil
				}
				return emb.Surface.Pins.List()
			},
			OnMask: func(batch, dropped int, note string) {
				if run.Recorder == nil {
					return
				}
				_ = run.Recorder.RecordMask(batch, dropped, note)
			},
			OnCompact: func(from, to, kept int, note string) {
				if run.Recorder == nil {
					return
				}
				_ = run.Recorder.RecordCompaction(from, to, kept, note)
			},
		},
		Tools:          toolRunner,
		LeaseHeartbeat: wireengine.LeaseHeartbeatHook(emb.Leases),
		LeaseRelease:   wireengine.LeaseReleaseHook(emb.Leases),
	})
	// Effective wall budget (Sections 7.3, 18.4, 23.2): the explicit
	// --timeout first, else 90% of the Harbor/operator override, so
	// the tracker trips closing mode below 10% and pauses with the
	// budget motive at zero instead of dying on the context edge.
	erun := engine.NewRun(string(run.RunID), string(run.TaskID), engine.Profile{
		Name: "child", Role: types.RoleExecutor, ContextRole: "child",
		Limit: tasks.Limit{MaxSteps: 25},
	})
	// Resolve relative artifact paths against the run project
	// directory for the required-artifact completion gate.
	erun.Workdir = opts.ProjectDir
	if maxSecs := engine.ResolveMaxSeconds(opts.TimeoutSecs); maxSecs > 0 {
		erun.Profile.Limit.MaxSeconds = maxSecs
		erun.Tracker.Limit.MaxSeconds = maxSecs
		// An operator-derived budget (no explicit flag) still bounds
		// the wall clock: the context enforces the edge while the
		// tracker owns closing mode and the honest paused report.
		if opts.TimeoutSecs == 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(maxSecs)*time.Second)
			defer cancel()
		}
	}
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
	// incremental into resources.jsonl (Appendix F), with every sample
	// also feeding the task metrics as a peak (cumulative /proc CPU
	// counters keep the max, never the sum). The first sample is taken
	// synchronously so short trials still record a peak instead of
	// zero; inside a task container the harness IS the container
	// workload, so its peak is the container signal.
	stopSampler := make(chan struct{})
	if run.Recorder != nil {
		sampler := newResourceSampler()
		pid := os.Getpid()
		observe := func() {
			sample, err := sampler.Sample(pid)
			if err != nil {
				return
			}
			if run.Metrics != nil {
				run.Metrics.ObserveDaemonPeak(sample.RSSBytes, sample.CPUSeconds)
			}
			_ = run.Recorder.RecordResource(eval.ResourceSample{
				HarnessRSS: sample.RSSBytes, CPUSeconds: sample.CPUSeconds,
				Processes: sample.Processes, OOMEvents: sample.OOMKills,
			})
		}
		observe()
		safego.Go("bench-sampler", nil, func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopSampler:
					return
				case <-ticker.C:
					observe()
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
	// Appendix F efficiency counters that the engine owns: gate
	// evaluations and loop detections, synced into the task metrics
	// so metrics.json carries them even when the JSONL fallback is
	// what the index reads for older records.
	if run.Metrics != nil {
		run.Metrics.AddGateAttempts(erun.GateAttempts)
		if erun.Loop != nil {
			for _, n := range erun.Loop.Counts() {
				run.Metrics.AddLoopDetections(n)
			}
		}
	}
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
		// Final resource sample with runtime memory. The live sample
		// also feeds the metrics peak so the final metrics.json row
		// carries the observed peak even when no tick ever fired.
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		sampler := newResourceSampler()
		if sample, err := sampler.Sample(os.Getpid()); err == nil {
			if run.Metrics != nil {
				run.Metrics.ObserveDaemonPeak(sample.RSSBytes, sample.CPUSeconds)
			}
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

// probeHost extracts the host:port dialed by the env snapshot's
// reachability probe from the provider base URL. Empty when the
// endpoint is unknown, which records reachability as unknown rather
// than guessing. No request is ever sent; the probe only dials TCP.
func probeHost(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}
