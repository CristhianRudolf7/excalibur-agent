package migrations

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// upV3 creates the memory, identity, document and skill tables with
// FTS5 indexes (Appendix A, memory group). Two tokenizers cover the
// Section 10.7 retrieval paths: unicode61 with diacritic folding for
// natural text (Spanish-friendly) and trigram for code, paths and
// identifiers.
func upV3(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE IF NOT EXISTS memories(
			id TEXT PRIMARY KEY,
			scope_id TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'fact',
			statement TEXT NOT NULL DEFAULT '',
			entities_json TEXT NOT NULL DEFAULT '[]',
			valid_from_ms INTEGER NOT NULL DEFAULT 0,
			valid_to_ms INTEGER NOT NULL DEFAULT 0,
			recorded_ms INTEGER NOT NULL DEFAULT 0,
			expires_at_ms INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'candidate',
			superseded_by TEXT NOT NULL DEFAULT '',
			confidence REAL NOT NULL DEFAULT 0,
			importance REAL NOT NULL DEFAULT 0,
			sources_json TEXT NOT NULL DEFAULT '[]',
			trust TEXT NOT NULL DEFAULT 'agent',
			last_used_ms INTEGER NOT NULL DEFAULT 0,
			use_count INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			principal_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope_id, status)`,
		`CREATE TABLE IF NOT EXISTS entities(
			id TEXT PRIMARY KEY,
			scope_id TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT '',
			canonical TEXT NOT NULL DEFAULT '',
			aliases_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_scope ON entities(scope_id)`,
		`CREATE TABLE IF NOT EXISTS relation_types(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			domain TEXT NOT NULL DEFAULT '',
			range TEXT NOT NULL DEFAULT '',
			inverse TEXT NOT NULL DEFAULT '',
			origin TEXT NOT NULL DEFAULT 'seed',
			use_count INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT '',
			principal_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS edges(
			id TEXT PRIMARY KEY,
			layer TEXT NOT NULL,
			from_type TEXT NOT NULL DEFAULT '',
			from_id TEXT NOT NULL DEFAULT '',
			rel TEXT NOT NULL DEFAULT '',
			to_type TEXT NOT NULL DEFAULT '',
			to_id TEXT NOT NULL DEFAULT '',
			scope_id TEXT NOT NULL DEFAULT '',
			provenance_json TEXT NOT NULL DEFAULT '{}',
			valid_from_ms INTEGER NOT NULL DEFAULT 0,
			valid_to_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(layer, from_type, from_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(layer, to_type, to_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_scope ON edges(scope_id)`,
		`CREATE TABLE IF NOT EXISTS identity_docs(
			id TEXT PRIMARY KEY,
			scope_id TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'profile',
			title TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS doc_fragments(
			id TEXT PRIMARY KEY,
			doc_id TEXT NOT NULL,
			heading_path TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL DEFAULT 0,
			text TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL DEFAULT 1,
			hash TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			FOREIGN KEY(doc_id) REFERENCES identity_docs(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_fragments_doc ON doc_fragments(doc_id, position)`,
		`CREATE TABLE IF NOT EXISTS documents(
			id TEXT PRIMARY KEY,
			scope_id TEXT NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			blob_digest TEXT NOT NULL DEFAULT '',
			mime TEXT NOT NULL DEFAULT '',
			origin TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS doc_chunks(
			id TEXT PRIMARY KEY,
			doc_id TEXT NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			page INTEGER NOT NULL DEFAULT 0,
			paragraph INTEGER NOT NULL DEFAULT 0,
			char_start INTEGER NOT NULL DEFAULT 0,
			char_end INTEGER NOT NULL DEFAULT 0,
			section_path TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(doc_id) REFERENCES documents(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_chunks_doc ON doc_chunks(doc_id, version)`,
		`CREATE TABLE IF NOT EXISTS source_events(
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL DEFAULT '',
			query TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			fetched_ms INTEGER NOT NULL DEFAULT 0,
			content_hash TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS embedding_spaces(
			id TEXT PRIMARY KEY,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL DEFAULT 512,
			quantization TEXT NOT NULL DEFAULT 'int8',
			status TEXT NOT NULL DEFAULT 'active',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS skills(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			index_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			scope_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS skill_versions(
			id TEXT PRIMARY KEY,
			skill_id TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			hash TEXT NOT NULL DEFAULT '',
			manifest_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS tool_packages(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			manifest_json TEXT NOT NULL DEFAULT '{}',
			impl_json TEXT NOT NULL DEFAULT '{}',
			tests_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS routines(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			definition_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		// FTS5: natural-text indexes with diacritic folding.
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			id UNINDEXED, text, tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_messages_fts_insert AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(id, text) VALUES(NEW.id, NEW.text);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_messages_fts_delete AFTER DELETE ON messages BEGIN
			DELETE FROM messages_fts WHERE id=OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_messages_fts_update AFTER UPDATE OF text ON messages BEGIN
			DELETE FROM messages_fts WHERE id=OLD.id;
			INSERT INTO messages_fts(id, text) VALUES(NEW.id, NEW.text);
		END`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
			id UNINDEXED, text, tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_memories_fts_insert AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(id, text) VALUES(NEW.id, NEW.statement);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_memories_fts_delete AFTER DELETE ON memories BEGIN
			DELETE FROM memories_fts WHERE id=OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_memories_fts_update AFTER UPDATE OF statement ON memories BEGIN
			DELETE FROM memories_fts WHERE id=OLD.id;
			INSERT INTO memories_fts(id, text) VALUES(NEW.id, NEW.statement);
		END`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS doc_chunks_fts USING fts5(
			id UNINDEXED, text, tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_chunks_fts_insert AFTER INSERT ON doc_chunks BEGIN
			INSERT INTO doc_chunks_fts(id, text) VALUES(NEW.id, NEW.text);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_chunks_fts_delete AFTER DELETE ON doc_chunks BEGIN
			DELETE FROM doc_chunks_fts WHERE id=OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_chunks_fts_update AFTER UPDATE OF text ON doc_chunks BEGIN
			DELETE FROM doc_chunks_fts WHERE id=OLD.id;
			INSERT INTO doc_chunks_fts(id, text) VALUES(NEW.id, NEW.text);
		END`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS doc_fragments_fts USING fts5(
			id UNINDEXED, text, tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_fragments_fts_insert AFTER INSERT ON doc_fragments BEGIN
			INSERT INTO doc_fragments_fts(id, text) VALUES(NEW.id, NEW.text);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_fragments_fts_delete AFTER DELETE ON doc_fragments BEGIN
			DELETE FROM doc_fragments_fts WHERE id=OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_fragments_fts_update AFTER UPDATE OF text ON doc_fragments BEGIN
			DELETE FROM doc_fragments_fts WHERE id=OLD.id;
			INSERT INTO doc_fragments_fts(id, text) VALUES(NEW.id, NEW.text);
		END`,
	})
}
