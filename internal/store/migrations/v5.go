package migrations

import (
	"context"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// upV5 adds the wiring tables: every row the SQL repositories behind
// the store porter need that v1-v4 did not create, plus the columns
// the repository structs require on shared tables (Section 4.6:
// expand-then-contract; only additive changes, so the previous
// binary keeps reading the migrated database).
//
// New tables:
//   - task_requirements: task -> requirement edges (Section 8.2).
//   - task_history: previous incarnations of reopened tasks (8.5).
//   - plan_versions: superseded plan versions stay queryable (8.4,
//     8.5); the plans table keeps the latest version only.
//   - instruction_revs: per-scope instruction revision for evidence
//     freshness (Section 8.3).
//   - file_fingerprints: path fingerprints for impact-area freshness
//     (Section 8.3).
//   - criterion_versions: optimistic-concurrency versions for
//     criteria, whose shared struct carries no version column (8.10).
//   - run_reports: orchestrator delivery reports (Sections 7.6, 15.6).
//   - run_usage: accumulated per-run spend (Sections 5.2, 15.9).
//   - agenda_fires: delivered fire ids and pending fires across
//     restarts (Sections 17.3, 17.4).
//   - trigger_events: (rule, event) dedup marks and firing times for
//     rate limiting (Section 21.3).
//   - workflow_versions: versioned workflow definitions; instances
//     pin one version (Section 21.4).
//   - sessions: long-initiative work sessions (Section 17.6).
//   - worktrees: worktrees created outside the project root (16.4).
//   - integration_queue: persisted child-branch queue (Section 16.4).
//   - project_modes: per-project work-mode overrides (Section 16.1).
//   - skill_stats: per-model outcomes with and without a skill (12.4).
//   - skill_disabled: groups disabled per model (Section 12.4).
//
// New columns (all with defaults, so old rows stay valid):
//   - leases.activity, criteria.status,
//     evidence.instruction_rev_seen, runs.lane, runs.wait_reason,
//     runs.pause_reason, runs.block_reason, agent_messages.ack_note,
//     projects.agents_json, triggers.enabled.
func upV5(ctx context.Context, ex store.Execer) error {
	if err := execAll(ctx, ex, []string{
		`CREATE TABLE IF NOT EXISTS task_requirements(
			task_id TEXT NOT NULL,
			requirement_id TEXT NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(task_id, requirement_id)
		)`,
		`CREATE TABLE IF NOT EXISTS task_history(
			id TEXT NOT NULL,
			incarnation INTEGER NOT NULL,
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
			principal_id TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(id, incarnation)
		)`,
		`CREATE TABLE IF NOT EXISTS plan_versions(
			plan_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			scope_id TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT '',
			task_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'open',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			principal_id TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(plan_id, version)
		)`,
		`CREATE TABLE IF NOT EXISTS instruction_revs(
			scope_id TEXT PRIMARY KEY,
			rev INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS file_fingerprints(
			path TEXT PRIMARY KEY,
			fingerprint TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS criterion_versions(
			criterion_id TEXT PRIMARY KEY,
			version INTEGER NOT NULL DEFAULT 1,
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS run_reports(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			agent_id TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			deliverables_json TEXT NOT NULL DEFAULT '[]',
			evidence_json TEXT NOT NULL DEFAULT '[]',
			files_json TEXT NOT NULL DEFAULT '[]',
			assumptions_json TEXT NOT NULL DEFAULT '[]',
			queries_json TEXT NOT NULL DEFAULT '[]',
			pending_json TEXT NOT NULL DEFAULT '[]',
			usage_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_run_reports_run ON run_reports(run_id, id)`,
		`CREATE TABLE IF NOT EXISTS run_usage(
			run_id TEXT PRIMARY KEY,
			steps INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0,
			elapsed_ms INTEGER NOT NULL DEFAULT 0,
			tool_calls INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS agenda_fires(
			fire_id TEXT PRIMARY KEY,
			schedule_id TEXT NOT NULL DEFAULT '',
			scheduled_ms INTEGER NOT NULL DEFAULT 0,
			fired_ms INTEGER NOT NULL DEFAULT 0,
			late INTEGER NOT NULL DEFAULT 0,
			payload TEXT NOT NULL DEFAULT '',
			scope_id TEXT NOT NULL DEFAULT '',
			delivered INTEGER NOT NULL DEFAULT 0,
			pending INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agenda_fires_pending ON agenda_fires(pending, scheduled_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_agenda_fires_schedule ON agenda_fires(schedule_id)`,
		`CREATE TABLE IF NOT EXISTS trigger_events(
			rule_id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			fired_ms INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(rule_id, event_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trigger_events_rule ON trigger_events(rule_id, fired_ms)`,
		`CREATE TABLE IF NOT EXISTS workflow_versions(
			workflow_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			definition_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(workflow_id, version)
		)`,
		`CREATE TABLE IF NOT EXISTS sessions(
			id TEXT PRIMARY KEY,
			initiative TEXT NOT NULL DEFAULT '',
			milestone TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'scheduled',
			budget_usd REAL NOT NULL DEFAULT 0,
			window_start_ms INTEGER NOT NULL DEFAULT 0,
			startup_json TEXT NOT NULL DEFAULT '[]',
			handoff TEXT NOT NULL DEFAULT '',
			heartbeat_ms INTEGER NOT NULL DEFAULT 0,
			long_tool TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_initiative ON sessions(initiative, state)`,
		`CREATE TABLE IF NOT EXISTS worktrees(
			project_id TEXT NOT NULL,
			child_id TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			branch TEXT NOT NULL DEFAULT '',
			base_commit TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(project_id, child_id)
		)`,
		`CREATE TABLE IF NOT EXISTS integration_queue(
			queue_id TEXT NOT NULL,
			child_id TEXT NOT NULL,
			branch TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'queued',
			enqueued_ms INTEGER NOT NULL DEFAULT 0,
			report TEXT NOT NULL DEFAULT '',
			attempted INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(queue_id, child_id)
		)`,
		`CREATE TABLE IF NOT EXISTS project_modes(
			project_id TEXT PRIMARY KEY,
			mode TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS skill_stats(
			skill_id TEXT NOT NULL,
			model TEXT NOT NULL,
			with_total INTEGER NOT NULL DEFAULT 0,
			with_success INTEGER NOT NULL DEFAULT 0,
			without_total INTEGER NOT NULL DEFAULT 0,
			without_success INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(skill_id, model)
		)`,
		`CREATE TABLE IF NOT EXISTS skill_disabled(
			model TEXT NOT NULL,
			skill_group TEXT NOT NULL,
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(model, skill_group)
		)`,
	}); err != nil {
		return err
	}
	// Additive columns on shared tables. Columns may already exist
	// when the migration re-runs over a partially migrated file, so
	// duplicate-column errors are tolerated (expand-then-contract,
	// Section 4.6).
	adds := [][2]string{
		{"leases", "activity TEXT NOT NULL DEFAULT ''"},
		{"criteria", "status TEXT NOT NULL DEFAULT ''"},
		{"evidence", "instruction_rev_seen INTEGER NOT NULL DEFAULT 0"},
		{"runs", "lane TEXT NOT NULL DEFAULT 'P1'"},
		{"runs", "wait_reason TEXT NOT NULL DEFAULT ''"},
		{"runs", "pause_reason TEXT NOT NULL DEFAULT ''"},
		{"runs", "block_reason TEXT NOT NULL DEFAULT ''"},
		{"agent_messages", "ack_note TEXT NOT NULL DEFAULT ''"},
		{"projects", "agents_json TEXT NOT NULL DEFAULT '[]'"},
		{"triggers", "enabled INTEGER NOT NULL DEFAULT 1"},
	}
	for _, add := range adds {
		if _, err := ex.Exec(ctx, "ALTER TABLE "+add[0]+" ADD COLUMN "+add[1]); err != nil {
			if !isDuplicateColumn(err) {
				return err
			}
		}
	}
	return nil
}

// isDuplicateColumn reports the tolerant ALTER TABLE outcome: the
// column already exists, so the migration is already applied.
func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate column name")
}
