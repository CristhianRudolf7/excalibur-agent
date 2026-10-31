package store_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func mustWriteTele(t *testing.T, tele *store.DB, stmt string, args ...any) {
	t.Helper()
	if err := tele.Write(context.Background(), store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, stmt, args...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// TestRetentionCompactsJournals keeps 30 days of full journals, then
// compacts terminal runs into one summary blob (Section 4.7).
func TestRetentionCompactsJournals(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()

	seedRun := func(id string, status string, daysAgo int, keep bool) {
		spent := "{}"
		if keep {
			spent = `{"keep_full_journal":true}`
		}
		ts := time.Now().AddDate(0, 0, -daysAgo).UnixMilli()
		mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			if _, err := ex.Exec(ctx, `INSERT INTO runs(id,status,spent_json,created_at,updated_at)
				VALUES(?,?,?, ?,?)`, id, status, spent, ts, ts); err != nil {
				return err
			}
			for seq := 0; seq < 3; seq++ {
				if _, err := ex.Exec(ctx, `INSERT INTO journal(run_id,seq,type,payload,status,created_at)
					VALUES(?,?, 'model_response', ?, 'done', ?)`, id, seq, "response text", ts); err != nil {
					return err
				}
			}
			return nil
		})
	}
	seedRun("run-old-done", "completed", 40, false)
	seedRun("run-recent", "completed", 5, false)
	seedRun("run-old-kept", "completed", 40, true)
	seedRun("run-old-active", "executing", 40, false)

	if err := store.ApplyRetention(ctx, db, nil, db.Blob(), store.DefaultRetention()); err != nil {
		t.Fatal(err)
	}

	journalCount := func(run string) int {
		var n int
		if err := db.Read(ctx, func(_ context.Context, q store.Queryer) error {
			return q.QueryRow(ctx, "SELECT COUNT(*) FROM journal WHERE run_id=?", run).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Old terminal run: one marker row pointing at a blob.
	if got := journalCount("run-old-done"); got != 1 {
		t.Fatalf("compacted journal rows = %d, want 1 marker", got)
	}
	var digest string
	if err := db.Read(ctx, func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, "SELECT blob_digest FROM journal WHERE run_id=? AND seq=-1", "run-old-done").Scan(&digest)
	}); err != nil {
		t.Fatal(err)
	}
	rc, err := db.Blob().Open(ctx, digest)
	if err != nil {
		t.Fatalf("compaction blob missing: %v", err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(rc)
	_ = rc.Close()
	if !bytes.Contains(buf.Bytes(), []byte("response text")) {
		t.Fatal("compaction blob lost the trace")
	}
	// Recent, kept-for-eval and non-terminal runs untouched.
	for run, want := range map[string]int{"run-recent": 3, "run-old-kept": 3, "run-old-active": 3} {
		if got := journalCount(run); got != want {
			t.Fatalf("journal %s rows = %d, want %d", run, got, want)
		}
	}
}

// TestRetentionToolBlobs releases tool outputs 30 days after their
// run, unless evidence, documents or file versions keep them
// (Section 4.7).
func TestRetentionToolBlobs(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	blobs := db.Blob()

	plain, err := blobs.Put(ctx, bytes.NewReader([]byte("tool output, forgotten")))
	if err != nil {
		t.Fatal(err)
	}
	evid, err := blobs.Put(ctx, bytes.NewReader([]byte("tool output, evidenced")))
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `INSERT INTO evidence(id,check_id,run_id,result,blob_digest)
			VALUES('evd_1','chk_1','run_1','pass',?)`, evid); err != nil {
			return err
		}
		return nil
	})
	old := time.Now().AddDate(0, 0, -40).UnixMilli()
	for _, d := range []string{plain, evid} {
		mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			_, err := ex.Exec(ctx, "UPDATE blob_refs SET kind='tool_output', created_at=?, refcount=1 WHERE digest=?", old, d)
			return err
		})
	}

	if err := store.ApplyRetention(ctx, db, nil, blobs, store.DefaultRetention()); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.CollectGarbage(ctx, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Open(ctx, plain); err == nil {
		t.Fatal("unreferenced tool blob survived retention+GC")
	}
	rc, err := blobs.Open(ctx, evid)
	if err != nil {
		t.Fatalf("evidenced blob collected: %v", err)
	}
	_ = rc.Close()
}

// TestRetentionTelemetry trims raw events past 90 days and keeps
// daily aggregates (Section 4.7).
func TestRetentionTelemetry(t *testing.T) {
	ctx := context.Background()
	teleOpts := store.TelemetryOptions()
	teleOpts.BlobDir = t.TempDir()
	tele, err := store.Open(ctx, t.TempDir()+"/tele.db", teleOpts, migrations.Telemetry())
	if err != nil {
		t.Fatal(err)
	}
	defer tele.Close()

	old := time.Now().AddDate(0, 0, -100).UnixMilli()
	mustWriteTele(t, tele, `INSERT INTO telemetry_events(run_id,type,created_ms) VALUES('r1','model_call',?)`, old)
	mustWriteTele(t, tele, `INSERT INTO telemetry_events(run_id,type,created_ms) VALUES('r2','model_call',?)`, time.Now().UnixMilli())
	mustWriteTele(t, tele, `INSERT INTO telemetry_metrics_daily(day,tokens_in) VALUES('2026-01-01',100)`)

	db := openSchema(t, nil)
	if err := store.ApplyRetention(ctx, db, tele, db.Blob(), store.DefaultRetention()); err != nil {
		t.Fatal(err)
	}
	var events, agg int
	if err := tele.Read(ctx, func(_ context.Context, q store.Queryer) error {
		if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM telemetry_events").Scan(&events); err != nil {
			return err
		}
		return q.QueryRow(ctx, "SELECT COUNT(*) FROM telemetry_metrics_daily").Scan(&agg)
	}); err != nil {
		t.Fatal(err)
	}
	if events != 1 || agg != 1 {
		t.Fatalf("telemetry rows = %d events %d agg, want 1 and 1", events, agg)
	}
}
