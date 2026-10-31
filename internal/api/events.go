package api

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Typed event payloads carried in types.Event.Payload. Each payload
// pairs with one types.EventType; the EventTypes registry below
// enumerates the pairing for schema export and conformance tests.

// MessageEvent notifies subscribers of a new persisted message.
type MessageEvent struct {
	MessageID      types.MessageID      `json:"message_id"`
	ConversationID types.ConversationID `json:"conversation_id"`
	Role           string               `json:"role"`
	Text           string               `json:"text"`
}

// RunProgressEvent carries run progress: state, step and the live tool
// line of Section 15.6.
type RunProgressEvent struct {
	RunID        types.RunID    `json:"run_id"`
	State        types.RunState `json:"state"`
	Step         int            `json:"step"`
	PlanDone     int            `json:"plan_done"`
	PlanTotal    int            `json:"plan_total"`
	ToolInFlight string         `json:"tool_in_flight,omitempty"`
	LiveOutput   string         `json:"live_output,omitempty"`
	LastProgress string         `json:"last_progress,omitempty"`
}

// PlanApprovalEvent asks the user to approve a macro plan.
type PlanApprovalEvent struct {
	PlanID  string       `json:"plan_id"`
	TaskID  types.TaskID `json:"task_id"`
	Summary string       `json:"summary"`
}

// QuestionEvent surfaces a pending user question or plan approval.
type QuestionEvent struct {
	QuestionID string      `json:"question_id"`
	RunID      types.RunID `json:"run_id"`
	Blocking   bool        `json:"blocking"`
	Content    string      `json:"content"`
	Default    string      `json:"default,omitempty"`
}

// NoticeEvent delivers a grouped or urgent notice (Section 5.4).
type NoticeEvent struct {
	NoticeID string            `json:"notice_id"`
	TaskID   types.TaskID      `json:"task_id"`
	Class    types.NoticeClass `json:"class"`
	Content  string            `json:"content"`
}

// MetricEvent reports one measured metric sample (Section 22.2).
type MetricEvent struct {
	RunID  types.RunID   `json:"run_id"`
	Name   string        `json:"name"`
	Value  float64       `json:"value"`
	Unit   string        `json:"unit"`
	TaskID *types.TaskID `json:"task_id,omitempty"`
}

// EventTypeInfo pairs an event type with its payload shape.
type EventTypeInfo struct {
	Type    types.EventType `json:"type"`
	Payload string          `json:"payload"`
	Summary string          `json:"summary"`
}

// EventTypes enumerates every typed event of the bus contract.
var EventTypes = []EventTypeInfo{
	{types.EventMessageCreated, "MessageEvent", "A message was persisted."},
	{types.EventRunStateChanged, "RunProgressEvent", "A run changed state."},
	{types.EventRunProgress, "RunProgressEvent", "Run progress update."},
	{types.EventPlanApprovalAsked, "PlanApprovalEvent", "A macro plan needs approval."},
	{types.EventQuestionAsked, "QuestionEvent", "A question needs an answer."},
	{types.EventNoticePosted, "NoticeEvent", "A notice was posted."},
	{types.EventMetricRecorded, "MetricEvent", "A metric sample was recorded."},
	{types.EventToolCalled, "ToolCallEvent", "A tool call started."},
	{types.EventToolCompleted, "ToolCallEvent", "A tool call finished."},
	{types.EventMemoryWritten, "MemoryEvent", "A memory was written."},
	{types.EventCompactionCompleted, "CompactionEvent", "Context compaction completed."},
	{types.EventLoopDetected, "LoopEvent", "The loop detector fired."},
	{types.EventDirectiveSent, "DirectiveEvent", "A directive was sent."},
	{types.EventFileEdited, "FileEditEvent", "A file was edited, with diff."},
	{types.EventErrorOccurred, "ErrorEvent", "An error occurred."},
}

// ToolCallEvent reports tool start and completion.
type ToolCallEvent struct {
	RunID  types.RunID `json:"run_id"`
	Tool   string      `json:"tool"`
	Status string      `json:"status"`
}

// MemoryEvent reports a memory write.
type MemoryEvent struct {
	MemoryID types.MemoryID `json:"memory_id"`
	Scope    types.Scope    `json:"scope"`
}

// CompactionEvent reports a completed compaction.
type CompactionEvent struct {
	RunID       types.RunID `json:"run_id"`
	TokensSaved int         `json:"tokens_saved"`
}

// LoopEvent reports a detector firing.
type LoopEvent struct {
	RunID     types.RunID `json:"run_id"`
	Signature string      `json:"signature"`
}

// DirectiveEvent reports a sent directive.
type DirectiveEvent struct {
	RunID    types.RunID `json:"run_id"`
	Priority string      `json:"priority"`
	Summary  string      `json:"summary"`
}

// FileEditEvent reports an edit with its diff for live UIs.
type FileEditEvent struct {
	RunID types.RunID `json:"run_id"`
	Path  string      `json:"path"`
	Diff  string      `json:"diff"`
}

// ErrorEvent reports an error.
type ErrorEvent struct {
	RunID   *types.RunID `json:"run_id,omitempty"`
	Code    int          `json:"code"`
	Message string       `json:"message"`
}
