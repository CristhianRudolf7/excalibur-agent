package store_test

import (
	"context"
	"path/filepath"
	"testing"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

// appendixATables lists every content table of Appendix A that H2
// must create.
var appendixATables = []string{
	"principals", "conversations", "messages", "projects", "tasks",
	"plans", "plan_items", "dependencies", "milestones", "requirements",
	"criteria", "checks", "evidence", "assumptions", "decisions",
	"instructions", "project_log", "agent_profiles", "agents", "runs",
	"notifications", "client_messages", "journal", "agent_messages",
	"leases", "questions", "memories", "entities", "relation_types",
	"edges", "identity_docs", "doc_fragments", "documents", "doc_chunks",
	"source_events", "embedding_spaces", "skills", "skill_versions",
	"tool_packages", "routines", "schedules", "reminders", "triggers",
	"workflows", "workflow_runs", "file_versions", "blob_refs",
	// H2 infrastructure: tombstones (4.7), short_counters (4.4),
	// meta, embedding_vectors (10.8 shadow).
	"tombstones", "short_counters", "meta", "embedding_vectors",
}

func tableExists(t *testing.T, db *store.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.Read(context.Background(), func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(context.Background(),
			"SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table','virtual table') AND name=?", name).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// TestSchemaCompleteness asserts every Appendix A table exists after
// migration, at the latest version.
func TestSchemaCompleteness(t *testing.T) {
	db := openSchema(t, nil)
	for _, table := range appendixATables {
		if !tableExists(t, db, table) {
			t.Errorf("missing table %s", table)
		}
	}
	for _, fts := range []string{"messages_fts", "memories_fts", "doc_chunks_fts", "doc_fragments_fts", "file_paths_trg"} {
		if !tableExists(t, db, fts) {
			t.Errorf("missing FTS table %s", fts)
		}
	}
	var v int
	if err := db.Read(context.Background(), func(ctx context.Context, q store.Queryer) error {
		var err error
		v, err = store.SchemaVersion(ctx, q)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v != migrations.Main().Latest() {
		t.Fatalf("schema version = %d, want %d", v, migrations.Main().Latest())
	}
}

// TestFTSDiacritics asserts unicode61 folding: Spanish queries match
// with or without accents (Section 10.7).
func TestFTSDiacritics(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `INSERT INTO conversations(id,channel) VALUES('c1','cli')`); err != nil {
			return err
		}
		_, err := ex.Exec(ctx, `INSERT INTO messages(id,conversation_id,role,text) VALUES('m1','c1','user','acción rápida del camión')`)
		return err
	})
	if got := ftsHits(t, db, "messages_fts", "accion"); got != 1 {
		t.Fatalf("folded search hits = %d, want 1", got)
	}
	if got := ftsHits(t, db, "messages_fts", "camion"); got != 1 {
		t.Fatalf("folded search hits = %d, want 1", got)
	}
	if got := ftsHits(t, db, "messages_fts", `"rápida del camión"`); got != 1 {
		t.Fatalf("phrase hits = %d, want 1", got)
	}
	// Trigram index covers code and paths.
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO file_versions(id,path) VALUES('f1','internal/store/porter.go')`)
		return err
	})
	if got := ftsHits(t, db, "file_paths_trg", "porter"); got != 1 {
		t.Fatalf("trigram hits = %d, want 1", got)
	}
	// Purge path: deleting the row clears the index.
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `DELETE FROM messages WHERE id='m1'`)
		return err
	})
	if got := ftsHits(t, db, "messages_fts", "accion"); got != 0 {
		t.Fatalf("deleted row still indexed: %d hits", got)
	}
}

// TestVecRoundTrip stores a vector and finds it by KNN on backends
// with the module; elsewhere it asserts the recorded degradation
// (Section 10.8).
func TestVecRoundTrip(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	available, err := db.VecAvailable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.Read(ctx, func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, "SELECT value FROM meta WHERE key='vector_search'").Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if !available {
		if status != "unavailable" {
			t.Fatalf("meta vector_search = %q, want unavailable", status)
		}
		t.Skip("vec0 module missing on this backend (pure-Go fallback)")
	}
	if status != "available" {
		t.Fatalf("meta vector_search = %q, want available", status)
	}
	vec := make([]float32, 512)
	vec[0] = 1
	other := make([]float32, 512)
	other[1] = 1
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		if err := store.PutEmbedding(ctx, ex, "memory", "mem_a", "space_1", "general", 512, vec); err != nil {
			return err
		}
		return store.PutEmbedding(ctx, ex, "memory", "mem_b", "space_1", "general", 512, other)
	})
	ids, _, err := db.KNN(ctx, "memory", vec, 2, []string{"general"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "mem_a" {
		t.Fatalf("knn = %v, want [mem_a mem_b]", ids)
	}
	// Scope isolation at the index: other scopes never match.
	ids, _, err = db.KNN(ctx, "memory", vec, 2, []string{"project:other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("cross-scope knn = %v, want none", ids)
	}
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		return store.RebuildVec(ctx, ex)
	})
	ids, _, err = db.KNN(ctx, "memory", vec, 2, []string{"general"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("knn after rebuild = %v", ids)
	}
}

// TestForwardCompat migrates with N+1 and reopens with the N registry:
// expand-then-contract lets the old binary keep working (4.6, 25.3).
// The N+1 migration only adds a column with a default, so the N
// schema stays readable and writable.
func TestForwardCompat(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(dir, "blobs")

	future := store.NewRegistry()
	for _, m := range migrations.Main().Ordered() {
		if err := future.Register(m); err != nil {
			t.Fatal(err)
		}
	}
	next := migrations.Main().Latest() + 1
	if err := future.Register(store.Migration{Version: next, Name: "test-additive", Up: func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "ALTER TABLE principals ADD COLUMN future_note TEXT NOT NULL DEFAULT ''")
		return err
	}}); err != nil {
		t.Fatal(err)
	}
	migrated, err := store.Open(ctx, path, opts, future)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteT(t, migrated, ctx, "INSERT INTO principals(id,name) VALUES('p1','ana')")
	if err := migrated.Close(); err != nil {
		t.Fatal(err)
	}

	// The N binary opens the N+1 database and works.
	old, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatalf("N binary on N+1 database: %v", err)
	}
	defer old.Close()
	var v int
	if err := old.Read(ctx, func(_ context.Context, q store.Queryer) error {
		var err error
		v, err = store.SchemaVersion(ctx, q)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v != next {
		t.Fatalf("schema version = %d, want %d", v, next)
	}
	mustWriteT(t, old, ctx, "INSERT INTO principals(id,name) VALUES('p2','ben')")
	var n int
	if err := old.Read(ctx, func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, "SELECT COUNT(*) FROM principals").Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("principals = %d, want 2", n)
	}
}

// TestFailingMigrationLeavesDBIntact asserts a bad migration aborts
// the open with ErrMigrationFailed and the file keeps its version.
func TestFailingMigrationLeavesDBIntact(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := dir + "/t.db"

	good := store.NewRegistry()
	_ = good.Register(store.Migration{Version: 1, Name: "v1", Up: func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "CREATE TABLE t1(a TEXT)")
		return err
	}})
	opts := store.DefaultOptions()
	opts.BlobDir = dir + "/blobs"
	first, err := store.Open(ctx, path, opts, good)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	bad := store.NewRegistry()
	_ = bad.Register(store.Migration{Version: 1, Name: "v1", Up: func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "CREATE TABLE t1(a TEXT)")
		return err
	}})
	_ = bad.Register(store.Migration{Version: 2, Name: "broken", Up: func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "CREATE TABLE broken syntax error here")
		return err
	}})
	if _, err := store.Open(ctx, path, opts, bad); err == nil {
		t.Fatal("expected migration failure")
	} else if !isMigrationFailed(err) {
		t.Fatalf("err = %v, want ErrMigrationFailed", err)
	}
	// The file is untouched: v1 opens and works.
	again, err := store.Open(ctx, path, opts, good)
	if err != nil {
		t.Fatalf("db not intact after failed migration: %v", err)
	}
	defer again.Close()
	var v int
	if err := again.Read(ctx, func(_ context.Context, q store.Queryer) error {
		var err error
		v, err = store.SchemaVersion(ctx, q)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("schema version = %d, want 1", v)
	}
}

func isMigrationFailed(err error) bool {
	for err != nil {
		if err == store.ErrMigrationFailed {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
