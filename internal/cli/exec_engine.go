package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
// `finish` is a harness control tool, not a benchmark tool: it is
// always allowed when the explicit-finish switch is on, and the
// frozen table never lists it so the profile hash never moves.
func (g *profileGatedRunner) check(name string, args json.RawMessage) error {
	if name == engine.FinishToolName && engine.ExplicitFinishSwitchOn() {
		return nil
	}
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
	if name == "call_tool" {
		// Check the superset of repairable shapes even when argument
		// normalization is disabled: CallToolTool can promote names directly.
		normalized := tools.NormalizeArgs(name, args, tools.DefaultSurfaceTuning())
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(normalized, &call) == nil && call.Name != "" && call.Name != "call_tool" {
			if err := g.check(call.Name, call.Arguments); err != nil {
				return err
			}
		}
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
// search_tools only indexes deferred entries. The runner gate also
// checks call_tool targets so hidden tools stay outside the profile.
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
// the run (mode, catalog hash, suggestions, error-cards switch)
// next to the other trial artifacts. A missing dir skips the write;
// failures never fail the run.
func writeSkillsRecord(dir string, mode skills.LibraryMode, compact bool, errorCards bool, ix *skills.Index, sugs []ctxcompiler.SkillView) {
	if dir == "" || ix == nil {
		return
	}
	type suggestion struct {
		Name string `json:"name"`
		Why  string `json:"why"`
	}
	rec := map[string]any{
		"mode": string(mode), "compact": compact, "error_cards": errorCards,
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

// writeLongHorizonRecord persists the long-horizon ablation record for
// the run (switch, milestone plan, continuation trips) next to the
// other trial artifacts. A missing dir skips the write; failures never
// fail the run.
func writeLongHorizonRecord(dir string, cfg tasks.LongHorizonConfig, plan []tasks.LHMilestone, trips int, closing, large bool) {
	if dir == "" {
		return
	}
	titles := make([]string, 0, len(plan))
	for _, m := range plan {
		titles = append(titles, m.ID+" "+m.Title)
	}
	rec := map[string]any{
		"enabled": cfg.Enable, "continuation": cfg.EnableContinuation,
		"milestones": cfg.EnableMilestones, "pacing": cfg.EnablePacing,
		"background": cfg.EnableBackgroundNudge,
		"max_cont":   cfg.MaxContinuations, "every": cfg.MilestoneEvery,
		"large": large, "plan": titles, "trips": trips, "closing": closing,
	}
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(dir+"/longhorizon.json", append(buf, '\n'), 0o644)
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
	if cachePayloadTelemetryEnabled() {
		account := func(key string, value any) {
			raw, _ := json.Marshal(value)
			digest := sha256.Sum256(raw)
			layerHashes[key] = hex.EncodeToString(digest[:])
			layerSizes[key] = len(raw)
		}
		for i, msg := range req.Messages {
			account(fmt.Sprintf("msg_%d_%s", i, msg.Role), msg)
			if len(msg.ToolCalls) > 0 {
				account(fmt.Sprintf("msg_%d_tool_calls", i), msg.ToolCalls)
			}
			if msg.ToolResult != nil {
				account(fmt.Sprintf("msg_%d_tool_result", i), msg.ToolResult)
			}
		}
		account("request_payload", req)
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
			if errors.Is(err, models.ErrProviderQuotaExhausted) {
				providerErr = "provider_quota_exhausted"
			}
		} else {
			providerErr = "unknown"
		}
	}
	effort := req.ReasoningEffort
	if effort == "" {
		effort = m.settings.ReasoningEffort
	}
	// The current adapter contract has no raw finish reason. Record that
	// absence explicitly; truncation is inferred only from its typed error.
	finishReason := "unavailable"
	if providerErr == string(providers.KindTruncated) {
		finishReason = "length"
	}
	callSite := engine.ModelCallSite(ctx)
	if callSite == "" {
		callSite = "model." + string(role)
	}
	_ = m.run.Recorder.RecordModelCall(eval.ModelCall{
		ProviderReason: models.ProviderErrorReason(err), CredentialSwitches: meta.CredentialSwitches,
		MaxOutputTokens: req.MaxTokens, VerifierVerdict: tasks.VerifierVerdictEnabled(),
		CallSite: callSite, FinishReason: finishReason, FinishReasonSource: "inferred_from_contract", UsageAvailable: resp.Usage != (providers.Usage{}),
		Seq: m.run.modelSeq, Role: string(role), Model: model, Effort: effort,
		LayerHashes: layerHashes, LayerSizes: layerSizes,
		InputTokens: resp.Usage.Input, CacheRead: resp.Usage.CacheRead, CacheWrite: resp.Usage.CacheWrite,
		OutputTokens: resp.Usage.Output, ReasoningTok: resp.Usage.Reasoning,
		LatencySecs: latency.Seconds(), ProviderError: providerErr, Retries: retries,
		RetryWaitCount: meta.RetryWaitCount, RetryWaitSeconds: meta.RetryWaitSeconds,
	})
	// Budget consumption over time (incremental).
	_ = m.run.Recorder.RecordBudget(eval.BudgetPoint{TokensUsed: resp.Usage.Input + resp.Usage.Output, CostUSD: 0, WallSource: m.run.wallSource, Note: "model_call"})
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
	// exec reports the real shell exit (128+n for signals) in ExitCode.
	if res.ExitCode != nil {
		exitCode = *res.ExitCode
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
func runEnginePath(ctx context.Context, opts ExecOptions, run *ExecRun, settings models.ModelSettings, gw *models.Gateway, layerCfg config.Config) (types.RunState, providers.Usage, string, error) {
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
	// but no injected suggestions, auto (legacy default) adds the
	// task-start auto-suggest from the instruction plus
	// project/PATH environment signals, and cards (the default)
	// wires working search/load with nothing injected: the model
	// pulls short cards on demand and failed tool results gain one
	// error-triggered card excerpt (EXCALIBUR_SKILLS_ERROR_CARDS=0
	// disables the excerpts). Everything lands in the records
	// (skills.json, skills_wired event) for the ablation.
	skillsMode := skills.LibraryModeFromEnv(nil)
	skillsCompact := compactSkillsCatalog()
	skillsErrorCards := skills.ErrorCardsFromEnv(nil)
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
		if profile == ProfileEval {
			skillsIndex = skills.NewEvalBuiltinIndex()
		}
		if err := skillsIndex.Scan(); err != nil {
			return "", providers.Usage{}, "", fmt.Errorf("cli: scan skills catalog: %w", err)
		}
		skillCatalog = skillsIndex.RenderCatalogForMode(skills.CatalogConfig{
			WindowTokens: effWindow, Pertinent: skills.DefaultPertinentForRole("programming"), Compact: skillsCompact,
		}, skillsMode)
		// Cards mode starts with a pull pointer. Matching tool intent may
		// add one focused hint after the deferred catalog is wired.
		if skillsMode.CardsEnabled() {
			skillCatalog = skills.CardsPointer()
		}
		env := skills.CollectEnvSignals(opts.ProjectDir, exec.LookPath)
		for _, s := range skillsIndex.SuggestWithEnvForMode(opts.Request, env, settings.Model, ctxCfg.SuggestedSkillsMax, skillsMode) {
			suggestedSkills = append(suggestedSkills, ctxcompiler.SkillView{Name: s.Name, Why: s.Why})
		}
	}
	var media *tools.MediaDeps
	if profile != ProfileEval && settings.Provider == "openai" && tools.VisionOCREnabled() {
		visionProvider, visionErr := BuildProvider(ctx, settings, VaultSecretResolver(opts.Vault, string(run.RunID)))
		if visionErr == nil {
			defer visionProvider.Close()
			media, visionErr = wireengine.ModelVisionMedia(visionProvider, settings.GatewayConfig(layerCfg.Provider), strings.TrimSpace(os.Getenv("EXCALIBUR_VISION_MODEL")))
		}
		if visionErr != nil {
			fmt.Fprintf(os.Stderr, "exec: vision unavailable: %v\n", visionErr)
		}
	}
	webRuntime, err := embeddedBrowserWeb(profile, opts, run, layerCfg)
	if err != nil {
		return "", providers.Usage{}, "", err
	}
	defer func() { _ = webRuntime.Close() }()
	var web *tools.WebDeps
	if webRuntime != nil {
		web = webRuntime.Web
	}
	reminders, closeReminders, err := embeddedReminders(ctx, profile)
	if err != nil {
		return "", providers.Usage{}, "", fmt.Errorf("cli: open reminders: %w", err)
	}
	defer closeReminders()
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{
		Reminders: reminders,
		Profile:   string(profile),
		Media:     media,
		Web:       web,
		RunID:     string(run.RunID), Project: opts.ProjectDir, Vault: opts.Vault,
		Delegated:    profile == ProfileDelegate,
		Autonomous:   true, // This entry point runs non-interactive exec/delegate/eval work.
		KnownSecrets: runCredentialValues(ctx, settings, opts.Vault, string(run.RunID)),
		Skills:       skillsIndex,
		Workdir:      opts.ProjectDir,
	})
	if err != nil {
		return "", providers.Usage{}, "", fmt.Errorf("cli: wire embedded catalog: %w", err)
	}
	defer emb.Close()
	if webRuntime != nil {
		if err := wireengine.WireBrowser(emb, embeddedBrowserCapability(webRuntime)); err != nil {
			return "", providers.Usage{}, "", err
		}
	}
	if gw == nil {
		return "", providers.Usage{}, "", fmt.Errorf("cli: engine path needs a gateway")
	}
	var restoreRun *engine.Run
	if tools.InputRestoreEnabled() {
		if err := emb.Registry.Register(&tools.RestoreInputTool{Restore: func(path, destination string) error { return restoreRun.RestoreInput(path, destination) }}); err != nil {
			return "", providers.Usage{}, "", err
		}
	}
	baseRunner := engine.NewRegistryRunner(emb.Registry, emb.Exec)
	var toolBase engine.ToolRunner = baseRunner
	// Best-of-N attempts map their paths onto their own copy before the
	// tool runs (nil outside attempts). Recording wraps this, so the
	// records keep the arguments the model sent.
	toolBase = attemptPathRunnerFor(toolBase, opts.attemptMap)
	if emb.Surface != nil && emb.Surface.History != nil {
		toolBase = &historyToolRunner{base: toolBase, log: emb.Surface.History}
	}
	runner := newProfileGatedRunner(profile, toolBase)
	if tools.InputRestoreEnabled() {
		runner.allowed["restore_input"] = true
	}
	if profile == ProfileEval {
		pruneDeferredToProfile(emb.Catalog, runner.allowed)
	}
	activation := wireengine.SuggestTools(emb.Catalog, skillsIndex, opts.Request, settings.Model, string(profile))
	for _, hint := range activation.Skills {
		// Keep a single hint when auto mode already suggested the same skill.
		found := false
		for i := range suggestedSkills {
			if suggestedSkills[i].Name == hint.Name {
				suggestedSkills[i] = hint
				found = true
				break
			}
		}
		if !found {
			suggestedSkills = append([]ctxcompiler.SkillView{hint}, suggestedSkills...)
		}
	}
	if len(suggestedSkills) > skills.DefaultSuggestionsMax {
		suggestedSkills = suggestedSkills[:skills.DefaultSuggestionsMax]
	}
	if skillsIndex != nil {
		writeSkillsRecord(run.Dir, skillsMode, skillsCompact, skillsErrorCards, skillsIndex, suggestedSkills)
		_ = run.EmitEvent("skills_wired", map[string]any{
			"mode": string(skillsMode), "compact": skillsCompact,
			"error_cards":  skillsErrorCards,
			"catalog_hash": skillsIndex.CatalogHash(), "suggested": skillNames(suggestedSkills),
		})
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
	// Explicit finish protocol (agent quality): the run ends ONLY
	// through a `finish` call with summary and evidence; text-only
	// turns never end it. Gated by EXCALIBUR_EXPLICIT_FINISH
	// (default on); off restores the legacy text-finish behaviour.
	explicitFinish := engine.ExplicitFinishSwitchOn()
	if explicitFinish {
		fixedInstructions = append(fixedInstructions,
			"When everything is verified, end the run with the `finish` tool: summary says what was done, evidence lists the commands run and their results proving each requirement.",
			"Never stop with a text-only answer: without a `finish` call the run continues. Use status blocked with a concrete reason only when genuinely stuck.",
		)
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
	// Wall-clock-only binding (W8): closing mode must leave the best
	// deliverable in place and call `finish`. Gated by
	// EXCALIBUR_TIME_BUDGET_ONLY (default ON for embedded exec).
	if engine.TimeBudgetOnlyEnabled(true) {
		fixedInstructions = append(fixedInstructions,
			"Closing mode: when time is almost exhausted, leave the best deliverable in place, verify what exists quickly, and call `finish` with status completed and evidence now. Do not start new work. Never end blocked in closing; never end paused except on the hard wall clock.",
		)
	}
	// Strict blocked policy (W8 plus W10 run-end fixes):
	// `finish status=blocked` needs deterministic external evidence
	// while time remains; closing mode never blocks.
	if engine.StrictBlockedEnabled() {
		fixedInstructions = append(fixedInstructions,
			"Blocked policy: call `finish` with status blocked only for external impossibilities (required network resource unreachable/404 after retries, required file/tool provably absent and not installable, explicit permission denial). Validation failures are never a valid blocked reason while time remains: fix and verify instead. Missing reference solutions, coverage targets or verifier maps are never valid blocked reasons. Killing or stopping a process the task requires is a failure, not a blocked reason: restart it, verify it serves, and keep working. In closing mode (time almost exhausted) never declare blocked: leave the best deliverable in place and call `finish` with status completed and evidence.",
		)
	}
	// Run-end services (W10): background sessions detach and survive.
	// Gated by EXCALIBUR_DETACH_BACKGROUND (default ON).
	if tools.DetachBackgroundEnabled() {
		fixedInstructions = append(fixedInstructions,
			"Run-end services: background:true sessions detach into their own session and keep running after the run ends (stdin from /dev/null, output to a log under /tmp). Services the instruction requires (servers, indexes, databases, VMs, daemons) must be left running and verified (curl the port, run the client check) before finishing; declare keep_running in the finish evidence. Stop only temporary helpers with exec_io close/signal. Never kill a required service to finish faster.",
		)
	}
	// Long-horizon persistence (EXCALIBUR_LONG_HORIZON, default on):
	// the pacing line shows elapsed/remaining budget every step, the
	// milestone plan orders large work, and long commands belong in
	// background sessions. The switch keeps the ablation baseline
	// byte-identical when off.
	if tasks.DefaultLongHorizonConfig().Enable {
		fixedInstructions = append(fixedInstructions,
			"Work long tasks to completion: every step shows a pacing line (elapsed, steps/time left, milestone progress); pace yourself against it and do not finish while significant budget remains and evidence is thin.",
			"Follow the milestone plan in order when one is given (explore, implement each item, verify end to end, report); verify each milestone with an executable check before moving on.",
			"Run long commands with background:true and poll with exec_io wait_for/read; never finish while a session is still alive and unverified — poll it, verify the service, then stop it or declare it intentionally persistent (keep_running). Required services stay running after the run; killing them is a failure.",
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
	// The profile core and the bounded request-selected deferred tools ride
	// the model request in order and remain fixed for this run. Other
	// deferred tools resolve through search_tools (11.6, 13.3).
	// Under eval the surface is the frozen Section 23.2 table: the
	// gate rejects anything else even if the model asks for it.
	allowed := runner.allowed
	toolDefs := func(erun *engine.Run) []providers.ToolDefinition {
		var out []providers.ToolDefinition
		for _, name := range emb.Catalog.CoreNames() {
			// `finish` rides the prefix only when the protocol is
			// on; off restores the legacy prefix byte-for-byte.
			if name == engine.FinishToolName && !explicitFinish {
				continue
			}
			if profile == ProfileEval && !allowed[name] && name != engine.FinishToolName {
				continue
			}
			// Idle-tool pruning (agent quality): ack_directive
			// while no directive is pending and clock under eval
			// stay out of the prefix. Both remain executable, and
			// the switches restore the legacy prefix. `finish`
			// is never pruned: the protocol needs it visible.
			if name != engine.FinishToolName && pruneFromPrefix(profile, emb, name) {
				continue
			}
			tool, err := emb.Registry.Get(name)
			if err != nil {
				continue
			}
			def := tool.Definition()
			out = append(out, providers.ToolDefinition{Name: def.Name, Description: def.Description, Schema: def.InputSchema})
		}
		for _, def := range activation.Definitions {
			out = append(out, providers.ToolDefinition{Name: def.Name, Description: def.Description, Schema: def.InputSchema})
		}
		if tools.InputRestoreEnabled() {
			tool, _ := emb.Registry.Get("restore_input")
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
	// Cards mode appends one error-triggered skill card to failed
	// tool results (inside the recorder, so the blob shows exactly
	// when the mechanism fired). Other modes keep the legacy bytes.
	if skillsMode.CardsEnabled() {
		toolRunner = &skillCardsRunner{base: runner, index: skillsIndex, errorCards: skillsErrorCards}
	}
	if run.Recorder != nil {
		modelEndpoint = &recordingModelEndpoint{base: gw, run: run, settings: settings}
		toolRunner = &recordingToolRunner{base: toolRunner, run: run}
		_ = run.Recorder.RecordTransition("created", "executing", "engine admitted")
	}
	eng := engine.New(engine.Deps{
		ActiveSessions: func() []tools.SessionInfo {
			if emb.Sessions == nil {
				return nil
			}
			return emb.Sessions.ActiveSessions()
		},
		Models: modelEndpoint,
		Context: &engine.DefaultBuilder{
			Config: ctxCfg, Counter: ctxcompiler.NewEstimator(0),
			Snapshot: snap, ToolDefs: toolDefs, ReasoningEffort: effort,
			ModelID: settings.Model,
			// Remaining-budget line in L5 (Section 18.4) plus the
			// long-horizon pacing line (elapsed/remaining steps and
			// time, milestone progress): compact steps/tools/time/
			// cost left plus pacing, every call. Each half stays
			// silent under its own switch so ablations add no bytes.
			Budget: func(erun *engine.Run) string {
				now := time.Now().UTC()
				base := ""
				if engine.TimeBudgetEnabled() {
					base = erun.BudgetLine(now)
				}
				pacing := erun.LongHorizonPacing(now)
				switch {
				case base != "" && pacing != "":
					return base + "; " + pacing
				case base != "":
					return base
				default:
					return pacing
				}
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
			// Pairing invariant (Section 19.2 strict wires):
			// pre-request repairs and orphan-400 retries land in
			// pairing.jsonl so trials show when the invariant fired.
			OnPairRepair: func(repaired int, note string) {
				if run.Recorder == nil {
					return
				}
				_ = run.Recorder.RecordPairRepair(repaired, note)
			},
		},
		Tools:          toolRunner,
		LeaseHeartbeat: wireengine.LeaseHeartbeatHook(emb.Leases),
		LeaseRelease:   wireengine.LeaseReleaseHook(emb.Leases),
		// Required-artifact checks resolve through the attempt map too
		// (nil outside attempts: the default filesystem checks).
		ArtifactStat: attemptArtifactStat(opts.attemptMap),
		ArtifactRead: attemptArtifactRead(opts.attemptMap),
		OnPairRetry: func(repaired int, firstErr string) {
			if run.Recorder == nil {
				return
			}
			_ = run.Recorder.RecordPairRepair(repaired, "retry after "+firstErr)
		},
	})
	if run.Recorder != nil {
		_ = run.Recorder.RecordPairRepair(0, fmt.Sprintf("pairing configuration: atomic=%t check=%t retry=%t",
			ctxcompiler.PairAtomicEnabled(nil), ctxcompiler.PairCheckEnabled(nil), ctxcompiler.PairRetryEnabled(nil)))
	}
	// Effective run-level budgets (W8, Sections 7.3, 18.4, 23.2):
	// in exec/delegate/eval with EXCALIBUR_TIME_BUDGET_ONLY=on (default) the
	// wall guidance is explicit --timeout or EXCALIBUR_EVAL_WALL_SECS,
	// else a 900-second estimate with a separate 4x hard safety bound and
	// advisory safety caps and enforced cost/input-token spend caps;
	// otherwise the legacy limits apply. The tracker trips closing
	// mode below 10% and pauses with the budget motive at zero.
	effBudgets := EffectiveBudgetsFor(opts, profile)
	erun := engine.NewRun(string(run.RunID), string(run.TaskID), engine.Profile{
		Name: "child", Role: types.RoleExecutor, ContextRole: "child",
		Limit: tasks.Limit{
			MaxSteps:   effBudgets.MaxSteps,
			MaxSeconds: effBudgets.WallSecs,
			MaxCostUSD: effBudgets.CostCapUSD,
		},
		// Explicit finish (agent quality): exec runs are
		// non-interactive, so they end ONLY through `finish` when
		// the switch is on; off restores the legacy text finish.
		ExplicitFinish: explicitFinish,
		TimeBudgetOnly: effBudgets.TimeBudgetOnly,
		WallSource:     effBudgets.WallSource,
	})
	// Resolve relative artifact paths against the run project
	// directory for the required-artifact completion gate.
	erun.Workdir = opts.ProjectDir
	if engine.DeadlineEnabled() && !run.startedAt.IsZero() {
		erun.StartedAt = run.startedAt
	}
	if erun.Workdir == "" {
		erun.Workdir, _ = os.Getwd()
	}
	if effBudgets.HardWallSecs > 0 {
		// Keep the legacy deadline-switch path consistent with the
		// resolved hard bound, including the larger fallback safety cap.
		if opts.TimeoutSecs == 0 && !engine.DeadlineEnabled() {
			var cancel context.CancelFunc
			ctx, cancel = effBudgets.wallContext(ctx, time.Now())
			defer cancel()
		}
	}
	// Record the effective budgets for the ablation (budgets.json
	// next to skills.json/explore.json plus the budgets event).
	writeBudgetsRecord(run.Dir, effBudgets)
	_ = run.EmitEvent("budgets", map[string]any{
		"time_budget_only": effBudgets.TimeBudgetOnly,
		"strict_blocked":   effBudgets.StrictBlocked,
		"wall_secs":        effBudgets.WallSecs,
		"wall_source":      effBudgets.WallSource,
		"hard_wall_secs":   effBudgets.HardWallSecs,
		"deadline":         effBudgets.Deadline,
		"max_steps":        effBudgets.MaxSteps,
		"cost_cap_usd":     effBudgets.CostCapUSD,
		"cost_source":      effBudgets.CostSource,
	})
	rec := eng.AttachJournal(erun.ID, store)
	if err := activation.Record(ctx, store, erun.ID); err != nil {
		return "", providers.Usage{}, "", fmt.Errorf("cli: record tool activation: %w", err)
	}
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
		return "", providers.Usage{}, "", err
	}
	if _, err := rec.RunStarted(ctx, map[string]string{"task": opts.Request}); err != nil {
		close(stopSampler)
		return "", providers.Usage{}, "", err
	}
	wireFinishAudit(run, erun)
	erun.SetPromptFinishRecordDir(filepath.Dir(run.ATIF.Path()))
	restoreRun = erun
	erun.SetInputCopyDir(filepath.Join(run.Dir, "input-originals"))
	if err := eng.Start(ctx, erun); err != nil {
		close(stopSampler)
		return "", providers.Usage{}, "", err
	}
	if err := eng.Run(ctx, erun); err != nil {
		// An interrupt cancels the context without failing: the
		// orchestrator owns the resulting state.
		if ctx.Err() == nil {
			if !acceptedFinishSurvives(erun.State()) {
				close(stopSampler)
				return "", providers.Usage{}, "", err
			}
			// The error is recorded by class only, never by message.
			_ = run.EmitEvent("post_finish_error", map[string]any{"class": models.ProviderErrorReason(err)})
		}
	}
	close(stopSampler)
	scopeRecord := erun.ScopeGuardRecord()
	if b, err := json.MarshalIndent(scopeRecord, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(run.Dir, "scope_guard.json"), append(b, '\n'), 0600)
	}
	_ = run.EmitEvent("scope_guard", scopeRecord)
	// Long-horizon ablation record: milestone plan, continuation trips
	// and closing flag land next to skills.json/explore.json so the
	// mechanism is auditable per trial (Appendix F).
	if plan, trips, closing, large, _ := erun.LongHorizonState(); true {
		writeLongHorizonRecord(run.Dir, tasks.DefaultLongHorizonConfig(), plan, trips, closing, large)
		_ = run.EmitEvent("longhorizon", map[string]any{
			"large": large, "plan": len(plan), "trips": trips, "closing": closing,
		})
	}
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
			return "", providers.Usage{}, "", err
		}
		if err := run.EmitStep(telemetry.ATIFStep{
			Role:       "tool",
			ToolResult: &telemetry.ATIFToolResult{CallID: ev.ToolResult.CallID, Name: ev.ToolResult.Name, Output: ev.Content},
		}, "tool_result"); err != nil {
			return "", providers.Usage{}, "", err
		}
	}
	if run.Recorder != nil {
		// Completion-gate attempts with criteria and evidence.
		if !run.finishAuditWired {
			for i := 0; i < erun.GateAttempts; i++ {
				_ = run.Recorder.RecordGate(eval.GateAttempt{Attempt: i + 1, Passed: erun.State() == types.RunCompleted, Note: rep.Summary})
			}
			if erun.GateAttempts == 0 {
				_ = run.Recorder.RecordGate(eval.GateAttempt{Attempt: 1, Passed: erun.State() == types.RunCompleted, Note: rep.Summary})
			}
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
	// Explicit-finish records (agent quality): the switch, the
	// model's raw final text and the `finish` arguments land next to
	// skills.json / explore.json for the ablation.
	writeExplicitFinishRecord(run.Dir, explicitFinish, erun)
	// The finish detail the trial record and run_finished carry: the
	// delivery summary, the model's final text and the cited evidence.
	run.FinalText = erun.FinalText
	if erun.Finish != nil {
		run.FinishSummary = erun.Finish.Summary
		run.FinishEvidence = erun.Finish.Evidence
	}
	_ = run.EmitEvent("explicit_finish", map[string]any{
		"enabled": explicitFinish, "finish_calls": erun.FinishCalls,
		"refusals": erun.FinishRefusals, "confirmed": erun.Finish != nil,
	})
	// Run-end surviving services (W10): detached background processes
	// (pid, command, log) that outlive the agent, next to the other
	// trial artifacts.
	if emb.Sessions != nil {
		// Finite package/build helpers end with the run; open sessions and
		// listening services retain their process and file-backed output.
		emb.Sessions.StopHelperSessions()
		surviving := emb.Sessions.Surviving()
		writeServicesRecord(run.Dir, surviving, tools.DetachBackgroundEnabled())
		_ = run.EmitEvent("services", map[string]any{
			"detach_background": tools.DetachBackgroundEnabled(),
			"count":             len(surviving),
		})
	} else {
		writeServicesRecord(run.Dir, nil, tools.DetachBackgroundEnabled())
	}
	summary := rep.Summary
	if summary == "" {
		if explicitFinish {
			// Never echo the instruction: the delivery summary
			// is the `finish.summary`. Without one, name the
			// honest terminal state generically.
			switch erun.State() {
			case types.RunCompleted:
				summary = "completed"
			case types.RunBlocked:
				summary = "blocked"
			case types.RunPaused:
				summary = "paused: budget exhausted"
			case types.RunFailed:
				summary = "failed"
			default:
				summary = string(erun.State())
			}
		} else {
			summary = "completed"
		}
	}
	if err := run.EmitStep(telemetry.ATIFStep{Role: "assistant", Content: summary}, "message"); err != nil {
		return "", providers.Usage{}, "", err
	}
	return erun.State(), usage, summary, nil
}

// acceptedFinishSurvives reports whether an engine error may be recorded
// without failing the run because the run already completed through an
// accepted finish (item 48). A provider or engine error after acceptance
// cannot change the completed state, the trial record or run_finished.
func acceptedFinishSurvives(state types.RunState) bool {
	return state == types.RunCompleted
}

// writeExplicitFinishRecord persists the explicit-finish ablation
// record for the run (switch, finish arguments, raw final text,
// streak, refusals, strict-blocked refusals, time-only closing) next
// to the other trial artifacts. A missing dir skips the write;
// failures never fail the run.
func writeExplicitFinishRecord(dir string, enabled bool, erun *engine.Run) {
	if dir == "" {
		return
	}
	timeOnly, strictRefusals, closingNoted, safetyNoted := false, 0, false, false
	if erun != nil {
		timeOnly = erun.TimeOnlyForRecord()
		strictRefusals = erun.StrictBlockedRefusalsForRecord()
		closingNoted, safetyNoted = erun.ClosingNotedForRecord()
	}
	rec := map[string]any{
		"enabled":             enabled,
		"actionable_refusals": engine.ActionableRefusalsEnabled(),
		"time_budget_only":    timeOnly,
		"strict_blocked":      engine.StrictBlockedEnabled(),
		"strict_refusals":     strictRefusals,
		"closing_noted":       closingNoted,
		"safety_cap_noted":    safetyNoted,
		"time_only_closing":   closingNoted,
	}
	if erun != nil {
		rec["refusal_items"] = erun.RefusalRecords()
		rec["finish_calls"] = erun.FinishCalls
		rec["refusals"] = erun.FinishRefusals
		rec["strict_blocked_refusals"] = erun.StrictBlockedRefusals
		rec["text_only_streak"] = erun.TextOnlyStreak()
		if erun.Finish != nil {
			rec["summary"] = erun.Finish.Summary
			rec["evidence"] = erun.Finish.Evidence
			rec["status"] = erun.Finish.Status
			rec["reason"] = erun.Finish.Reason
			rec["raw_args"] = erun.Finish.RawArgs
		}
		if erun.FinalText != "" {
			rec["final_text"] = erun.FinalText
		}
		if raw := erun.RawFinish; raw != "" {
			rec["raw_finish"] = raw
		}
		if erun.LastRefusal != "" {
			rec["last_refusal"] = erun.LastRefusal
		}
	}
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(dir+"/explicit_finish.json", append(buf, '\n'), 0o644)
}

// writeServicesRecord persists the run-end surviving services (W10):
// detached background processes (pid, command, log path) that outlive
// the agent's exit. A missing dir skips the write; failures never fail
// the run.
func writeServicesRecord(dir string, surviving []tools.SurvivingProcess, detachEnabled bool) {
	if dir == "" {
		return
	}
	rec := map[string]any{
		"detach_background": detachEnabled,
		"surviving":         surviving,
		"count":             len(surviving),
	}
	if rec["surviving"] == nil {
		rec["surviving"] = []tools.SurvivingProcess{}
	}
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(dir+"/services.json", append(buf, '\n'), 0o644)
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

// Cache telemetry includes full structured messages without persisting payloads.
func cachePayloadTelemetryEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_CACHE_PAYLOAD_TELEMETRY"))) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// wireFinishAudit writes each verdict immediately; finalization does not
// manufacture duplicate passing rows from the final run state.
func wireFinishAudit(run *ExecRun, erun *engine.Run) {
	if !engine.FinishAuditEnabled() {
		return
	}
	recorder := run.Recorder
	if recorder == nil {
		// Non-eval exec runs keep their single audit stream in the run directory.
		// Keep the full evaluation recorder unwired so no other export changes.
		var err error
		recorder, err = eval.NewTrialRecorder(run.Dir)
		if err != nil {
			return
		}
	}
	run.finishAuditWired = true
	erun.SetFinishAudit(func(v engine.FinishAuditVerdict) error {
		return recorder.RecordGate(eval.GateAttempt{At: v.At, Attempt: v.Sequence, AttemptID: v.AttemptID, Mechanism: v.Mechanism, Outcome: v.Outcome, Passed: v.Outcome == "acceptance" || v.Outcome == "reserve_acceptance", Note: v.Note})
	})
}
