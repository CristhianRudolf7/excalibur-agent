package types

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// EventType identifies a typed bus event (Section 22.1). Clients
// subscribe with a cursor and resume from it after disconnects.
type EventType string

// Event types.
const (
	EventMessageCreated      EventType = "message.created"
	EventRunStateChanged     EventType = "run.state_changed"
	EventRunProgress         EventType = "run.progress"
	EventPlanApprovalAsked   EventType = "plan.approval_requested"
	EventQuestionAsked       EventType = "question.asked"
	EventNoticePosted        EventType = "notice.posted"
	EventMetricRecorded      EventType = "metric.recorded"
	EventToolCalled          EventType = "tool.called"
	EventToolCompleted       EventType = "tool.completed"
	EventMemoryWritten       EventType = "memory.written"
	EventCompactionCompleted EventType = "compaction.completed"
	EventLoopDetected        EventType = "loop.detected"
	EventDirectiveSent       EventType = "directive.sent"
	EventFileEdited          EventType = "file.edited"
	EventErrorOccurred       EventType = "error.occurred"
)

// Event is the envelope carried by the live bus and persisted to
// telemetry. Payload holds the event-specific typed body as JSON.
type Event struct {
	// Seq is the per-bus sequence number; it backs the cursor.
	Seq uint64 `json:"seq"`
	// ID is the unique event identifier (unused for ordering).
	ID string `json:"id"`
	// Type is the typed event name.
	Type EventType `json:"type"`
	// At is the UTC emission time.
	At time.Time `json:"at"`
	// RunID and TaskID link the event to its run and task when any.
	RunID  *RunID  `json:"run_id,omitempty"`
	TaskID *TaskID `json:"task_id,omitempty"`
	// Payload is the event-specific body.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Cursor is an opaque subscription position: the last delivered
// sequence number. Clients send it back to resume without gaps or
// duplicates (Section 3.5).
type Cursor struct {
	Seq uint64
}

// String renders the cursor for transport.
func (c Cursor) String() string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(c.Seq, 10)))
}

// ParseCursor parses a cursor produced by Cursor.String.
func ParseCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("types: bad cursor %q: %w", s, err)
	}
	seq, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("types: bad cursor %q: %w", s, err)
	}
	return Cursor{Seq: seq}, nil
}
