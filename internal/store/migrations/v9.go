package migrations

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func upV9(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE automations (id TEXT PRIMARY KEY, status TEXT NOT NULL, paused_reason TEXT NOT NULL, consecutive_failures INTEGER NOT NULL, next_run_at TEXT, data_json TEXT NOT NULL)`,
		`CREATE TABLE automation_versions (automation_id TEXT NOT NULL REFERENCES automations(id) ON DELETE CASCADE, version INTEGER NOT NULL, note TEXT NOT NULL, created_at TEXT NOT NULL, spec_json TEXT NOT NULL, PRIMARY KEY(automation_id,version))`,
		`CREATE TABLE automation_runs (id TEXT PRIMARY KEY, automation_id TEXT NOT NULL REFERENCES automations(id) ON DELETE CASCADE, started_at TEXT NOT NULL, steps_json TEXT NOT NULL, data_json TEXT NOT NULL)`,
		`CREATE INDEX idx_automation_runs ON automation_runs(automation_id,started_at DESC)`,
	})
}
