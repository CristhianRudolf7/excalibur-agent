package master

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// ModelCaller is the seam to the interlocutor model (Section 5.3.7):
// nil or failing callers degrade to the deterministic receipt.
type ModelCaller interface {
	// Complete generates one turn step. prompt carries the compact
	// context; it must honor ctx cancellation (5.3.4 <2 s rule).
	Complete(ctx context.Context, prompt string) (text string, err error)
	// Available reports whether the provider path is usable now.
	Available() bool
}

// FakeModel is a scriptable ModelCaller for tests.
type FakeModel struct {
	mu         sync.Mutex
	Available_ bool
	Fn         func(ctx context.Context, prompt string) (string, error)
	Calls      int
	Prompts    []string
	Block      chan struct{} // when non-nil, Complete waits here
	Started    chan struct{}
	CancelSeen chan struct{}
}

// Complete implements ModelCaller.
func (f *FakeModel) Complete(ctx context.Context, prompt string) (string, error) {
	f.mu.Lock()
	f.Calls++
	f.Prompts = append(f.Prompts, prompt)
	block, started := f.Block, f.Started
	f.mu.Unlock()
	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if block != nil {
		select {
		case <-ctx.Done():
			select {
			case <-f.CancelSeen:
			default:
				close(f.CancelSeen)
			}
			return "", ctx.Err()
		case <-block:
		}
	}
	if f.Fn != nil {
		return f.Fn(ctx, prompt)
	}
	return "ok", nil
}

// Available implements ModelCaller.
func (f *FakeModel) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Available_
}

// TurnConfig carries the Appendix B turn limits.
type TurnConfig struct {
	// MaxSteps caps one master turn at 8.
	MaxSteps int
	// CancelWindow is the 2 s <2 s rule of Section 5.3.4.
	CancelWindow time.Duration
}

// DefaultTurnConfig returns the Appendix B values.
func DefaultTurnConfig() TurnConfig {
	return TurnConfig{MaxSteps: 8, CancelWindow: 2 * time.Second}
}

// Turn holds the live state of one conversation turn.
type Turn struct {
	ConversationID string
	StartedAt      time.Time
	Ctx            context.Context
	Cancel         context.CancelFunc
	Steps          int
	ShownOutput    bool // true once any partial output reached the user
	Pending        []IntakeResult
	Done           chan struct{}
	Err            error
}

// TurnManager runs Section 5.3 turns: conversations advance in
// parallel, turns within one conversation run sequentially with
// redirection (5.3.4-5.3.5). A message arriving before CancelWindow
// with no shown output cancels the generation and restarts with both
// messages (cached prefix, cheap); later messages inject at the next
// safe point. Control orders bypass the turn via the fast lane.
type TurnManager struct {
	mu     sync.Mutex
	cfg    TurnConfig
	model  ModelCaller
	turns  map[string]*Turn
	now    func() time.Time
	events func(TurnEvent)
}

// TurnEvent observes the turn lifecycle in tests and the bus bridge.
type TurnEvent struct {
	ConversationID string
	Kind           string // started|canceled_restart|injected|finished|failed|fallback
	Detail         string
}

// NewTurnManager returns a manager over model (may be nil).
func NewTurnManager(cfg TurnConfig, model ModelCaller, now func() time.Time, events func(TurnEvent)) *TurnManager {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 8
	}
	if cfg.CancelWindow <= 0 {
		cfg.CancelWindow = 2 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &TurnManager{cfg: cfg, model: model, turns: map[string]*Turn{}, now: now, events: events}
}

func (m *TurnManager) emit(e TurnEvent) {
	if m.events != nil {
		m.events(e)
	}
}

// Deliver hands one intake result to its conversation: it opens a
// turn, restarts the generation under the <2 s rule, or queues for
// the next safe point (Section 5.3.4).
func (m *TurnManager) Deliver(conv string, in IntakeResult) {
	m.mu.Lock()
	t, active := m.turns[conv]
	if !active {
		t = &Turn{ConversationID: conv, StartedAt: m.now(), Done: make(chan struct{})}
		t.Ctx, t.Cancel = context.WithCancel(context.Background())
		m.turns[conv] = t
		m.mu.Unlock()
		m.emit(TurnEvent{ConversationID: conv, Kind: "started"})
		safego.Go("master-turn", func(owner string, recovered any, stack []byte) {
			m.finish(conv, fmt.Errorf("panic: %v", recovered))
		}, func() { m.run(conv, t, in) })
		return
	}
	elapsed := m.now().Sub(t.StartedAt)
	if !t.ShownOutput && elapsed < m.cfg.CancelWindow {
		t.Cancel()
		t.Pending = append([]IntakeResult{in}, t.Pending...)
		m.emit(TurnEvent{ConversationID: conv, Kind: "canceled_restart", Detail: "new message within 2s, no output shown"})
		m.mu.Unlock()
		m.waitDone(t)
		m.mu.Lock()
		nt := &Turn{ConversationID: conv, StartedAt: m.now(), Done: make(chan struct{}), Pending: t.Pending}
		nt.Ctx, nt.Cancel = context.WithCancel(context.Background())
		m.turns[conv] = nt
		m.mu.Unlock()
		m.emit(TurnEvent{ConversationID: conv, Kind: "started"})
		safego.Go("master-turn", func(owner string, recovered any, stack []byte) {
			m.finish(conv, fmt.Errorf("panic: %v", recovered))
		}, func() { m.run(conv, nt, in) })
		return
	}
	t.Pending = append(t.Pending, in)
	m.emit(TurnEvent{ConversationID: conv, Kind: "injected", Detail: "queued for next safe point"})
	m.mu.Unlock()
}

// run executes one turn: up to MaxSteps model steps, draining pending
// injections between steps. Provider failure without a backup ends
// the turn with the deterministic receipt (Section 5.3.7): the queue
// stays and the fast lane keeps answering.
func (m *TurnManager) run(conv string, t *Turn, first IntakeResult) {
	inputs := append([]IntakeResult{first}, t.Pending...)
	m.mu.Lock()
	t.Pending = nil
	m.mu.Unlock()
	steps := 0
	for _, in := range inputs {
		if steps >= m.cfg.MaxSteps {
			break
		}
		if err := t.Ctx.Err(); err != nil {
			break
		}
		steps++
		m.mu.Lock()
		t.Steps = steps
		m.mu.Unlock()
		if m.model == nil || !m.model.Available() {
			m.emit(TurnEvent{ConversationID: conv, Kind: "fallback", Detail: "received; no model available, will attend when it returns"})
			continue
		}
		if _, err := m.model.Complete(t.Ctx, in.Msg.Text); err != nil {
			m.emit(TurnEvent{ConversationID: conv, Kind: "fallback", Detail: "received; no model available, will attend when it returns"})
			continue
		}
		m.mu.Lock()
		t.ShownOutput = true
		more := t.Pending
		t.Pending = nil
		m.mu.Unlock()
		inputs = append(inputs, more...)
	}
	m.finish(conv, nil)
}

func (m *TurnManager) waitDone(t *Turn) {
	select {
	case <-t.Done:
	default:
		// The canceled generation observes ctx cancellation
		// promptly; wait briefly, then proceed regardless.
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-t.Done:
		case <-timer.C:
		}
	}
}

func (m *TurnManager) finish(conv string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.turns[conv]; ok {
		select {
		case <-t.Done:
		default:
			close(t.Done)
		}
		t.Err = err
		delete(m.turns, conv)
	}
	if err != nil {
		m.emit(TurnEvent{ConversationID: conv, Kind: "failed", Detail: err.Error()})
		return
	}
	m.emit(TurnEvent{ConversationID: conv, Kind: "finished"})
}

// Active reports whether a conversation has a live turn.
func (m *TurnManager) Active(conv string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.turns[conv]
	return ok
}

// PendingCount reports queued injections for a conversation.
func (m *TurnManager) PendingCount(conv string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.turns[conv]; ok {
		return len(t.Pending)
	}
	return 0
}
