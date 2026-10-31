package migrations

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// upV1 creates conversation and work tables plus the storage
// infrastructure rows (Appendix A; tombstones for Section 4.7,
// short_counters for Section 4.4, blob_refs and meta for
// Sections 4.2-4.3).
func upV1(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE IF NOT EXISTS meta(
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tombstones(
			id TEXT PRIMARY KEY,
			scope_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			deleted_at_ms INTEGER NOT NULL,
			reason TEXT NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tombstones_scope ON tombstones(scope_id)`,
		`CREATE TABLE IF NOT EXISTS short_counters(
			scope_id TEXT NOT NULL,
			series TEXT NOT NULL,
			next_n INTEGER NOT NULL,
			PRIMARY KEY(scope_id, series)
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
		`CREATE INDEX IF NOT EXISTS idx_blob_refs_kind ON blob_refs(kind, created_at)`,
		`CREATE TABLE IF NOT EXISTS principals(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			timezone TEXT NOT NULL DEFAULT 'UTC',
			language TEXT NOT NULL DEFAULT 'en',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS conversations(
			id TEXT PRIMARY KEY,
			principal_id TEXT NOT NULL DEFAULT '',
			channel TEXT NOT NULL DEFAULT '',
			active_scope TEXT NOT NULL DEFAULT 'general',
			persona TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'open',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT 'general',
			FOREIGN KEY(principal_id) REFERENCES principals(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_scope ON conversations(scope_id)`,
		`CREATE TABLE IF NOT EXISTS messages(
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			role TEXT NOT NULL,
			text TEXT NOT NULL DEFAULT '',
			attachments_json TEXT NOT NULL DEFAULT '[]',
			language TEXT NOT NULL DEFAULT '',
			reply_to TEXT NOT NULL DEFAULT '',
			client_msg_id TEXT NOT NULL DEFAULT '',
			sent_at_ms INTEGER NOT NULL DEFAULT 0,
			user_tz TEXT NOT NULL DEFAULT 'UTC',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT 'general',
			principal_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(conversation_id) REFERENCES conversations(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, sent_at_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_scope ON messages(scope_id)`,
		`CREATE TABLE IF NOT EXISTS client_messages(
			client_msg_id TEXT NOT NULL,
			client_id TEXT NOT NULL DEFAULT '',
			msg_id TEXT NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(client_msg_id, client_id)
		)`,
		`CREATE TABLE IF NOT EXISTS projects(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			alias TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			roots_json TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'active',
			vcs TEXT NOT NULL DEFAULT '',
			settings_json TEXT NOT NULL DEFAULT '{}',
			skill_groups_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT 'general',
			principal_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS plans(
			id TEXT PRIMARY KEY,
			scope_id TEXT NOT NULL,
			type TEXT NOT NULL,
			task_id TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL DEFAULT 'open',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			principal_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plans_scope ON plans(scope_id)`,
		`CREATE TABLE IF NOT EXISTS tasks(
			id TEXT PRIMARY KEY,
			short_code TEXT NOT NULL DEFAULT '',
			scope_id TEXT NOT NULL,
			level TEXT NOT NULL DEFAULT 'N1',
			request_json TEXT NOT NULL DEFAULT '{}',
			origin_conversation_id TEXT NOT NULL DEFAULT '',
			notice_policy TEXT NOT NULL DEFAULT '',
			root_run_id TEXT NOT NULL DEFAULT '',
			budget_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT 'open',
			version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			principal_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_scope ON tasks(scope_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_short ON tasks(short_code)`,
		`CREATE TABLE IF NOT EXISTS plan_items(
			id TEXT PRIMARY KEY,
			short_code TEXT NOT NULL DEFAULT '',
			plan_id TEXT NOT NULL,
			task_id TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			priority TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			estimate_json TEXT NOT NULL DEFAULT '{}',
			version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(plan_id) REFERENCES plans(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_items_plan ON plan_items(plan_id)`,
		`CREATE TABLE IF NOT EXISTS dependencies(
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			PRIMARY KEY(from_id, to_id)
		)`,
		`CREATE TABLE IF NOT EXISTS milestones(
			id TEXT PRIMARY KEY,
			plan_id TEXT NOT NULL,
			short_code TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			criteria_json TEXT NOT NULL DEFAULT '[]',
			target_date_ms INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'open',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(plan_id) REFERENCES plans(id)
		)`,
		`CREATE TABLE IF NOT EXISTS requirements(
			id TEXT PRIMARY KEY,
			short_code TEXT NOT NULL DEFAULT '',
			scope_id TEXT NOT NULL,
			text TEXT NOT NULL DEFAULT '',
			origin_msg_id TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'functional',
			priority TEXT NOT NULL DEFAULT 'should',
			status TEXT NOT NULL DEFAULT 'proposed',
			version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			principal_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_requirements_scope ON requirements(scope_id)`,
		`CREATE TABLE IF NOT EXISTS criteria(
			id TEXT PRIMARY KEY,
			short_code TEXT NOT NULL DEFAULT '',
			requirement_id TEXT NOT NULL,
			statement TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT 'inspection',
			expected TEXT NOT NULL DEFAULT '',
			impact_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(requirement_id) REFERENCES requirements(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_criteria_requirement ON criteria(requirement_id)`,
		`CREATE TABLE IF NOT EXISTS checks(
			id TEXT PRIMARY KEY,
			criterion_id TEXT NOT NULL,
			definition_json TEXT NOT NULL DEFAULT '{}',
			environment TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(criterion_id) REFERENCES criteria(id)
		)`,
		`CREATE TABLE IF NOT EXISTS evidence(
			id TEXT PRIMARY KEY,
			check_id TEXT NOT NULL,
			run_id TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL,
			blob_digest TEXT NOT NULL DEFAULT '',
			code_version_json TEXT NOT NULL DEFAULT '{}',
			fingerprints_json TEXT NOT NULL DEFAULT '{}',
			tool_call_id TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_evidence_check ON evidence(check_id)`,
		`CREATE TABLE IF NOT EXISTS assumptions(
			id TEXT PRIMARY KEY,
			text TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			linked_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS decisions(
			id TEXT PRIMARY KEY,
			statement TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			alternatives_json TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'active',
			superseded_by TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS instructions(
			id TEXT PRIMARY KEY,
			scope_id TEXT NOT NULL,
			text TEXT NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1,
			origin_msg_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_instructions_scope ON instructions(scope_id, status)`,
		`CREATE TABLE IF NOT EXISTS project_log(
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL DEFAULT '',
			refs_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(project_id) REFERENCES projects(id)
		)`,
	})
}
