package migrations

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// upV6 adds extracted document metadata and locations (Section 10.9).
func upV6(ctx context.Context, ex store.Execer) error {
	for table, cols := range map[string][]string{
		"documents":  {"title TEXT NOT NULL DEFAULT ''", "source_name TEXT NOT NULL DEFAULT ''", "format TEXT NOT NULL DEFAULT ''", "size INTEGER NOT NULL DEFAULT 0", "status TEXT NOT NULL DEFAULT 'ready'", "progress_pages INTEGER NOT NULL DEFAULT 0", "total_pages INTEGER NOT NULL DEFAULT 0", "page_count INTEGER NOT NULL DEFAULT 0", "paginated INTEGER NOT NULL DEFAULT 0", "failure_stage TEXT NOT NULL DEFAULT ''", "error TEXT NOT NULL DEFAULT ''", "warnings_json TEXT NOT NULL DEFAULT '[]'", "summary TEXT NOT NULL DEFAULT ''", "content_sha TEXT NOT NULL DEFAULT ''", "uploaded_at INTEGER NOT NULL DEFAULT 0"},
		"doc_chunks": {"kind TEXT NOT NULL DEFAULT 'child'", "anchor TEXT NOT NULL DEFAULT ''", "page_label TEXT NOT NULL DEFAULT ''", "positions_json TEXT NOT NULL DEFAULT '[]'", "chunk_sha TEXT NOT NULL DEFAULT ''", "token_count INTEGER NOT NULL DEFAULT 0"},
	} {
		for _, col := range cols {
			if _, err := ex.Exec(ctx, "ALTER TABLE "+table+" ADD COLUMN "+col); err != nil && !isDuplicateColumn(err) {
				return err
			}
		}
	}
	return execAll(ctx, ex, []string{
		`UPDATE documents SET uploaded_at=created_at WHERE uploaded_at=0`,
		`CREATE INDEX idx_documents_upload ON documents(scope_id,uploaded_at DESC,id)`,
		`CREATE INDEX idx_documents_digest ON documents(scope_id,blob_digest)`,
		`CREATE TABLE doc_pages(doc_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE, version INTEGER NOT NULL, number INTEGER NOT NULL, label TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', width REAL NOT NULL DEFAULT 0, height REAL NOT NULL DEFAULT 0, method TEXT NOT NULL DEFAULT '', quality REAL NOT NULL DEFAULT 0, blocks_json TEXT NOT NULL DEFAULT '[]', warnings_json TEXT NOT NULL DEFAULT '[]', PRIMARY KEY(doc_id,version,number))`,
		`CREATE TRIGGER trg_documents_pages_delete AFTER DELETE ON documents BEGIN DELETE FROM doc_pages WHERE doc_id=OLD.id; END`,
		`DROP TRIGGER trg_chunks_fts_insert`, `DROP TRIGGER trg_chunks_fts_update`,
		`CREATE TRIGGER trg_chunks_fts_insert AFTER INSERT ON doc_chunks BEGIN INSERT INTO doc_chunks_fts(id,text) SELECT NEW.id, NEW.text || ' ' || NEW.section_path || ' ' || title || ' ' || source_name FROM documents WHERE id=NEW.doc_id; END`,
		`CREATE TRIGGER trg_chunks_fts_update AFTER UPDATE OF text,section_path ON doc_chunks BEGIN DELETE FROM doc_chunks_fts WHERE id=OLD.id; INSERT INTO doc_chunks_fts(id,text) SELECT NEW.id, NEW.text || ' ' || NEW.section_path || ' ' || title || ' ' || source_name FROM documents WHERE id=NEW.doc_id; END`,
		`CREATE TRIGGER trg_documents_fts_update AFTER UPDATE OF title,source_name ON documents BEGIN DELETE FROM doc_chunks_fts WHERE id IN (SELECT id FROM doc_chunks WHERE doc_id=NEW.id); INSERT INTO doc_chunks_fts(id,text) SELECT id,text || ' ' || section_path || ' ' || NEW.title || ' ' || NEW.source_name FROM doc_chunks WHERE doc_id=NEW.id; END`,
		`DELETE FROM doc_chunks_fts`,
		`INSERT INTO doc_chunks_fts(id,text) SELECT c.id,c.text || ' ' || c.section_path || ' ' || d.title || ' ' || d.source_name FROM doc_chunks c JOIN documents d ON d.id=c.doc_id`,
	})
}
