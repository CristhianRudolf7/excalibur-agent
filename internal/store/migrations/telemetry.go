package migrations

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// upTelemetry creates the telemetry.db schema: raw events and daily
// aggregates (Section 4.2). Detail and queries belong to milestone
// H10; storage only guarantees the file, its durability mode and
// rotation of old rows.
func upTelemetry(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE IF NOT EXISTS telemetry_events(
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL DEFAULT '',
			task_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_ms INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_telemetry_run ON telemetry_events(run_id, seq)`,
		`CREATE TABLE IF NOT EXISTS telemetry_metrics_daily(
			day TEXT PRIMARY KEY,
			tokens_in INTEGER NOT NULL DEFAULT 0,
			tokens_out INTEGER NOT NULL DEFAULT 0,
			cost_usd_micro INTEGER NOT NULL DEFAULT 0,
			calls INTEGER NOT NULL DEFAULT 0,
			updated_ms INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS blob_refs(
			digest TEXT PRIMARY KEY,
			refcount INTEGER NOT NULL DEFAULT 0,
			kind TEXT NOT NULL DEFAULT '',
			run_id TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			stored_size INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
	})
}

// upTelemetryV2 adds the Section 22 query tables: one row per task
// run with its exported metrics snapshot, and one row per exported
// span (the GenAI-convention attributes travel as JSON).
func upTelemetryV2(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE IF NOT EXISTS task_metrics(
			task_id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL DEFAULT '',
			snapshot_json TEXT NOT NULL DEFAULT '{}',
			cost_usd REAL NOT NULL DEFAULT 0,
			final_state TEXT NOT NULL DEFAULT '',
			updated_ms INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_metrics_run ON task_metrics(run_id)`,
		`CREATE TABLE IF NOT EXISTS spans(
			trace_id TEXT NOT NULL,
			span_id TEXT NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '',
			run_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT '',
			started_ms INTEGER NOT NULL DEFAULT 0,
			ended_ms INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			attrs_json TEXT NOT NULL DEFAULT '{}',
			PRIMARY KEY(trace_id, span_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_spans_run ON spans(run_id, started_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_spans_trace ON spans(trace_id, started_ms)`,
	})
}
