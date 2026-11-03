package migrations

import (
	"context"
	"fmt"
	"sort"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// Registry is the in-binary store.MigrationRegistry: embedded,
// numbered, transactional (Section 4.6).
type Registry struct {
	migrations []store.Migration
}

// New returns an empty registry.
func New() *Registry { return &Registry{} }

// Register implements store.MigrationRegistry.
func (r *Registry) Register(m store.Migration) error {
	r.migrations = append(r.migrations, m)
	return r.Validate()
}

// Ordered implements store.MigrationRegistry.
func (r *Registry) Ordered() []store.Migration {
	out := append([]store.Migration(nil), r.migrations...)
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// Latest implements store.MigrationRegistry.
func (r *Registry) Latest() int {
	ordered := r.Ordered()
	if len(ordered) == 0 {
		return 0
	}
	return ordered[len(ordered)-1].Version
}

// Validate implements store.MigrationRegistry: versions unique and
// gapless starting at 1 (expand-then-contract relies on order).
func (r *Registry) Validate() error {
	seen := map[int]bool{}
	max := 0
	for _, m := range r.migrations {
		if m.Version < 1 || seen[m.Version] {
			return fmt.Errorf("migrations: bad version %d", m.Version)
		}
		seen[m.Version] = true
		if m.Version > max {
			max = m.Version
		}
	}
	for v := 1; v <= max; v++ {
		if !seen[v] {
			return fmt.Errorf("migrations: gap at version %d", v)
		}
	}
	return nil
}

// Main returns the registry for excalibur.db: v1 conversation and
// work, v2 agents, v3 memory with FTS5, v4 agenda, files and vectors,
// v5 wiring tables for the SQL repositories plus additive columns on
// shared tables.
func Main() *Registry {
	r := New()
	_ = r.Register(store.Migration{Version: 1, Name: "conversation-work", Up: upV1})
	_ = r.Register(store.Migration{Version: 2, Name: "agents-runs", Up: upV2})
	_ = r.Register(store.Migration{Version: 3, Name: "memory-fts", Up: upV3})
	_ = r.Register(store.Migration{Version: 4, Name: "agenda-files-vec", Up: upV4})
	_ = r.Register(store.Migration{Version: 5, Name: "wiring-tables", Up: upV5})
	_ = r.Register(store.Migration{Version: 6, Name: "document-library", Up: upV6})
	_ = r.Register(LibraryJobs())
	_ = r.Register(store.Migration{Version: 8, Name: "whiteboards", Up: upV8})
	_ = r.Register(store.Migration{Version: 9, Name: "automations", Up: upV9})
	_ = r.Register(store.Migration{Version: 10, Name: "classroom", Up: upV10})
	return r
}

// Telemetry returns the registry for telemetry.db: events and daily
// aggregates (Section 4.2; detail belongs to milestone H10) plus v2
// task metrics and exported spans for Section 22 queries.
func Telemetry() *Registry {
	r := New()
	_ = r.Register(store.Migration{Version: 1, Name: "telemetry-base", Up: upTelemetry})
	_ = r.Register(store.Migration{Version: 2, Name: "telemetry-metrics-spans", Up: upTelemetryV2})
	return r
}

// execAll runs every statement in order inside the caller's
// transaction (the store runner owns BEGIN/COMMIT).
func execAll(ctx context.Context, ex store.Execer, stmts []string) error {
	for _, s := range stmts {
		if _, err := ex.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}
