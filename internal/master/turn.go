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

// TurnCaller runs a conversation-aware turn and returns its trusted citation run tree.
type TurnCaller interface {
	CompleteTurn(context.Context, string, Normalized) (string, []string, error)
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
	restarting     bool
}

// TurnManager runs Section 5.3 turns: conversations advance in
// parallel, turns within one conversation run sequentially with
// redirection (5.3.4-5.3.5). A message arriving before CancelWindow
// with no shown output cancels the generation and restarts with both
// messages (cached prefix, cheap); later messages inject at the next
// safe point. Control orders bypass the turn via the fast lane.
type TurnManager struct {
	mu      sync.Mutex
	cfg     TurnConfig
	model   ModelCaller
	caller  TurnCaller
	turns   map[string]*Turn
	stopped bool
	now     func() time.Time
	events  func(TurnEvent)
	reply   func(context.Context, string, string, []string) error
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
	if m.stopped {
		m.mu.Unlock()
		return
	}
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
	if !t.ShownOutput && elapsed < m.cfg.CancelWindow && !t.restarting {
		t.restarting = true
		t.Cancel()
		m.emit(TurnEvent{ConversationID: conv, Kind: "canceled_restart", Detail: "new message within 2s, no output shown"})
	} else {
		m.emit(TurnEvent{ConversationID: conv, Kind: "injected", Detail: "queued for next safe point"})
	}
	t.Pending = append(t.Pending, in)
	m.mu.Unlock()
}

// run drains queued messages at safe boundaries. The production caller bounds
// each engine run; intake never waits for cancellation or completion.
func (m *TurnManager) run(conv string, t *Turn, first IntakeResult) {
	inputs := []IntakeResult{first}
	for len(inputs) > 0 {
		in := inputs[0]
		m.mu.Lock()
		caller := m.caller
		m.mu.Unlock()
		var text string
		var tree []string
		var err error
		if caller != nil {
			text, tree, err = caller.CompleteTurn(t.Ctx, conv, in.Msg)
		} else if m.model != nil && m.model.Available() {
			text, err = m.model.Complete(t.Ctx, in.Msg.Text)
		} else {
			text = "No model configured"
			m.emit(TurnEvent{ConversationID: conv, Kind: "fallback", Detail: text})
		}
		m.mu.Lock()
		if t.restarting && !m.stopped {
			// The interrupted input remains first, with every pending message retained.
			t.Cancel()
			t.Ctx, t.Cancel = context.WithCancel(context.Background())
			t.StartedAt = m.now()
			t.restarting = false
			inputs = append(inputs, t.Pending...)
			t.Pending = nil
			m.mu.Unlock()
			continue
		}
		if m.stopped {
			t.Cancel()
			close(t.Done)
			delete(m.turns, conv)
			m.mu.Unlock()
			return
		}
		t.ShownOutput = true
		m.mu.Unlock()
		if err != nil {
			text = "I couldn't answer because the model is unavailable. Please try again."
			m.emit(TurnEvent{ConversationID: conv, Kind: "fallback", Detail: text})
		}
		if m.reply != nil {
			if err := m.reply(t.Ctx, conv, text, tree); err != nil {
				m.emit(TurnEvent{ConversationID: conv, Kind: "failed", Detail: err.Error()})
			}
		}
		m.mu.Lock()
		t.Steps++
		t.ShownOutput = true
		inputs = append(inputs[1:], t.Pending...)
		t.Pending = nil
		if len(inputs) == 0 {
			t.Cancel()
			close(t.Done)
			delete(m.turns, conv)
			m.mu.Unlock()
			m.emit(TurnEvent{ConversationID: conv, Kind: "finished"})
			return
		}
		m.mu.Unlock()
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

// Stop cancels active turns and waits for their goroutines before owners close
// model and storage services. New messages must stop at the gateway first.
func (m *TurnManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.stopped = true
	var done []<-chan struct{}
	for _, t := range m.turns {
		t.Cancel()
		done = append(done, t.Done)
	}
	m.mu.Unlock()
	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
