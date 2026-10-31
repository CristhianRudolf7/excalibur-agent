package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Config carries the Appendix B limits the orchestrator enforces:
// fan-out caps, directive-ack deadlines and report-reminder cadence.
type Config struct {
	// MaxActiveChildren caps admitted children per parent (15.3).
	MaxActiveChildren int
	// MaxRunningPerUser caps running executions per user (15.3).
	MaxRunningPerUser int
	// DirectiveAckSteps escalates an unacked directive after 2 steps.
	DirectiveAckSteps int
	// DirectiveAckSecs escalates an unacked running directive after 120s.
	DirectiveAckSecs int64
	// ReportReminderSteps reminds after 15 steps without report (15.6).
	ReportReminderSteps int
	// ReportReminderSecs reminds after 10 minutes without report.
	ReportReminderSecs int64
	// HysteresisFrac resumes below this fraction of the RAM threshold.
	HysteresisFrac float64
	// HysteresisSecs holds the low watermark this long before resuming.
	HysteresisSecs int64
	// Clock is injected for tests.
	Clock func() time.Time
}

// DefaultConfig returns the Appendix B values.
func DefaultConfig() Config {
	return Config{
		MaxActiveChildren:   4,
		MaxRunningPerUser:   8,
		DirectiveAckSteps:   2,
		DirectiveAckSecs:    120,
		ReportReminderSteps: 15,
		ReportReminderSecs:  600,
		HysteresisFrac:      0.8,
		HysteresisSecs:      60,
		Clock:               func() time.Time { return time.Now().UTC() },
	}
}

// EventType names one orchestrator bus event.
type EventType string

// Bus event types: state changes, deliveries, acks, escalations and
// notices for the master chain (Sections 5.4, 15.5).
const (
	EventStateChanged       EventType = "state_changed"
	EventMessageDelivered   EventType = "message_delivered"
	EventDirectiveAcked     EventType = "directive_acked"
	EventDirectiveEscalated EventType = "directive_escalated"
	EventNotice             EventType = "notice"
)

// Event is one live orchestrator notification. Embedded mode exposes
// the channel; the daemon will feed the event bus from it.
type Event struct {
	Type   EventType
	RunID  types.RunID
	From   types.AgentID
	To     types.AgentID
	Detail string
	At     time.Time
}

// Orchestrator is the Section 5.1 actor for runs: it owns the
// hierarchy, the ready queue, mailboxes, directives and resource
// admission. In embedded mode all methods are synchronous and
// mutex-serialized, which is the arrival-order serialization Section
// 15.8 requires; the daemon routes orders through the same methods.
// A background loop (Start) ticks admission and the RAM watchdog.
type Orchestrator struct {
	cfg   Config
	store Store
	jrnl  Journal

	mu      sync.Mutex
	runners RunnerRegistry
	bus     chan Event
	dropped int

	haltAdmit    bool
	lowSince     time.Time
	lastServed   map[string]int
	resThreshold uint64
	directives   map[types.MessageID]directiveWatch

	hooks Hooks

	stop   chan struct{}
	stopMu sync.Mutex
}

// Hooks are the seams to sibling milestones: planning progress,
// evidence invalidation, prohibition judgment, external-action
// knowledge, compensation execution and master notices. All hooks are
// optional; nil hooks degrade to deterministic no-ops.
type Hooks struct {
	// PlanProgress reports plan done/total for status (15.6).
	PlanProgress func(run types.RunID) (done, total int, milestone string)
	// InvalidateEvidence marks evidence stale after a correction.
	InvalidateEvidence func(taskIDs []types.TaskID, rev int)
	// Prohibits asks the decider whether an instruction forbids the
	// current action (15.7 point 4).
	Prohibits func(action, instruction string) (bool, float64)
	// PendingExternal reports a pending external action affected by
	// a change (15.7 point 4).
	PendingExternal func(run types.RunID) bool
	// Sampler reads the daemon RSS for the watchdog (5.2).
	Sampler func() (uint64, error)
	// KillTool destroys the largest tool process of a run (5.2 last
	// resort). It reports what it killed.
	KillTool func(run types.RunID) (string, error)
	// ReleaseLeases frees the file leases a run holds when it pauses
	// or blocks (Section 16.2, workspace milestone). Nil keeps the
	// orchestrator lease-agnostic in embedded mode.
	ReleaseLeases func(run types.RunID)
}

// New returns an orchestrator over store with journal.
func New(cfg Config, store Store, jrnl Journal) *Orchestrator {
	if cfg.MaxActiveChildren <= 0 {
		cfg.MaxActiveChildren = 4
	}
	if cfg.MaxRunningPerUser <= 0 {
		cfg.MaxRunningPerUser = 8
	}
	if cfg.DirectiveAckSteps <= 0 {
		cfg.DirectiveAckSteps = 2
	}
	if cfg.DirectiveAckSecs <= 0 {
		cfg.DirectiveAckSecs = 120
	}
	if cfg.Clock == nil {
		cfg.Clock = func() time.Time { return time.Now().UTC() }
	}
	if jrnl == nil {
		jrnl = NoopJournal{}
	}
	return &Orchestrator{
		cfg:        cfg,
		store:      store,
		jrnl:       jrnl,
		bus:        make(chan Event, 256),
		lastServed: map[string]int{},
		directives: map[types.MessageID]directiveWatch{},
		stop:       make(chan struct{}),
	}
}

// SetHooks installs the sibling-milestone seams. Nil hooks degrade to
// deterministic no-ops.
func (o *Orchestrator) SetHooks(h Hooks) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.hooks = h
}

// Watch exposes the live event channel for embedded consumers.
func (o *Orchestrator) Watch() <-chan Event { return o.bus }

// Dropped counts events lost to a full bus.
func (o *Orchestrator) Dropped() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dropped
}

func (o *Orchestrator) emit(e Event) {
	e.At = o.cfg.Clock()
	select {
	case o.bus <- e:
	default:
		o.dropped++
	}
}

func (o *Orchestrator) journal(run types.RunID, typ, payload string) {
	_ = o.jrnl.Append(context.Background(), run, typ, payload)
}

// Start launches the background admission/watchdog loop; Stop ends it.
// Every long-lived goroutine uses safego.Go (Section 3.2).
func (o *Orchestrator) Start(ctx context.Context) {
	o.stopMu.Lock()
	o.stop = make(chan struct{})
	stop := o.stop
	o.stopMu.Unlock()
	safego.Go("orchestrator-loop", func(owner string, recovered any, stack []byte) {
		o.mu.Lock()
		defer o.mu.Unlock()
		for _, rec := range o.store.ListRuns() {
			if !rec.Run.State.IsTerminal() {
				rec.Run.State = types.RunPaused
				paused := types.PauseSafeMode
				rec.Run.PauseReason = &paused
				_ = o.store.UpdateRun(rec)
			}
		}
	}, func() {
		admit := time.NewTicker(200 * time.Millisecond)
		defer admit.Stop()
		sample := time.NewTicker(time.Second)
		defer sample.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-admit.C:
				o.mu.Lock()
				o.admitLocked()
				o.mu.Unlock()
			case <-sample.C:
				o.mu.Lock()
				o.watchdogLocked()
				o.mu.Unlock()
			}
		}
	})
}

// Stop ends the background loop started by Start.
func (o *Orchestrator) Stop() {
	o.stopMu.Lock()
	defer o.stopMu.Unlock()
	select {
	case <-o.stop:
	default:
		close(o.stop)
	}
}

// RegisterMaster creates the level-0 master identity (Section 15.1).
func (o *Orchestrator) RegisterMaster(name string, scope types.Scope) (types.Agent, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	profile, err := agents.Lookup(agents.ProfileMaster)
	if err != nil {
		return types.Agent{}, err
	}
	a, err := agents.NewIdentity(name, "converses, decides, delegates, informs", profile, nil, scope)
	if err != nil {
		return types.Agent{}, err
	}
	if err := o.store.SaveAgent(a); err != nil {
		return types.Agent{}, err
	}
	return a, nil
}

// Spawn creates a child agent plus its run from a delegation card
// (Section 15.4). The run starts created and moves to queued; Admit
// moves it to running when capacity allows. Level-2 parents are
// rejected, the toolset is clamped to the profile, and the budget is
// capped by the parent remainder (Sections 15.1, 15.9).
func (o *Orchestrator) Spawn(parentID types.AgentID, card DelegationCard) (types.Agent, RunRecord, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	parent, err := o.store.GetAgent(parentID)
	if err != nil {
		return types.Agent{}, RunRecord{}, err
	}
	profile, err := agents.Lookup(card.Profile)
	if err != nil {
		// Default profile by level: coordinator below the master,
		// implementer below a parent (Section 15.3: brief tasks and
		// work delegation).
		level, lerr := agents.ChildLevel(parent)
		if lerr != nil {
			return types.Agent{}, RunRecord{}, lerr
		}
		if level == 1 {
			profile, _ = agents.Lookup(agents.ProfileCoordinator)
		} else {
			profile, _ = agents.Lookup(agents.ProfileImplementer)
		}
		card.Profile = profile.Name
	}
	level, err := agents.ChildLevel(parent)
	if err != nil {
		o.journal(types.RunID(""), "spawn_rejected", string(parent.ID))
		return types.Agent{}, RunRecord{}, err
	}
	if !agents.MaySpawn(mustLookup(parent.Profile), parent.Level) {
		return types.Agent{}, RunRecord{}, fmt.Errorf("orchestrator: agent %q must not spawn", parent.Name)
	}
	if err := card.Validate(o.cfg.Clock(), level); err != nil {
		return types.Agent{}, RunRecord{}, err
	}
	tools, err := agents.ClampTools(profile, level, card.Tools)
	if err != nil {
		return types.Agent{}, RunRecord{}, err
	}
	_ = tools
	child, err := agents.NewIdentity(card.Name, card.Description, profile, &parent, card.Scope)
	if err != nil {
		return types.Agent{}, RunRecord{}, err
	}
	parentRun, hasParentRun := o.parentRunLocked(parent.ID)
	budget := types.Budget{MaxSteps: profile.DefaultSteps}
	used := Usage{}
	if hasParentRun {
		used = o.store.GetUsage(parentRun.Run.ID)
	}
	if card.Budget.MaxSteps > 0 || card.Budget.MaxTokens > 0 || card.Budget.MaxCostUSD > 0 ||
		card.Budget.MaxSeconds > 0 || card.Budget.MaxProcs > 0 {
		if hasParentRun {
			budget = ClampChildBudget(parentRun.Run.Budget, used, card.Budget, profile.DefaultSteps)
		} else {
			budget = card.Budget
			if budget.MaxSteps <= 0 {
				budget.MaxSteps = profile.DefaultSteps
			}
		}
	} else if hasParentRun {
		budget = ClampChildBudget(parentRun.Run.Budget, used, types.Budget{}, profile.DefaultSteps)
	}
	now := o.cfg.Clock()
	lane := card.Lane
	if hasParentRun && lane == LaneP0 {
		lane = parentRun.Lane
	}
	if lane == LaneP0 {
		lane = LaneP1
	}
	rec := RunRecord{
		Run: types.Run{
			ID:          types.NewRunID(),
			AgentID:     child.ID,
			TaskID:      card.TaskID,
			State:       types.RunCreated,
			Budget:      budget,
			HeartbeatAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Lane:  lane,
		Scope: card.Scope,
	}
	if err := o.store.SaveAgent(child); err != nil {
		return types.Agent{}, RunRecord{}, err
	}
	if err := o.store.CreateRun(rec); err != nil {
		return types.Agent{}, RunRecord{}, err
	}
	o.journal(rec.Run.ID, JournalRunStarted, string(card.Profile))
	if err := o.transitionLocked(rec.Run.ID, types.RunQueued, "admitted to queue"); err != nil {
		return types.Agent{}, RunRecord{}, err
	}
	rec, _ = o.store.GetRun(rec.Run.ID)
	o.admitLocked()
	rec, _ = o.store.GetRun(rec.Run.ID)
	return child, rec, nil
}

func mustLookup(name string) agents.Profile {
	p, err := agents.Lookup(agents.ProfileName(name))
	if err != nil {
		return agents.Profile{Name: agents.ProfileName(name)}
	}
	return p
}

// parentRunLocked returns the newest non-terminal run of an agent, if
// any. Exported for control and tests as ParentRun.
func (o *Orchestrator) parentRunLocked(agent types.AgentID) (RunRecord, bool) {
	var best RunRecord
	found := false
	for _, rec := range o.store.ListRuns() {
		if rec.Run.AgentID != agent || rec.Run.State.IsTerminal() {
			continue
		}
		if !found || rec.Run.CreatedAt.After(best.Run.CreatedAt) {
			best, found = rec, true
		}
	}
	return best, found
}

// ParentRun returns the newest non-terminal run of an agent.
func (o *Orchestrator) ParentRun(agent types.AgentID) (RunRecord, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.parentRunLocked(agent)
}

// AttachRunner records the live engine driving run in embedded mode.
func (o *Orchestrator) AttachRunner(r Runner) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.runners.Attach(r)
}

// DetachRunner forgets the engine driving run.
func (o *Orchestrator) DetachRunner(run types.RunID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.runners.Detach(run)
}

// RunnerFor returns the live engine driving run, if any.
func (o *Orchestrator) RunnerFor(run types.RunID) (Runner, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.runners.Lookup(run)
}

// RunState reports the authoritative state of a run for engines and
// tests.
func (o *Orchestrator) RunState(id types.RunID) (types.RunState, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(id)
	if err != nil {
		return "", err
	}
	return rec.Run.State, nil
}

// Tree returns the agent subtree rooted at agent in deterministic
// order: the agent first, then each child subtree.
func (o *Orchestrator) Tree(agent types.AgentID) []types.Agent {
	o.mu.Lock()
	defer o.mu.Unlock()
	root, err := o.store.GetAgent(agent)
	if err != nil {
		return nil
	}
	out := []types.Agent{root}
	for _, c := range o.store.Children(agent) {
		out = append(out, o.treeLocked(c.ID)...)
	}
	return out
}

func (o *Orchestrator) treeLocked(agent types.AgentID) []types.Agent {
	root, err := o.store.GetAgent(agent)
	if err != nil {
		return nil
	}
	out := []types.Agent{root}
	for _, c := range o.store.Children(agent) {
		out = append(out, o.treeLocked(c.ID)...)
	}
	return out
}

// ActiveChildren counts admitted children of parent: runs in running,
// waiting, verifying or recovering. Queued, paused and blocked runs
// hold no engine and do not count (Section 15.3).
func (o *Orchestrator) ActiveChildren(parent types.AgentID) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.activeChildrenLocked(parent)
}

func (o *Orchestrator) activeChildrenLocked(parent types.AgentID) int {
	n := 0
	for _, c := range o.store.Children(parent) {
		rec, ok := o.parentRunLocked(c.ID)
		if !ok {
			continue
		}
		switch rec.Run.State {
		case types.RunRunning, types.RunWaiting, types.RunVerifying, types.RunRecovering:
			n++
		}
	}
	return n
}

// RunningCount counts executions in running for the admission cap.
// Waiting, paused and blocked runs do not count, so a parent waiting
// on its children never blocks their admission (Section 15.3).
func (o *Orchestrator) RunningCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.store.RunsByState(types.RunRunning))
}

// admitLocked moves queued runs to running while capacity allows:
// the global running cap, the per-parent active-children cap and the
// watchdog halt. Queued runs keep their queue position (Section 15.6).
func (o *Orchestrator) admitLocked() {
	if o.haltAdmit {
		return
	}
	queued := o.store.RunsByState(types.RunQueued)
	if len(queued) == 0 {
		return
	}
	// Fair sharing between scopes: round robin within each lane
	// (Section 5.2).
	byLane := map[Lane][]RunRecord{}
	for _, rec := range queued {
		byLane[rec.Lane] = append(byLane[rec.Lane], rec)
	}
	for _, lane := range []Lane{LaneP1, LaneP2, LaneP3} {
		q := byLane[lane]
		if len(q) == 0 {
			continue
		}
		ordered := o.roundRobinLocked(q)
		for _, rec := range ordered {
			if len(o.store.RunsByState(types.RunRunning)) >= o.cfg.MaxRunningPerUser {
				o.renumberLocked(lane)
				return
			}
			agent, err := o.store.GetAgent(rec.Run.AgentID)
			if err != nil {
				continue
			}
			if agent.ParentID != nil && o.activeChildrenLocked(*agent.ParentID) >= o.cfg.MaxActiveChildren {
				continue
			}
			if err := o.transitionLocked(rec.Run.ID, types.RunRunning, "admitted"); err != nil {
				continue
			}
		}
		o.renumberLocked(lane)
	}
}

// roundRobinLocked rotates each scope group past the last served
// index so projects share the lane fairly.
func (o *Orchestrator) roundRobinLocked(q []RunRecord) []RunRecord {
	groups := map[string][]RunRecord{}
	var scopes []string
	for _, rec := range q {
		key := rec.Scope.String()
		if _, ok := groups[key]; !ok {
			scopes = append(scopes, key)
		}
		groups[key] = append(groups[key], rec)
	}
	sort.Strings(scopes)
	start := 0
	if len(scopes) > 0 {
		laneKey := "lane:" + q[0].Lane.String()
		start = o.lastServed[laneKey] % len(scopes)
		o.lastServed[laneKey] = start + 1
	}
	var out []RunRecord
	for k := 0; k < len(scopes); k++ {
		g := groups[scopes[(start+k)%len(scopes)]]
		sort.Slice(g, func(i, j int) bool { return g[i].Run.CreatedAt.Before(g[j].Run.CreatedAt) })
		out = append(out, g...)
	}
	return out
}

// renumberLocked refreshes queue positions of one lane in creation
// order so status shows them (Section 15.6).
func (o *Orchestrator) renumberLocked(lane Lane) {
	pos := 0
	for _, rec := range o.store.RunsByState(types.RunQueued) {
		if rec.Lane != lane {
			continue
		}
		rec.Run.QueuePosition = pos
		pos++
		_ = o.store.UpdateRun(rec)
	}
}

// transitionLocked moves a run to a new state after checking the
// Section 7.2 diagram, records the journal row and emits the bus
// event. Reason fields are set by the caller beforehand, except for
// terminal clears handled here.
func (o *Orchestrator) transitionLocked(id types.RunID, to types.RunState, detail string) error {
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if rec.Run.State == to {
		return nil
	}
	if !types.CanTransition(rec.Run.State, to) {
		return fmt.Errorf("orchestrator: run %q cannot move %q -> %q",
			id, rec.Run.State, to)
	}
	from := rec.Run.State
	rec.Run.State = to
	if to.IsTerminal() {
		rec.Run.QueuePosition = 0
	} else {
		rec.Run.UpdatedAt = o.cfg.Clock()
	}
	if err := o.store.UpdateRun(rec); err != nil {
		return err
	}
	o.journal(id, JournalStateChange, string(from)+"->"+string(to))
	agent := rec.Run.AgentID
	o.emit(Event{Type: EventStateChanged, RunID: id, To: agent, Detail: detail})
	return nil
}
