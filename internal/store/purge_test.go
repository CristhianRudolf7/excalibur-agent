package store_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// seedMemory inserts one memory with FTS-visible text; scope defaults
// to general.
func seedMemory(t *testing.T, db *store.DB, id, scope, statement string) {
	t.Helper()
	mustWrite(t, db, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO memories(id,scope_id,kind,statement,status)
			VALUES(?,?,'fact',?,'current')`, id, scope, statement)
		return err
	})
}

func ftsHits(t *testing.T, db *store.DB, table, match string) int {
	t.Helper()
	// Phrase-quote: raw text may contain FTS5 operators (-, *, ...).
	phrase := `"` + strings.ReplaceAll(match, `"`, `""`) + `"`
	var n int
	if err := db.Read(context.Background(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, "SELECT rowid FROM "+table+" WHERE "+table+" MATCH ?", phrase)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			n++
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// searchEverywhere looks for text across content tables, FTS and the
// journal: the purge proof of Section 4.7.
func searchEverywhere(t *testing.T, db *store.DB, needle string) []string {
	t.Helper()
	ctx := context.Background()
	var hits []string
	tables := map[string]string{
		"messages": "text", "memories": "statement", "documents": "origin",
		"doc_chunks": "text", "journal": "payload", "questions": "answer",
		"notifications": "content",
	}
	for table, col := range tables {
		var n int
		if err := db.Read(ctx, func(_ context.Context, q store.Queryer) error {
			return q.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+col+" LIKE ?", "%"+needle+"%").Scan(&n)
		}); err != nil {
			t.Fatal(table, err)
		}
		if n > 0 {
			hits = append(hits, table)
		}
	}
	for _, fts := range []string{"messages_fts", "memories_fts", "doc_chunks_fts", "doc_fragments_fts"} {
		if ftsHits(t, db, fts, needle) > 0 {
			hits = append(hits, fts)
		}
	}
	return hits
}

// TestPurgeMemory removes the row, its FTS entry, its vector and its
// telemetry traces, leaving a tombstone (Section 4.7).
func TestPurgeMemory(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	tele := openTele(t)

	seedMemory(t, db, "mem_1", "general", "purple umbrella secret phrase")
	if got := ftsHits(t, db, "memories_fts", "umbrella"); got != 1 {
		t.Fatalf("fts hits = %d, want 1", got)
	}

	if err := store.PurgeMemory(ctx, db, tele, "mem_1", "user asked to forget"); err != nil {
		t.Fatal(err)
	}
	if hits := searchEverywhere(t, db, "umbrella"); len(hits) != 0 {
		t.Fatalf("purged text survives in %v", hits)
	}
	n, err := store.TombstoneCount(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("tombstones = %d, want 1", n)
	}
	if store.BackupPurgeNotice == "" || !strings.Contains(strings.ToLower(store.BackupPurgeNotice), "backup") {
		t.Fatal("purge must ship a backup-retention notice for the user")
	}
}

// TestPurgeConversation removes messages, staged memories and control
// rows but keeps task shells (Section 4.7).
func TestPurgeConversation(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	tele := openTele(t)

	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `INSERT INTO conversations(id,channel,active_scope) VALUES('cnv_1','cli','general')`); err != nil {
			return err
		}
		_, err := ex.Exec(ctx, `INSERT INTO messages(id,conversation_id,role,text) VALUES('msg_1','cnv_1','user','forget-me-not daisies')`)
		return err
	})
	seedMemory(t, db, "mem_9", "conversation:cnv_1", "daisies are forget-me-not rivals")

	if err := store.PurgeConversation(ctx, db, tele, "cnv_1", "user asked to forget"); err != nil {
		t.Fatal(err)
	}
	if hits := searchEverywhere(t, db, "forget-me-not"); len(hits) != 0 {
		t.Fatalf("purged text survives in %v", hits)
	}
	if hits := searchEverywhere(t, db, "daisies"); len(hits) != 0 {
		t.Fatalf("staged memory survives in %v", hits)
	}
}

// TestPurgeDocument removes metadata, chunks, vectors and the
// original blob reference (Section 4.7, library requirement 38).
func TestPurgeDocument(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	tele := openTele(t)

	digest, err := db.Blob().Put(ctx, bytes.NewReader([]byte("original quarantined document")))
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `INSERT INTO documents(id,scope_id,version,blob_digest,mime,origin)
			VALUES('doc_1','general',1,?, 'text/plain','quarantined-test')`, digest); err != nil {
			return err
		}
		_, err := ex.Exec(ctx, `INSERT INTO doc_chunks(id,doc_id,version,page,text,scope_id)
			VALUES('c1','doc_1',1,3,'quarantined paragraph seven','general')`)
		return err
	})
	if err := db.Blob().AddRef(ctx, digest); err != nil {
		t.Fatal(err)
	}

	if err := store.PurgeDocument(ctx, db, tele, db.Blob(), "doc_1", "user asked to forget"); err != nil {
		t.Fatal(err)
	}
	if hits := searchEverywhere(t, db, "quarantined"); len(hits) != 0 {
		t.Fatalf("purged document survives in %v", hits)
	}
}
