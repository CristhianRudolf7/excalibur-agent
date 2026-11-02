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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/scopeguard"
	"strings"
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
	// ExplicitFinish requires the explicit finish protocol: in
	// non-interactive profiles (exec, delegate, eval) a run ends ONLY
	// through a `finish` tool call. Interactive daemon runs leave it
	// false and keep the old text-only finish.
	ExplicitFinish bool
	// TimeBudgetOnly marks wall-clock-only binding (W8): only the wall
	// clock pauses the run; steps/cost/tokens are safety caps that
	// force closing instead of pausing. Set by non-interactive
	// drivers (eval default ON via EXCALIBUR_TIME_BUDGET_ONLY).
	TimeBudgetOnly bool
	// WallSource is explicit or default for non-interactive wall guidance.
	// Empty retains the configured-limit behavior of existing engine clients.
	WallSource string
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
	// ActiveSessions supplies live process facts for finish checks.
	ActiveSessions func() []tools.SessionInfo
	Models         ModelEndpoint
	Context        RequestBuilder
	Tools          ToolRunner
	Gate           GateChecker
	Expert         ExpertConsult
	Clock          func() time.Time
	NewKey         func() string
	// Quality gates the agent-quality loop mechanisms (working
	// method, family adaptation, working notes, loop nudges, finish
	// guard). Nil means production defaults with the EXCALIBUR_Q_*
	// environment overlay; see quality.go.
	Quality *QualityConfig
	// FinishGuard overrides the default thin-work finish guard.
	// Nil installs the default, which only fires when no gate is
	// wired. The verification workstream plugs real criteria here.
	FinishGuard FinishGuardFunc
	// ErrorHint suggests a recovery for one failed tool call; the
	// note is injected volatilely (at most one per step). Nil means
	// no per-error hint. The tool-surface workstream plugs precise
	// per-tool hints here.
	ErrorHint ErrorHintFunc
	// SelfVerify overrides the environment-backed self-verification
	// switches (tasks.DefaultSelfVerifyConfig). Nil selects the
	// defaults so every closing mechanism stays measurable on and
	// off without a rebuild.
	SelfVerify *tasks.SelfVerifyConfig
	// Artifacts overrides the environment-backed required-artifact
	// gate (tasks.DefaultArtifactConfig): missing/empty/unexecutable
	// files, unverified ports/URLs, live background sessions. Nil
	// selects the defaults (gate on).
	Artifacts *tasks.ArtifactConfig
	// ArtifactStat and ArtifactRead inject the filesystem for the
	// artifact gate; nil selects the live OS implementations.
	ArtifactStat tasks.StatFunc
	ArtifactRead tasks.ReadFunc
	// LongHorizon overrides the environment-backed long-horizon
	// persistence switches (tasks.DefaultLongHorizonConfig): time-
	// and evidence-aware continuation, milestone plan, budget pacing
	// and background nudges. Nil selects the defaults (all on).
	LongHorizon *tasks.LongHorizonConfig
	// ExplicitFinish overrides the EXCALIBUR_EXPLICIT_FINISH switch.
	// Nil selects the defaults (on) with the environment overlay, so
	// the protocol stays measurable on and off without a rebuild.
	ExplicitFinish *ExplicitFinishConfig
	// LeaseHeartbeat renews the run's file leases while it is
	// running or waiting on a tool or subagents (Section 16.2),
	// including during long tools. Nil disables heartbeats.
	LeaseHeartbeat func(runID string)
	// LeaseRelease frees the run's file leases when it parks
	// (paused, blocked, waiting on the user) or terminates, so
	// files are never immobilized (Section 16.2). Nil disables.
	LeaseRelease func(runID string)
	// PairRetry gates the single repair-and-retry when a strict wire
	// rejects an orphan call/result pair with a 400 (Section 19.2).
	// Nil means enabled with the EXCALIBUR_CONTEXT_PAIR_RETRY overlay
	// (default ON); explicit false disables for ablations.
	PairRetry *bool
	// OnPairRetry observes one orphan-400 repair-and-retry with the
	// repaired history orphan count and the first error text. Nil
	// skips observing; the retry still applies.
	OnPairRetry func(repaired int, firstErr string)
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

	Steps             int
	Tracker           tasks.Tracker
	Loop              *tasks.LoopDetector
	GateAttempts      int
	GateInput         tasks.GateInput
	LastGate          tasks.GateReport
	refusals          *refusalState
	controlKeys       []string
	noteJournal       *journal.Recorder
	contextRevision   int
	directiveSnapshot string
	literalDirectives []string
	sessionFacts      func() []tools.SessionInfo
	pendingEvidence   map[string]map[string]string
	observationMasks  map[string]*ctxcompiler.ObservationMask
	review            reviewVerdict
	prefix            requestPrefix
	Volatile          []string
	RecoveryNote      string
	History           []ctxcompiler.ActivityEvent
	// CompactionBlock is the latest compaction block re-rendered in
	// L3 (11.4 step 4). Empty when no compaction ran.
	CompactionBlock string
	// CompactionRevision numbers compactions for this run.
	CompactionRevision int
	// MaskBatches counts masking batches applied to this run.
	MaskBatches int
	// Compactions counts compactions applied to this run.
	Compactions int
	// PairRepairs counts orphan call/result halves converted to
	// plain-text summaries for this run (pre-request repairs and
	// orphan-400 history repairs combined).
	PairRepairs int
	// PairRetries counts orphan-400 repair-and-retry model calls for
	// this run (at most one per step).
	PairRetries int
	Children    []tasks.GateChild
	Contrast    *tasks.ContrastResult
	Summary     string
	Files       []string
	// Request is the literal original instruction, stashed from the
	// compiled task layer on the first step so the closing hooks can
	// extract checkable criteria and contrast the deliverable
	// without any driver changes.
	Request           string
	scopeGuard        *scopeguard.Guard
	scopeGuardStarted bool
	scopeGuardError   string
	// Workdir resolves relative artifact paths for the
	// required-artifact gate; absolute requirement paths are used
	// as-is. Embedded drivers set it to the run project directory.
	Workdir     string
	StartedAt   time.Time
	HeartbeatAt time.Time

	directives []Directive
	progress   tasks.ProgressState
	progressFn func(*Run) tasks.ProgressState

	// Live tracking for the orchestrator Runner snapshot (Section
	// 15.6): the in-flight model call and tool, plus directive acks.
	// Guarded by mu like the rest of the run state.
	waitingModel   bool
	toolName       string
	toolArgs       string
	toolKeys       []string
	toolStarted    time.Time
	toolTimeout    time.Duration
	toolCancel     types.CancelMode
	toolLiveTail   string
	toolNonBlock   bool
	ackedDirects   map[string]bool
	instrSeen      int
	lastProgress   string
	lastReportStep int
	lastPlanStep   int
	lastPlanAt     time.Time
	interrupted    bool
	toolOnlyStop   bool
	// Notes is the pinned working-notes block (agent quality):
	// key facts (paths, failures, loop patterns) kept verbatim so
	// small models do not lose them as history grows. Rendered
	// into L3 by the builder, most-recent-last, capped by
	// QualityConfig.MaxNotes.
	Notes []string
	// recentCalls holds the last tool signatures (tool + truncated
	// args, oldest first, capped) behind the stuck-recovery prompt.
	// Guarded by mu like the rest of the run state.
	recentCalls []string
	// prevSingleSig is the signature of the last single-call batch
	// with a deterministic outcome, behind the exact-repeat blocker.
	// Guarded by mu.
	prevSingleSig string
	// blockedSigs marks the signatures already blocked once this
	// run: the blocker fires once per signature, then the loop
	// detector owns persisting repeats. Guarded by mu.
	blockedSigs map[string]bool
	// lastOutputs remembers the last non-empty successful output
	// hash per tool, behind per-step novelty tracking for the
	// no-information rule. Guarded by mu.
	lastOutputs map[string]string
	// GuardTrips counts finish-guard nudges consumed by this run.
	GuardTrips int
	// drafts caches the extracted checkable criteria for the closing
	// hooks; refined marks the single model refine pass as done.
	drafts                      []tasks.DraftCriterion
	refined                     bool
	firstAllChecksPassAt        time.Time
	promptFinishSeen            int
	promptFinishStates          map[string]bool
	finishVerifierReport        finishVerifierReport
	promptFinishSignatures      map[[32]byte]bool
	promptFinishArtifactVersion string
	repeatedVerification        int
	promptFinishRecordDir       string
	// lhMilestones is the explicit milestone plan for large tasks
	// (long horizon): ordered checkable phases kept in task state.
	// Guarded by mu like the rest of the run state.
	lhMilestones []tasks.LHMilestone
	// lhInit marks the milestone plan decision as done (large with a
	// plan, or small with no plan). Guarded by mu.
	lhInit bool
	// lhLarge marks tasks that estimated as large. Guarded by mu.
	lhLarge bool
	// lhTrips counts long-horizon continuation nudges consumed by
	// this run. Guarded by mu.
	lhTrips int
	// lhClosing forces closing mode after the continuation bound is
	// reached: the remaining finish attempts render tool_choice none.
	// Guarded by mu.
	lhClosing bool
	// Explicit-finish protocol state (Section 7.3 closing). Guarded
	// by mu like the rest of the run state.
	// textOnlyStreak counts consecutive assistant turns without tool
	// calls; after MaxTextOnlyStreak the run enters closing mode
	// (tools stay available so `finish` remains callable).
	textOnlyStreak int
	// explicitClosing is set once the streak trips: the engine keeps
	// passing closing=false to the builder (so the prefix stays
	// stable and `finish` stays callable) and signals closing only
	// through volatile notes.
	explicitClosing bool
	// finishConfirmPending marks the confirmation round: the first
	// accepted finish armed it, the second ends the run.
	finishConfirmPending bool
	confirmProgress      confirmationProgress
	// loopRefusedResults identifies history sequences whose calls never executed.
	// Keep their error results visible while excluding them from finish evidence.
	loopRefusedResults map[int64]bool
	// Finish holds the last accepted `finish` arguments; RawFinish
	// holds the last attempted raw arguments (accepted or refused)
	// and FinalText holds the model's raw final text for the records.
	Finish    *FinishArgs
	RawFinish string
	FinalText string
	// FinishRefusals counts refused `finish` calls (unbounded: only
	// the time/step budget bounds them).
	FinishRefusals int
	// FinishCalls counts attempted `finish` calls.
	FinishCalls int
	// LastRefusal is the latest `finish` refusal reason for the
	// records (empty when the last attempt was accepted).
	LastRefusal string
	// StrictBlockedRefusals counts `finish status=blocked` refusals
	// under the strict blocked policy (W8) for the trial records.
	StrictBlockedRefusals int
	// fin holds the finish-protocol state (refusal fingerprint,
	// verifier budget, input snapshot). Guarded by mu.
	fin                 finishState
	finishAuditSink     func(FinishAuditVerdict) error
	finishAuditSequence int
	// timeOnlyClosingNoted marks the one-time wall-closing note in
	// time-only mode so closing reminds once without spamming.
	timeOnlyClosingNoted   bool
	earlyDeliverablesNoted bool
	deadlinePaced          uint8
	// safetyCapNoted marks the one-time safety-cap note in time-only
	// mode.
	safetyCapNoted bool
	// spendNoted marks the one-time spend-cap closing note.
	spendNoted bool
	// inputTokenCap is the per-run input-token cap (time-only runs).
	inputTokenCap int64
	unpricedUsage bool
	rechecks      recheckGuard

	// exact is the exact-call guard (loop guard v2). Guarded by mu.
	exact *tasks.ExactCallGuard
}

// NewRun returns a created run with its profile budgets installed.
func NewRun(id, taskID string, p Profile) *Run {
	now := time.Now().UTC()
	return &Run{
		ID: id, TaskID: taskID, Profile: p,
		state:         types.RunCreated,
		Tracker:       tasks.Tracker{Limit: p.Limit},
		Loop:          tasks.NewLoopDetector(),
		StartedAt:     now,
		HeartbeatAt:   now,
		inputTokenCap: InputTokenCap(),
		exact:         tasks.NewExactCallGuard(),
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
	r.controlNoteLocked(controlNoteKey(note), note)
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

// addFact appends one pinned working note, dropping exact
// duplicates and keeping at most max entries (oldest first out).
// Callers hold no lock.
func (r *Run) addFact(fact string, max int) {
	if fact == "" {
		return
	}
	if max <= 0 {
		max = 24
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.Notes {
		if n == fact {
			return
		}
	}
	r.Notes = append(r.Notes, fact)
	for len(r.Notes) > max {
		r.Notes = r.Notes[1:]
	}
}

// notesCopy returns the pinned working notes. Callers hold no lock.
func (r *Run) notesCopy() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.Notes...)
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

// SetWaitingModel records whether a model call is in flight. The
// Runner snapshot reads it for status and watchdogs (15.6).
func (r *Run) SetWaitingModel(waiting bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waitingModel = waiting
}

// SetCurrentTool records the in-flight tool for the snapshot. Keys
// are the Section 15.7 impact-area resource keys.
func (r *Run) SetCurrentTool(name, args string, keys []string, timeout time.Duration, cancel types.CancelMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolName = name
	r.toolArgs = args
	r.toolKeys = append([]string(nil), keys...)
	r.toolStarted = time.Now().UTC()
	r.toolTimeout = timeout
	r.toolCancel = cancel
}

// SetLiveTail records the queued live-output tail for status.
func (r *Run) SetLiveTail(tail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolLiveTail = tail
}

// ClearCurrentTool forgets the in-flight tool when it settles.
func (r *Run) ClearCurrentTool() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolName = ""
	r.toolArgs = ""
	r.toolKeys = nil
	r.toolLiveTail = ""
	r.toolNonBlock = false
}

// MarkDirectiveAcked records that a directive was applied at a safe
// point, for the 15.5 ack tracking in the snapshot.
func (r *Run) MarkDirectiveAcked(id string, step int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ackedDirects == nil {
		r.ackedDirects = map[string]bool{}
	}
	r.ackedDirects[id] = true
	r.instrSeen = step
}

// SetProgressNote records narrative progress for the 15.6 snapshot.
func (r *Run) SetProgressNote(note string, step int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastProgress = note
	r.lastReportStep = step
}

// RequestInterruptFlags records an orchestrator interrupt (15.8):
// full stops the loop after the current step, toolOnly only stops
// the in-flight tool and lets the run decide how to continue.
func (r *Run) RequestInterruptFlags(toolOnly bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interrupted = true
	if toolOnly {
		r.toolOnlyStop = true
	}
}

// consumeInterrupt returns and clears the pending interrupt flags.
func (r *Run) consumeInterrupt() (full, toolOnly bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	full, toolOnly = r.interrupted, r.toolOnlyStop
	r.interrupted, r.toolOnlyStop = false, false
	return full, toolOnly
}

// interruptedNow reports whether a full interrupt is pending without
// clearing it, so a canceled step can exit without failing the run.
func (r *Run) interruptedNow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.interrupted
}

// Engine runs the 7.3 loop for one run at a time.
type Engine struct {
	deps     Deps
	journals map[string]*journal.Recorder
	mu       sync.Mutex
	// stepCancels holds the current step cancel per run so
	// RequestInterrupt can stop the in-flight model call or tool
	// without waiting for its timeout (Sections 7.2, 15.8).
	stepCancels map[string]context.CancelFunc
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
	if d.Quality == nil {
		q := DefaultQualityConfig()
		d.Quality = &q
	}
	resolved := d.Quality.WithEnv()
	d.Quality = &resolved
	return &Engine{deps: d, journals: map[string]*journal.Recorder{}, stepCancels: map[string]context.CancelFunc{}}
}

// RequestInterrupt asks the engine to stop run: it cancels the
// in-flight model call and the current step's tools. With toolOnly
// the run stays running and decides how to continue; otherwise the
// loop exits promptly without failing and the orchestrator owns the
// resulting pause or cancel (Sections 7.2, 15.8).
func (e *Engine) RequestInterrupt(run *Run, toolOnly bool) {
	run.RequestInterruptFlags(toolOnly)
	e.mu.Lock()
	cancel := e.stepCancels[run.ID]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
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
	rec := e.recorder(run.ID)
	run.mu.Lock()
	defer run.mu.Unlock()
	run.noteJournal = rec
	run.sessionFacts = e.deps.ActiveSessions
	from := run.state
	if err := run.transition(types.RunRunning); err != nil {
		return err
	}
	run.HeartbeatAt = e.deps.Clock().UTC()
	if run.Loop != nil {
		run.Loop.ExactRepeatsByGuard = e.quality().LoopGuardV2
	}
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
func (e *Engine) processSignals(ctx context.Context, run *Run) (stop bool) {
	run.mu.Lock()
	pending := append([]Directive(nil), run.directives...)
	run.directives = nil
	step := run.Steps
	run.mu.Unlock()
	for _, d := range pending {
		if rec := e.recorder(run.ID); rec != nil {
			_, _ = rec.Directive(ctx, true, map[string]string{"id": d.ID, "text": d.Text})
		}
		run.MarkDirectiveAcked(d.ID, step)
		if switchOn("EXCALIBUR_CONTROL_TAIL") {
			run.mu.Lock()
			run.appendDirectiveLocked("directive "+d.ID+": "+d.Text, nil)
			run.mu.Unlock()
		} else {
			run.mu.Lock()
			run.rememberDirectiveLocked("directive " + d.ID + ": " + d.Text)
			run.mu.Unlock()
			run.InjectNote("directive " + d.ID + ": " + d.Text)
		}
	}
	if full, toolOnly := run.consumeInterrupt(); full || toolOnly {
		if full && !toolOnly {
			return true
		}
		if toolOnly {
			run.InjectNote("tool interrupted by the user; run decides how to continue")
		}
	}
	return false
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
// them (7.2). Each step runs under its own cancellable context so an
// orchestrator interrupt (15.8) stops the in-flight model call or
// tool without failing the run: a full interrupt exits the loop
// gracefully and the orchestrator owns the resulting state, while a
// tool-only interrupt injects a note and the run decides how to
// continue.
func (e *Engine) Run(ctx context.Context, run *Run) error {
	for {
		if err := ctx.Err(); err != nil {
			if run.interruptedNow() {
				run.consumeInterrupt()
				return nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				e.pauseDeadline(ctx, run)
				return nil
			}
			return err
		}
		if e.processSignals(ctx, run) {
			if e.deps.LeaseRelease != nil {
				e.deps.LeaseRelease(run.ID)
			}
			return nil
		}
		if e.deps.LeaseHeartbeat != nil {
			e.deps.LeaseHeartbeat(run.ID)
		}
		run.mu.Lock()
		state := run.state
		run.mu.Unlock()
		if state != types.RunRunning {
			return nil
		}
		stepCtx, cancel := context.WithCancel(ctx)
		e.mu.Lock()
		if e.stepCancels == nil {
			e.stepCancels = map[string]context.CancelFunc{}
		}
		e.stepCancels[run.ID] = cancel
		e.mu.Unlock()
		stop, err := e.step(stepCtx, run)
		cancel()
		e.mu.Lock()
		delete(e.stepCancels, run.ID)
		e.mu.Unlock()
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

	if reason := e.finishSafetyReason(run); reason != "" {
		run.auditFinish(e.deps.Clock(), "", "finish_safety_cap", "safety_cap", reason)
		return true, e.completeWithPending(ctx, run, []string{reason})
	}

	if pending := run.deliveryPendingReason(); pending != "" && e.pendingCommandReason(run, e.deps.Clock()) == "" {
		run.mu.Lock()
		used := run.fin.deliveryFinalizationUsed
		run.mu.Unlock()
		if used {
			return true, e.completeWithPending(ctx, run, []string{pending})
		}
	}
	e.announceTimeReserve(run, e.deps.Clock())
	if done, err := e.completePendingConfirmation(ctx, run, e.deps.Clock()); done || err != nil {
		return done, err
	}
	e.earlyDeliverablesCheck(run, e.deps.Clock())
	run.DeadlinePacing(e.deps.Clock())

	// Time-only binding (W8): only the wall clock pauses the run.
	// Step safety-cap exhaustion is advisory; spend caps retain their
	// closing and pause thresholds. The run works to the wall edge and leaves
	// the best deliverable in place with `finish`.
	timeOnly := func() bool {
		run.mu.Lock()
		defer run.mu.Unlock()
		return run.Profile.TimeBudgetOnly
	}()
	if timeOnly {
		if status := run.wallStatus(elapsed); status == tasks.StatusExhausted {
			e.pauseBudget(ctx, run)
			return true, nil
		}
		// Spend caps (USD cost, input tokens): at the closing share the
		// agent is asked to finish; at the stop share the run pauses
		// with the budget motive.
		spendOn := SpendCapEnabled()
		if spendOn {
			level, _ := run.spendLevel()
			if level >= 2 {
				e.pauseBudget(ctx, run)
				return true, nil
			}
			if level == 1 {
				run.injectOnce(&run.spendNoted, SpendCloseNote)
			}
		}
		if e.safetyCapExhausted(run, elapsed, spendOn) {
			// A step safety cap would have paused the legacy run: note
			// once and keep working toward the wall edge.
			run.injectOnce(&run.safetyCapNoted, SafetyCapNote)
		}
	} else {
		if status := e.checkBudgets(run, elapsed); status == tasks.StatusExhausted {
			e.pauseBudget(ctx, run)
			return true, nil
		}
	}
	closing := e.checkBudgets(run, elapsed) == tasks.StatusClosing
	if timeOnly {
		// In time-only mode closing follows the wall clock only so
		// safety caps never force an early close.
		closing = run.wallStatus(elapsed) == tasks.StatusClosing
		if closing {
			run.mu.Lock()
			noted := run.timeOnlyClosingNoted
			if !noted {
				run.timeOnlyClosingNoted = true
			}
			run.mu.Unlock()
			if !noted {
				run.InjectNote(TimeOnlyClosingNote)
			}
		}
	}
	// Long-horizon closing: after the continuation bound is reached
	// the remaining finish attempts render tool_choice none so the
	// run closes with a final report instead of new work.
	if e.lhClosingNow(run) {
		closing = true
	}
	// Explicit finish keeps the tool definitions in the request (so
	// the cache prefix stays stable and `finish` remains callable):
	// closing is signaled only through volatile notes, never through
	// tool_choice none.
	explicit := e.explicitFinishRequired(run)
	builderClosing := closing && !explicit

	e.promptFinishCheck(run)
	req, compiled, err := e.deps.Context.BuildRequest(ctx, run, builderClosing)
	if err != nil {
		if ctx.Err() != nil && run.interruptedNow() {
			run.consumeInterrupt()
			return true, nil
		}
		return true, e.fail(ctx, run, "context build: "+err.Error())
	}
	// Stash the literal instruction for the closing hooks: the task
	// layer renders request:\n... verbatim, so verification and
	// contrast need no driver changes.
	run.stashRequest(compiled.LayerText(ctxcompiler.LayerTask))
	scopeWasStarted := run.scopeGuardStarted
	e.startScopeGuard(run)
	e.startInputSnapshot(run)
	if run.scopeGuardError != "" {
		return true, e.fail(ctx, run, run.scopeGuardError)
	}
	if !scopeWasStarted && run.scopeGuardStarted {
		note := run.scopeGuardError
		if run.scopeGuard != nil {
			note = run.scopeGuard.Note()
		}
		message := providers.Message{Role: providers.RoleUser, Content: note}
		if switchOn("EXCALIBUR_STABLE_PREFIX") {
			// Startup guidance is append-only activity. Keep L5 last, and retain
			// the exact message on later executor requests.
			run.mu.Lock()
			run.History = append(run.History, ctxcompiler.ActivityEvent{Seq: run.nextHistorySeqLocked(), Role: message.Role, Content: message.Content})
			run.mu.Unlock()
			if len(req.Messages) > 0 {
				volatile := req.Messages[len(req.Messages)-1]
				req.Messages = append(req.Messages[:len(req.Messages)-1], message, volatile)
			} else {
				req.Messages = append(req.Messages, message)
			}
		} else {
			req.Messages = append(req.Messages, message)
		}
	}
	// Long-horizon persistence: the milestone plan decision needs the
	// stashed request, and the progress check injects the next
	// milestone for the following model call.
	e.ensureLHMilestones(run)
	e.lhProgressCheck(run)
	if rec := e.recorder(run.ID); rec != nil {
		_, _ = rec.ModelRequest(ctx, journal.HashRequest(prefixHash(compiled)))
	}

	run.SetWaitingModel(true)
	modelCtx, modelCancel := ctx, func() {}
	finalizing := switchOn("EXCALIBUR_MODEL_DELIVERY_RESERVE") && e.explicitFinishRequired(run) && (run.confirmArmed() || run.deliveryPendingReason() != "")
	if finalizing {
		modelCtx, modelCancel = e.modelDeliveryContext(ctx, run, true)
		if run.deliveryPendingReason() != "" {
			run.mu.Lock()
			run.fin.deliveryFinalizationUsed = true
			run.mu.Unlock()
		}
	}
	resp, meta, err := e.deps.Models.Chat(WithModelCallSite(modelCtx, "engine.step"), run.Profile.Role, req)
	modelExpired := modelCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil
	modelCancel()
	run.SetWaitingModel(false)
	if finalizing && modelExpired {
		e.markDeliveryPending(ctx, run, "engine.confirmation")
		if e.pendingCommandReason(run, e.deps.Clock()) != "" {
			return true, e.RequestPause(ctx, run, types.PauseBudget, "confirmation deadline with running computation")
		}
		return true, e.completeWithPending(ctx, run, []string{run.deliveryPendingReason()})
	}
	if err != nil {
		if ctx.Err() != nil && run.interruptedNow() {
			full, toolOnly := run.consumeInterrupt()
			if toolOnly {
				run.InjectNote("tool interrupted by the user; run decides how to continue")
				return false, nil
			}
			_ = full
			return true, nil
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			e.pauseDeadline(ctx, run)
			return true, nil
		}
		// Strict-wire orphan rejection: repair the history (orphan
		// halves become plain-text summaries) and retry the model
		// call exactly once with the rebuilt request.
		if retry, ok := e.retryOrphanPairs(ctx, run, builderClosing, err); ok {
			resp, meta, compiled = retry.resp, retry.meta, retry.compiled
		} else {
			if retry.err != nil {
				err = retry.err
			}
			// An interrupt can arrive during the recovery call too.
			if ctx.Err() != nil && run.interruptedNow() {
				_, toolOnly := run.consumeInterrupt()
				if toolOnly {
					run.InjectNote("tool interrupted by the user; run decides how to continue")
					return false, nil
				}
				return true, nil
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				e.pauseDeadline(ctx, run)
				return true, nil
			}
			if errors.Is(err, models.ErrProviderQuotaExhausted) {
				if waitErr := e.Wait(ctx, run, types.WaitCapability); waitErr != nil {
					return true, waitErr
				}
				return true, err
			}
			return true, e.fail(ctx, run, "model call: "+err.Error())
		}
	}
	run.mu.Lock()
	run.observeUsageLocked(tasks.Usage{
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
		if e.explicitFinishRequired(run) && hasFinishCall(resp.Message.ToolCalls) {
			// A `finish` call is a tool turn: the text-only streak
			// resets whether the call is accepted or refused.
			run.resetTextStreak()
			// Record the assistant message before the control
			// handling so the trajectory keeps the model's words.
			run.mu.Lock()
			seq := run.nextHistorySeqLocked()
			run.History = append(run.History, ctxcompiler.ActivityEvent{
				Seq: seq, Role: providers.RoleAssistant, Content: resp.Message.Content,
				ToolCalls: resp.Message.ToolCalls,
			})
			run.mu.Unlock()
			if len(resp.Message.ToolCalls) > 1 {
				run.recordFinishAttempt(string(resp.Message.ToolCalls[0].Arguments), resp.Message.Content)
				reason := e.refuseFinish(ctx, run, []string{"`finish` must be called alone, with no other tool calls in the same turn."})
				run.recordFinishRefusal(reason)
				for _, call := range resp.Message.ToolCalls {
					if isFinishCall(call) {
						run.auditFinish(e.deps.Clock(), call.ID, "finish_batch", "refusal", reason)
					}
				}
				e.recordCallResults(run, resp.Message.ToolCalls, reason)
				run.InjectNote(reason)
				return false, nil
			}
			if e.quality().LoopGuardV2 {
				if msg, refused := run.guardRefusal(resp.Message.ToolCalls[0]); refused {
					run.auditFinish(e.deps.Clock(), resp.Message.ToolCalls[0].ID, "loop_guard", "refusal", msg)
					e.recordBlockedCall(run, resp.Message.ToolCalls[0], tools.Result{Status: tools.StatusInputError, Output: msg})
					return false, nil
				}
			}
			stop, err := e.execFinishBatch(ctx, run, resp.Message.ToolCalls[0], resp.Message.Content)
			// A repeated finish with no change counts toward the loop
			// guard key: the note comes at the third identical result.
			refusal := ""
			if !stop {
				refusal = run.lastRefusal()
			}
			if note := run.observeFinish(resp.Message.ToolCalls[0], refusal, e.quality().LoopGuardV2); note != "" {
				run.InjectNote(note)
			}
			return stop, err
		}
		// Record the assistant tool-call message before execution:
		// the next model request must carry the function_call items
		// its function_call_output items answer, or strict wires
		// (Responses) reject the turn and the history loses the
		// model's own words (Section 19.2).
		run.mu.Lock()
		seq := run.nextHistorySeqLocked()
		run.History = append(run.History, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleAssistant, Content: resp.Message.Content,
			ToolCalls: resp.Message.ToolCalls,
		})
		run.mu.Unlock()
		run.resetTextStreak()
		return e.execTools(ctx, run, resp.Message.ToolCalls)
	}

	if e.explicitFinishRequired(run) {
		return e.handleTextOnly(ctx, run, resp.Message.Content)
	}
	return e.finishTurn(ctx, run, resp.Message.Content)
}

// checkBudgets folds elapsed time into the usage before evaluating.
func (e *Engine) checkBudgets(run *Run, elapsed time.Duration) tasks.Status {
	run.mu.Lock()
	defer run.mu.Unlock()
	dup := run.Tracker
	dup.Used.Elapsed = elapsed
	if run.assumedWallLocked() {
		dup.Limit.MaxSeconds = HardWallSeconds(dup.Limit.MaxSeconds, "default")
	}
	return dup.Evaluate()
}

// safetyCapExhausted reports whether a step safety cap is reached in
// time-only mode. With the spend caps on, the USD cost cap is not a
// safety cap: it has its own closing and stop rules (spendLevel).
func (e *Engine) safetyCapExhausted(run *Run, elapsed time.Duration, spendOn bool) bool {
	run.mu.Lock()
	dup := run.Tracker
	if run.assumedWallLocked() {
		dup.Limit.MaxSeconds = HardWallSeconds(dup.Limit.MaxSeconds, "default")
	}
	run.mu.Unlock()
	dup.Used.Elapsed = elapsed
	if spendOn {
		dup.Limit.MaxCostUSD = 0
	}
	return dup.Evaluate() == tasks.StatusExhausted
}

// injectOnce volatile-notes text the first time flag is claimed.
// Callers hold no lock.
func (r *Run) injectOnce(flag *bool, note string) {
	r.mu.Lock()
	noted := *flag
	*flag = true
	r.mu.Unlock()
	if !noted {
		r.InjectNote(note)
	}
}

// timeFooterFor renders the time footer of a tool result observed now.
func (e *Engine) timeFooterFor(run *Run) string {
	return withFooter(run.timeFooter(e.deps.Clock()), run.confirmationFooter())
}

// pauseDeadline persists a wall-clock stop even after the call context expires.
func (e *Engine) pauseDeadline(ctx context.Context, run *Run) {
	run.mu.Lock()
	run.HeartbeatAt = e.deps.Clock().UTC()
	run.mu.Unlock()
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if done, _ := e.completePendingConfirmation(persist, run, e.deps.Clock()); done {
		return
	}
	e.pauseBudget(persist, run)
}

// pauseBudget parks the run with the budget motive and its delivery
// report (18.4). The user may extend the budget and resume.
func (e *Engine) pauseBudget(ctx context.Context, run *Run) {
	run.mu.Lock()
	from := run.state
	if err := run.transition(types.RunPaused); err != nil {
		run.mu.Unlock()
		return
	}
	run.pauseReason = types.PauseBudget
	elapsed := run.HeartbeatAt.Sub(run.StartedAt)
	dup := run.Tracker
	dup.Used.Elapsed = elapsed
	exhausted := dup.ExhaustedBy()
	timeOnly := run.Profile.TimeBudgetOnly
	run.mu.Unlock()
	if timeOnly {
		// In wall-clock-only mode the honest report names the wall
		// clock, plus any spend cap that reached its stop share.
		exhausted = timeOnlyExhaustedBy(dup.Limit, elapsed)
		_, over := run.spendLevel()
		exhausted = append(exhausted, over...)
		if len(exhausted) == 0 {
			exhausted = []string{"time"}
		}
	}
	run.InjectNote("budget exhausted: " + joinList(exhausted))
	e.recordState(ctx, run, from, types.RunPaused, "budget")
	if e.deps.LeaseRelease != nil {
		e.deps.LeaseRelease(run.ID)
	}
}

// fail moves the run to failed with its cause.
func (e *Engine) fail(ctx context.Context, run *Run, cause string) error {
	run.mu.Lock()
	from := run.state
	// A terminal run (an accepted finish) keeps its recorded state:
	// a later provider error never downgrades it (items 47, 48).
	if err := run.transition(types.RunFailed); err != nil {
		run.mu.Unlock()
		return fmt.Errorf("engine: run %s failed after %s: %s", run.ID, from, cause)
	}
	run.mu.Unlock()
	e.recordState(ctx, run, from, types.RunFailed, cause)
	if e.deps.LeaseRelease != nil {
		e.deps.LeaseRelease(run.ID)
	}
	return fmt.Errorf("engine: run %s failed: %s", run.ID, cause)
}

// finishTurn evaluates the gate when the model answers without tools
// (7.3): the run moves running -> verifying while the deterministic
// gate runs. The mandatory self-verification phase merges in next:
// fresh executable evidence, contrast of the deliverable against the
// literal instruction, and the independent-verdict duty on big
// tasks. On success it completes; otherwise the compact note is
// injected, the run returns to executing and the agent continues.
// After GateAttempts failures the run ends in its honest state, never
// claiming success without evidence.
//
// Before the gate, the finish guard (agent quality) sends runs with
// no verified work and no wired gate back to work with a verification
// reminder, so thin answers cannot slip through an unwired gate.
func (e *Engine) finishTurn(ctx context.Context, run *Run, finalText string) (bool, error) {
	if cont, note := e.guardFinish(run); cont {
		run.auditFinish(e.deps.Clock(), "", "finish_guard", "refusal", note)
		run.InjectNote(note)
		return false, nil
	}
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
	rep = e.selfVerify(ctx, run, rep, finalText)
	if reason := scopeRefusal(run); reason != "" {
		rep.OK = false
		rep.Pendings = append(rep.Pendings, reason)
	}
	// Long-horizon persistence: when significant budget remains and
	// executable evidence is still thin, continue with a concise "what
	// is still unproven" message instead of finishing shallow. This
	// fires both on a vacuous pass and when the honest end would land
	// while time remains. Bounded; after the bound the run closes
	// instead of continuing.
	if rep.OK {
		if cont, note := e.lhShouldContinue(run, rep); cont {
			run.mu.Lock()
			back := run.state
			_ = run.transition(types.RunRunning)
			run.mu.Unlock()
			e.recordState(ctx, run, back, types.RunRunning, "long-horizon continuation")
			run.auditFinish(e.deps.Clock(), "", "long_horizon", "refusal", note)
			run.InjectNote(note)
			return false, nil
		}
	}
	run.mu.Lock()
	run.LastGate = rep
	attempts := run.GateAttempts
	run.mu.Unlock()
	if rep.OK {
		run.auditFinish(e.deps.Clock(), "", "completion_gate", "acceptance", finalText)
		return true, e.complete(ctx, run, types.RunCompleted, "")
	}
	run.mu.Lock()
	run.GateAttempts++
	run.mu.Unlock()
	run.auditFinish(e.deps.Clock(), "", "completion_gate", "refusal", tasks.CompactNote(rep))
	if attempts+1 < tasks.GateAttempts {
		run.mu.Lock()
		back := run.state
		_ = run.transition(types.RunRunning)
		run.mu.Unlock()
		e.recordState(ctx, run, back, types.RunRunning, "gate retry")
		run.InjectNote(verificationNote(rep))
		return false, nil
	}
	// Gate attempts exhausted but significant budget remains and
	// evidence is still thin: the long-horizon continuation extends
	// the run instead of ending honest early.
	if cont, note := e.lhShouldContinue(run, rep); cont {
		run.mu.Lock()
		back := run.state
		_ = run.transition(types.RunRunning)
		run.mu.Unlock()
		e.recordState(ctx, run, back, types.RunRunning, "long-horizon continuation")
		run.InjectNote(note)
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
	if e.deps.LeaseRelease != nil && to.IsTerminal() {
		e.deps.LeaseRelease(run.ID)
	}
	return nil
}

// execTools runs one batch plan with write-ahead journaling, history
// append and loop observation (7.3-7.4). It returns stop=true when a
// execution fails or a non-loop stop is requested.
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
//
// Loop guard v2 (default on) refuses a call that sits inside its refusal
// window and counts every executed call by its exact key; the legacy
// exact-repeat blocker applies only when the guard is switched off.
// Every call's result reaches the model with the time footer appended.
func (e *Engine) execBatch(ctx context.Context, run *Run, batch []providers.ToolCall) (bool, error) {
	q := e.quality()
	// Exact-repeat blocker (agent quality, loop guard v2 off): a
	// single-call batch that repeats the previous single call
	// byte-for-byte, with a deterministic outcome, is rejected once per
	// run and signature instead of executing. Retryable outcomes
	// (transient, timeout, canceled) are never blocked: an immediate
	// retry may be the correct repair. Multi-call batches pass through:
	// the blocker only covers the unambiguous one-call repeat.
	if len(batch) == 1 && q.RepeatBlock && !q.LoopGuardV2 {
		if msg, blocked := checkRepeatBlock(run, batch[0]); blocked {
			res := tools.Result{Status: tools.StatusInputError, Output: msg}
			e.recordBlockedCall(run, batch[0], res)
			for _, d := range run.Loop.ObserveNoInfo(false) {
				if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
					return stop, err
				}
			}
			return false, nil
		}
	}
	// Calls inside a refusal window are not executed: their result is
	// the refusal text. The rest of the batch runs as usual.
	refused := make([]string, len(batch))
	var runCalls []providers.ToolCall
	var runIdx []int
	for i, tc := range batch {
		if q.LoopGuardV2 {
			if msg, ok := run.guardRefusal(tc); ok {
				refused[i] = msg
				continue
			}
		}
		runCalls = append(runCalls, tc)
		runIdx = append(runIdx, i)
	}
	beforeRecheck := run.recheckSnapshot()
	results := make([]tools.Result, len(batch))
	if len(runCalls) > 0 {
		ran := e.runParallel(ctx, run, runCalls)
		for j, i := range runIdx {
			results[i] = ran[j]
		}
	}
	for i, msg := range refused {
		if msg != "" {
			results[i] = tools.Result{Status: tools.StatusInputError, Output: msg}
		}
	}
	if run.scopeGuard != nil {
		for i, r := range results {
			if refused[i] != "" {
				continue
			}
			run.scopeGuard.ObserveCall(batch[i].Name, string(batch[i].Arguments), r.Output, r.Status != tools.StatusOK || strings.Contains(r.Output, "AssertionError") || strings.Contains(r.Output, "FAIL"))
		}
	}
	afterRecheck := run.recheckSnapshot()
	unchanged := beforeRecheck != "" && beforeRecheck == afterRecheck
	for range batch {
		run.observeConfirmation(unchanged)
	}
	liveProgress := false
	for i, tc := range batch {
		res := results[i]
		recordCall(run, callSig(tc))
		out := withFooter(res.Output, e.timeFooterFor(run))
		run.mu.Lock()
		dependencies := run.pendingEvidence[tc.ID]
		delete(run.pendingEvidence, tc.ID)
		run.mu.Unlock()
		run.mu.Lock()
		seq := run.nextHistorySeqLocked()
		if refused[i] != "" {
			run.markLoopRefusedLocked(seq)
		}
		run.History = append(run.History, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleTool,
			Content: out, Dependencies: dependencies,
			ToolResult: &providers.ToolResult{CallID: tc.ID, Name: tc.Name, Output: out, IsError: res.Status != tools.StatusOK},
		})
		run.mu.Unlock()
		if refused[i] != "" {
			// A refused call never executed, so it stays out of the
			// detector windows, as the legacy blocker did.
			for _, d := range run.Loop.ObserveNoInfo(false) {
				if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
					return stop, err
				}
			}
			continue
		}
		if note := run.observeRecheck(res, unchanged, tc); note != "" {
			run.InjectNote(note)
		}
		if q.LoopGuardV2 {
			if note := run.observeExact(tc, res); note != "" {
				run.InjectNote(note)
			}
		}
		// Running build/install output must not trigger legacy near-repeat,
		// cycle or no-information rules either. The exact guard also skips it.
		if q.LoopGuardV2 && run.sessionRunning(tc, res) {
			liveProgress = true
			continue
		}
		for _, d := range run.Loop.ObserveTool(tasks.ToolEvent{
			Tool: tc.Name, Args: string(tc.Arguments), Result: res.Output,
			Error: toolErrorOf(res),
		}) {
			if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
				return stop, err
			}
		}
	}
	rememberSingle(run, batch, results)
	if q.StuckRecovery {
		informative := markNovelty(run, batch, results)
		for _, d := range run.Loop.ObserveNoInfo(informative || liveProgress) {
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
	// R4 needs a real progress signal. While none is installed (exec
	// and eval single-run scope: nothing ever calls SetProgressFunc)
	// the zero state would fire spuriously every 8 steps, so the
	// stuck-recovery mechanism skips it and tracks novelty from
	// tool results instead. Off restores the legacy observation.
	hasSignal := progressFn != nil || prog != (tasks.ProgressState{})
	if (!q.StuckRecovery || hasSignal) && !liveProgress {
		for _, d := range run.Loop.ObserveProgress(prog) {
			if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
				return stop, err
			}
		}
	}
	e.harvestBatch(run, batch, results)
	return false, nil
}

// callSig is the exact-repeat identity of one tool call: the tool
// name plus the raw argument bytes.
func callSig(tc providers.ToolCall) string {
	return tc.Name + "\x00" + string(tc.Arguments)
}

// displaySig shortens a call signature for notes and prompts.
func displaySig(sig string) string {
	tool, args, _ := strings.Cut(sig, "\x00")
	if strings.TrimSpace(args) == "" {
		return tool
	}
	return tool + " " + truncOneLine(args, 80)
}

// checkRepeatBlock reports whether a single tool call repeats the
// run's previous single call with identical bytes and a
// deterministic outcome, and has not been blocked before. The one
// block per signature is claimed here, so concurrent checks cannot
// double-block.
func checkRepeatBlock(run *Run, tc providers.ToolCall) (string, bool) {
	sig := callSig(tc)
	run.mu.Lock()
	defer run.mu.Unlock()
	if sig != run.prevSingleSig || run.prevSingleSig == "" {
		return "", false
	}
	if run.blockedSigs != nil && run.blockedSigs[sig] {
		return "", false
	}
	if run.blockedSigs == nil {
		run.blockedSigs = map[string]bool{}
	}
	run.blockedSigs[sig] = true
	msg := "blocked: exact repeat of the previous " + displaySig(sig) +
		" with identical arguments. It would produce no new information. " +
		"Summarise what the last result showed, pick a different approach " +
		"(a different tool or different arguments), and execute that instead."
	return msg, true
}

// rememberSingle updates the previous-single-call state after an
// executed batch: a single call with a deterministic outcome arms
// the blocker; anything else (multi-call batches, retryable
// outcomes) disarms it.
func rememberSingle(run *Run, batch []providers.ToolCall, results []tools.Result) {
	run.mu.Lock()
	defer run.mu.Unlock()
	if len(batch) == 1 && isDeterministic(results[0]) {
		run.prevSingleSig = callSig(batch[0])
		return
	}
	run.prevSingleSig = ""
}

// isDeterministic reports whether repeating the call is pointless:
// retryable outcomes (transient, timeout, canceled) may succeed on
// retry, every other outcome will not change by itself.
func isDeterministic(res tools.Result) bool {
	if res.Transient {
		return false
	}
	switch res.Status {
	case tools.StatusTimeout, tools.StatusCanceled:
		return false
	default:
		return true
	}
}

// recordBlockedCall journals a rejected repeat as an error tool
// result (so the model sees the guidance on its next step) without
// feeding the loop detector: the call never executed, so it must
// not pollute the R1/R2 windows.
func (e *Engine) recordBlockedCall(run *Run, tc providers.ToolCall, res tools.Result) {
	recordCall(run, callSig(tc))
	out := withFooter(res.Output, e.timeFooterFor(run))
	run.mu.Lock()
	seq := run.nextHistorySeqLocked()
	run.markLoopRefusedLocked(seq)
	run.History = append(run.History, ctxcompiler.ActivityEvent{
		Seq: seq, Role: providers.RoleTool,
		Content:    out,
		ToolResult: &providers.ToolResult{CallID: tc.ID, Name: tc.Name, Output: out, IsError: true},
	})
	run.mu.Unlock()
	e.harvestBatch(run, []providers.ToolCall{tc}, []tools.Result{res})
}

// markNovelty updates the per-tool output hashes and reports whether
// this batch produced new information: a successful call whose
// trimmed output is non-empty and differs from the same tool's
// previous successful output. Errors, empty outputs and repeats of
// the previous output are dull.
func markNovelty(run *Run, batch []providers.ToolCall, results []tools.Result) bool {
	informative := false
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.lastOutputs == nil {
		run.lastOutputs = map[string]string{}
	}
	for i, tc := range batch {
		res := results[i]
		if res.Status != tools.StatusOK {
			continue
		}
		out := strings.TrimSpace(res.Output)
		if out == "" {
			continue
		}
		sum := sha256.Sum256([]byte(out))
		h := hex.EncodeToString(sum[:])
		if run.lastOutputs[tc.Name] != h {
			informative = true
		}
		run.lastOutputs[tc.Name] = h
	}
	return informative
}

// harvestBatch pins one working note per tool outcome (paths,
// failures) and injects at most one error-hint note per step (agent
// quality), plus the long-horizon background nudge for long commands.
// Both are bounded and switch-gated.
func (e *Engine) harvestBatch(run *Run, batch []providers.ToolCall, results []tools.Result) {
	q := e.quality()
	if q.WorkingNotes {
		for i, tc := range batch {
			fact := factForToolCall(tc.Name, string(tc.Arguments), results[i])
			run.addFact(fact, q.MaxNotes)
		}
	}
	// Long-horizon discipline: long commands belong in background
	// sessions polled with exec_io instead of blocking or ending the
	// run. One volatile note per step at most.
	e.lhBackgroundNudge(run, batch, results)
	if !q.LoopNudges || e.deps.ErrorHint == nil {
		return
	}
	for i, tc := range batch {
		if results[i].Status == tools.StatusOK {
			continue
		}
		hint := e.deps.ErrorHint(tc.Name, string(tc.Arguments), toolErrorOf(results[i]))
		if hint == "" {
			continue
		}
		run.InjectNote("recovery hint for " + tc.Name + ": " + truncOneLine(hint, 300))
		return
	}
}

func toolErrorOf(res tools.Result) string {
	if res.Status == tools.StatusOK {
		return ""
	}
	return string(res.Status) + ": " + res.Output
}

// onDetection applies the staged loop response (18.3): a corrective
// note first, an expert consult or switch second, blocked
// with a report third. The near-repeat and no-information rules
// carry the structured stuck-recovery prompt at stage 1
// (agent quality); every other rule keeps its actionable nudge.
// Every detection is also pinned to the working notes so the
// pattern survives context growth.
func (e *Engine) onDetection(ctx context.Context, run *Run, d tasks.Detection) (bool, error) {
	q := e.quality()
	pin := func(note string) {
		if q.WorkingNotes {
			run.addFact("loop pattern: "+truncOneLine(d.Note, 140), q.MaxNotes)
		}
	}
	switch d.Action {
	case tasks.ActionNote:
		if q.StuckRecovery && (d.Rule == tasks.RuleNearRepeat || d.Rule == tasks.RuleNoInfo) {
			run.InjectNote(recoveryPrompt(q, d, recentTried(run, 3)))
		} else {
			run.InjectNote(nudgeFor(q, d, ""))
		}
		pin(d.Note)
		return false, nil
	case tasks.ActionEscalate:
		diag := ""
		if e.deps.Expert != nil {
			if expert, err := e.deps.Expert(ctx, run, d); err == nil && expert != "" {
				diag = expert
			}
		}
		run.InjectNote(escalateNote(q, d, diag))
		pin(d.Note)
		return false, nil
	case tasks.ActionBlock:
		// A loop never ends the run (backlog 99). The third and later
		// stages repeat the strategy-switch note without another expert
		// consult, so they cost no model call.
		run.InjectNote(escalateNote(q, d, ""))
		pin(d.Note)
		return false, nil
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
