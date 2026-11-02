package migrations_test

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"path/filepath"
	"testing"
)

func TestV6PreservesV5DocumentsAndFTSTriggers(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	opts := store.DefaultOptions()
	old := migrations.New()
	for _, m := range migrations.Main().Ordered() {
		if m.Version <= 5 {
			if err := old.Register(m); err != nil {
				t.Fatal(err)
			}
		}
	}
	db, err := store.Open(ctx, path, opts, old)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		if _, e := ex.Exec(ctx, `INSERT INTO documents(id,scope_id) VALUES('d','general')`); e != nil {
			return e
		}
		_, e := ex.Exec(ctx, `INSERT INTO doc_chunks(id,doc_id,text,scope_id) VALUES('c','d','original passage','general')`)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var status, kind, text string
		var uploaded int64
		if e := q.QueryRow(ctx, `SELECT status,uploaded_at FROM documents WHERE id='d'`).Scan(&status, &uploaded); e != nil {
			return e
		}
		if status != "ready" || uploaded == 0 {
			t.Fatalf("old document defaults: %q %d", status, uploaded)
		}
		if e := q.QueryRow(ctx, `SELECT kind,text FROM doc_chunks WHERE id='c'`).Scan(&kind, &text); e != nil {
			return e
		}
		if kind != "child" || text != "original passage" {
			t.Fatalf("old chunk changed: %q %q", kind, text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, e := ex.Exec(ctx, `UPDATE documents SET title='Ornithology',source_name='bird.txt' WHERE id='d'`)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var id string
		return q.QueryRow(ctx, `SELECT id FROM doc_chunks_fts WHERE doc_chunks_fts MATCH 'Ornithology'`).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, e := ex.Exec(ctx, `DELETE FROM doc_chunks WHERE id='c'`)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var n int
		if e := q.QueryRow(ctx, `SELECT count(*) FROM doc_chunks_fts`).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			t.Fatal("delete trigger left FTS entry")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
