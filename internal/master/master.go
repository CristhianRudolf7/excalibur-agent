package master

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ConversationStore persists intake messages and the active scope
// per conversation (Sections 6.1, 9.3). The daemon and tests currently
// use MemStore; the interface also permits durable implementations.
type ConversationStore interface {
	SaveMessage(ctx context.Context, conv string, msg Normalized) (types.MessageID, error)
	History(conv string, limit int) []Normalized
	ActiveScope(conv string) string
	SetActiveScope(conv string, scope string)
	LookupClientMsg(clientID, clientMsgID string) (types.MessageID, bool)
	RecordClientMsg(clientID, clientMsgID string, msgID types.MessageID)
	// MessageIDs returns the assigned ids of one conversation oldest
	// first for the gate history family.
	MessageIDs(conv string, limit int) []types.MessageID
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
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	msg.Context = api.CloneAppContext(msg.Context)
	msg.Changes = api.CloneChanges(msg.Changes)
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
	out := append([]Normalized(nil), rows...)
	for i := range out {
		out[i].Context = api.CloneAppContext(out[i].Context)
		out[i].Changes = api.CloneChanges(out[i].Changes)
	}
	return out
}

// MessageIDs implements ConversationStore.
func (m *MemStore) MessageIDs(conv string, limit int) []types.MessageID {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.ids[conv]
	if limit > 0 && len(ids) > limit {
		ids = ids[len(ids)-limit:]
	}
	return append([]types.MessageID(nil), ids...)
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
	cfg          Config
	store        ConversationStore
	lex          *Lexicon
	decider      Decider
	lane         *FastLane
	turns        *TurnManager
	notifier     *Notifier
	events       []TurnEvent
	mu           sync.Mutex
	zoneResolver func(context.Context) (string, error)
	loc          *time.Location
	output       AssistantOutput
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
	if dec != nil && controls != nil && intakeSwitch("EXCALIBUR_INTAKE_REQUIRE_ACTIVE_WORK") {
		m.decider = activeWorkDecider{Decider: dec, controls: controls}
	}
	m.lane = &FastLane{Controls: controls, HasDecider: dec != nil}
	m.turns = NewTurnManager(cfg.Turns, model, time.Now, func(e TurnEvent) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.events = append(m.events, e)
	})
	m.turns.reply = func(ctx context.Context, conv, text string, tree []string) error {
		_, err := m.SendAssistant(ctx, conv, text, tree)
		return err
	}
	return m
}

// History returns one conversation's persisted messages oldest first
// for the gate conversation.history family, including validated assistant output.
type HistoryMessage struct {
	RelayedBy      string
	CreatedAt      time.Time
	ID             types.MessageID
	ConversationID string
	Role           string
	Text           string
	Changes        []api.FileChange
	Sources        []api.SourceRef
}

// History implements the gate history read over the conversation
// store. A nil limit returns everything.
func (m *Master) History(conv string, limit int) []HistoryMessage {
	var rows []Normalized
	var ids []types.MessageID
	if paired, ok := m.store.(interface {
		HistoryWithIDs(string, int) ([]Normalized, []types.MessageID)
	}); ok {
		rows, ids = paired.HistoryWithIDs(conv, limit)
	} else {
		rows = m.store.History(conv, limit)
		ids = m.store.MessageIDs(conv, limit)
	}
	out := make([]HistoryMessage, 0, len(rows))
	for i, n := range rows {
		var id types.MessageID
		if i < len(ids) {
			id = ids[i]
		}
		role := n.Role
		if role == "" {
			role = "user"
		}
		out = append(out, HistoryMessage{RelayedBy: n.RelayedBy, CreatedAt: n.CreatedAt, ID: id, ConversationID: conv, Role: role, Text: n.Text, Sources: cloneSources(n.Sources), Changes: api.CloneChanges(n.Changes)})
	}
	return out
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
	return m.ReceiveWithAttachments(ctx, clientID, clientMsgID, conv, text, nil)
}

// ReceiveWithAttachments preserves document ids supplied by a conversation
// transport. Receive remains compatible with text-only channel adapters.
func (m *Master) ReceiveWithAttachments(ctx context.Context, clientID, clientMsgID, conv, text string, attachments []string) (types.MessageID, Outcome, error) {
	return m.ReceiveWithContext(ctx, clientID, clientMsgID, conv, text, attachments, api.AppContextFrom(ctx))
}

// ReceiveWithContext carries per-message app state through normal channel intake.
// Previous messages never implicitly lend their context to the current turn.
func (m *Master) ReceiveWithContext(ctx context.Context, clientID, clientMsgID, conv, text string, attachments []string, app *api.AppContext) (types.MessageID, Outcome, error) {
	if err := app.Validate(); err != nil {
		return "", Outcome{}, err
	}
	if strings.TrimSpace(text) == "" {
		return "", Outcome{}, fmt.Errorf("master: empty message")
	}
	if id, ok := m.store.LookupClientMsg(clientID, clientMsgID); ok {
		return id, Outcome{}, nil
	}
	loc := m.userLocation(ctx)
	msg := Normalized{UserTimezone: loc.String(), Context: api.CloneAppContext(app), Text: text, Attachments: append([]string(nil), attachments...), Channel: "api", Language: "en"}
	persist := func(n Normalized) (types.MessageID, error) {
		return m.store.SaveMessage(ctx, conv, n)
	}
	in, err := Intake(ctx, msg, persist, m.lex, m.decider, time.Now(), loc)
	if err != nil {
		return "", Outcome{}, err
	}
	m.store.RecordClientMsg(clientID, clientMsgID, in.MsgID)
	if out := m.lane.Handle(ctx, in); out.Handled && out.Rest == "" {
		return in.MsgID, out, nil
	} else if out.Handled {
		// Mixed: control ran; the rest joins the turn.
		rest := IntakeResult{Msg: Normalized{UserTimezone: loc.String(), Text: out.Rest, Channel: msg.Channel, Context: api.CloneAppContext(msg.Context), Attachments: append([]string(nil), msg.Attachments...)}, Pre: Preanalyze(out.Rest, m.lex, time.Now(), loc), MsgID: in.MsgID, SentAt: time.Now()}
		m.turns.Deliver(conv, rest)
		return in.MsgID, out, nil
	}
	m.turns.Deliver(conv, in)
	return in.MsgID, Outcome{}, nil
}

// ReceiveDeferred is the transport entry point: it persists the message and
// returns its id at once (the Section 5.3.1 millisecond receipt), then runs
// triage, the fast lane and turn delivery in the background. A fast-lane
// reply is handed to onReply. Duplicate client_msg_id returns the known id.
func (m *Master) ReceiveDeferred(ctx context.Context, clientID, clientMsgID, conv, text string, attachments []string, onReply func(string)) (types.MessageID, error) {
	return m.ReceiveDeferredWithContext(ctx, clientID, clientMsgID, conv, text, attachments, api.AppContextFrom(ctx), onReply)
}

// ReceiveDeferredWithContext preserves per-message app state through deferred
// intake, including the content left after a fast-lane control operation.
func (m *Master) ReceiveDeferredWithContext(ctx context.Context, clientID, clientMsgID, conv, text string, attachments []string, app *api.AppContext, onReply func(string)) (types.MessageID, error) {
	if err := app.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("master: empty message")
	}
	loc := m.userLocation(ctx)
	msg := Normalized{UserTimezone: loc.String(), Context: api.CloneAppContext(app), Text: text, Attachments: append([]string(nil), attachments...), Channel: "api", Language: "en", CreatedAt: time.Now().UTC()}
	id, duplicate, err := m.saveClientMessage(ctx, conv, msg, clientID, clientMsgID)
	if err != nil || duplicate {
		return id, err
	}
	now := time.Now()
	safego.Go("master-intake", nil, func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		in, err := Intake(bg, msg, func(Normalized) (types.MessageID, error) { return id, nil }, m.lex, m.decider, now, loc)
		if err != nil {
			return
		}
		out := m.lane.Handle(bg, in)
		if out.Handled && out.Rest == "" {
			if out.Reply != "" && onReply != nil {
				onReply(out.Reply)
			}
			return
		}
		if out.Handled {
			if out.Reply != "" && onReply != nil {
				onReply(out.Reply)
			}
			rest := IntakeResult{Msg: Normalized{UserTimezone: loc.String(), Text: out.Rest, Channel: msg.Channel, Context: api.CloneAppContext(msg.Context), Attachments: append([]string(nil), msg.Attachments...)}, Pre: Preanalyze(out.Rest, m.lex, time.Now(), loc), MsgID: in.MsgID, SentAt: time.Now()}
			m.turns.Deliver(conv, rest)
			return
		}
		m.turns.Deliver(conv, in)
	})
	return id, nil
}

// SetUserZoneResolver installs the daemon profile source. Resolve once per
// receipt so persistence and deferred temporal analysis share the same zone.
func (m *Master) SetUserZoneResolver(resolve func(context.Context) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.zoneResolver = resolve
}

func (m *Master) userLocation(ctx context.Context) *time.Location {
	m.mu.Lock()
	resolve, fallback := m.zoneResolver, m.loc
	m.mu.Unlock()
	if resolve != nil {
		if zone, err := resolve(ctx); err == nil {
			if loc, err := time.LoadLocation(zone); err == nil && zone != "" && zone != "Local" {
				return loc
			}
		}
	}
	return fallback
}

// SetTurnCaller installs production engine turns before accepting messages.
func (m *Master) SetTurnCaller(caller TurnCaller) {
	m.turns.mu.Lock()
	defer m.turns.mu.Unlock()
	m.turns.caller = caller
}

// TurnHistory exposes bounded normalized context, including attachment ids.
func (m *Master) TurnHistory(conv string, limit int) []Normalized {
	return m.store.History(conv, limit)
}

// activeWorkDecider skips classifying status-style questions while no run is
// active: with nothing running, the fast lane has no status to serve, so the
// master answers them directly ("what is MuJoCo?" contains a status phrase).
// Messages naming a task or run, or carrying another control intent, are
// still classified. EXCALIBUR_INTAKE_REQUIRE_ACTIVE_WORK=0 disables it.
type activeWorkDecider struct {
	Decider
	controls StatusResolver
}

// SkipClassify implements classifySkipper.
func (d activeWorkDecider) SkipClassify(pre Preanalysis) bool {
	if pre.ControlIntent != "" && pre.ControlIntent != "status" {
		return false
	}
	if len(pre.TaskRefs) > 0 || len(pre.RunRefs) > 0 {
		return false
	}
	for _, run := range d.controls.ListRuns() {
		if !run.State.IsTerminal() {
			return false
		}
	}
	return true
}
