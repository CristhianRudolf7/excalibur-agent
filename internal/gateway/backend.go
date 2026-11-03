package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Backend is the seam every JSON-RPC method family calls. The daemon
// wires the real backend (master intake, orchestrator control,
// tasks, projects, memory, vault…); tests and embedded profiles use
// MemoryBackend. Returning an *api.Error maps to a JSON-RPC error
// response with the same code; any other error maps to CodeInternal.
type Backend interface {
	// ConversationSend persists one client message. It MUST be
	// idempotent on clientMsgID: a duplicate returns the already
	// assigned msgID with Duplicate true (Section 3.5).
	ConversationSend(ctx context.Context, clientID, clientMsgID, conversationID, text string) (msgID types.MessageID, duplicate bool, err error)
	// ConversationHistory pages stored messages.
	ConversationHistory(ctx context.Context, conversationID string, before string, limit int) ([]StoredMessage, error)
	// RunControl executes one run.* order and returns the snapshot.
	RunControl(ctx context.Context, method, runID string, interrupt bool, text string) (api.RunStatusResult, error)
	// RunList lists runs visible to the caller.
	RunList(ctx context.Context) ([]api.RunStatusResult, error)
	// TaskOp executes one task.* delegation call through the daemon
	// when active (Section 3.6). Result is already JSON-marshalable.
	TaskOp(ctx context.Context, method string, params json.RawMessage) (any, error)
	// FilesList serves the lazy paginated listing of Section 3.5.
	FilesList(ctx context.Context, params api.FilesListParams) (any, error)
	// Passthrough handles the remaining families (project, plan,
	// memory, skills, capability, vault, question, metrics, config).
	Passthrough(ctx context.Context, method string, params json.RawMessage) (any, error)
}

// ConversationContextBackend is an optional additive conversation transport.
// Existing text-only channel adapters and Backend implementations remain valid.
type ConversationContextBackend interface {
	ConversationSendContext(context.Context, string, string, string, string, []string, *api.AppContext) (types.MessageID, bool, error)
}

// StoredMessage is one persisted conversation message.
type StoredMessage struct {
	ID             types.MessageID      `json:"id"`
	ConversationID types.ConversationID `json:"conversation_id"`
	Role           string               `json:"role"`
	Text           string               `json:"text"`
	Changes        []api.FileChange     `json:"changes,omitempty"`
	Sources        []api.SourceRef      `json:"sources,omitempty"`
}

// MemoryBackend is the in-process Backend for tests and embedded
// profiles: idempotent send, history, a pluggable run/task hook and a
// bus-backed event feed. All methods are safe for concurrent use.
type MemoryBackend struct {
	mu       sync.Mutex
	msgs     map[string]StoredMessage // msgID -> message
	byClient map[string]string        // clientID + "\x00" + clientMsgID -> msgID
	order    []string
	runs     map[string]api.RunStatusResult
	bus      *telemetry.Bus
	tasks    map[string]json.RawMessage
	taskSeq  int
}

// NewMemoryBackend returns an empty backend. Bus may be nil.
func NewMemoryBackend(bus *telemetry.Bus) *MemoryBackend {
	return &MemoryBackend{
		msgs:     map[string]StoredMessage{},
		byClient: map[string]string{},
		runs:     map[string]api.RunStatusResult{},
		bus:      bus,
		tasks:    map[string]json.RawMessage{},
	}
}

// ConversationSend implements Backend with client_msg_id idempotency.
func (m *MemoryBackend) ConversationSend(ctx context.Context, clientID, clientMsgID, conversationID, text string) (types.MessageID, bool, error) {
	if clientMsgID == "" {
		return "", false, &api.Error{Code: api.CodeInvalidParams, Message: "missing client_msg_id"}
	}
	if conversationID == "" {
		return "", false, &api.Error{Code: api.CodeInvalidParams, Message: "missing conversation_id"}
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := clientID + "\x00" + clientMsgID
	if id, ok := m.byClient[key]; ok {
		return types.MessageID(id), true, nil
	}
	id := types.NewMessageID()
	m.byClient[key] = string(id)
	sm := StoredMessage{ID: id, ConversationID: types.ConversationID(conversationID), Role: "user", Text: text}
	m.msgs[string(id)] = sm
	m.order = append(m.order, string(id))
	bus := m.bus
	payload, _ := json.Marshal(api.MessageEvent{MessageID: id, ConversationID: types.ConversationID(conversationID), Role: "user", Text: text})
	var evPayload json.RawMessage = payload
	_ = evPayload
	m.mu.Unlock()
	if bus != nil {
		_, _ = bus.Publish(types.EventMessageCreated, nil, nil, api.MessageEvent{MessageID: id, ConversationID: types.ConversationID(conversationID), Role: "user", Text: text})
	}
	m.mu.Lock()
	return id, false, nil
}

// ConversationHistory implements Backend.
func (m *MemoryBackend) ConversationHistory(_ context.Context, conversationID string, before string, limit int) ([]StoredMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []StoredMessage
	for _, id := range m.order {
		sm := m.msgs[id]
		if string(sm.ConversationID) != conversationID {
			continue
		}
		if before != "" && string(sm.ID) >= before {
			continue
		}
		out = append(out, sm)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// SeedRun installs a run snapshot the run.* methods serve.
func (m *MemoryBackend) SeedRun(s api.RunStatusResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[s.RunID] = s
}

// RunControl implements Backend: pause/resume/interrupt/cancel/
// redirect/status mutate the seeded snapshot deterministically.
func (m *MemoryBackend) RunControl(_ context.Context, method, runID string, _ bool, _ string) (api.RunStatusResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if method == api.MethodRunList {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeInvalidParams, Message: "use run.list"}
	}
	s, ok := m.runs[runID]
	if !ok {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeUnknownTarget, Message: fmt.Sprintf("unknown run %q", runID)}
	}
	switch method {
	case api.MethodRunStatus:
		return s, nil
	case api.MethodRunPause:
		s.State = types.RunPaused
		s.Reason = "user"
	case api.MethodRunResume:
		s.State = types.RunQueued
		s.Reason = "resumed"
	case api.MethodRunInterrupt:
		s.State = types.RunPaused
		s.Reason = "interrupted"
	case api.MethodRunCancel:
		s.State = types.RunCanceled
		s.Reason = "canceled"
	case api.MethodRunRedirect:
		s.State = types.RunQueued
		s.Reason = "redirected"
	default:
		return api.RunStatusResult{}, &api.Error{Code: api.CodeMethodNotFound, Message: fmt.Sprintf("unknown method %q", method)}
	}
	m.runs[runID] = s
	return s, nil
}

// RunList implements Backend.
func (m *MemoryBackend) RunList(_ context.Context) ([]api.RunStatusResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]api.RunStatusResult, 0, len(m.runs))
	for _, s := range m.runs {
		out = append(out, s)
	}
	return out, nil
}

// TaskOp implements Backend with a tiny in-memory task registry that
// mirrors the Section 3.6 CLI surface.
func (m *MemoryBackend) TaskOp(_ context.Context, method string, params json.RawMessage) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch method {
	case api.MethodTaskList:
		ids := make([]string, 0, len(m.tasks))
		for id := range m.tasks {
			ids = append(ids, id)
		}
		return map[string]any{"tasks": ids}, nil
	case api.MethodTaskStart:
		var p api.TaskStartParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad task.start params"}
			}
		}
		if p.Request == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "missing request"}
		}
		m.taskSeq++
		id := fmt.Sprintf("tsk_test_%04d", m.taskSeq)
		rec, _ := json.Marshal(map[string]any{"task_id": id, "status": "in_progress", "request": p.Request})
		m.tasks[id] = rec
		return map[string]any{"task_id": id, "status": string(types.TaskInProgress)}, nil
	default:
		var probe map[string]any
		if len(params) > 0 {
			if err := json.Unmarshal(params, &probe); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad params"}
			}
		}
		id, _ := probe["task_id"].(string)
		if id == "" {
			if tid, ok := probe["taskId"]; ok {
				id, _ = tid.(string)
			}
		}
		raw, ok := m.tasks[id]
		if !ok {
			return nil, &api.Error{Code: api.CodeUnknownTarget, Message: fmt.Sprintf("unknown task %q", id)}
		}
		switch method {
		case api.MethodTaskStatus, api.MethodTaskResult, api.MethodTaskLogs, api.MethodTaskWait:
			return json.RawMessage(raw), nil
		case api.MethodTaskReply, api.MethodTaskCancel, api.MethodTaskPause, api.MethodTaskResume:
			return map[string]any{"task_id": id, "ok": true}, nil
		default:
			return nil, &api.Error{Code: api.CodeMethodNotFound, Message: fmt.Sprintf("unknown method %q", method)}
		}
	}
}

// FilesList implements Backend with an empty page.
func (m *MemoryBackend) FilesList(_ context.Context, _ api.FilesListParams) (any, error) {
	return map[string]any{"entries": []string{}, "next_cursor": ""}, nil
}

// Passthrough implements Backend: unknown families are rejected so
// tests observe CodeMethodNotFound rather than silent success.
func (m *MemoryBackend) Passthrough(_ context.Context, method string, _ json.RawMessage) (any, error) {
	return nil, &api.Error{Code: api.CodeMethodNotFound, Message: fmt.Sprintf("unknown method %q", method)}
}
