package store

import (
	"context"
	"fmt"
	"strings"
)

// rebuildFTS repopulates every FTS5 index from its content table via
// triggers. It runs after a backup restore ( Indexes are
// reconstructible, Section 4.3) and is safe to call any time.
func rebuildFTS(ctx context.Context, ex Execer) error {
	rows, err := ex.Query(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND sql LIKE '%USING fts5%'")
	if err != nil {
		return err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if !validFTSName(name) {
			continue
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range tables {
		stmt := fmt.Sprintf("INSERT INTO %s(%s) VALUES('rebuild')", quoteIdent(t), quoteIdent(t))
		if _, err := ex.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("store: rebuild %s: %w", t, err)
		}
	}
	return nil
}

// RebuildFTS repopulates every FTS5 index through the porter.
func (db *DB) RebuildFTS(ctx context.Context) error {
	return db.Write(ctx, PriorityBackground, func(ctx context.Context, ex Execer) error {
		return rebuildFTS(ctx, ex)
	})
}

// validFTSName allows only our known index names (never interpolate
// arbitrary sqlite_master rows into SQL).
func validFTSName(name string) bool {
	switch name {
	case "messages_fts", "memories_fts", "doc_chunks_fts",
		"doc_fragments_fts", "file_paths_trg":
		return true
	}
	return false
}

// quoteIdent double-quotes an identifier known to be alnum+underscore.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
