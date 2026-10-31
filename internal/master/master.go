package master

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ConversationStore persists intake messages and the active scope
// per conversation (Sections 6.1, 9.3). The daemon backs it with
// SQLite; tests use MemStore.
type ConversationStore interface {
	SaveMessage(ctx context.Context, conv string, msg Normalized) (types.MessageID, error)
	History(conv string, limit int) []Normalized
	ActiveScope(conv string) string
	SetActiveScope(conv string, scope string)
	LookupClientMsg(clientID, clientMsgID string) (types.MessageID, bool)
	RecordClientMsg(clientID, clientMsgID string, msgID types.MessageID)
}

// MemStore is the in-process ConversationStore for tests.
type MemStore struct {
	mu       sync.Mutex
	msgs     map[string][]Normalized
	ids      map[string][]types.MessageID
	scopes   map[string]string
	byClient map[string]types.MessageID
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{
		msgs:     map[string][]Normalized{},
		ids:      map[string][]types.MessageID{},
		scopes:   map[string]string{},
		byClient: map[string]types.MessageID{},
	}
}

// SaveMessage implements ConversationStore.
func (m *MemStore) SaveMessage(_ context.Context, conv string, msg Normalized) (types.MessageID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := types.NewMessageID()
	m.msgs[conv] = append(m.msgs[conv], msg)
	m.ids[conv] = append(m.ids[conv], id)
	return id, nil
}

// History implements ConversationStore.
func (m *MemStore) History(conv string, limit int) []Normalized {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.msgs[conv]
	if limit > 0 && len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return append([]Normalized(nil), rows...)
}

// ActiveScope implements ConversationStore.
func (m *MemStore) ActiveScope(conv string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.scopes[conv]; ok {
		return s
	}
	return "general"
}

// SetActiveScope implements ConversationStore.
func (m *MemStore) SetActiveScope(conv, scope string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scopes[conv] = scope
}

// LookupClientMsg implements ConversationStore.
func (m *MemStore) LookupClientMsg(clientID, clientMsgID string) (types.MessageID, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byClient[clientID+"\x00"+clientMsgID]
	return id, ok
}

// RecordClientMsg implements ConversationStore.
func (m *MemStore) RecordClientMsg(clientID, clientMsgID string, msgID types.MessageID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byClient[clientID+"\x00"+clientMsgID] = msgID
}

// OrchestratorAPI is the H8 engine surface the master drives:
// delegate work, read snapshots, control runs (Sections 5, 15).
type OrchestratorAPI interface {
	StatusResolver
	SnapshotTree(runID string) (api.RunStatusResult, error)
	SpawnChild(parentAgent, name, goal string) (runID string, err error)
	NotifyRun(runID, note string)
}

// Config wires the master and its intake.
type Config struct {
	Turns    TurnConfig
	UserZone string
}

// Master is the Section 5 conversational agent: intake, fast lane,
// turns, request cards and notices. It never runs long work itself:
// file, terminal, navigation or research jobs spawn orchestrator runs
// (Sections 5.3.2, 15.3).
type Master struct {
	cfg      Config
	store    ConversationStore
	lex      *Lexicon
	decider  Decider
	lane     *FastLane
	turns    *TurnManager
	notifier *Notifier
	events   []TurnEvent
	mu       sync.Mutex
	loc      *time.Location
}

// New returns a master over its seams.
func New(cfg Config, store ConversationStore, lex *Lexicon, dec Decider, controls ControlRunner, model ModelCaller, notifier *Notifier) *Master {
	if store == nil {
		store = NewMemStore()
	}
	if lex == nil {
		lex = NewLexicon()
	}
	loc := time.UTC
	if cfg.UserZone != "" {
		if l, err := time.LoadLocation(cfg.UserZone); err == nil {
			loc = l
		}
	}
	m := &Master{cfg: cfg, store: store, lex: lex, decider: dec, loc: loc, notifier: notifier}
	m.lane = &FastLane{Controls: controls, HasDecider: dec != nil}
	m.turns = NewTurnManager(cfg.Turns, model, time.Now, func(e TurnEvent) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.events = append(m.events, e)
	})
	return m
}

// Lexicon exposes the mention index for registration flows.
func (m *Master) Lexicon() *Lexicon { return m.lex }

// Notifier exposes the Section 5.4 dispatcher.
func (m *Master) Notifier() *Notifier { return m.notifier }

// Turns exposes the turn manager.
func (m *Master) Turns() *TurnManager { return m.turns }

// Events replays turn lifecycle events for tests.
func (m *Master) Events() []TurnEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]TurnEvent(nil), m.events...)
}

// Receive is the Section 6.1 intake entry: normalize, persist with
// interactive priority, preanalyze, triage, fast-lane or turn. The
// receipt is confirmed in milliseconds without waiting for any turn,
// run or tool (Section 5.3.1). Duplicate client_msg_id returns the
// already assigned msg_id (Section 3.5).
func (m *Master) Receive(ctx context.Context, clientID, clientMsgID, conv string, text string) (types.MessageID, Outcome, error) {
	if strings.TrimSpace(text) == "" {
		return "", Outcome{}, fmt.Errorf("master: empty message")
	}
	if id, ok := m.store.LookupClientMsg(clientID, clientMsgID); ok {
		return id, Outcome{}, nil
	}
	msg := Normalized{Text: text, Channel: "api", Language: "en"}
	persist := func(n Normalized) (types.MessageID, error) {
		return m.store.SaveMessage(ctx, conv, n)
	}
	in, err := Intake(ctx, msg, persist, m.lex, m.decider, time.Now(), m.loc)
	if err != nil {
		return "", Outcome{}, err
	}
	m.store.RecordClientMsg(clientID, clientMsgID, in.MsgID)
	if out := m.lane.Handle(ctx, in); out.Handled && out.Rest == "" {
		return in.MsgID, out, nil
	} else if out.Handled {
		// Mixed: control ran; the rest joins the turn.
		rest := IntakeResult{Msg: Normalized{Text: out.Rest, Channel: msg.Channel}, Pre: Preanalyze(out.Rest, m.lex, time.Now(), m.loc), MsgID: in.MsgID, SentAt: time.Now()}
		m.turns.Deliver(conv, rest)
		return in.MsgID, out, nil
	}
	m.turns.Deliver(conv, in)
	return in.MsgID, Outcome{}, nil
}
