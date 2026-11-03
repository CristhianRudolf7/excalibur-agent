package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/library"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robot"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robotlab"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// OrchestratorRunStore adapts the H8 orchestrator store to the boot
// RunStore seam (Section 17.3): unfinished runs order by boot
// priority (master turns first, then user-initiated, scheduled and
// background), and boot parks them through the same transitions the
// control orders use.
type OrchestratorRunStore struct {
	Store orchestrator.Store
}

// Unfinished implements RunStore: every non-terminal run, oldest
// journal touch first within its kind.
func (s *OrchestratorRunStore) Unfinished() ([]RunSnapshot, error) {
	if s.Store == nil {
		return nil, nil
	}
	var out []RunSnapshot
	for _, rec := range s.Store.ListRuns() {
		if rec.Run.State.IsTerminal() {
			continue
		}
		kind := KindUser
		switch rec.Lane {
		case orchestrator.LaneP0:
			kind = KindMasterTurn
		case orchestrator.LaneP2:
			kind = KindScheduled
		case orchestrator.LaneP3:
			kind = KindBackground
		}
		waitingUser := rec.Run.State == types.RunWaiting
		out = append(out, RunSnapshot{
			ID:            string(rec.Run.ID),
			Kind:          kind,
			State:         rec.Run.State,
			LastJournalAt: rec.Run.UpdatedAt,
			WaitingUser:   waitingUser,
		})
	}
	return out, nil
}

// SetState implements RunStore through the store record.
func (s *OrchestratorRunStore) SetState(id string, state types.RunState, pause *types.PauseReason, block *types.BlockReason, note string) error {
	if s.Store == nil {
		return fmt.Errorf("daemon: no orchestrator store")
	}
	rec, err := s.Store.GetRun(types.RunID(id))
	if err != nil {
		return err
	}
	rec.Run.State = state
	rec.Run.PauseReason = pause
	rec.Run.BlockReason = block
	if note != "" {
		rec.Run.LastProgress = note
	}
	rec.Run.UpdatedAt = time.Now().UTC()
	return s.Store.UpdateRun(rec)
}

// EngineReconciler adapts the durable engine recovery to the boot
// RunReconciler seam (Sections 17.2, 17.3 step 4): it rebuilds the
// engine run from the orchestrator record, loads the journal, probes
// pending intents and applies the recovery decision. Uncertain
// non-idempotent legs never repeat automatically.
type EngineReconciler struct {
	Engine *engine.Engine
	Jrnl   journal.Store
	Store  orchestrator.Store
	Prober journal.Prober
}

// Reconcile implements RunReconciler.
func (r *EngineReconciler) Reconcile(ctx context.Context, snap *RunSnapshot) error {
	if r.Engine == nil || r.Store == nil {
		return fmt.Errorf("daemon: engine reconciler incomplete")
	}
	rec, err := r.Store.GetRun(types.RunID(snap.ID))
	if err != nil {
		return err
	}
	var entries []journal.Entry
	if r.Jrnl != nil {
		var lerr error
		entries, lerr = r.Jrnl.List(ctx, snap.ID)
		if lerr != nil {
			return fmt.Errorf("daemon: list journal %q: %w", snap.ID, lerr)
		}
	} else {
		// Without a journal there is nothing to replay: the run is
		// uncertain by definition and Boot parks it without ever
		// repeating an effect (Section 17.3 conservative rule).
		snap.Uncertain = true
		return nil
	}
	erun := engine.NewRun(snap.ID, string(rec.Run.TaskID), engine.Profile{
		Name: "child",
		Limit: tasks.Limit{
			MaxSteps:   rec.Run.Budget.MaxSteps,
			MaxTokens:  rec.Run.Budget.MaxTokens,
			MaxCostUSD: rec.Run.Budget.MaxCostUSD,
			MaxSeconds: rec.Run.Budget.MaxSeconds,
			MaxProcs:   rec.Run.Budget.MaxProcs,
		},
	})
	prober := r.Prober
	if prober == nil {
		prober = recoverProber{}
	}
	rep := r.Engine.Recover(ctx, erun, entries, prober, snap.WaitingUser)
	snap.Uncertain = len(rep.Uncertain) > 0
	snap.TreeState = rep.Notice
	if rep.Decision == engine.RecoverAskUser {
		snap.WaitingUser = true
	}
	if len(rep.Uncertain) > 0 {
		names := make([]string, 0, len(rep.Uncertain))
		for _, p := range rep.Uncertain {
			names = append(names, p.Intent.Tool)
		}
		snap.TreeState = "uncertain tools: " + strings.Join(names, ", ")
	}
	// Terminal sessions died with the old process (17.3: processes die
	// with the daemon through Pdeathsig and the process group), so any
	// run that was executing loses them; the recovery note names that.
	if !snap.WaitingUser && snap.State == types.RunRunning {
		snap.LostSessions = []string{"all"}
	}
	if err := r.Engine.ApplyRecovery(ctx, erun, rep); err != nil {
		return fmt.Errorf("daemon: apply recovery %q: %w", snap.ID, err)
	}
	return nil
}

// recoverProber adapts tools.Prober to the journal seam for boot
// reconciliation. engine.RecoverProber declares Probe with []byte and
// therefore does not satisfy journal.Prober (which takes
// json.RawMessage); this adapter closes that gap without touching
// either contract.
type recoverProber struct {
	tools map[string]tools.Tool
}

// Probe implements journal.Prober: only tools declaring the probe
// count; anything else reports not-found without error.
func (p recoverProber) Probe(ctx context.Context, name string, args json.RawMessage) (bool, error) {
	t, ok := p.tools[name]
	if !ok {
		return false, nil
	}
	pr, ok := t.(tools.Prober)
	if !ok {
		return false, nil
	}
	return pr.Probe(ctx, args)
}

// EngineRunner attaches one engine run to its orchestrator run: it is
// the daemon supervision-tree implementation of the orchestrator
// Runner seam (Sections 7, 15). Interrupts arrive as immediate
// directives plus an injected note so the step loop observes them at
// the next safe point; cooperative tools finish their step first per
// their cancel class.
type EngineRunner struct {
	Eng *engine.Engine
	Run *engine.Run
}

// RunID implements orchestrator.Runner.
func (r *EngineRunner) RunID() types.RunID { return types.RunID(r.Run.ID) }

// RequestInterrupt implements orchestrator.Runner.
func (r *EngineRunner) RequestInterrupt(toolOnly bool) {
	note := "interrupted by user"
	if toolOnly {
		note = "tool interrupted by user; run continues"
	}
	r.Run.InjectNote(note)
	_ = r.Eng.DeliverDirective(context.Background(), r.Run, engine.Directive{
		ID: "interrupt", Text: note, Priority: "immediate", At: time.Now().UTC(),
	})
}

// ApplyDirective implements orchestrator.Runner.
func (r *EngineRunner) ApplyDirective(d orchestrator.Directive) {
	prio := "normal"
	if d.Action == orchestrator.ActionStop {
		prio = "interrupt"
	}
	_ = r.Eng.DeliverDirective(context.Background(), r.Run, engine.Directive{
		ID: string(d.ID), Text: d.Text, Priority: prio, At: time.Now().UTC(),
	})
}

// Snapshot implements orchestrator.Runner.
func (r *EngineRunner) Snapshot() orchestrator.RunnerSnapshot {
	notes := r.Run.VolatileNotes()
	last := r.Run.Summary
	if len(notes) > 0 {
		last = notes[len(notes)-1]
	}
	return orchestrator.RunnerSnapshot{
		State:        r.Run.State(),
		StepsDone:    r.Run.Steps,
		LastProgress: last,
	}
}

// Ensure EngineRunner satisfies the orchestrator seam.
var _ orchestrator.Runner = (*EngineRunner)(nil)

// NewEngine builds the production step-loop engine over the builtin
// plus capability tool registries: the same engine the daemon
// supervision tree attaches to orchestrator runs (Sections 7, 15).
// Models, context and gate plug through engine.Deps by their owners;
// boot reconciliation uses this engine as built.
func NewEngine() *engine.Engine {
	eng, _, err := buildEngine(engineWiring{Caps: capabilities.DefaultRegistry()})
	if err != nil {
		panic(err)
	}
	return eng
}

// MemoryReviewLoop runs the Section 10.6 daily review in the 21.9
// internal-jobs window: per scope it checks ShouldReview, runs
// ReviewScope on the background lane and yields to user activity
// between scopes (P3 work never contends with the interactive path).
type MemoryReviewLoop struct {
	Service *memory.Service
	// Active reports user activity; when true the loop yields.
	Active func() bool
	// Interval paces review checks; zero defaults to 15 minutes.
	Interval time.Duration
	now      func() time.Time
}

// Start runs the loop until ctx ends in one safego.Go goroutine. The
// returned channel closes when the goroutine exits.
func (l *MemoryReviewLoop) Start(ctx context.Context, scopes []string) <-chan struct{} {
	interval := l.Interval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	now := l.now
	if now == nil {
		now = time.Now
	}
	done := make(chan struct{})
	safego.Go("daemon-memory-review", nil, func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				l.once(ctx, scopes, now())
			}
		}
	})
	return done
}

func (l *MemoryReviewLoop) once(ctx context.Context, scopes []string, now time.Time) {
	if l.Service == nil {
		return
	}
	for _, scope := range scopes {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if l.Active != nil && l.Active() {
			return
		}
		ok, _ := l.Service.ShouldReview(ctx, scope, now)
		if !ok {
			continue
		}
		bg := l.Service.Background()
		_, _ = bg.ReviewScope(ctx, scope, memory.ReviewOptions{})
	}
}

// RAMWatchdog samples RSS through the platform sampler and sheds load
// through the orchestrator RAM policy (Section 5.2): over the
// threshold it pauses P3-first; the orchestrator resumes below 80%
// for 60 s. One safego.Go goroutine; sampling failures never stop the
// daemon.
type RAMWatchdog struct {
	Sampler   platform.ResourceSampler
	Orch      *orchestrator.Orchestrator
	Threshold uint64
	// Interval paces samples; zero defaults to one second (22.2).
	Interval time.Duration
	now      func() time.Time
}

// Start runs the watchdog until ctx ends in one safego.Go goroutine.
// The returned channel closes when the goroutine exits.
func (w *RAMWatchdog) Start(ctx context.Context) <-chan struct{} {
	interval := w.Interval
	if interval <= 0 {
		interval = time.Second
	}
	now := w.now
	if now == nil {
		now = time.Now
	}
	done := make(chan struct{})
	safego.Go("daemon-watchdog", nil, func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case tm := <-t.C:
				w.once(tm)
				_ = now
			}
		}
	})
	return done
}

func (w *RAMWatchdog) once(tm time.Time) {
	if w.Sampler == nil || w.Orch == nil || w.Threshold == 0 {
		return
	}
	sample, err := w.Sampler.Sample(0)
	if err != nil {
		return
	}
	w.Orch.WatchdogTick(sample.RSSBytes, tm.Unix())
}

// Services is the assembled daemon backend surface: every seam the
// gate, master, agenda and CLIs call resolves to a real package here.
// Assemblers fill defaults (memory stores, deterministic fakes where
// the architecture mandates stand-ins); callers override pieces for
// tests.
type Services struct {
	RobotLab       *robotlab.Service
	RobotLabTool   *robotlab.Tool
	ClassroomAgent classroom.AgentService
	engineOptions  engineWiring
	delegations    *chatDelegations
	History        *workspace.History
	Sources        tools.SourceRecorder
	Whiteboard     *whiteboard.Service
	Classroom      *classroom.Service
	BrowserWeb     *wireengine.BrowserWeb
	Robot          *robot.Service

	Automations AutomationLifecycle
	Agenda      *agenda.Agenda
	Triggers    *agenda.TriggerPump
	Orch        *orchestrator.Orchestrator
	Store       orchestrator.Store
	Jrnl        journal.Store
	Engine      *engine.Engine
	// Tools is the daemon tool surface the engine executes; Catalog is
	// its deferred catalog (search_tools, call_tool).
	Tools   *tools.MapRegistry
	Catalog *tools.DeferredCatalog
	// Exec runs tool calls for the engine: secret resolution and output
	// redaction are installed on it (Section 20.1).
	Exec     *tools.Executor
	Caps     *capabilities.Registry
	Library  *library.Ingester
	Memory   *memory.Service
	Skills   *skills.Index
	Projects *workspace.Registry
	Bus      *telemetry.Bus
	Decider  ChoiceDecider
	Review   *MemoryReviewLoop
	Dog      *RAMWatchdog
	// Retention runs the Section 4.7 retention and backup pass in the
	// maintenance window (21.9). Nil disables it.
	Retention *RetentionLoop
	// Events feeds the trigger pump (21.3): daemon-side adapters push
	// normalized events here. Nil runs the pump without a source.
	Events chan agenda.Event
	// ReviewScopes arms the daily-review loop (21.9); empty disables it.
	ReviewScopes  []string
	masterAgentID types.AgentID
}

// AssembleOptions overrides pieces of the assembled stack. Nil pieces
// get working defaults; DB-backed callers pass their handles.
type AssembleOptions struct {
	// DataDir owns the lazy Robot Lab runtime and its explicitly installed venv.
	DataDir        string
	RobotLab       *robotlab.Service
	RobotLabTool   *robotlab.Tool
	ClassroomAgent classroom.AgentService
	Whiteboard     *whiteboard.Service
	Classroom      *classroom.Service
	Robot          *robot.Service
	// Media supplies optional real document backends.
	Media *tools.MediaDeps
	// BrowserWeb is supplied by production startup; nil preserves test fakes.
	BrowserWeb *wireengine.BrowserWebOptions
	// Models is the configured model endpoint for daemon runs. Nil
	// leaves the engine unconfigured: its steps fail with a clear
	// error instead of a missing-endpoint panic.
	Models engine.ModelEndpoint
	// ModelID names the configured model for prompt and limits
	// selection (19.1).
	ModelID string
	// Vault binds secret resolution for tools and output redaction
	// (Section 20.1). Nil keeps pattern-only redaction.
	Vault *vault.Vault
	// Orch is the orchestrator the stack drives. Nil builds one over
	// Store; callers that also register agents on it (agenda,
	// triggers) must pass theirs so every component shares one.
	Orch     *orchestrator.Orchestrator
	Store    orchestrator.Store
	Jrnl     journal.Store
	OrchJrnl orchestrator.Journal
	Bus      *telemetry.Bus
	Decider  ChoiceDecider
	Skills   *skills.Index
	Projects *workspace.Registry
	Memory   *memory.Service
	Sampler  platform.ResourceSampler
	// RAMThresholdBytes arms the watchdog; zero disables it.
	RAMThresholdBytes uint64
	// ReviewScopes arms the daily-review loop; empty disables it.
	ReviewScopes []string
}

// Assemble builds the full daemon service stack with real packages:
// orchestrator over its store, engine over the builtin plus
// capability tool registries, agenda over memory stores (SQLite via
// NewAssembledSQL when a DB is available), capabilities over the
// default registry, memory over its service, skills over the builtin
// index and workspace over its registry.
func Assemble(opts AssembleOptions) (*Services, error) {
	s := &Services{Robot: opts.Robot, Bus: opts.Bus, Decider: opts.Decider}
	s.Classroom = opts.Classroom
	if s.Classroom == nil {
		s.Classroom = classroom.NewService(nil, opts.Bus)
	}
	s.ClassroomAgent = opts.ClassroomAgent
	if s.ClassroomAgent == nil {
		s.ClassroomAgent = classroom.AgentAdapter{Service: s.Classroom}
	}
	s.RobotLabTool = opts.RobotLabTool
	s.RobotLab = opts.RobotLab
	if s.RobotLab == nil {
		if opts.DataDir != "" {
			s.RobotLab = robotlab.NewRuntimeService(opts.DataDir, opts.Bus)
		} else {
			s.RobotLab = robotlab.NewService(nil, opts.Bus)
		}
	}
	assembled := false
	defer func() {
		if !assembled && opts.RobotLab == nil {
			_ = s.RobotLab.Close()
		}
	}()
	if s.RobotLabTool == nil {
		var asker robotlab.VisionAsker
		if opts.Media != nil {
			asker, _ = opts.Media.Vision.(robotlab.VisionAsker)
		}
		s.RobotLabTool = robotlab.NewTool(s.RobotLab.Sidecar, s.RobotLab, asker, &s.RobotLab.VisionStep)
	}
	s.Whiteboard = opts.Whiteboard
	if s.Whiteboard == nil {
		s.Whiteboard = whiteboard.NewService(nil, opts.Bus)
	}
	s.Store = opts.Store
	if s.Store == nil {
		s.Store = orchestrator.NewMemoryStore()
	}
	s.Jrnl = opts.Jrnl
	if s.Jrnl == nil {
		s.Jrnl = journal.NewMemStore()
	}
	if opts.Orch != nil {
		s.Orch = opts.Orch
	} else {
		jrnl := opts.OrchJrnl
		if jrnl == nil {
			jrnl = orchestrator.NoopJournal{}
		}
		s.Orch = orchestrator.New(orchestrator.DefaultConfig(), s.Store, jrnl)
	}
	s.Caps = capabilities.DefaultRegistry()
	if opts.BrowserWeb != nil {
		webOpts := *opts.BrowserWeb
		webOpts.Vault = opts.Vault
		if opts.Projects != nil {
			for _, p := range opts.Projects.List() {
				webOpts.UploadDirs = append(webOpts.UploadDirs, p.Roots...)
			}
		}
		var err error
		s.BrowserWeb, err = wireengine.NewBrowserWeb(webOpts)
		if err != nil {
			return nil, err
		}
		if err = s.BrowserWeb.SwapBrowser(s.Caps); err != nil {
			_ = s.BrowserWeb.Close()
			return nil, err
		}
	}
	var orchStore *orchestrator.MemoryStore
	if ms, ok := s.Store.(*orchestrator.MemoryStore); ok {
		orchStore = ms
	}
	root, err := s.Orch.RegisterMaster("subagent-root", types.GeneralScope())
	if err != nil {
		return nil, fmt.Errorf("daemon: register subagent root: %w", err)
	}
	s.masterAgentID = root.ID
	if opts.Skills != nil {
		s.Skills = opts.Skills
	} else {
		s.Skills = wireengine.DefaultSkillsIndex()
	}
	wiring := engineWiring{
		RobotLabTool: s.RobotLabTool,
		Classroom:    s.ClassroomAgent, Whiteboard: s.Whiteboard, BrowserWeb: s.BrowserWeb, Caps: s.Caps, Models: opts.Models, ModelID: opts.ModelID, Media: opts.Media,
		Robot: s.Robot,
		Vault: opts.Vault, Memory: opts.Memory, Skills: s.Skills,
		Orch: s.Orch, OrchStore: orchStore, Root: root.ID,
	}
	eng, dt, err := buildEngine(wiring)
	if err != nil {
		_ = s.BrowserWeb.Close()
		return nil, err
	}
	wiring.History, wiring.Leases = dt.SharedHistory, dt.Leases
	s.engineOptions = wiring
	s.History = dt.History
	s.Sources = dt.Sources
	s.Engine = eng
	s.Tools = dt.Registry
	s.Catalog = dt.Catalog
	s.Exec = dt.Exec
	if opts.Projects != nil {
		s.Projects = opts.Projects
	} else {
		s.Projects = workspace.NewRegistry()
	}
	if opts.Memory != nil {
		s.Memory = opts.Memory
	}
	s.ReviewScopes = opts.ReviewScopes
	assembled = true
	return s, nil
}

// capabilityInvoker adapts a capabilities.Capability to the
// tools.CapabilityInvoker seam.
type capabilityInvoker struct {
	id  string
	cap capabilities.Capability
}

func (c capabilityInvoker) Manifest() capabilities.Manifest { return c.cap.Manifest() }
func (c capabilityInvoker) Invoke(op string, args json.RawMessage, key string) (capabilities.TaskResult, error) {
	return c.cap.Invoke(op, args, key)
}
func (c capabilityInvoker) Poll(taskID string) (capabilities.TaskResult, error) {
	return c.cap.Poll(taskID)
}
func (c capabilityInvoker) Cancel(taskID string) (capabilities.TaskResult, error) {
	return c.cap.Cancel(taskID)
}
func (c capabilityInvoker) Update(taskID string, answer json.RawMessage) (capabilities.TaskResult, error) {
	return c.cap.Update(taskID, answer)
}

// daemonToolRunner adapts the tools executor plus its registry to the
// engine ToolRunner seam: execution runs the pipeline, definitions
// resolve from the same registry the daemon assembled.
type daemonToolRunner struct {
	ex        *tools.Executor
	reg       *tools.MapRegistry
	artifacts *artifactTurn
}

// Execute implements engine.ToolRunner.
func (r *daemonToolRunner) Execute(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	res, err := r.ex.Execute(ctx, name, args, opts)
	artifactErr := r.artifacts.collect(ctx, opts.CallID, res.Artifacts)
	if artifactErr != nil {
		err = errors.Join(err, artifactErr)
	}
	return res, err
}

// Definition implements engine.ToolRunner.
func (r *daemonToolRunner) Definition(name string) (tools.Definition, error) {
	t, err := r.reg.Get(name)
	if err != nil {
		return tools.Definition{}, err
	}
	return t.Definition(), nil
}
