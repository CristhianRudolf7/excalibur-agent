package agenda

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ReminderService serializes client and tool edits over the same agenda.
// The agenda owns the timer and its lifetime; embedded callers persist only.
type ReminderService struct {
	mu     sync.Mutex
	Store  ReminderStore
	Agenda *Agenda
	DB     *store.DB
	Bus    *telemetry.Bus
}

func (s *ReminderService) timezone(ctx context.Context) string {
	zone := "UTC"
	if s.DB != nil {
		_ = s.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
			return q.QueryRow(ctx, `SELECT timezone FROM principals ORDER BY CASE WHEN id='local' THEN 0 ELSE 1 END,id LIMIT 1`).Scan(&zone)
		})
	}
	if zone == "" {
		zone = "UTC"
	}
	return zone
}
func ReminderView(r *Reminder) api.Reminder {
	status := "scheduled"
	switch r.State {
	case ReminderDone:
		status = "done"
	case ReminderSnoozed:
		status = "snoozed"
	case ReminderCanceled:
		status = "cancelled"
	}
	due := r.At
	if r.State == ReminderDelivered && !r.NextFireUTC.IsZero() {
		due = r.NextFireUTC
	}
	if r.State == ReminderSnoozed {
		due = r.SnoozedUntil
	}
	return api.Reminder{ID: r.ID, Text: r.Text, DueAt: due.UTC(), Timezone: r.Timezone, Repeat: r.Recur, Status: status, CreatedAt: r.CreatedAt.UTC(), LastFiredAt: r.LastFiredAt}
}
func (s *ReminderService) publish(typ types.EventType, r *Reminder) {
	if s.Bus != nil {
		_, _ = s.Bus.Publish(typ, nil, nil, api.ReminderEvent{Reminder: ReminderView(r)})
	}
}
func invalidReminder(msg string) error { return &api.Error{Code: api.CodeInvalidParams, Message: msg} }
func recurrence(raw string, at time.Time, zone string) (string, error) {
	switch raw {
	case "":
		return "", nil
	case "daily", "weekly", "monthly":
		loc, err := LoadZone(zone)
		if err != nil {
			return "", err
		}
		t := at.In(loc)
		spec := timeCron(t)
		if raw == "weekly" {
			spec = timeCron(t) + " * * " + string(rune('0'+t.Weekday()))
		} else if raw == "monthly" {
			spec += t.Format(" 2") + " * *"
		} else {
			spec += " * * *"
		}
		return spec, nil
	default:
		_, err := ParseCron(raw)
		return raw, err
	}
}
func timeCron(t time.Time) string { return t.Format("4 15") }

// Op implements the additive reminders CRUD contract.
func (s *ReminderService) Op(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	return s.op(ctx, method, raw, "")
}

func (s *ReminderService) op(ctx context.Context, method string, raw json.RawMessage, createID string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.Store == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "persistent reminders unavailable"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Agenda != nil {
		s.Agenda.reminderMu.Lock()
		defer s.Agenda.reminderMu.Unlock()
	}
	var p api.RemindersParams
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, invalidReminder("params must be an object")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, invalidReminder(err.Error())
	}
	if method == api.MethodRemindersList {
		items := []api.Reminder{}
		reminders := s.Store.ListReminders()
		if persistent, ok := s.Store.(*SQLReminderStore); ok {
			var err error
			reminders, err = persistent.ReadReminders(ctx)
			if err != nil {
				return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
			}
		}
		for _, r := range reminders {
			if !p.IncludeDone && (r.State == ReminderDone || r.State == ReminderCanceled) {
				continue
			}
			items = append(items, ReminderView(r))
		}
		return map[string]any{"reminders": items}, nil
	}
	var r *Reminder
	if method == api.MethodRemindersCreate {
		if p.Text == nil || strings.TrimSpace(*p.Text) == "" || p.DueAt == nil || p.DueAt.IsZero() {
			return nil, invalidReminder("text and due_at are required")
		}
		if p.Timezone == "" {
			p.Timezone = s.timezone(ctx)
		}
		r = &Reminder{ID: types.NewID("rem"), Text: *p.Text, At: p.DueAt.UTC(), Timezone: p.Timezone, State: ReminderPending, NextFireUTC: p.DueAt.UTC(), CreatedAt: time.Now().UTC().Truncate(time.Millisecond), Repeat: DefaultRepeatPolicy}
	} else {
		if p.ID == "" {
			return nil, invalidReminder("id is required")
		}
		var ok bool
		if persistent, isSQL := s.Store.(*SQLReminderStore); isSQL {
			var err error
			r, err = persistent.ReadReminder(ctx, p.ID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
			}
			ok = r != nil
		} else {
			r, ok = s.Store.Reminder(p.ID)
		}
		if !ok {
			return nil, &api.Error{Code: api.CodeUnknownTarget, Message: "unknown reminder"}
		}
	}
	if method == api.MethodRemindersDelete {
		if err := s.Store.DeleteReminder(p.ID); err != nil {
			return nil, err
		}
		r.Cancel()
		s.publish(api.EventReminderUpdated, r)
		return map[string]any{"id": p.ID, "deleted": true}, nil
	}
	if method != api.MethodRemindersCreate && method != api.MethodRemindersUpdate {
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown reminder method"}
	}
	if method == api.MethodRemindersCreate && createID != "" {
		r.ID = createID
	}

	if p.Text != nil {
		r.Text = strings.TrimSpace(*p.Text)
	}
	if p.DueAt != nil {
		r.At = p.DueAt.UTC()
		r.NextFireUTC = r.At
		r.SnoozedUntil = time.Time{}
		r.State = ReminderPending
		r.Acked = false
		r.Deliveries = 0
	}
	if p.Repeat != nil {
		recur, err := recurrence(*p.Repeat, r.At, r.Timezone)
		if err != nil {
			return nil, invalidReminder(err.Error())
		}
		r.Recur = recur
	}
	switch p.Status {
	case "":
	case "scheduled":
		r.State = ReminderPending
		r.Acked = false
		r.NextFireUTC = r.At
	case "done":
		r.Complete()
	case "cancelled":
		r.Cancel()
	case "snoozed":
		if p.SnoozeUntil == nil || !p.SnoozeUntil.After(time.Now()) {
			return nil, invalidReminder("snoozed needs a future snooze_until")
		}
		_ = r.Snooze(*p.SnoozeUntil)
	default:
		return nil, invalidReminder("invalid reminder status")
	}
	if err := r.Validate(); err != nil {
		return nil, invalidReminder(err.Error())
	}
	if method == api.MethodRemindersCreate && createID != "" {
		if persistent, ok := s.Store.(*SQLReminderStore); ok {
			saved, created, err := persistent.CreateReminder(ctx, r)
			if err != nil {
				return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
			}
			if !created {
				return ReminderView(saved), nil
			}
		} else {
			if saved, ok := s.Store.Reminder(createID); ok {
				return ReminderView(saved), nil
			}
			if err := s.Store.SaveReminder(r); err != nil {
				return nil, err
			}
		}
	} else if err := s.Store.SaveReminder(r); err != nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
	}

	if s.Agenda != nil {
		s.Agenda.wake()
	}

	s.publish(api.EventReminderUpdated, r)
	return ReminderView(r), nil
}

// WireTool fills the existing stub provider, retaining its pinned definition.
func (s *ReminderService) WireTool(reg *tools.MapRegistry) {
	t, err := reg.Get("reminders")
	if err != nil {
		return
	}
	stub, ok := t.(*tools.StubTool)
	if !ok {
		return
	}
	stub.Provider = func(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
		if err := ctx.Err(); err != nil {
			return tools.Result{Status: tools.StatusCanceled}, err
		}
		var p map[string]any
		if err := json.Unmarshal(inv.Arguments, &p); err != nil {
			return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
		}
		if p == nil {
			return tools.Result{Status: tools.StatusInputError, Output: "params must be an object"}, nil
		}
		action, _ := p["action"].(string)
		method := api.MethodRemindersList
		switch action {
		case "", "list":
			p["include_done"] = p["done"]
		case "create":
			method = api.MethodRemindersCreate
			p["due_at"] = p["when"]
		case "snooze":
			method = api.MethodRemindersUpdate
			p["status"] = "snoozed"
			p["snooze_until"] = p["when"]
		case "complete":
			method = api.MethodRemindersUpdate
			p["status"] = "done"
		case "delete":
			method = api.MethodRemindersDelete
		default:
			return tools.Result{Status: tools.StatusInputError, Output: "unknown reminders action"}, nil
		}
		raw, _ := json.Marshal(p)
		createID := ""
		if method == api.MethodRemindersCreate && inv.IdempotencyKey != "" {
			sum := sha256.Sum256([]byte(inv.IdempotencyKey))
			createID = "rem_" + hex.EncodeToString(sum[:16])
		}
		out, err := s.op(ctx, method, raw, createID)
		if err != nil {
			return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
		}
		raw, err = json.Marshal(out)
		return tools.Result{Status: tools.StatusOK, Output: string(raw)}, err
	}
}
