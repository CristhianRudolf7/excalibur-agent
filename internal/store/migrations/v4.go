package migrations

import (
	"context"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// upV4 creates the agenda, workflow, file and vector tables
// (Appendix A, agenda and files groups). vec0 tables are created
// only when the module is present: the pure-Go fallback backend has
// no sqlite-vec, so the migration probes and records the outcome in
// meta instead of failing (Section 10.8 degradation).
func upV4(ctx context.Context, ex store.Execer) error {
	if err := execAll(ctx, ex, []string{
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
		`CREATE INDEX IF NOT EXISTS idx_schedules_fire ON schedules(next_fire_ms)`,
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
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
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
		`CREATE TABLE IF NOT EXISTS file_versions(
			id TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			preimage_digest TEXT NOT NULL DEFAULT '',
			postimage_digest TEXT NOT NULL DEFAULT '',
			run_id TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_versions_path ON file_versions(path, created_at)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS file_paths_trg USING fts5(
			path, tokenize='trigram'
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_file_paths_insert AFTER INSERT ON file_versions BEGIN
			INSERT INTO file_paths_trg(rowid, path) VALUES(NEW.rowid, NEW.path);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_file_paths_delete AFTER DELETE ON file_versions BEGIN
			DELETE FROM file_paths_trg WHERE rowid=OLD.rowid;
		END`,
		// Shadow vectors: the portable source of truth. vec0 tables
		// index them when the module exists; rebuilds and model
		// changes (Section 10.8) always start here.
		`CREATE TABLE IF NOT EXISTS embedding_vectors(
			entity_kind TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			space_id TEXT NOT NULL DEFAULT '',
			dims INTEGER NOT NULL DEFAULT 512,
			vector BLOB NOT NULL,
			scope_id TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(entity_kind, entity_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_embedding_vectors_scope ON embedding_vectors(scope_id)`,
	}); err != nil {
		return err
	}

	vecTables := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS memories_vec USING vec0(
			memory_id TEXT PRIMARY KEY,
			embedding float[512],
			scope_id TEXT PARTITION KEY
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS doc_chunks_vec USING vec0(
			chunk_id TEXT PRIMARY KEY,
			embedding float[512],
			scope_id TEXT PARTITION KEY
		)`,
	}
	vecOK := true
	for _, stmt := range vecTables {
		if _, err := ex.Exec(ctx, stmt); err != nil {
			if isMissingModule(err) {
				vecOK = false
				break
			}
			return err
		}
	}
	status := "available"
	if !vecOK {
		status = "unavailable"
		// Roll back the half-created virtual table, if any.
		_, _ = ex.Exec(ctx, "DROP TABLE IF EXISTS memories_vec")
		_, _ = ex.Exec(ctx, "DROP TABLE IF EXISTS doc_chunks_vec")
	}
	_, err := ex.Exec(ctx, "INSERT INTO meta(key,value) VALUES('vector_search',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", status)
	return err
}

// isMissingModule reports the "no such module: vec0" probe failure.
func isMissingModule(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such module")
}
