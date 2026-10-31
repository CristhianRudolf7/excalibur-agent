package memory

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestReindexDualRead covers the Section 10.8 model change: the new
// space backfills in the background while both spaces answer; the
// old space retires only after full coverage.
func TestReindexDualRead(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Reindexed memory about harbors"})
	if _, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "", "nothing", 1); err != nil {
		t.Fatal(err)
	}

	if err := svc.StartReindex(context.Background(), NewFakeEmbedder("fake-v2", 512)); err != nil {
		t.Fatal(err)
	}
	// Old space alone has no v2 vectors, yet retrieval still answers
	// through the retiring space.
	ret, err := svc.Retrieve(context.Background(), Query{Text: "reindexed harbors", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) == 0 {
		t.Fatal("dual read answered nothing during reindex")
	}
	mem, chk, err := svc.ReindexBatch(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if mem < 1 {
		t.Fatalf("reindexed memories = %d", mem)
	}
	left, _, err := svc.ReindexCoverage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("coverage left = %d", left)
	}
	_ = chk
	if err := svc.FinishReindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	cur, prev, _ := svc.embedSnapshot()
	if cur == nil || prev != nil {
		t.Fatal("retiring embedder should clear after finish")
	}
	ret, err = svc.Retrieve(context.Background(), Query{Text: "reindexed harbors", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) == 0 {
		t.Fatal("post-reindex retrieval empty")
	}
}

// TestReindexIdempotentSpaces asserts the space registry holds one
// row per model with lifecycle statuses.
func TestReindexIdempotentSpaces(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Space registry probe"})
	if err := svc.StartReindex(context.Background(), NewFakeEmbedder("fake-v2", 512)); err != nil {
		t.Fatal(err)
	}
	rels, err := svc.ListRelationTypes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = rels
	var active, reindexing int
	err = db.Read(context.Background(), func(_ context.Context, q store.Queryer) error {
		if err := q.QueryRow(context.Background(), `SELECT COUNT(*) FROM embedding_spaces WHERE status='active'`).Scan(&active); err != nil {
			return err
		}
		return q.QueryRow(context.Background(), `SELECT COUNT(*) FROM embedding_spaces WHERE status='reindexing'`).Scan(&reindexing)
	})
	if err != nil {
		t.Fatal(err)
	}
	if active != 1 || reindexing != 1 {
		t.Fatalf("spaces active=%d reindexing=%d, want 1 and 1", active, reindexing)
	}
}
