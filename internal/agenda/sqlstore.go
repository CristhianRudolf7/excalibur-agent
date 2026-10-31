// SQL-backed agenda persistence (Sections 17.4, 17.6, 21.1-21.4).
//
// Schedules and reminders persist as JSON envelopes with their
// queryable columns synced (kind, next fire, timezone, missed
// policy, status), so crash recovery and boot reconciliation read
// rows, never logs. Fires, trigger events, workflow versions and
// sessions live in the v5 wiring tables. The in-memory Agenda,
// ReminderStore and TriggerEngine stay the working set; each gains an
// optional SQL mirror with Restore after a restart.
package agenda

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func bg() context.Context { return context.Background() }

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

func fromMs(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func isNotFound(err error) bool { return err == sql.ErrNoRows }

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"encode_error":%q}`, err.Error())
	}
	return string(raw)
}

func fromJSON(raw string, v any) error {
	if raw == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), v)
}

// --- Schedules, fires ---

// SQLAgendaStore implements AgendaStore over schedules (envelopes)
// and agenda_fires (delivered set plus pending queue).
type SQLAgendaStore struct {
	W store.Writer
	R sqlReader
}

// sqlReader is the read half: satisfied by *store.DB and test fakes.
type sqlReader interface {
	Read(ctx context.Context, fn store.ReadFunc) error
}

// OpenAgendaStore returns an agenda store over db.
func OpenAgendaStore(db *store.DB) *SQLAgendaStore { return &SQLAgendaStore{W: db, R: db} }

func (s *SQLAgendaStore) write(fn func(store.Execer) error) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		return fn(ex)
	})
}

func (s *SQLAgendaStore) read(fn func(store.Queryer) error) error {
	return s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error { return fn(q) })
}

func scanSchedule(scan func(...any) error) (*Schedule, error) {
	var sched Schedule
	var id, scopeID, kind, specRaw string
	var nextFire int64
	var tz, missed string
	var created, updated int64
	if err := scan(&id, &scopeID, &kind, &specRaw, &nextFire, &tz, &missed, &created, &updated); err != nil {
		return nil, err
	}
	if err := fromJSON(specRaw, &sched); err != nil {
		return nil, fmt.Errorf("agenda: decode schedule %q: %w", id, err)
	}
	sched.ID = id
	sched.ScopeID = scopeID
	sched.Kind = ScheduleKind(kind)
	sched.Timezone = tz
	sched.Missed = MissedPolicy(missed)
	sched.NextFireUTC = fromMs(nextFire)
	return &sched, nil
}

const schedCols = `id,scope_id,kind,spec_json,next_fire_ms,tz,missed_policy,created_at,updated_at`

// SaveSchedule implements AgendaStore.
func (s *SQLAgendaStore) SaveSchedule(sched *Schedule) error {
	if err := sched.Validate(); err != nil {
		return err
	}
	return s.write(func(ex store.Execer) error {
		ctx := bg()
		_, err := ex.Exec(ctx, `INSERT INTO schedules(`+schedCols+`) VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET scope_id=excluded.scope_id,kind=excluded.kind,spec_json=excluded.spec_json,
			next_fire_ms=excluded.next_fire_ms,tz=excluded.tz,missed_policy=excluded.missed_policy,updated_at=excluded.updated_at`,
			sched.ID, sched.ScopeID, string(sched.Kind), mustJSON(sched), ms(sched.NextFireUTC),
			sched.Timezone, string(sched.Missed), ms(time.Now().UTC()), ms(time.Now().UTC()))
		return err
	})
}

// Schedule implements AgendaStore.
func (s *SQLAgendaStore) Schedule(id string) (*Schedule, bool) {
	var out *Schedule
	_ = s.read(func(q store.Queryer) error {
		sched, err := scanSchedule(func(dest ...any) error {
			return q.QueryRow(bg(), `SELECT `+schedCols+` FROM schedules WHERE id=?`, id).Scan(dest...)
		})
		if err != nil {
			return err
		}
		out = sched
		return nil
	})
	return out, out != nil
}

// DeleteSchedule implements AgendaStore.
func (s *SQLAgendaStore) DeleteSchedule(id string) error {
	return s.write(func(ex store.Execer) error {
		ctx := bg()
		res, err := ex.Exec(ctx, `DELETE FROM schedules WHERE id=?`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("agenda: unknown schedule %q", id)
		}
		_, err = ex.Exec(ctx, `DELETE FROM agenda_fires WHERE schedule_id=?`, id)
		return err
	})
}

// ListSchedules implements AgendaStore ordered by next fire.
func (s *SQLAgendaStore) ListSchedules() []*Schedule {
	var out []*Schedule
	_ = s.read(func(q store.Queryer) error {
		rows, err := q.Query(bg(), `SELECT `+schedCols+` FROM schedules ORDER BY next_fire_ms,id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			sched, err := scanSchedule(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, sched)
		}
		return rows.Err()
	})
	return out
}

func scanFire(scan func(...any) error) (Fire, error) {
	var f Fire
	var fireID, scheduleID, payload, scope string
	var scheduled, fired int64
	var late, delivered, pending int
	if err := scan(&fireID, &scheduleID, &scheduled, &fired, &late, &payload, &scope, &delivered, &pending); err != nil {
		return Fire{}, err
	}
	f.FireID = fireID
	f.ScheduleID = scheduleID
	f.Scheduled = fromMs(scheduled)
	f.FiredAt = fromMs(fired)
	f.Late = late != 0
	f.Payload = payload
	_ = scope
	_ = delivered
	_ = pending
	return f, nil
}

// MarkDelivered implements AgendaStore: a fire_id never fires twice,
// even when the delivery itself crashed (Section 17.4).
func (s *SQLAgendaStore) MarkDelivered(f Fire) error {
	return s.write(func(ex store.Execer) error {
		ctx := bg()
		_, err := ex.Exec(ctx, `INSERT INTO agenda_fires(fire_id,schedule_id,scheduled_ms,fired_ms,late,payload,scope_id,delivered,pending)
			VALUES(?,?,?,?,?,?,?,1,0) ON CONFLICT(fire_id) DO UPDATE SET delivered=1,pending=0`,
			f.FireID, f.ScheduleID, ms(f.Scheduled), ms(f.FiredAt), boolToInt(f.Late), f.Payload, "")
		return err
	})
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Delivered implements AgendaStore.
func (s *SQLAgendaStore) Delivered(fireID string) bool {
	var delivered bool
	_ = s.read(func(q store.Queryer) error {
		var n int
		if err := q.QueryRow(bg(), `SELECT delivered FROM agenda_fires WHERE fire_id=?`, fireID).Scan(&n); err != nil {
			return err
		}
		delivered = n != 0
		return nil
	})
	return delivered
}

// QueuePending implements AgendaStore: parks a fire while safe mode
// holds launches (Section 17.3).
func (s *SQLAgendaStore) QueuePending(f Fire) error {
	return s.write(func(ex store.Execer) error {
		ctx := bg()
		_, err := ex.Exec(ctx, `INSERT INTO agenda_fires(fire_id,schedule_id,scheduled_ms,fired_ms,late,payload,scope_id,delivered,pending)
			VALUES(?,?,?,?,?,?,?,0,1) ON CONFLICT(fire_id) DO UPDATE SET pending=1`,
			f.FireID, f.ScheduleID, ms(f.Scheduled), ms(f.FiredAt), boolToInt(f.Late), f.Payload, "")
		return err
	})
}

// PendingFires implements AgendaStore ordered by scheduled time.
func (s *SQLAgendaStore) PendingFires() []Fire {
	var out []Fire
	_ = s.read(func(q store.Queryer) error {
		rows, err := q.Query(bg(), `SELECT fire_id,schedule_id,scheduled_ms,fired_ms,late,payload,scope_id,delivered,pending
			FROM agenda_fires WHERE pending=1 ORDER BY scheduled_ms`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanFire(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	return out
}

// ClearPending implements AgendaStore.
func (s *SQLAgendaStore) ClearPending(fireID string) error {
	return s.write(func(ex store.Execer) error {
		_, err := ex.Exec(bg(), `UPDATE agenda_fires SET pending=0 WHERE fire_id=?`, fireID)
		return err
	})
}

// --- Reminders ---

// SQLReminderStore implements ReminderStore: full Reminder envelopes
// with the next fire and status synced for due queries.
type SQLReminderStore struct {
	W store.Writer
	R sqlReader
}

// OpenReminderStore returns a reminder store over db.
func OpenReminderStore(db *store.DB) *SQLReminderStore { return &SQLReminderStore{W: db, R: db} }

func scanReminder(scan func(...any) error) (*Reminder, error) {
	var r Reminder
	var id, scopeID, text, recurRaw, channel, taskID, status string
	var fire, created, updated int64
	if err := scan(&id, &scopeID, &text, &fire, &recurRaw, &channel, &taskID, &status, &created, &updated); err != nil {
		return nil, err
	}
	if err := fromJSON(recurRaw, &r); err != nil {
		return nil, fmt.Errorf("agenda: decode reminder %q: %w", id, err)
	}
	r.ID = id
	r.Text = text
	r.Channel = channel
	r.TaskID = taskID
	r.State = ReminderState(status)
	r.NextFireUTC = fromMs(fire)
	return &r, nil
}

const remCols = `id,scope_id,text,fire_ms,recurrence_json,channel,task_id,status,created_at,updated_at`

// SaveReminder implements ReminderStore.
func (s *SQLReminderStore) SaveReminder(r *Reminder) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		now := ms(time.Now().UTC())
		_, err := ex.Exec(ctx, `INSERT INTO reminders(`+remCols+`) VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET text=excluded.text,fire_ms=excluded.fire_ms,recurrence_json=excluded.recurrence_json,
			channel=excluded.channel,task_id=excluded.task_id,status=excluded.status,updated_at=excluded.updated_at`,
			r.ID, "", r.Text, ms(r.NextFireUTC), mustJSON(r), r.Channel, r.TaskID, string(r.State), now, now)
		return err
	})
}

// Reminder implements ReminderStore.
func (s *SQLReminderStore) Reminder(id string) (*Reminder, bool) {
	var out *Reminder
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		r, err := scanReminder(func(dest ...any) error {
			return q.QueryRow(ctx, `SELECT `+remCols+` FROM reminders WHERE id=?`, id).Scan(dest...)
		})
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, out != nil
}

// DeleteReminder implements ReminderStore.
func (s *SQLReminderStore) DeleteReminder(id string) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `DELETE FROM reminders WHERE id=?`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("agenda: unknown reminder %q", id)
		}
		return nil
	})
}

// DueReminders implements ReminderStore ordered by next fire.
func (s *SQLReminderStore) DueReminders(now time.Time) []*Reminder {
	var out []*Reminder
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+remCols+` FROM reminders
			WHERE status IN ('pending','snoozed','delivered') AND fire_ms<>0 AND fire_ms<=? ORDER BY fire_ms`, ms(now))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanReminder(rows.Scan)
			if err != nil {
				return err
			}
			if r.State == ReminderSnoozed && !r.SnoozedUntil.IsZero() && r.SnoozedUntil.After(now) {
				continue
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out
}

// ListReminders implements ReminderStore ordered by next fire.
func (s *SQLReminderStore) ListReminders() []*Reminder {
	var out []*Reminder
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+remCols+` FROM reminders ORDER BY fire_ms,id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanReminder(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out
}

// --- Triggers ---

// rateEnvelope carries the rate-limit half of a rule row.
type rateEnvelope struct {
	MaxPerHour  int   `json:"max_per_hour"`
	DedupWindow int64 `json:"dedup_window_ns"`
}

// SQLTriggerStore persists TriggerEngine rules and firing history:
// (rule, event) dedup marks plus firing times for rate limiting
// (Section 21.3).
type SQLTriggerStore struct {
	W store.Writer
	R sqlReader
}

// OpenTriggerStore returns a trigger store over db.
func OpenTriggerStore(db *store.DB) *SQLTriggerStore { return &SQLTriggerStore{W: db, R: db} }

func scanRule(scan func(...any) error) (*TriggerRule, error) {
	var r TriggerRule
	var id, scopeID, name, filterRaw, actionRaw, rateRaw string
	var enabled int
	var created, updated int64
	if err := scan(&id, &scopeID, &name, &filterRaw, &actionRaw, &rateRaw, &enabled, &created, &updated); err != nil {
		return nil, err
	}
	r.ID = id
	_ = scopeID
	_ = name
	_ = created
	_ = updated
	if err := fromJSON(filterRaw, &r.Filter); err != nil {
		return nil, err
	}
	var action struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}
	if err := fromJSON(actionRaw, &action); err != nil {
		return nil, err
	}
	r.Action = TriggerAction(action.Action)
	r.Target = action.Target
	var rate rateEnvelope
	if err := fromJSON(rateRaw, &rate); err != nil {
		return nil, err
	}
	r.MaxPerHour = rate.MaxPerHour
	r.DedupWindow = time.Duration(rate.DedupWindow)
	r.Enabled = enabled != 0
	return &r, nil
}

// SaveRule creates or replaces a rule.
func (s *SQLTriggerStore) SaveRule(r *TriggerRule) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		now := ms(time.Now().UTC())
		_, err := ex.Exec(ctx, `INSERT INTO triggers(id,scope_id,name,filter_json,action_json,rate_limit_json,enabled,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET filter_json=excluded.filter_json,action_json=excluded.action_json,
			rate_limit_json=excluded.rate_limit_json,enabled=excluded.enabled,updated_at=excluded.updated_at`,
			r.ID, "", "", mustJSON(r.Filter),
			mustJSON(map[string]string{"action": string(r.Action), "target": r.Target}),
			mustJSON(rateEnvelope{MaxPerHour: r.MaxPerHour, DedupWindow: int64(r.DedupWindow)}),
			boolToInt(r.Enabled), now, now)
		return err
	})
}

// DeleteRule removes a rule.
func (s *SQLTriggerStore) DeleteRule(id string) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `DELETE FROM triggers WHERE id=?`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("agenda: unknown trigger rule %q", id)
		}
		_, err = ex.Exec(ctx, `DELETE FROM trigger_events WHERE rule_id=?`, id)
		return err
	})
}

// Rules lists rules in id order.
func (s *SQLTriggerStore) Rules() []*TriggerRule {
	var out []*TriggerRule
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,scope_id,name,filter_json,action_json,rate_limit_json,enabled,created_at,updated_at FROM triggers ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRule(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out
}

// RecordHit stores one firing for dedup and rate limiting.
func (s *SQLTriggerStore) RecordHit(ruleID, eventID string, at time.Time) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO trigger_events(rule_id,event_id,fired_ms) VALUES(?,?,?)
			ON CONFLICT(rule_id,event_id) DO UPDATE SET fired_ms=excluded.fired_ms`,
			ruleID, eventID, ms(at))
		return err
	})
}

// SeenSince reports whether (rule, event) fired within window of now.
func (s *SQLTriggerStore) SeenSince(ruleID, eventID string, window time.Duration, now time.Time) bool {
	var fired int64
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		_ = q.QueryRow(ctx, `SELECT fired_ms FROM trigger_events WHERE rule_id=? AND event_id=?`, ruleID, eventID).Scan(&fired)
		return nil
	})
	if fired == 0 {
		return false
	}
	return now.Sub(fromMs(fired)) < window
}

// HitsSince counts firings of a rule after cutoff (rate limiting).
func (s *SQLTriggerStore) HitsSince(ruleID string, cutoff time.Time) int {
	var n int
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM trigger_events WHERE rule_id=? AND fired_ms>?`, ruleID, ms(cutoff)).Scan(&n)
		return nil
	})
	return n
}

// Prune drops firing marks older than before.
func (s *SQLTriggerStore) Prune(before time.Time) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `DELETE FROM trigger_events WHERE fired_ms<?`, ms(before))
		return err
	})
}

// AttachTriggerStore wires the SQL mirror into the engine: rule
// changes and every firing write through, and RestoreTriggers reloads
// rules plus recent history after a restart.
func (e *TriggerEngine) AttachTriggerStore(s *SQLTriggerStore) { e.persist = s }

// RestoreTriggers reloads rules and recent firing marks (dedup window
// plus the rate-limit hour) so a restart neither refires nor drops
// limits.
func (e *TriggerEngine) RestoreTriggers(now time.Time) {
	if e.persist == nil {
		return
	}
	if e.seen == nil {
		e.seen = map[string]time.Time{}
	}
	if e.hits == nil {
		e.hits = map[string][]time.Time{}
	}
	rules := e.persist.Rules()
	e.rules = append([]*TriggerRule(nil), rules...)
	maxWindow := defaultDedupWindow
	for _, r := range e.rules {
		if w := e.dedupWindow(r); w > maxWindow {
			maxWindow = w
		}
	}
	_ = e.persist.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT rule_id,event_id,fired_ms FROM trigger_events WHERE fired_ms>? ORDER BY fired_ms`, ms(now.Add(-maxWindow)))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ruleID, eventID string
			var fired int64
			if err := rows.Scan(&ruleID, &eventID, &fired); err != nil {
				return err
			}
			at := fromMs(fired)
			e.seen[ruleID+"\x00"+eventID] = at
			if now.Sub(at) < time.Hour {
				e.hits[ruleID] = append(e.hits[ruleID], at)
			}
		}
		return rows.Err()
	})
	for id, times := range e.hits {
		sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
		e.hits[id] = times
	}
}

// --- Workflows ---

// SQLWorkflowStore persists versioned workflow definitions and their
// instances: definitions append-only per version (21.4: publishing a
// new version never alters running instances), instances pinned to
// the version they started on.
type SQLWorkflowStore struct {
	W store.Writer
	R sqlReader
}

// OpenWorkflowStore returns a workflow store over db.
func OpenWorkflowStore(db *store.DB) *SQLWorkflowStore { return &SQLWorkflowStore{W: db, R: db} }

// SaveDef stores one definition version and refreshes the latest row.
func (s *SQLWorkflowStore) SaveDef(d *WorkflowDef) error {
	if d.ID == "" || len(d.Nodes) == 0 {
		return fmt.Errorf("agenda: workflow definition needs an id and nodes")
	}
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		now := ms(time.Now().UTC())
		if _, err := ex.Exec(ctx, `INSERT INTO workflow_versions(workflow_id,version,definition_json,created_at)
			VALUES(?,?,?,?) ON CONFLICT(workflow_id,version) DO UPDATE SET definition_json=excluded.definition_json`,
			d.ID, d.Version, mustJSON(d), now); err != nil {
			return err
		}
		_, err := ex.Exec(ctx, `INSERT INTO workflows(id,scope_id,name,version,definition_json,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,version=excluded.version,
			definition_json=excluded.definition_json,updated_at=excluded.updated_at`,
			d.ID, "", d.Name, d.Version, mustJSON(d), now, now)
		return err
	})
}

// GetDef returns one definition version.
func (s *SQLWorkflowStore) GetDef(id string, version int) (*WorkflowDef, error) {
	var out *WorkflowDef
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		var raw string
		if err := q.QueryRow(ctx, `SELECT definition_json FROM workflow_versions WHERE workflow_id=? AND version=?`,
			id, version).Scan(&raw); err != nil {
			if isNotFound(err) {
				return fmt.Errorf("agenda: unknown workflow %q version %d", id, version)
			}
			return err
		}
		var d WorkflowDef
		if err := fromJSON(raw, &d); err != nil {
			return err
		}
		out = &d
		return nil
	})
	return out, err
}

// LatestDef returns the newest stored version.
func (s *SQLWorkflowStore) LatestDef(id string) (*WorkflowDef, error) {
	var out *WorkflowDef
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		var raw string
		if err := q.QueryRow(ctx, `SELECT definition_json FROM workflow_versions WHERE workflow_id=? ORDER BY version DESC LIMIT 1`,
			id).Scan(&raw); err != nil {
			if isNotFound(err) {
				return fmt.Errorf("agenda: unknown workflow %q", id)
			}
			return err
		}
		var d WorkflowDef
		if err := fromJSON(raw, &d); err != nil {
			return err
		}
		out = &d
		return nil
	})
	return out, err
}

// SaveInstance creates or updates an instance, syncing its status.
func (s *SQLWorkflowStore) SaveInstance(in *WorkflowInstance) error {
	if in.ID == "" || in.WorkflowID == "" {
		return fmt.Errorf("agenda: workflow instance needs an id and workflow id")
	}
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		now := ms(time.Now().UTC())
		_, err := ex.Exec(ctx, `INSERT INTO workflow_runs(id,workflow_id,version_pinned,status,state_json,created_at,updated_at,scope_id)
			VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,state_json=excluded.state_json,
			version_pinned=excluded.version_pinned,updated_at=excluded.updated_at`,
			in.ID, in.WorkflowID, in.VersionPinned, string(in.State), mustJSON(in), now, now, "")
		return err
	})
}

// GetInstance returns one instance by id.
func (s *SQLWorkflowStore) GetInstance(id string) (*WorkflowInstance, error) {
	var out *WorkflowInstance
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		var raw string
		if err := q.QueryRow(ctx, `SELECT state_json FROM workflow_runs WHERE id=?`, id).Scan(&raw); err != nil {
			if isNotFound(err) {
				return fmt.Errorf("agenda: unknown workflow instance %q", id)
			}
			return err
		}
		var in WorkflowInstance
		if err := fromJSON(raw, &in); err != nil {
			return err
		}
		out = &in
		return nil
	})
	return out, err
}

// ListInstances lists instances of one workflow, newest first.
func (s *SQLWorkflowStore) ListInstances(workflowID string) []*WorkflowInstance {
	var out []*WorkflowInstance
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT state_json FROM workflow_runs WHERE workflow_id=? ORDER BY rowid DESC`, workflowID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var in WorkflowInstance
			if err := fromJSON(raw, &in); err != nil {
				return err
			}
			out = append(out, &in)
		}
		return rows.Err()
	})
	return out
}

// --- Sessions ---

// SQLSessionStore persists long-initiative work sessions (Section
// 17.6): heartbeats, handoffs and startup state survive restarts.
type SQLSessionStore struct {
	W store.Writer
	R sqlReader
}

// OpenSessionStore returns a session store over db.
func OpenSessionStore(db *store.DB) *SQLSessionStore { return &SQLSessionStore{W: db, R: db} }

func scanSession(scan func(...any) error) (*Session, error) {
	var sn Session
	var id, initiative, milestone, state string
	var budget float64
	var windowStart, heartbeat int64
	var startupRaw, handoff, longTool string
	var created, updated int64
	if err := scan(&id, &initiative, &milestone, &state, &budget, &windowStart, &startupRaw,
		&handoff, &heartbeat, &longTool, &created, &updated); err != nil {
		return nil, err
	}
	sn.ID = id
	sn.Initiative = initiative
	sn.Milestone = milestone
	sn.State = SessionState(state)
	sn.BudgetUSD = budget
	sn.WindowStart = fromMs(windowStart)
	_ = fromJSON(startupRaw, &sn.StartupCmds)
	sn.Handoff = handoff
	sn.Heartbeat = fromMs(heartbeat)
	sn.LongTool = longTool
	return &sn, nil
}

// Save creates or replaces a session.
func (s *SQLSessionStore) Save(sn *Session) error {
	if sn.ID == "" {
		return fmt.Errorf("agenda: session needs an id")
	}
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		now := ms(time.Now().UTC())
		_, err := ex.Exec(ctx, `INSERT INTO sessions(id,initiative,milestone,state,budget_usd,window_start_ms,
			startup_json,handoff,heartbeat_ms,long_tool,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET initiative=excluded.initiative,
			milestone=excluded.milestone,state=excluded.state,budget_usd=excluded.budget_usd,
			window_start_ms=excluded.window_start_ms,startup_json=excluded.startup_json,handoff=excluded.handoff,
			heartbeat_ms=excluded.heartbeat_ms,long_tool=excluded.long_tool,updated_at=excluded.updated_at`,
			sn.ID, sn.Initiative, sn.Milestone, string(sn.State), sn.BudgetUSD, ms(sn.WindowStart),
			mustJSON(sn.StartupCmds), sn.Handoff, ms(sn.Heartbeat), sn.LongTool, now, now)
		return err
	})
}

// Get returns one session.
func (s *SQLSessionStore) Get(id string) (*Session, error) {
	var out *Session
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		sn, err := scanSession(func(dest ...any) error {
			return q.QueryRow(ctx, `SELECT id,initiative,milestone,state,budget_usd,window_start_ms,
				startup_json,handoff,heartbeat_ms,long_tool,created_at,updated_at FROM sessions WHERE id=?`, id).Scan(dest...)
		})
		if err != nil {
			if isNotFound(err) {
				return fmt.Errorf("agenda: unknown session %q", id)
			}
			return err
		}
		out = sn
		return nil
	})
	return out, err
}

// ListByInitiative lists sessions of one initiative in id order.
func (s *SQLSessionStore) ListByInitiative(initiative string) []*Session {
	var out []*Session
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,initiative,milestone,state,budget_usd,window_start_ms,
			startup_json,handoff,heartbeat_ms,long_tool,created_at,updated_at FROM sessions WHERE initiative=? ORDER BY id`, initiative)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			sn, err := scanSession(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, sn)
		}
		return rows.Err()
	})
	return out
}

// Delete removes a session.
func (s *SQLSessionStore) Delete(id string) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `DELETE FROM sessions WHERE id=?`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("agenda: unknown session %q", id)
		}
		return nil
	})
}

// --- Daemon-branch compatibility (wave 4 merge) ---
//
// The daemon boot path (cmd/excalibur/daemon.go, internal/daemon
// wiring tests, tests/walkthrough_test.go) was built against the
// New*/EnsureSQLSchema/Save/Load/Delete spelling. Those constructors
// are aliases over the migration-aligned Open* stores above: the
// v4/v5 tables are the single source of truth (Appendix A), and
// EnsureSQLSchema only guarantees they exist so standalone databases
// opened without the migration registry still work.

// agendaCompatDDL mirrors the v4/v5 DDL for the agenda tables so a
// database opened without migrations.Main still boots. Every
// statement is IF NOT EXISTS and the v5 ALTER COLUMN is duplicate
// tolerant, so running it on a migrated database is a no-op.
var agendaCompatDDL = []string{
	`CREATE TABLE IF NOT EXISTS schedules(
		id TEXT PRIMARY KEY,
		scope_id TEXT NOT NULL,
		kind TEXT NOT NULL DEFAULT 'at',
		spec_json TEXT NOT NULL DEFAULT '{}',
		next_fire_ms INTEGER NOT NULL DEFAULT 0,
		tz TEXT NOT NULL DEFAULT 'UTC',
		missed_policy TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
	)`,
	`CREATE TABLE IF NOT EXISTS reminders(
		id TEXT PRIMARY KEY,
		scope_id TEXT NOT NULL,
		text TEXT NOT NULL DEFAULT '',
		fire_ms INTEGER NOT NULL DEFAULT 0,
		recurrence_json TEXT NOT NULL DEFAULT '{}',
		channel TEXT NOT NULL DEFAULT '',
		task_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
	)`,
	`CREATE TABLE IF NOT EXISTS triggers(
		id TEXT PRIMARY KEY,
		scope_id TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		filter_json TEXT NOT NULL DEFAULT '{}',
		action_json TEXT NOT NULL DEFAULT '{}',
		rate_limit_json TEXT NOT NULL DEFAULT '{}',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
	)`,
	`CREATE TABLE IF NOT EXISTS agenda_fires(
		fire_id TEXT PRIMARY KEY,
		schedule_id TEXT NOT NULL DEFAULT '',
		scheduled_ms INTEGER NOT NULL DEFAULT 0,
		fired_ms INTEGER NOT NULL DEFAULT 0,
		late INTEGER NOT NULL DEFAULT 0,
		payload TEXT NOT NULL DEFAULT '',
		scope_id TEXT NOT NULL DEFAULT '',
		delivered INTEGER NOT NULL DEFAULT 0,
		pending INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS trigger_events(
		rule_id TEXT NOT NULL,
		event_id TEXT NOT NULL,
		fired_ms INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(rule_id, event_id)
	)`,
	`CREATE TABLE IF NOT EXISTS workflow_versions(
		workflow_id TEXT NOT NULL,
		version INTEGER NOT NULL,
		definition_json TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
		PRIMARY KEY(workflow_id, version)
	)`,
	`CREATE TABLE IF NOT EXISTS workflows(
		id TEXT PRIMARY KEY,
		scope_id TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		version INTEGER NOT NULL DEFAULT 1,
		definition_json TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
	)`,
	`CREATE TABLE IF NOT EXISTS workflow_runs(
		id TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		version_pinned INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL DEFAULT 'created',
		state_json TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
		scope_id TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS sessions(
		id TEXT PRIMARY KEY,
		initiative TEXT NOT NULL DEFAULT '',
		milestone TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'scheduled',
		budget_usd REAL NOT NULL DEFAULT 0,
		window_start_ms INTEGER NOT NULL DEFAULT 0,
		startup_json TEXT NOT NULL DEFAULT '[]',
		handoff TEXT NOT NULL DEFAULT '',
		heartbeat_ms INTEGER NOT NULL DEFAULT 0,
		long_tool TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
	)`,
}

// EnsureSQLSchema creates the agenda tables when missing. On a
// database opened through migrations.Main it is a no-op; on a bare
// database it creates the same tables the migrations would.
func EnsureSQLSchema(ctx context.Context, db *store.DB) error {
	return db.Write(ctx, store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		for _, stmt := range agendaCompatDDL {
			if _, err := ex.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("agenda: ensure schema: %w", err)
			}
		}
		return nil
	})
}

// NewSQLAgendaStore is the daemon-spelling alias for OpenAgendaStore.
func NewSQLAgendaStore(db *store.DB) *SQLAgendaStore { return OpenAgendaStore(db) }

// NewSQLReminderStore is the daemon-spelling alias for OpenReminderStore.
func NewSQLReminderStore(db *store.DB) *SQLReminderStore { return OpenReminderStore(db) }

// NewSQLTriggerStore is the daemon-spelling alias for OpenTriggerStore.
func NewSQLTriggerStore(db *store.DB) *SQLTriggerStore { return OpenTriggerStore(db) }

// Save creates or replaces a rule (daemon spelling of SaveRule).
func (s *SQLTriggerStore) Save(r *TriggerRule) error { return s.SaveRule(r) }

// Load returns every stored rule in id order (daemon spelling of Rules).
func (s *SQLTriggerStore) Load() []*TriggerRule { return s.Rules() }

// Delete removes a rule (daemon spelling of DeleteRule).
func (s *SQLTriggerStore) Delete(id string) error { return s.DeleteRule(id) }
