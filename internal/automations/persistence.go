package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

type storedVersion struct {
	Version
	Spec Spec
}
type record struct {
	Automation Automation
	Versions   []storedVersion
	Runs       []Run
}

// Historical runs and versions are immutable. Copy their slice headers and
// the current automation instead of re-encoding the entire retained history
// on every step transition.
func copyRecord(r *record) *record {
	return &record{Automation: clone(r.Automation), Versions: append([]storedVersion(nil), r.Versions...), Runs: append([]Run(nil), r.Runs...)}
}

func clone[T any](v T) T {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out T
	if err = json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func (m *Manager) load(ctx context.Context) error {
	if m.cfg.DB == nil {
		return nil
	}
	return m.cfg.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT data_json FROM automations`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			var a Automation
			if err = json.Unmarshal([]byte(raw), &a); err != nil {
				rows.Close()
				return err
			}
			m.records[a.ID] = &record{Automation: a}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = q.Query(ctx, `SELECT automation_id,version,note,created_at,spec_json FROM automation_versions ORDER BY version`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, created, raw string
			var v storedVersion
			if err = rows.Scan(&id, &v.Version.Version, &v.Note, &created, &raw); err != nil {
				rows.Close()
				return err
			}
			if err = json.Unmarshal([]byte(raw), &v.Spec); err != nil {
				rows.Close()
				return err
			}
			if err = v.CreatedAt.UnmarshalText([]byte(created)); err != nil {
				rows.Close()
				return err
			}
			if r := m.records[id]; r != nil {
				r.Versions = append(r.Versions, v)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = q.Query(ctx, `SELECT automation_id,data_json FROM automation_runs ORDER BY started_at DESC, rowid DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, raw string
			var run Run
			if err = rows.Scan(&id, &raw); err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(raw), &run); err != nil {
				return err
			}
			if r := m.records[id]; r != nil {
				r.Runs = append(r.Runs, run)
			}
		}
		return rows.Err()
	})
}

// persistLocked writes a complete per-automation snapshot atomically. Memory is
// replaced only after the writer acknowledges the transaction.
func (m *Manager) persistLocked(ctx context.Context, r *record) error {
	if m.cfg.DB != nil {
		err := m.cfg.DB.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
			a := r.Automation
			raw, err := json.Marshal(a)
			if err != nil {
				return err
			}
			var next any
			if a.NextRunAt != nil {
				next = a.NextRunAt.UTC().Format(timeFormat)
			}
			if _, err = ex.Exec(ctx, `INSERT INTO automations(id,status,paused_reason,consecutive_failures,next_run_at,data_json) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,paused_reason=excluded.paused_reason,consecutive_failures=excluded.consecutive_failures,next_run_at=excluded.next_run_at,data_json=excluded.data_json`, a.ID, a.Status, a.PausedReason, a.ConsecutiveFailures, next, string(raw)); err != nil {
				return err
			}
			for _, v := range r.Versions {
				raw, err = json.Marshal(v.Spec)
				if err != nil {
					return err
				}
				if _, err = ex.Exec(ctx, `INSERT INTO automation_versions(automation_id,version,note,created_at,spec_json) VALUES(?,?,?,?,?) ON CONFLICT(automation_id,version) DO NOTHING`, a.ID, v.Version.Version, v.Note, v.CreatedAt.UTC().Format(timeFormat), string(raw)); err != nil {
					return err
				}
			}
			if _, err = ex.Exec(ctx, `DELETE FROM automation_runs WHERE automation_id=?`, a.ID); err != nil {
				return err
			}
			// Oldest first makes rowid a stable tiebreaker when a fake clock is fixed.
			for i := len(r.Runs) - 1; i >= 0; i-- {
				run := r.Runs[i]
				raw, err = json.Marshal(run)
				if err != nil {
					return err
				}
				steps, err := json.Marshal(run.Steps)
				if err != nil {
					return err
				}
				if _, err = ex.Exec(ctx, `INSERT INTO automation_runs(id,automation_id,started_at,steps_json,data_json) VALUES(?,?,?,?,?)`, run.ID, a.ID, run.StartedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), string(steps), string(raw)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("persist automation: %w", err)
		}
	}
	m.records[r.Automation.ID] = r
	return nil
}
