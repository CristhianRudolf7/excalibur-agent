// Agent engine (Sections 7.1-7.4, H3 engine half).
//
// One engine serves every level: the master, parent and child
// subagents share the step loop and differ only by their Profile
// (eligible tools, model role, budgets, context role). The loop
// implements the 7.3 step algorithm with closing mode, parallel tool
// batches (7.4), the deterministic completion gate (7.5, via
// internal/tasks), the loop detector (18.3, via internal/tasks) and
// explicit budgets (18.4, via internal/tasks).
//
// The engine is storage-backed by the journal: every request,
// response, intent, result and transition is recorded before the
// step moves on, so recovery reuses recorded responses instead of
// calling the model again and never repeats an uncertain effect.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Profile distinguishes the engine instance per level (7.1): the
// tool set, the model role, the budgets and the context role.
type Profile struct {
	// Name is master, parent or child (plus test profiles).
	Name string
	// Role is the model role for every call of this run.
	Role types.ModelRole
	// Tools are the eligible tool names for this profile.
	Tools []string
	// Limit caps the run (18.4).
	Limit tasks.Limit
	// ContextRole sizes the compiled context (11.2).
	ContextRole string
}

// ModelEndpoint is the single-model gateway seam (19.2-19.6). The
// production implementation is *models.Gateway; tests use fakes.
type ModelEndpoint interface {
	Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error)
}

// RequestBuilder compiles one model request for a run. Closing mode
// keeps the tool definitions (so the cache prefix is stable) but
// sets tool_choice none and asks for a final report (7.3).
type RequestBuilder interface {
	BuildRequest(ctx context.Context, run *Run, closing bool) (providers.ChatRequest, ctxcompiler.Compiled, error)
}

// ToolRunner executes one tool call. The production implementation
// wraps *tools.Executor with its registry; tests use scripted tools.
type ToolRunner interface {
	Execute(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error)
	Definition(name string) (tools.Definition, error)
}

// GateChecker evaluates the completion gate (7.5). The default
// evaluates the run's GateInput with tasks.Evaluate.
type GateChecker interface {
	Check(run *Run) tasks.GateReport
}

// GateFunc adapts a function to a GateChecker.
type GateFunc func(run *Run) tasks.GateReport

// Check implements GateChecker.
func (f GateFunc) Check(run *Run) tasks.GateReport { return f(run) }

// ExpertConsult answers one escalated loop detection with a diagnosis
// (19.3, 18.3 stage 2). Nil means escalation only injects a note.
type ExpertConsult func(ctx context.Context, run *Run, d tasks.Detection) (string, error)

// Deps wires the engine to the real packages.
type Deps struct {
	Models  ModelEndpoint
	Context RequestBuilder
	Tools   ToolRunner
	Gate    GateChecker
	Expert  ExpertConsult
	Clock   func() time.Time
	NewKey  func() string
}

// Directive is a pending instruction for the run (15.7). Any
// directive wakes a waiting run (7.2).
type Directive struct {
	ID       string
	Text     string
	Priority string
	At       time.Time
}

// WakeEvent resumes a waiting run (7.2): the awaited tool result,
// the user answer, a child completion/question/report, a directive,
// an agenda timer, a capability update or a freed lease.
type WakeEvent struct {
	Kind string
	Text string
	At   time.Time
}

// Wake kinds.
const (
	WakeTool       = "tool"
	WakeUser       = "user"
	WakeSubagents  = "subagents"
	WakeDirective  = "directive"
	WakeAgenda     = "agenda"
	WakeCapability = "capability"
	WakeLease      = "lease"
)

// Run is one execution instance. State transitions enforce the 7.2
// diagram through types.CanTransition; every change is journaled.
type Run struct {
	ID      string
	TaskID  string
	Profile Profile

	mu          sync.Mutex
	state       types.RunState
	waitReason  types.WaitReason
	pauseReason types.PauseReason
	blockReason types.BlockReason
	pausedBy    string

	Steps        int
	Tracker      tasks.Tracker
	Loop         *tasks.LoopDetector
	GateAttempts int
	GateInput    tasks.GateInput
	LastGate     tasks.GateReport
	Volatile     []string
	RecoveryNote string
	History      []ctxcompiler.ActivityEvent
	Children     []tasks.GateChild
	Contrast     *tasks.ContrastResult
	Summary      string
	Files        []string
	StartedAt    time.Time
	HeartbeatAt  time.Time

	directives []Directive
	progress   tasks.ProgressState
	progressFn func(*Run) tasks.ProgressState
}

// NewRun returns a created run with its profile budgets installed.
func NewRun(id, taskID string, p Profile) *Run {
	now := time.Now().UTC()
	return &Run{
		ID: id, TaskID: taskID, Profile: p,
		state:       types.RunCreated,
		Tracker:     tasks.Tracker{Limit: p.Limit},
		Loop:        tasks.NewLoopDetector(),
		StartedAt:   now,
		HeartbeatAt: now,
	}
}

// State returns the current state.
func (r *Run) State() types.RunState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Reasons returns the current wait/pause/block motives.
func (r *Run) Reasons() (types.WaitReason, types.PauseReason, types.BlockReason, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waitReason, r.pauseReason, r.blockReason, r.pausedBy
}

// VolatileNotes returns the injected notes for the next request.
func (r *Run) VolatileNotes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.Volatile...)
}

// InjectNote adds a volatile note for the next model call: gate
// pendings, loop notes, recovery notices (7.3).
func (r *Run) InjectNote(note string) {
	if note == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Volatile = append(r.Volatile, note)
}

// PendingDirectives returns the queued directives.
func (r *Run) PendingDirectives() []Directive {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Directive(nil), r.directives...)
}

// SetProgressFunc installs the per-step progress sampler feeding the
// R4 no-progress rule. Nil keeps the zero progress.
func (r *Run) SetProgressFunc(fn func(*Run) tasks.ProgressState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progressFn = fn
}

// transition moves the run state, enforcing the 7.2 diagram. Callers
// hold no lock; journaling happens in Engine wrappers.
func (r *Run) transition(to types.RunState) error {
	if !types.CanTransition(r.state, to) {
		return fmt.Errorf("engine: illegal transition %s -> %s", r.state, to)
	}
	r.state = to
	return nil
}

// Engine runs the 7.3 loop for one run at a time.
type Engine struct {
	deps     Deps
	journals map[string]*journal.Recorder
	mu       sync.Mutex
}

// New returns an Engine with defaulted helpers.
func New(d Deps) *Engine {
	if d.Clock == nil {
		d.Clock = time.Now
	}
	if d.NewKey == nil {
		d.NewKey = func() string {
			var b [16]byte
			_, _ = rand.Read(b[:])
			return hex.EncodeToString(b[:])
		}
	}
	return &Engine{deps: d, journals: map[string]*journal.Recorder{}}
}

// AttachJournal binds a journal store to a run for write-ahead
// recording. The recorder must be attached before Run starts.
func (e *Engine) AttachJournal(runID string, store journal.Store) *journal.Recorder {
	rec := &journal.Recorder{Store: store, RunID: runID}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.journals[runID] = rec
	return rec
}

func (e *Engine) recorder(runID string) *journal.Recorder {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.journals[runID]
}

func (e *Engine) recordState(ctx context.Context, run *Run, from, to types.RunState, reason string) {
	if rec := e.recorder(run.ID); rec != nil {
		_, _ = rec.StateChange(ctx, from, to, reason)
	}
}

// Admit moves created -> queued.
func (e *Engine) Admit(ctx context.Context, run *Run) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	from := run.state
	if err := run.transition(types.RunQueued); err != nil {
		return err
	}
	e.recordState(ctx, run, from, types.RunQueued, "")
	return nil
}

// Start moves queued -> running.
func (e *Engine) Start(ctx context.Context, run *Run) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	from := run.state
	if err := run.transition(types.RunRunning); err != nil {
		return err
	}
	run.HeartbeatAt = e.deps.Clock().UTC()
	e.recordState(ctx, run, from, types.RunRunning, "")
	return nil
}

// RequestPause pauses a run from queued, running or waiting (7.2, 15.8).
func (e *Engine) RequestPause(ctx context.Context, run *Run, reason types.PauseReason, by string) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	from := run.state
	if err := run.transition(types.RunPaused); err != nil {
		return err
	}
	run.pauseReason = reason
	run.pausedBy = by
	e.recordState(ctx, run, from, types.RunPaused, string(reason))
	return nil
}

// Resume returns paused -> queued; the orchestrator restarts it.
func (e *Engine) Resume(ctx context.Context, run *Run) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	from := run.state
	if err := run.transition(types.RunQueued); err != nil {
		return err
	}
	run.pauseReason = ""
	run.pausedBy = ""
	e.recordState(ctx, run, from, types.RunQueued, "")
	return nil
}

// Cancel moves any non-terminal run to canceled (7.2).
func (e *Engine) Cancel(ctx context.Context, run *Run) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	from := run.state
	if err := run.transition(types.RunCanceled); err != nil {
		return err
	}
	e.recordState(ctx, run, from, types.RunCanceled, "")
	return nil
}

// Wait moves running -> waiting with its motive (7.2).
func (e *Engine) Wait(ctx context.Context, run *Run, reason types.WaitReason) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	from := run.state
	if err := run.transition(types.RunWaiting); err != nil {
		return err
	}
	run.waitReason = reason
	e.recordState(ctx, run, from, types.RunWaiting, string(reason))
	return nil
}

// Wake moves waiting -> running on any wake event (7.2). Any
// directive wakes the run; orders of pause/cancel need no wake: the
// orchestrator changes the state directly.
func (e *Engine) Wake(ctx context.Context, run *Run, ev WakeEvent) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	from := run.state
	if from != types.RunWaiting {
		return fmt.Errorf("engine: wake needs waiting, run is %s", from)
	}
	if err := run.transition(types.RunRunning); err != nil {
		return err
	}
	run.waitReason = ""
	run.HeartbeatAt = e.deps.Clock().UTC()
	e.recordState(ctx, run, from, types.RunRunning, "wake:"+ev.Kind)
	return nil
}

// DeliverDirective journals the directive and wakes a waiting run:
// any directive, whatever its priority, is a wake event (7.2).
func (e *Engine) DeliverDirective(ctx context.Context, run *Run, d Directive) error {
	if d.At.IsZero() {
		d.At = e.deps.Clock().UTC()
	}
	run.mu.Lock()
	wasWaiting := run.state == types.RunWaiting
	run.directives = append(run.directives, d)
	run.mu.Unlock()
	if rec := e.recorder(run.ID); rec != nil {
		_, _ = rec.Directive(ctx, false, map[string]string{"id": d.ID, "text": d.Text})
	}
	if wasWaiting {
		return e.Wake(ctx, run, WakeEvent{Kind: WakeDirective, Text: d.ID, At: d.At})
	}
	return nil
}

// processSignals applies queued directives at the top of every step
// (7.3): each is journaled as applied and folded into a volatile
// note. Cancellation and pause arrive as state changes from the
// orchestrator, never through here.
func (e *Engine) processSignals(ctx context.Context, run *Run) {
	run.mu.Lock()
	pending := append([]Directive(nil), run.directives...)
	run.directives = nil
	run.mu.Unlock()
	for _, d := range pending {
		if rec := e.recorder(run.ID); rec != nil {
			_, _ = rec.Directive(ctx, true, map[string]string{"id": d.ID, "text": d.Text})
		}
		run.InjectNote("directive " + d.ID + ": " + d.Text)
	}
}

func (e *Engine) gateCheck(run *Run) tasks.GateReport {
	if e.deps.Gate != nil {
		return e.deps.Gate.Check(run)
	}
	run.mu.Lock()
	in := run.GateInput
	run.mu.Unlock()
	return tasks.Evaluate(in)
}

// Run executes the 7.3 loop until the run leaves running: completion,
// honest stop, pause, block, cancel or failure. Waiting runs return
// immediately without calling the model: only a wake event resumes
// them (7.2).
func (e *Engine) Run(ctx context.Context, run *Run) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.processSignals(ctx, run)
		run.mu.Lock()
		state := run.state
		run.mu.Unlock()
		if state != types.RunRunning {
			return nil
		}
		stop, err := e.step(ctx, run)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
}

// step runs one 7.3 iteration. It reports whether the run left
// running.
func (e *Engine) step(ctx context.Context, run *Run) (bool, error) {
	run.mu.Lock()
	run.Steps++
	step := run.Steps
	run.HeartbeatAt = e.deps.Clock().UTC()
	elapsed := run.HeartbeatAt.Sub(run.StartedAt)
	run.mu.Unlock()

	if status := e.checkBudgets(run, elapsed); status == tasks.StatusExhausted {
		e.pauseBudget(ctx, run)
		return true, nil
	}
	closing := e.checkBudgets(run, elapsed) == tasks.StatusClosing

	req, compiled, err := e.deps.Context.BuildRequest(ctx, run, closing)
	if err != nil {
		return true, e.fail(ctx, run, "context build: "+err.Error())
	}
	if rec := e.recorder(run.ID); rec != nil {
		_, _ = rec.ModelRequest(ctx, journal.HashRequest(prefixHash(compiled)))
	}

	resp, meta, err := e.deps.Models.Chat(ctx, run.Profile.Role, req)
	if err != nil {
		return true, e.fail(ctx, run, "model call: "+err.Error())
	}
	run.mu.Lock()
	run.Tracker.Observe(tasks.Usage{
		Steps: 1, InputTokens: int(resp.Usage.Input), OutputTokens: int(resp.Usage.Output),
		CostUSD: meta.CostUSD, Elapsed: elapsed, ToolCalls: len(resp.Message.ToolCalls),
	})
	run.mu.Unlock()

	if rec := e.recorder(run.ID); rec != nil {
		names := make([]string, 0, len(resp.Message.ToolCalls))
		calls := make([]journal.RecordedCall, 0, len(resp.Message.ToolCalls))
		for _, tc := range resp.Message.ToolCalls {
			names = append(names, tc.Name)
			calls = append(calls, journal.RecordedCall{CallID: tc.ID, Tool: tc.Name, Args: tc.Arguments})
		}
		_, _ = rec.ModelResponse(ctx, journal.ModelResponsePayload{
			Hash: journal.HashRequest(prefixHash(compiled)), Tools: names,
			FinalText: len(names) == 0,
			UsageIn:   resp.Usage.Input, UsageOut: resp.Usage.Output,
			Calls: calls,
		})
		if journal.ShouldCheckpoint(step) {
			_, _ = rec.Checkpoint(ctx, step, nil)
		}
	}

	if len(resp.Message.ToolCalls) > 0 {
		return e.execTools(ctx, run, resp.Message.ToolCalls)
	}

	return e.finishTurn(ctx, run)
}

// checkBudgets folds elapsed time into the usage before evaluating.
func (e *Engine) checkBudgets(run *Run, elapsed time.Duration) tasks.Status {
	run.mu.Lock()
	defer run.mu.Unlock()
	dup := run.Tracker
	dup.Used.Elapsed = elapsed
	return dup.Evaluate()
}

// pauseBudget parks the run with the budget motive and its delivery
// report (18.4). The user may extend the budget and resume.
func (e *Engine) pauseBudget(ctx context.Context, run *Run) {
	run.mu.Lock()
	from := run.state
	_ = run.transition(types.RunPaused)
	run.pauseReason = types.PauseBudget
	exhausted := run.Tracker.ExhaustedBy()
	elapsed := run.HeartbeatAt.Sub(run.StartedAt)
	dup := run.Tracker
	dup.Used.Elapsed = elapsed
	run.mu.Unlock()
	run.InjectNote("budget exhausted: " + joinList(exhausted))
	e.recordState(ctx, run, from, types.RunPaused, "budget")
}

// fail moves the run to failed with its cause.
func (e *Engine) fail(ctx context.Context, run *Run, cause string) error {
	run.mu.Lock()
	from := run.state
	_ = run.transition(types.RunFailed)
	run.mu.Unlock()
	e.recordState(ctx, run, from, types.RunFailed, cause)
	return fmt.Errorf("engine: run %s failed: %s", run.ID, cause)
}

// finishTurn evaluates the gate when the model answers without tools
// (7.3): the run moves running -> verifying while the deterministic
// gate runs. On success it completes; otherwise the compact note is
// injected, the run returns to executing and the agent continues.
// After GateAttempts failures the run ends in its honest state, never
// claiming success without evidence.
func (e *Engine) finishTurn(ctx context.Context, run *Run) (bool, error) {
	run.mu.Lock()
	from := run.state
	if from == types.RunRunning {
		if err := run.transition(types.RunVerifying); err != nil {
			run.mu.Unlock()
			return true, err
		}
	}
	run.mu.Unlock()
	e.recordState(ctx, run, from, types.RunVerifying, "gate")
	rep := e.gateCheck(run)
	run.mu.Lock()
	run.LastGate = rep
	attempts := run.GateAttempts
	run.mu.Unlock()
	if rep.OK {
		return true, e.complete(ctx, run, types.RunCompleted, "")
	}
	run.mu.Lock()
	run.GateAttempts++
	run.mu.Unlock()
	if attempts+1 < tasks.GateAttempts {
		run.mu.Lock()
		back := run.state
		_ = run.transition(types.RunRunning)
		run.mu.Unlock()
		e.recordState(ctx, run, back, types.RunRunning, "gate retry")
		run.InjectNote("missing: " + tasks.CompactNote(rep))
		return false, nil
	}
	return true, e.complete(ctx, run, tasks.HonestState(rep), tasks.CompactNote(rep))
}

// complete moves verifying to a terminal state and journals it. A
// control order may win the race: if the run already left verifying
// (paused, canceled), the control state stands and the pendings stay
// in LastGate for the report.
func (e *Engine) complete(ctx context.Context, run *Run, to types.RunState, reason string) error {
	run.mu.Lock()
	from := run.state
	if from != types.RunVerifying {
		run.mu.Unlock()
		return nil
	}
	err := run.transition(to)
	run.mu.Unlock()
	if err != nil {
		return nil
	}
	e.recordState(ctx, run, from, to, reason)
	return nil
}

// execTools runs one batch plan with write-ahead journaling, history
// append and loop observation (7.3-7.4). It returns stop=true when a
// loop escalation blocks the run.
func (e *Engine) execTools(ctx context.Context, run *Run, calls []providers.ToolCall) (bool, error) {
	batches, err := e.PlanBatches(ctx, run, calls)
	if err != nil {
		return true, e.fail(ctx, run, "batch plan: "+err.Error())
	}
	for _, batch := range batches {
		if stop, err := e.execBatch(ctx, run, batch); err != nil || stop {
			return stop, err
		}
	}
	return false, nil
}

// execBatch executes one parallel batch in request order (7.4).
func (e *Engine) execBatch(ctx context.Context, run *Run, batch []providers.ToolCall) (bool, error) {
	results := e.runParallel(ctx, run, batch)
	for i, tc := range batch {
		res := results[i]
		run.mu.Lock()
		seq := int64(len(run.History))
		run.History = append(run.History, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleTool,
			Content:    res.Output,
			ToolResult: &providers.ToolResult{CallID: tc.ID, Name: tc.Name, Output: res.Output, IsError: res.Status != tools.StatusOK},
		})
		run.mu.Unlock()
		for _, d := range run.Loop.ObserveTool(tasks.ToolEvent{
			Tool: tc.Name, Args: string(tc.Arguments), Result: res.Output,
			Error: toolErrorOf(res),
		}) {
			if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
				return stop, err
			}
		}
	}
	run.mu.Lock()
	prog := run.progress
	progressFn := run.progressFn
	run.mu.Unlock()
	if progressFn != nil {
		prog = progressFn(run)
	}
	for _, d := range run.Loop.ObserveProgress(prog) {
		if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
			return stop, err
		}
	}
	return false, nil
}

func toolErrorOf(res tools.Result) string {
	if res.Status == tools.StatusOK {
		return ""
	}
	return string(res.Status) + ": " + res.Output
}

// onDetection applies the staged loop response (18.3): a corrective
// note first, an expert consult or strategy switch second, blocked
// with a report third.
func (e *Engine) onDetection(ctx context.Context, run *Run, d tasks.Detection) (bool, error) {
	switch d.Action {
	case tasks.ActionNote:
		run.InjectNote("loop: " + d.Note)
		return false, nil
	case tasks.ActionEscalate:
		note := "loop escalated: " + d.Note
		if e.deps.Expert != nil {
			if diag, err := e.deps.Expert(ctx, run, d); err == nil && diag != "" {
				note = "expert: " + diag
			}
		}
		run.InjectNote(note)
		return false, nil
	case tasks.ActionBlock:
		run.mu.Lock()
		from := run.state
		_ = run.transition(types.RunBlocked)
		run.blockReason = types.BlockLoop
		run.mu.Unlock()
		e.recordState(ctx, run, from, types.RunBlocked, "loop: "+d.Note)
		return true, nil
	default:
		return false, nil
	}
}

// Report builds the delivery report of 7.6 for a stopped run.
func (e *Engine) Report(run *Run) tasks.DeliveryReport {
	run.mu.Lock()
	defer run.mu.Unlock()
	elapsed := run.HeartbeatAt.Sub(run.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	return tasks.BuildReport(tasks.ReportInput{
		State:   run.state,
		Reason:  string(run.pauseReason) + string(run.blockReason) + string(run.waitReason),
		Summary: run.Summary,
		Files:   append([]string(nil), run.Files...),
		Gate:    run.LastGate,
		Metrics: tasks.ReportMetrics{
			InputTokens: run.Tracker.Used.InputTokens, OutputTokens: run.Tracker.Used.OutputTokens,
			CostUSD: run.Tracker.Used.CostUSD, Elapsed: elapsed, ToolCalls: run.Tracker.Used.ToolCalls,
		},
	})
}

func joinList(items []string) string {
	if len(items) == 0 {
		return "budget"
	}
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
