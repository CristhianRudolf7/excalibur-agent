package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// OrchestratorLauncher delivers agenda fires and reminders through
// the H8 orchestrator (Sections 17.4, 21.1, 21.2): every Launch call
// is quick (it enqueues work, never runs it) because the agenda Tick
// invokes it synchronously to preserve fire order. Fires spawn one
// coordinator child under the daemon-owned agenda agent; reminders
// post a Section 5.4 notice (grouped, once per channel) and spawn a
// follow-up run only when they link a task.
type OrchestratorLauncher struct {
	chat     *DaemonBackend
	orch     *orchestrator.Orchestrator
	notify   *master.Notifier
	parent   types.AgentID
	scope    types.Scope
	seq      atomic.Uint64
	now      func() time.Time
	deadline time.Duration
}

// NewOrchestratorLauncher returns a launcher that spawns under a
// daemon-owned agenda agent. It registers that agent once; without an
// orchestrator every call is a no-op error so miswiring surfaces
// loudly instead of dropping fires silently.
func NewOrchestratorLauncher(orch *orchestrator.Orchestrator, notify *master.Notifier) (*OrchestratorLauncher, error) {
	if orch == nil {
		return nil, fmt.Errorf("daemon: orchestrator launcher needs an orchestrator")
	}
	scope := types.GeneralScope()
	agent, err := orch.RegisterMaster("agenda", scope)
	if err != nil {
		return nil, fmt.Errorf("daemon: register agenda agent: %w", err)
	}
	return &OrchestratorLauncher{
		orch: orch, notify: notify,
		parent: agent.ID, scope: scope,
		now:      time.Now,
		deadline: time.Hour,
	}, nil
}

// LaunchFire implements agenda.Launcher: one fire becomes one
// coordinator child carrying the fire id for idempotency (17.4).
func (l *OrchestratorLauncher) LaunchFire(ctx context.Context, fire agenda.Fire) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n := l.seq.Add(1)
	scope := l.scope
	if strings.TrimSpace(fire.Payload) != "" {
		// Payloads may pin a project scope as "project:<id>".
		if id, ok := strings.CutPrefix(strings.TrimSpace(fire.Payload), "project:"); ok {
			if i := strings.Index(id, " "); i >= 0 {
				id = id[:i]
			}
			if id != "" {
				scope = types.Scope{Kind: types.ScopeProject, ID: id}
			}
		}
	}
	goal := strings.TrimSpace(fire.Payload)
	if goal == "" {
		goal = "run the scheduled work for " + fire.ScheduleID
	}
	card := orchestrator.DelegationCard{
		Name:        fmt.Sprintf("fire-%s-%d", fire.ScheduleID, n),
		Description: "scheduled work launcher",
		Goal:        goal,
		Deliverable: "fire outcome recorded on the fire id " + fire.FireID,
		Scope:       scope,
		Done:        []string{"outcome recorded"},
		Report:      "what ran, what changed, what is pending",
		Deadline:    l.now().Add(l.deadline),
	}
	if _, _, err := l.orch.Spawn(l.parent, card); err != nil {
		return fmt.Errorf("daemon: launch fire %q: %w", fire.FireID, err)
	}
	return nil
}

// LaunchReminder implements agenda.Launcher: the reminder text goes
// to the Section 5.4 notifier (delivery, grouping and quiet hours
// live there), and a linked task spawns a coordinator follow-up.
func (l *OrchestratorLauncher) LaunchReminder(ctx context.Context, r agenda.Reminder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	conv := ClassroomConversation
	if l.chat != nil {
		conv = l.chat.SystemConversation("")
		if _, err := l.chat.Master.SendAssistant(ctx, conv, r.Text, nil); err != nil {
			return err
		}
	}
	if l.notify != nil {
		l.notify.Post(master.Notice{
			ConversationID: conv,
			ID:             "reminder-" + r.ID,
			Class:          types.NoticeNormal,
			Content:        r.Text,
			CreatedAt:      l.now().UTC(),
		})
	}
	if strings.TrimSpace(r.TaskID) == "" {
		return nil
	}
	n := l.seq.Add(1)
	card := orchestrator.DelegationCard{
		Name:        fmt.Sprintf("reminder-%s-%d", r.ID, n),
		Description: "reminder follow-up",
		Goal:        r.Text,
		Deliverable: "reminder task outcome recorded",
		Scope:       l.scope,
		Done:        []string{"outcome recorded"},
		Report:      "what ran, what changed, what is pending",
		Deadline:    l.now().Add(l.deadline),
	}
	if _, _, err := l.orch.Spawn(l.parent, card); err != nil {
		return fmt.Errorf("daemon: launch reminder %q: %w", r.ID, err)
	}
	return nil
}

// QuestionConfirmer resolves agenda user-confirmation nodes through
// the orchestrator question flow (Sections 6.4, 21.4): the confirm
// node becomes a blocking question answered on the gate
// (question.answer), the CLI, or any client. A nil store declines
// every confirmation so dry runs never block.
type QuestionConfirmer struct {
	store orchestrator.Store
	// PollInterval paces the answer wait; zero defaults to 500 ms.
	PollInterval time.Duration
	now          func() time.Time
}

// NewQuestionConfirmer returns a confirmer over the orchestrator
// question store.
func NewQuestionConfirmer(store orchestrator.Store) *QuestionConfirmer {
	return &QuestionConfirmer{store: store, PollInterval: 500 * time.Millisecond, now: time.Now}
}

// WaitConfirm implements agenda.Confirmer.
func (c *QuestionConfirmer) WaitConfirm(ctx context.Context, instanceID, nodeID, prompt string) (bool, error) {
	if c.store == nil {
		return false, nil
	}
	id := "confirm-" + instanceID + "-" + nodeID
	q := types.Question{
		ID: id, Content: prompt, Blocking: true, Default: "no",
		Deadline: c.now().UTC().Add(15 * time.Minute),
		State:    "open", CreatedAt: c.now().UTC(), UpdatedAt: c.now().UTC(),
	}
	// A re-asked node reuses its question instead of duplicating it.
	if _, err := c.store.GetQuestion(id); err != nil {
		if serr := c.store.SaveQuestion(q); serr != nil {
			return false, serr
		}
	}
	interval := c.PollInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-t.C:
			got, err := c.store.GetQuestion(id)
			if err != nil {
				return false, err
			}
			if got.State == "answered" {
				return confirmYes(got.Answer), nil
			}
		}
	}
}

// confirmYes maps a user answer to the confirm verdict. Empty answers
// fall back to the question default (decline). Leading affirmative
// words ("yes, go ahead") confirm; anything else declines.
func confirmYes(answer string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(answer)))
	if len(fields) == 0 {
		return false
	}
	first := strings.Trim(fields[0], ".,;:!?\"'")
	switch first {
	case "yes", "y", "ok", "confirm", "approve", "approved", "go", "true", "confirm.":
		return true
	default:
		return false
	}
}

// DaemonTriggerLauncher delivers trigger-rule hits (Section 21.3):
// task starts and workflow launches spawn orchestrator children,
// notifications post to the Section 5.4 notifier, session starts
// enqueue an agenda fire for the project session workflow.
type DaemonTriggerLauncher struct {
	orch   *orchestrator.Orchestrator
	store  orchestrator.Store
	notify *master.Notifier
	parent types.AgentID
	now    func() time.Time
}

// NewDaemonTriggerLauncher returns the trigger launcher. The parent
// agent carries trigger-spawned work; without an orchestrator every
// call fails loudly.
func NewDaemonTriggerLauncher(orch *orchestrator.Orchestrator, store orchestrator.Store, notify *master.Notifier) (*DaemonTriggerLauncher, error) {
	if orch == nil {
		return nil, fmt.Errorf("daemon: trigger launcher needs an orchestrator")
	}
	agent, err := orch.RegisterMaster("triggers", types.GeneralScope())
	if err != nil {
		return nil, fmt.Errorf("daemon: register trigger agent: %w", err)
	}
	return &DaemonTriggerLauncher{orch: orch, store: store, notify: notify, parent: agent.ID, now: time.Now}, nil
}

// StartTask implements agenda.TriggerLauncher.
func (l *DaemonTriggerLauncher) StartTask(ctx context.Context, target string, ev agenda.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	card := orchestrator.DelegationCard{
		Name:        "trigger-" + target,
		Description: "event-triggered work",
		Goal:        "handle " + ev.Type + " event " + ev.ID + " for target " + target,
		Deliverable: "event handled and outcome recorded",
		Scope:       types.GeneralScope(),
		Done:        []string{"outcome recorded"},
		Report:      "what ran, what changed, what is pending",
		Deadline:    l.now().Add(time.Hour),
	}
	_, _, err := l.orch.Spawn(l.parent, card)
	return err
}

// LaunchFlow implements agenda.TriggerLauncher: the workflow starts
// as an orchestrator child pinned to the workflow target.
func (l *DaemonTriggerLauncher) LaunchFlow(ctx context.Context, target string, ev agenda.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	card := orchestrator.DelegationCard{
		Name:        "flow-" + target,
		Description: "event-triggered workflow",
		Goal:        "run workflow " + target + " for event " + ev.ID,
		Deliverable: "workflow outcome recorded",
		Scope:       types.GeneralScope(),
		Inputs:      []string{ev.ID},
		Done:        []string{"outcome recorded"},
		Report:      "what ran, what changed, what is pending",
		Deadline:    l.now().Add(2 * time.Hour),
	}
	_, _, err := l.orch.Spawn(l.parent, card)
	return err
}

// Notify implements agenda.TriggerLauncher.
func (l *DaemonTriggerLauncher) Notify(_ context.Context, target string, ev agenda.Event) error {
	if l.notify == nil {
		return fmt.Errorf("daemon: no notifier for trigger target %q", target)
	}
	l.notify.Post(master.Notice{
		ID:        "trigger-" + ev.ID,
		Class:     types.NoticeNormal,
		Content:   target + ": " + ev.Type + " (" + ev.ID + ")",
		CreatedAt: l.now().UTC(),
	})
	return nil
}

// StartSession implements agenda.TriggerLauncher: a session start
// becomes a coordinator child compiling a fresh context (17.6).
func (l *DaemonTriggerLauncher) StartSession(ctx context.Context, target string, ev agenda.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	card := orchestrator.DelegationCard{
		Name:        "session-" + target,
		Description: "project session",
		Goal:        "run a work session for " + target + " from stored state (event " + ev.ID + ")",
		Deliverable: "session outcome and next-session state recorded",
		Scope:       types.GeneralScope(),
		Done:        []string{"outcome recorded"},
		Report:      "what ran, what changed, what is pending",
		Deadline:    l.now().Add(4 * time.Hour),
	}
	_, _, err := l.orch.Spawn(l.parent, card)
	return err
}

// BindChat routes reminder output through the current favorite conversation.
func (l *OrchestratorLauncher) BindChat(b *DaemonBackend) { l.chat = b }
