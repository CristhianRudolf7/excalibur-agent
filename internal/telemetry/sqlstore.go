// SQL-backed telemetry persistence (Section 22).
//
// The live Bus stays the in-memory fan-out; SQLStore is the durable
// query side over telemetry.db (migrations.Telemetry()): raw events,
// per-task metric snapshots and exported spans. Purge and retention
// (store package, Section 4.7) trim telemetry_events and raw detail;
// aggregates and liveness rows stay queryable. Production opens it
// with OpenTelemetryStore next to the bus; unit tests keep the bus
// alone.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// sqlReader is the read half: satisfied by *store.DB and test fakes.
type sqlReader interface {
	Read(ctx context.Context, fn store.ReadFunc) error
}

// SQLStore persists telemetry rows through the porter: writes on the
// background lane (telemetry never blocks interaction), reads from
// the pool. It is safe for concurrent use.
type SQLStore struct {
	W store.Writer
	R sqlReader
}

// OpenTelemetryStore returns a telemetry store over a database opened
// with the Telemetry() migration registry. Use TelemetryOptions for
// it (synchronous=NORMAL, Section 4.2).
func OpenTelemetryStore(db *store.DB) *SQLStore { return &SQLStore{W: db, R: db} }

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

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"encode_error":%q}`, err.Error())
	}
	return string(raw)
}

// AppendEvent records one typed event and returns its row sequence.
// The sequence is the cursor subscribers resume from (Section 22.1).
func (s *SQLStore) AppendEvent(runID, taskID, typ string, payload json.RawMessage, at time.Time) (int64, error) {
	var seq int64
	err := s.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `INSERT INTO telemetry_events(run_id,task_id,type,payload_json,created_ms)
			VALUES(?,?,?,?,?)`, runID, taskID, typ, string(payload), ms(at))
		if err != nil {
			return err
		}
		id, err := res.RowsAffected()
		_ = id
		if err != nil {
			return err
		}
		return ex.QueryRow(ctx, `SELECT last_insert_rowid()`).Scan(&seq)
	})
	return seq, err
}

func scanEvent(scan func(...any) error) (types.Event, error) {
	var ev types.Event
	var seq int64
	var runID, taskID, typ, payload string
	var created int64
	if err := scan(&seq, &runID, &taskID, &typ, &payload, &created); err != nil {
		return types.Event{}, err
	}
	ev.Seq = uint64(seq)
	ev.ID = fmt.Sprintf("evt_%d", seq)
	ev.Type = types.EventType(typ)
	ev.At = fromMs(created)
	if runID != "" {
		r := types.RunID(runID)
		ev.RunID = &r
	}
	if taskID != "" {
		t := types.TaskID(taskID)
		ev.TaskID = &t
	}
	if payload != "" && payload != "{}" {
		ev.Payload = json.RawMessage(payload)
	}
	return ev, nil
}

// EventsSince lists events after a sequence cursor, oldest first,
// capped at limit (0 means 1000). It backs cursor resume without gaps
// or duplicates (Section 3.5).
func (s *SQLStore) EventsSince(after uint64, limit int) ([]types.Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	var out []types.Event
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT seq,run_id,task_id,type,payload_json,created_ms
			FROM telemetry_events WHERE seq>? ORDER BY seq LIMIT ?`, after, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			ev, err := scanEvent(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, ev)
		}
		return rows.Err()
	})
	return out, err
}

// EventsForRun lists events of one run in order.
func (s *SQLStore) EventsForRun(runID string) ([]types.Event, error) {
	var out []types.Event
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT seq,run_id,task_id,type,payload_json,created_ms
			FROM telemetry_events WHERE run_id=? ORDER BY seq`, runID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			ev, err := scanEvent(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, ev)
		}
		return rows.Err()
	})
	return out, err
}

// SaveMetrics upserts the exported snapshot of one task run
// (Section 22.2).
func (s *SQLStore) SaveMetrics(snap Snapshot) error {
	if snap.TaskID == "" {
		return fmt.Errorf("telemetry: metrics snapshot needs a task id")
	}
	return s.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO task_metrics(task_id,run_id,snapshot_json,cost_usd,final_state,updated_ms)
			VALUES(?,?,?,?,?,?) ON CONFLICT(task_id) DO UPDATE SET run_id=excluded.run_id,
			snapshot_json=excluded.snapshot_json,cost_usd=excluded.cost_usd,final_state=excluded.final_state,
			updated_ms=excluded.updated_ms`,
			string(snap.TaskID), string(snap.RunID), mustJSON(snap), snap.CostUSD, snap.FinalState, ms(time.Now().UTC()))
		return err
	})
}

// Metrics returns the snapshot of one task run.
func (s *SQLStore) Metrics(taskID types.TaskID) (Snapshot, bool) {
	var out Snapshot
	var ok bool
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		var raw string
		if err := q.QueryRow(ctx, `SELECT snapshot_json FROM task_metrics WHERE task_id=?`, string(taskID)).Scan(&raw); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return err
		}
		ok = true
		return nil
	})
	return out, ok
}

// AppendSpan records one exported span (Section 22.3).
func (s *SQLStore) AppendSpan(sp Span, runID types.RunID) error {
	return s.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO spans(trace_id,span_id,parent_id,run_id,name,kind,started_ms,ended_ms,status,attrs_json)
			VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(trace_id,span_id) DO UPDATE SET ended_ms=excluded.ended_ms,
			status=excluded.status,attrs_json=excluded.attrs_json`,
			sp.TraceID, sp.SpanID, sp.ParentID, string(runID), sp.Name, sp.Kind,
			ms(sp.StartedAt), ms(sp.EndedAt), sp.Status, mustJSON(sp.Attributes))
		return err
	})
}

// SpansForTrace lists spans of one trace in start order.
func (s *SQLStore) SpansForTrace(traceID string) ([]Span, error) {
	var out []Span
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT span_id,parent_id,run_id,name,kind,started_ms,ended_ms,status,attrs_json
			FROM spans WHERE trace_id=? ORDER BY started_ms`, traceID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sp Span
			var runID string
			var started, ended int64
			var attrs string
			if err := rows.Scan(&sp.SpanID, &sp.ParentID, &runID, &sp.Name, &sp.Kind,
				&started, &ended, &sp.Status, &attrs); err != nil {
				return err
			}
			sp.TraceID = traceID
			sp.StartedAt = fromMs(started)
			sp.EndedAt = fromMs(ended)
			_ = json.Unmarshal([]byte(attrs), &sp.Attributes)
			out = append(out, sp)
		}
		return rows.Err()
	})
	return out, err
}
