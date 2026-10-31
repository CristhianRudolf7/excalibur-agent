package migrations

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// upV2 creates the agent, run, journal and coordination tables
// (Appendix A, agents group).
func upV2(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE IF NOT EXISTS agent_profiles(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT '',
			tools_json TEXT NOT NULL DEFAULT '[]',
			model TEXT NOT NULL DEFAULT '',
			budgets_json TEXT NOT NULL DEFAULT '{}',
			identity_doc_id TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS agents(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			profile_id TEXT NOT NULL DEFAULT '',
			parent_id TEXT NOT NULL DEFAULT '',
			level INTEGER NOT NULL DEFAULT 1,
			scope_id TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS runs(
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL DEFAULT '',
			task_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'created',
			reason TEXT NOT NULL DEFAULT '',
			paused_by TEXT NOT NULL DEFAULT '',
			queue_pos INTEGER NOT NULL DEFAULT 0,
			budget_json TEXT NOT NULL DEFAULT '{}',
			spent_json TEXT NOT NULL DEFAULT '{}',
			heartbeat_ms INTEGER NOT NULL DEFAULT 0,
			last_progress TEXT NOT NULL DEFAULT '',
			instructions_rev_seen INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_task ON runs(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status)`,
		`CREATE TABLE IF NOT EXISTS journal(
			run_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			type TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL DEFAULT '',
			blob_digest TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(run_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_journal_run ON journal(run_id, seq)`,
		`CREATE TABLE IF NOT EXISTS agent_messages(
			id TEXT PRIMARY KEY,
			from_id TEXT NOT NULL DEFAULT '',
			to_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			priority TEXT NOT NULL DEFAULT 'normal',
			delivered INTEGER NOT NULL DEFAULT 0,
			ack_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_to ON agent_messages(to_id, delivered)`,
		`CREATE TABLE IF NOT EXISTS leases(
			path TEXT PRIMARY KEY,
			holder TEXT NOT NULL,
			expires_ms INTEGER NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS questions(
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT '',
			content_json TEXT NOT NULL DEFAULT '{}',
			scope TEXT NOT NULL DEFAULT '',
			blocking INTEGER NOT NULL DEFAULT 0,
			default_value TEXT NOT NULL DEFAULT '',
			deadline_ms INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'open',
			answer TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_questions_run ON questions(run_id, status)`,
		`CREATE TABLE IF NOT EXISTS notifications(
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL DEFAULT '',
			run_id TEXT NOT NULL DEFAULT '',
			class TEXT NOT NULL DEFAULT 'normal',
			content TEXT NOT NULL DEFAULT '',
			channels_json TEXT NOT NULL DEFAULT '[]',
			delivery_status TEXT NOT NULL DEFAULT 'pending',
			ack_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT ''
		)`,
	})
}
