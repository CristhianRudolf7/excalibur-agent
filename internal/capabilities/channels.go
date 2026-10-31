package capabilities

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Channels and services (Section 14.9). A channel is only a door: the
// conversation and the master are the same behind Telegram, mail or
// meetings. Messages to third parties, sent mail and calendar writes
// are irreversible external actions, so they need explicit user
// confirmation (Section 6.4); meetings need recording consent and the
// agent always identifies itself as an assistant, never as the user.

// MessagingManifest publishes contact_message: sending to a third
// party over a channel, confirmed by the user.
func MessagingManifest() Manifest {
	return Manifest{
		ID: "messaging", Version: "1",
		Operations: []Operation{
			Op("contact_message", "Send a message to a third party over a channel. Irreversible external action: needs user confirmation", "communication", "keyed", "cooperative", false, "communication", []string{"message", "contact", "telegram", "whatsapp", "third-party"}, ObjSchema([]string{"channel", "to", "text", "confirm"}, map[string]string{"channel": "string", "to": "string", "text": "string", "confirm": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// WorkspaceManifest publishes the mail and calendar operations, served
// by connectors or browser routines.
func WorkspaceManifest() Manifest {
	return Manifest{
		ID: "workspace", Version: "1",
		Operations: []Operation{
			Op("email", "List, read, search, draft and send mail. Sending is irreversible and needs user confirmation", "communication", "keyed", "cooperative", false, "communication", []string{"email", "mail", "inbox", "send"}, ObjSchema([]string{"action"}, map[string]string{"action": "string", "id": "string", "to": "string", "subject": "string", "body": "string", "query": "string", "confirm": "string"})),
			Op("calendar", "Check availability and create, move or cancel events in the user timezone. Reads are fast", "write_external", "keyed", "cooperative", true, "communication", []string{"calendar", "events", "availability", "schedule"}, ObjSchema([]string{"action"}, map[string]string{"action": "string", "id": "string", "title": "string", "start": "string", "end": "string", "confirm": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// MeetingManifest publishes the meeting operation: join, speak (TTS),
// listen (STT) and leave, with consent and disclosure.
func MeetingManifest() Manifest {
	return Manifest{
		ID: "meetings", Version: "1",
		Operations: []Operation{
			Op("meeting", "Join a call, speak, read the live transcript and leave, with consent and assistant disclosure", "communication", "keyed", "cooperative", false, "communication", []string{"meeting", "call", "join", "transcript", "consent"}, ObjSchema([]string{"action", "meeting_id"}, map[string]string{"action": "string", "meeting_id": "string", "text": "string", "consent": "string", "identify": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// Mail is one workspace message.
type Mail struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// CalendarEvent is one calendar entry in the user timezone.
type CalendarEvent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// MessagingFake is the deterministic messaging simulation. Sends are
// keyed (a resend with the same key records a single effect) and
// require confirm=yes: without it the operation fails instead of
// contacting a third party.
type MessagingFake struct {
	mu    sync.Mutex
	sent  []map[string]any
	dedup KeyDedup
}

// NewMessagingFake returns a messaging simulation.
func NewMessagingFake() *MessagingFake { return &MessagingFake{} }

// Manifest implements Capability.
func (m *MessagingFake) Manifest() Manifest { return MessagingManifest() }

// Sent returns the recorded third-party sends.
func (m *MessagingFake) Sent() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any(nil), m.sent...)
}

// Invoke implements Capability.
func (m *MessagingFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if operation != "contact_message" {
		return Failed("", fmt.Sprintf("messaging: unknown operation %q", operation), ErrInput), nil
	}
	if !strings.EqualFold(argString(args, "confirm"), "yes") {
		return Failed("", "messaging: contact_message needs explicit user confirmation (confirm=yes)", ErrInput), nil
	}
	channel, to, text := argString(args, "channel"), argString(args, "to"), argString(args, "text")
	if channel == "" || to == "" || text == "" {
		return Failed("", "messaging: channel, to and text are required", ErrInput), nil
	}
	if m.dedup.First(operation + "|" + key) {
		m.sent = append(m.sent, map[string]any{"channel": channel, "to": to, "text": text, "at": time.Now().UTC().Format(time.RFC3339)})
	}
	return Completed("contact-1", map[string]any{"sent": true, "channel": channel, "to": to}), nil
}

// Poll implements Capability.
func (m *MessagingFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (m *MessagingFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (m *MessagingFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return m.Poll(taskID)
}

// WorkspaceFake is the deterministic mail and calendar simulation.
type WorkspaceFake struct {
	mu     sync.Mutex
	mail   []Mail
	events map[string]CalendarEvent
	dedup  KeyDedup
	seq    int
}

// NewWorkspaceFake returns a workspace simulation with one seed mail.
func NewWorkspaceFake() *WorkspaceFake {
	return &WorkspaceFake{
		mail:   []Mail{{ID: "m1", From: "boss@example.com", To: "me", Subject: "Q3 plan", Body: "Please review the Q3 plan."}},
		events: map[string]CalendarEvent{},
	}
}

// Manifest implements Capability.
func (w *WorkspaceFake) Manifest() Manifest { return WorkspaceManifest() }

// Invoke implements Capability.
func (w *WorkspaceFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch operation {
	case "email":
		return w.email(args, key)
	case "calendar":
		return w.calendar(args, key)
	default:
		return Failed("", fmt.Sprintf("workspace: unknown operation %q", operation), ErrInput), nil
	}
}

func (w *WorkspaceFake) email(args json.RawMessage, key string) (TaskResult, error) {
	action := argString(args, "action")
	if action == "" {
		action = "list"
	}
	switch action {
	case "list", "search":
		query := strings.ToLower(argString(args, "query"))
		out := []Mail{}
		for _, m := range w.mail {
			if query == "" || strings.Contains(strings.ToLower(m.Subject+m.Body+m.From), query) {
				out = append(out, m)
			}
		}
		return Completed("mailbox", map[string]any{"mails": out}), nil
	case "read":
		id := argString(args, "id")
		for _, m := range w.mail {
			if m.ID == id {
				return Completed(id, map[string]any{"mail": m}), nil
			}
		}
		return Failed("", fmt.Sprintf("workspace: unknown mail %q", id), ErrInput), nil
	case "draft":
		return Completed("draft-1", map[string]any{"draft": true}), nil
	case "send":
		if !strings.EqualFold(argString(args, "confirm"), "yes") {
			return Failed("", "workspace: sending mail is irreversible and needs user confirmation (confirm=yes)", ErrInput), nil
		}
		to, subject, body := argString(args, "to"), argString(args, "subject"), argString(args, "body")
		if to == "" || subject == "" {
			return Failed("", "workspace: to and subject are required", ErrInput), nil
		}
		if w.dedup.First("email|" + key) {
			w.seq++
			w.mail = append(w.mail, Mail{ID: fmt.Sprintf("m%d", w.seq+1), From: "me", To: to, Subject: subject, Body: body})
		}
		return Completed("send-1", map[string]any{"sent": true, "to": to}), nil
	default:
		return Failed("", fmt.Sprintf("workspace: unknown email action %q", action), ErrInput), nil
	}
}

func (w *WorkspaceFake) calendar(args json.RawMessage, key string) (TaskResult, error) {
	action := argString(args, "action")
	if action == "" {
		action = "availability"
	}
	switch action {
	case "availability", "list":
		out := []CalendarEvent{}
		for _, e := range w.events {
			out = append(out, e)
		}
		return Completed("cal", map[string]any{"events": out}), nil
	case "create", "move":
		if action == "create" && !strings.EqualFold(argString(args, "confirm"), "yes") {
			return Failed("", "workspace: creating an event writes externally and needs confirmation (confirm=yes)", ErrInput), nil
		}
		if !w.dedup.First("calendar|" + key) {
			return Completed("cal-cached", map[string]any{"cached": true}), nil
		}
		id := argString(args, "id")
		if id == "" || action == "create" {
			w.seq++
			id = fmt.Sprintf("e%d", w.seq)
		}
		w.events[id] = CalendarEvent{ID: id, Title: argString(args, "title"), Start: argString(args, "start"), End: argString(args, "end")}
		// The inverse is declared on the deferred tool: deleting the
		// created event undoes the effect (Section 15.8).
		return Completed(id, map[string]any{"event": w.events[id], "inverse": "calendar cancel " + id}), nil
	case "cancel":
		id := argString(args, "id")
		if id == "" {
			return Failed("", "workspace: id is required", ErrInput), nil
		}
		delete(w.events, id)
		return Completed(id, map[string]any{"cancelled": id}), nil
	default:
		return Failed("", fmt.Sprintf("workspace: unknown calendar action %q", action), ErrInput), nil
	}
}

// Poll implements Capability.
func (w *WorkspaceFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (w *WorkspaceFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (w *WorkspaceFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return w.Poll(taskID)
}

// MeetingState is one joined meeting.
type MeetingState struct {
	ID       string   `json:"id"`
	Joined   bool     `json:"joined"`
	Consent  bool     `json:"consent"`
	Identity string   `json:"identity"`
	Said     []string `json:"said"`
}

// MeetingFake is the deterministic meeting simulation. Joining
// requires identify=assistant: the agent discloses it is an assistant
// and never presents itself as the user (Section 14.9). A user avatar
// is only usable with explicit authorization and still identified as
// the agent. Listening without recording consent fails.
type MeetingFake struct {
	mu       sync.Mutex
	meetings map[string]*MeetingState
	dedup    KeyDedup
}

// NewMeetingFake returns a meeting simulation.
func NewMeetingFake() *MeetingFake {
	return &MeetingFake{meetings: map[string]*MeetingState{}}
}

// Manifest implements Capability.
func (m *MeetingFake) Manifest() Manifest { return MeetingManifest() }

// Invoke implements Capability.
func (m *MeetingFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if operation != "meeting" {
		return Failed("", fmt.Sprintf("meetings: unknown operation %q", operation), ErrInput), nil
	}
	action := argString(args, "action")
	id := argString(args, "meeting_id")
	if action == "" || id == "" {
		return Failed("", "meetings: action and meeting_id are required", ErrInput), nil
	}
	switch action {
	case "join":
		identify := strings.ToLower(argString(args, "identify"))
		if identify == "" {
			identify = "assistant"
		}
		if identify != "assistant" {
			return Failed("", "meetings: the agent must join as an assistant, never as the user", ErrInput), nil
		}
		consent := strings.EqualFold(argString(args, "consent"), "yes")
		m.meetings[id] = &MeetingState{ID: id, Joined: true, Consent: consent, Identity: "assistant"}
		return Completed(id, map[string]any{"meeting_id": id, "joined": true, "identity": "assistant", "consent": consent}), nil
	case "speak":
		st, ok := m.meetings[id]
		if !ok || !st.Joined {
			return Failed("", "meetings: join the meeting before speaking", ErrInput), nil
		}
		text := argString(args, "text")
		if text == "" {
			return Failed("", "meetings: text is required", ErrInput), nil
		}
		if m.dedup.First("meeting-speak|" + key) {
			st.Said = append(st.Said, text)
		}
		return Completed(id, map[string]any{"said": text, "as": "assistant"}), nil
	case "listen":
		st, ok := m.meetings[id]
		if !ok || !st.Joined {
			return Failed("", "meetings: join the meeting before listening", ErrInput), nil
		}
		if !st.Consent {
			return Failed(id, "meetings: recording consent is required before transcribing", ErrInput), nil
		}
		return Completed(id, map[string]any{
			"segments": []map[string]any{{"speaker": "host", "start": "00:01", "text": "welcome"}},
		}), nil
	case "leave":
		delete(m.meetings, id)
		return Completed(id, map[string]any{"meeting_id": id, "left": true}), nil
	default:
		return Failed("", fmt.Sprintf("meetings: unknown meeting action %q", action), ErrInput), nil
	}
}

// Poll implements Capability.
func (m *MeetingFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (m *MeetingFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (m *MeetingFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return m.Poll(taskID)
}
