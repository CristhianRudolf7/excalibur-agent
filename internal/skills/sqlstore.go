// SQL-backed skill index cache (Sections 12.2, 12.4).
//
// Scanning every root reparses every SKILL.md; the cache persists
// the merged index (entries keyed by name), the per-model outcome
// statistics and the per-model disabled groups in the Appendix A
// skills tables, so a restart reloads the cache and only rescans for
// drift. Index.SaveCache/LoadCache are the production pair: the
// daemon loads the cache at boot, rescans, and saves on an interval.
// skill_versions rows record installs for drift detection (12.8).
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// sqlReader is the read half: satisfied by *store.DB and test fakes.
type sqlReader interface {
	Read(ctx context.Context, fn store.ReadFunc) error
}

// SQLSkillCache persists the index over db.
type SQLSkillCache struct {
	W store.Writer
	R sqlReader
}

// OpenSkillCache returns a cache over db.
func OpenSkillCache(db *store.DB) *SQLSkillCache { return &SQLSkillCache{W: db, R: db} }

func bg() context.Context { return context.Background() }

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"encode_error":%q}`, err.Error())
	}
	return string(raw)
}

// SaveEntry upserts one merged index entry, keyed by name.
func (c *SQLSkillCache) SaveEntry(e *Entry) error {
	if e == nil || e.Name == "" {
		return fmt.Errorf("skills: cache entry needs a name")
	}
	return c.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		now := ms(time.Now().UTC())
		_, err := ex.Exec(ctx, `INSERT INTO skills(id,name,index_json,created_at,updated_at,scope_id)
			VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,index_json=excluded.index_json,
			updated_at=excluded.updated_at`,
			e.Name, e.Name, mustJSON(e), now, now, "")
		return err
	})
}

// DeleteEntry drops one cached entry.
func (c *SQLSkillCache) DeleteEntry(name string) error {
	return c.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `DELETE FROM skills WHERE id=?`, name)
		return err
	})
}

// LoadEntries returns cached entries in name order.
func (c *SQLSkillCache) LoadEntries() ([]*Entry, error) {
	var out []*Entry
	err := c.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT index_json FROM skills ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var e Entry
			if err := json.Unmarshal([]byte(raw), &e); err != nil {
				return fmt.Errorf("skills: decode cached entry: %w", err)
			}
			cp := e
			out = append(out, &cp)
		}
		return rows.Err()
	})
	return out, err
}

// SaveStats records the per-model outcomes of one skill (12.4).
func (c *SQLSkillCache) SaveStats(skill, model string, st modelStats) error {
	return c.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO skill_stats(skill_id,model,with_total,with_success,without_total,without_success,updated_at)
			VALUES(?,?,?,?,?,?,?) ON CONFLICT(skill_id,model) DO UPDATE SET with_total=excluded.with_total,
			with_success=excluded.with_success,without_total=excluded.without_total,
			without_success=excluded.without_success,updated_at=excluded.updated_at`,
			skill, model, st.WithTotal, st.WithSuccess, st.WithoutTotal, st.WithoutSucc, ms(time.Now().UTC()))
		return err
	})
}

// LoadStats returns stats keyed by skill then model.
func (c *SQLSkillCache) LoadStats() (map[string]map[string]*modelStats, error) {
	out := map[string]map[string]*modelStats{}
	err := c.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT skill_id,model,with_total,with_success,without_total,without_success FROM skill_stats`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var skill, model string
			var st modelStats
			if err := rows.Scan(&skill, &model, &st.WithTotal, &st.WithSuccess, &st.WithoutTotal, &st.WithoutSucc); err != nil {
				return err
			}
			if out[skill] == nil {
				out[skill] = map[string]*modelStats{}
			}
			cp := st
			out[skill][model] = &cp
		}
		return rows.Err()
	})
	return out, err
}

// SetGroupDisabled pins or releases a group for a model (12.4).
func (c *SQLSkillCache) SetGroupDisabled(model, group string, disabled bool) error {
	return c.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		if !disabled {
			_, err := ex.Exec(ctx, `DELETE FROM skill_disabled WHERE model=? AND skill_group=?`, model, group)
			return err
		}
		_, err := ex.Exec(ctx, `INSERT INTO skill_disabled(model,skill_group,updated_at) VALUES(?,?,?)
			ON CONFLICT(model,skill_group) DO UPDATE SET updated_at=excluded.updated_at`,
			model, group, ms(time.Now().UTC()))
		return err
	})
}

// LoadDisabled returns disabled groups keyed by model.
func (c *SQLSkillCache) LoadDisabled() (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	err := c.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT model,skill_group FROM skill_disabled`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var model, group string
			if err := rows.Scan(&model, &group); err != nil {
				return err
			}
			if out[model] == nil {
				out[model] = map[string]bool{}
			}
			out[model][group] = true
		}
		return rows.Err()
	})
	return out, err
}

// RecordVersion stores one installed skill version for drift
// detection (Section 12.8).
func (c *SQLSkillCache) RecordVersion(skillID, version, hash, manifest string) error {
	return c.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		id := skillID + "@" + version
		_, err := ex.Exec(ctx, `INSERT INTO skill_versions(id,skill_id,version,hash,manifest_json,created_at)
			VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET hash=excluded.hash,manifest_json=excluded.manifest_json`,
			id, skillID, version, hash, manifest, ms(time.Now().UTC()))
		return err
	})
}

// Version returns the newest recorded version row of a skill.
func (c *SQLSkillCache) Version(skillID string) (version, hash string, ok bool) {
	_ = c.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		err := q.QueryRow(ctx, `SELECT version,hash FROM skill_versions WHERE skill_id=? ORDER BY rowid DESC LIMIT 1`,
			skillID).Scan(&version, &hash)
		if err != nil {
			return err
		}
		ok = true
		return nil
	})
	return version, hash, ok
}

// SaveCache persists the merged index: every entry, every stat row
// and every disabled group. Best-effort per row would leave a
// half-written cache after a crash, so entries write in one porter
// transaction while stats and disabled groups follow in their own.
func (ix *Index) SaveCache(c *SQLSkillCache) error {
	ix.mu.RLock()
	entries := make([]*Entry, 0, len(ix.entries))
	for _, e := range ix.entries {
		cp := *e
		entries = append(entries, &cp)
	}
	stats := map[string]map[string]modelStats{}
	for skill, per := range ix.stats {
		stats[skill] = map[string]modelStats{}
		for model, st := range per {
			if st != nil {
				stats[skill][model] = *st
			}
		}
	}
	disabled := map[string][]string{}
	for model, groups := range ix.disabled {
		for g, off := range groups {
			if off {
				disabled[model] = append(disabled[model], g)
			}
		}
	}
	ix.mu.RUnlock()
	if err := c.W.Write(bg(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		now := ms(time.Now().UTC())
		for _, e := range entries {
			if _, err := ex.Exec(ctx, `INSERT INTO skills(id,name,index_json,created_at,updated_at,scope_id)
				VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET index_json=excluded.index_json,updated_at=excluded.updated_at`,
				e.Name, e.Name, mustJSON(e), now, now, ""); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for skill, per := range stats {
		for model, st := range per {
			if err := c.SaveStats(skill, model, st); err != nil {
				return err
			}
		}
	}
	for model, groups := range disabled {
		for _, g := range groups {
			if err := c.SetGroupDisabled(model, g, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// LoadCache restores entries, stats and disabled groups saved by
// SaveCache. It does not replace Scan: after loading, the daemon
// rescans and any drift revalidates (Section 12.2).
func (ix *Index) LoadCache(c *SQLSkillCache) error {
	entries, err := c.LoadEntries()
	if err != nil {
		return err
	}
	stats, err := c.LoadStats()
	if err != nil {
		return err
	}
	disabled, err := c.LoadDisabled()
	if err != nil {
		return err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.entries == nil {
		ix.entries = map[string]*Entry{}
	}
	for _, e := range entries {
		ix.entries[e.Name] = e
	}
	if ix.stats == nil {
		ix.stats = map[string]map[string]*modelStats{}
	}
	for skill, per := range stats {
		if ix.stats[skill] == nil {
			ix.stats[skill] = map[string]*modelStats{}
		}
		for model, st := range per {
			ix.stats[skill][model] = st
		}
	}
	if ix.disabled == nil {
		ix.disabled = map[string]map[string]bool{}
	}
	for model, groups := range disabled {
		if ix.disabled[model] == nil {
			ix.disabled[model] = map[string]bool{}
		}
		for g := range groups {
			ix.disabled[model][g] = true
		}
	}
	return nil
}
