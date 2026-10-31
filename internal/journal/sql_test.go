// SQL journal store: ordering, idempotency keys and crash
// reopening over the porter (Sections 17.1, 17.8).
package journal

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func openJournalDB(t *testing.T, path string) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLStoreAppendList(t *testing.T) {
	ctx := context.Background()
	db := openJournalDB(t, filepath.Join(t.TempDir(), "j.db"))
	s := &SQLStore{W: db, R: db}
	rec := &Recorder{Store: s, RunID: "run_sql"}
	if _, err := rec.RunStarted(ctx, map[string]string{"task": "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.ModelRequest(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.ToolIntent(ctx, ToolIntentPayload{CallID: "c1", Tool: "exec", IdempotencyKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List(ctx, "run_sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Seq != 0 || entries[2].Seq != 2 {
		t.Fatalf("entries: %+v", entries)
	}
	if entries[2].IdempotencyKey != "k1" {
		t.Fatalf("key: %q", entries[2].IdempotencyKey)
	}
}

// TestSQLStoreSurvivesReopen simulates kill -9 between appends: the
// porter closes without a checkpoint and the reopened database holds
// every confirmed entry in order.
func TestSQLStoreSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "crash.db")
	db := openJournalDB(t, path)
	s := &SQLStore{W: db, R: db}
	rec := &Recorder{Store: s, RunID: "run_crash"}
	for _, h := range []string{"h1", "h2", "h3"} {
		if _, err := rec.ModelRequest(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openJournalDB(t, path)
	s2 := &SQLStore{W: db2, R: db2}
	entries, err := s2.List(ctx, "run_crash")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("after reopen: %d entries", len(entries))
	}
	rec2 := &Recorder{Store: s2, RunID: "run_crash"}
	if _, err := rec2.ModelResponse(ctx, ModelResponsePayload{Hash: "h3"}); err != nil {
		t.Fatal(err)
	}
	entries, err = s2.List(ctx, "run_crash")
	if err != nil || len(entries) != 4 || entries[3].Seq != 3 {
		t.Fatalf("resume append: %v %+v", err, entries)
	}
}
