package orchestrator

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// MessageType names one Section 15.5 mailbox row kind.
type MessageType string

// Mailbox message types.
const (
	MsgDirective  MessageType = "directive"
	MsgQuestion   MessageType = "question"
	MsgAnswer     MessageType = "answer"
	MsgReport     MessageType = "report"
	MsgCompletion MessageType = "completion"
	MsgBlocked    MessageType = "blocked"
)

// DirectivePriority is normal (apply at the next safe point) or
// interrupt (cancel the in-flight model call and stop the tool per
// its cancel class) from Section 15.5.
type DirectivePriority string

// Directive priorities.
const (
	PriorityNormal    DirectivePriority = "normal"
	PriorityInterrupt DirectivePriority = "interrupt"
)

// Message is one agent_messages row: at-least-once delivery with an
// ack; duplicates are detected by id (Section 15.5).
type Message struct {
	ID        types.MessageID
	From      types.AgentID
	To        types.AgentID
	Type      MessageType
	Priority  DirectivePriority
	Payload   string
	CreatedAt time.Time
	AckedAt   time.Time
	Acked     bool
	AckNote   string
}

// DirectiveAction tells the recipient what the correction requires:
// adjust, replan or stop (Section 15.7).
type DirectiveAction string

// Directive actions.
const (
	ActionAdjust      DirectiveAction = "adjust"
	ActionReplan      DirectiveAction = "replan"
	ActionStop        DirectiveAction = "stop"
	ActionAcknowledge DirectiveAction = "acknowledge"
)

// Directive is the structured form of a MsgDirective payload.
type Directive struct {
	ID             types.MessageID
	Action         DirectiveAction
	InstructionRev int
	Text           string
	Summary        string
}

// Usage accumulates one run's spend for budgets and status.
type Usage struct {
	Steps        int
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Elapsed      time.Duration
	ToolCalls    int
}

// RunRecord is the orchestrator-side row for one execution: the
// shared types.Run plus the scheduling lane and the run's scope for
// fair sharing between projects (Sections 5.2, 15.3).
type RunRecord struct {
	Run   types.Run
	Lane  Lane
	Scope types.Scope
}

// Lane is a Section 5.2 priority lane. P0 is reserved for master
// turns; runs live on P1 (user-initiated), P2 (scheduled) or P3
// (maintenance).
type Lane int

// Priority lanes.
const (
	LaneP0 Lane = iota
	LaneP1
	LaneP2
	LaneP3
)

// String renders the lane name.
func (l Lane) String() string {
	switch l {
	case LaneP0:
		return "P0"
	case LaneP1:
		return "P1"
	case LaneP2:
		return "P2"
	case LaneP3:
		return "P3"
	default:
		return "unknown"
	}
}

// Store is the orchestrator persistence contract: agents, runs,
// mailbox, directives, instructions, questions, reports and usage.
// The embedded mode uses MemoryStore; the daemon will back this with
// SQLite without changing callers.
type Store interface {
	SaveAgent(a types.Agent) error
	GetAgent(id types.AgentID) (types.Agent, error)
	ListAgents() []types.Agent
	Children(parent types.AgentID) []types.Agent
	FindAgentsByName(name string) []types.Agent

	CreateRun(r RunRecord) error
	GetRun(id types.RunID) (RunRecord, error)
	UpdateRun(r RunRecord) error
	ListRuns() []RunRecord
	RunsByState(s types.RunState) []RunRecord

	AppendMessage(m Message) error
	GetMessage(id types.MessageID) (Message, error)
	PendingMessages(to types.AgentID) []Message
	UnackedDirectives(to types.AgentID) []Message
	AckMessage(id types.MessageID, note string) error

	SaveInstruction(ins types.Instruction) error
	LatestInstruction(scope types.Scope) (types.Instruction, bool)
	InstructionText(scope types.Scope, rev int) (string, bool)

	SaveQuestion(q types.Question) error
	GetQuestion(id string) (types.Question, error)
	PendingQuestions(run types.RunID) []types.Question
	AnswerQuestion(id, answer string) error

	SaveReport(rep DeliveryReport) error
	LatestReport(run types.RunID) (DeliveryReport, bool)

	RecordUsage(run types.RunID, u Usage) error
	GetUsage(run types.RunID) Usage
}

// MemoryStore is the in-memory Store: a mutex plus maps. It is the
// store unit tests and the embedded eval profile use; it is not
// durable.
type MemoryStore struct {
	mu           sync.Mutex
	agents       map[types.AgentID]types.Agent
	runs         map[types.RunID]RunRecord
	messages     map[types.MessageID]Message
	order        []types.MessageID
	instructions map[string][]types.Instruction
	questions    map[string]types.Question
	reports      map[types.RunID][]DeliveryReport
	usage        map[types.RunID]Usage
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		agents:       map[types.AgentID]types.Agent{},
		runs:         map[types.RunID]RunRecord{},
		messages:     map[types.MessageID]Message{},
		instructions: map[string][]types.Instruction{},
		questions:    map[string]types.Question{},
		reports:      map[types.RunID][]DeliveryReport{},
		usage:        map[types.RunID]Usage{},
	}
}

// SaveAgent implements Store.
func (m *MemoryStore) SaveAgent(a types.Agent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[a.ID]; ok {
		return fmt.Errorf("orchestrator: agent %q already exists", a.ID)
	}
	m.agents[a.ID] = a
	return nil
}

// GetAgent implements Store.
func (m *MemoryStore) GetAgent(id types.AgentID) (types.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return types.Agent{}, fmt.Errorf("orchestrator: unknown agent %q", id)
	}
	return a, nil
}

// ListAgents implements Store in deterministic (id) order.
func (m *MemoryStore) ListAgents() []types.Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]types.Agent, 0, len(m.agents))
	for _, a := range m.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Children implements Store in deterministic (id) order.
func (m *MemoryStore) Children(parent types.AgentID) []types.Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.Agent
	for _, a := range m.agents {
		if a.ParentID != nil && *a.ParentID == parent {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FindAgentsByName implements Store in deterministic (id) order.
func (m *MemoryStore) FindAgentsByName(name string) []types.Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.Agent
	for _, a := range m.agents {
		if a.Name == name {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CreateRun implements Store.
func (m *MemoryStore) CreateRun(r RunRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[r.Run.ID]; ok {
		return fmt.Errorf("orchestrator: run %q already exists", r.Run.ID)
	}
	m.runs[r.Run.ID] = r
	return nil
}

// GetRun implements Store.
func (m *MemoryStore) GetRun(id types.RunID) (RunRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return RunRecord{}, fmt.Errorf("orchestrator: unknown run %q", id)
	}
	return r, nil
}

// UpdateRun implements Store.
func (m *MemoryStore) UpdateRun(r RunRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[r.Run.ID]; !ok {
		return fmt.Errorf("orchestrator: unknown run %q", r.Run.ID)
	}
	m.runs[r.Run.ID] = r
	return nil
}

// ListRuns implements Store in deterministic (id) order.
func (m *MemoryStore) ListRuns() []RunRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RunRecord, 0, len(m.runs))
	for _, r := range m.runs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Run.ID < out[j].Run.ID })
	return out
}

// RunsByState implements Store in deterministic (id) order.
func (m *MemoryStore) RunsByState(s types.RunState) []RunRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []RunRecord
	for _, r := range m.runs {
		if r.Run.State == s {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Run.ID < out[j].Run.ID })
	return out
}

// AppendMessage implements Store: a duplicate id is a redelivery and
// is ignored, which gives at-least-once delivery without doubles
// (Section 15.5).
func (m *MemoryStore) AppendMessage(msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.messages[msg.ID]; ok {
		return nil
	}
	m.messages[msg.ID] = msg
	m.order = append(m.order, msg.ID)
	return nil
}

// GetMessage implements Store.
func (m *MemoryStore) GetMessage(id types.MessageID) (Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.messages[id]
	if !ok {
		return Message{}, fmt.Errorf("orchestrator: unknown message %q", id)
	}
	return msg, nil
}

// PendingMessages implements Store in send order.
func (m *MemoryStore) PendingMessages(to types.AgentID) []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Message
	for _, id := range m.order {
		msg := m.messages[id]
		if msg.To == to && !msg.Acked {
			out = append(out, msg)
		}
	}
	return out
}

// UnackedDirectives implements Store in send order.
func (m *MemoryStore) UnackedDirectives(to types.AgentID) []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Message
	for _, id := range m.order {
		msg := m.messages[id]
		if msg.To == to && msg.Type == MsgDirective && !msg.Acked {
			out = append(out, msg)
		}
	}
	return out
}

// AckMessage implements Store.
func (m *MemoryStore) AckMessage(id types.MessageID, note string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.messages[id]
	if !ok {
		return fmt.Errorf("orchestrator: unknown message %q", id)
	}
	if msg.Acked {
		return nil
	}
	msg.Acked = true
	msg.AckNote = note
	msg.AckedAt = time.Now().UTC()
	m.messages[id] = msg
	return nil
}

// SaveInstruction implements Store: revisions only move forward per
// scope.
func (m *MemoryStore) SaveInstruction(ins types.Instruction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := ins.Scope.String()
	for _, prev := range m.instructions[key] {
		if prev.Revision >= ins.Revision {
			return fmt.Errorf("orchestrator: instruction revision %d not newer than %d for scope %q",
				ins.Revision, prev.Revision, key)
		}
	}
	m.instructions[key] = append(m.instructions[key], ins)
	return nil
}

// LatestInstruction implements Store.
func (m *MemoryStore) LatestInstruction(scope types.Scope) (types.Instruction, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.instructions[scope.String()]
	if len(rows) == 0 {
		return types.Instruction{}, false
	}
	return rows[len(rows)-1], true
}

// InstructionText implements Store.
func (m *MemoryStore) InstructionText(scope types.Scope, rev int) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ins := range m.instructions[scope.String()] {
		if ins.Revision == rev {
			return ins.Text, true
		}
	}
	return "", false
}

// SaveQuestion implements Store.
func (m *MemoryStore) SaveQuestion(q types.Question) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.questions[q.ID]; ok {
		return fmt.Errorf("orchestrator: question %q already exists", q.ID)
	}
	m.questions[q.ID] = q
	return nil
}

// GetQuestion implements Store.
func (m *MemoryStore) GetQuestion(id string) (types.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.questions[id]
	if !ok {
		return types.Question{}, fmt.Errorf("orchestrator: unknown question %q", id)
	}
	return q, nil
}

// PendingQuestions implements Store in id order.
func (m *MemoryStore) PendingQuestions(run types.RunID) []types.Question {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.Question
	for _, q := range m.questions {
		if q.RunID == run && q.State == "open" {
			out = append(out, q)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AnswerQuestion implements Store.
func (m *MemoryStore) AnswerQuestion(id, answer string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.questions[id]
	if !ok {
		return fmt.Errorf("orchestrator: unknown question %q", id)
	}
	q.State = "answered"
	q.Answer = answer
	q.UpdatedAt = time.Now().UTC()
	m.questions[id] = q
	return nil
}

// SaveReport implements Store.
func (m *MemoryStore) SaveReport(rep DeliveryReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports[rep.RunID] = append(m.reports[rep.RunID], rep)
	return nil
}

// LatestReport implements Store.
func (m *MemoryStore) LatestReport(run types.RunID) (DeliveryReport, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.reports[run]
	if len(rows) == 0 {
		return DeliveryReport{}, false
	}
	return rows[len(rows)-1], true
}

// RecordUsage implements Store: usage accumulates per run.
func (m *MemoryStore) RecordUsage(run types.RunID, u Usage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.usage[run]
	cur.Steps += u.Steps
	cur.InputTokens += u.InputTokens
	cur.OutputTokens += u.OutputTokens
	cur.CostUSD += u.CostUSD
	cur.Elapsed += u.Elapsed
	cur.ToolCalls += u.ToolCalls
	m.usage[run] = cur
	return nil
}

// GetUsage implements Store.
func (m *MemoryStore) GetUsage(run types.RunID) Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage[run]
}
