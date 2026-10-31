package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// memRegistry is the store-native MigrationRegistry used when Open
// receives nil: numbered, unique, gapless (Section 4.6).
type memRegistry struct {
	migrations []Migration
}

// NewRegistry returns an empty migration registry.
func NewRegistry() MigrationRegistry { return &memRegistry{} }

// Register implements MigrationRegistry.
func (r *memRegistry) Register(m Migration) error {
	r.migrations = append(r.migrations, m)
	return r.Validate()
}

// Ordered implements MigrationRegistry.
func (r *memRegistry) Ordered() []Migration {
	out := append([]Migration(nil), r.migrations...)
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// Latest implements MigrationRegistry.
func (r *memRegistry) Latest() int {
	ordered := r.Ordered()
	if len(ordered) == 0 {
		return 0
	}
	return ordered[len(ordered)-1].Version
}

// Validate implements MigrationRegistry.
func (r *memRegistry) Validate() error {
	seen := map[int]bool{}
	max := 0
	for _, m := range r.migrations {
		if m.Version < 1 || seen[m.Version] {
			return fmt.Errorf("store: bad migration version %d", m.Version)
		}
		seen[m.Version] = true
		if m.Version > max {
			max = m.Version
		}
	}
	for v := 1; v <= max; v++ {
		if !seen[v] {
			return fmt.Errorf("store: migration gap at version %d", v)
		}
	}
	return nil
}

// applyMigrations brings the database to reg.Latest(), one pending
// migration per transaction. Each migration either commits fully or
// leaves the schema untouched (Section 4.6), so a failed update can
// restore the backup without losing data.
func applyMigrations(ctx context.Context, conn *sql.Conn, reg MigrationRegistry) error {
	if err := reg.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrMigrationFailed, err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("%w: bootstrap: %v", ErrMigrationFailed, err)
	}
	current := 0
	rows, err := conn.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("%w: read versions: %v", ErrMigrationFailed, err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("%w: scan versions: %v", ErrMigrationFailed, err)
		}
		if v > current {
			current = v
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("%w: versions: %v", ErrMigrationFailed, err)
	}
	rows.Close()

	for _, m := range reg.Ordered() {
		if m.Version <= current {
			continue
		}
		if err := applyOne(ctx, conn, m); err != nil {
			return err
		}
	}
	return nil
}

// applyOne runs a single migration in its own transaction.
func applyOne(ctx context.Context, conn *sql.Conn, m Migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: v%d begin: %v", ErrMigrationFailed, m.Version, err)
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			_ = tx.Rollback()
		}
	}()
	if m.Up == nil {
		return fmt.Errorf("%w: v%d has no Up", ErrMigrationFailed, m.Version)
	}
	if err := m.Up(ctx, txExecer{tx: tx}); err != nil {
		_ = tx.Rollback()
		rolledBack = true
		return fmt.Errorf("%w: v%d %s: %v", ErrMigrationFailed, m.Version, m.Name, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,strftime('%s','now')*1000)", m.Version, m.Name); err != nil {
		_ = tx.Rollback()
		rolledBack = true
		return fmt.Errorf("%w: v%d record: %v", ErrMigrationFailed, m.Version, err)
	}
	if err := tx.Commit(); err != nil {
		rolledBack = true
		return fmt.Errorf("%w: v%d commit: %v", ErrMigrationFailed, m.Version, err)
	}
	rolledBack = true
	return nil
}

// SchemaVersion returns the applied migration version.
func SchemaVersion(ctx context.Context, q Queryer) (int, error) {
	row := q.QueryRow(ctx, "SELECT MAX(version) FROM schema_migrations")
	var out sql.NullInt64
	if err := row.Scan(&out); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, err
	}
	if !out.Valid {
		return 0, nil
	}
	return int(out.Int64), nil
}
