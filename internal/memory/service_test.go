package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

// openTestDB opens a full-schema database with the memory auxiliary
// tables for tests.
func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"), opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

// testSettings shrinks vectors to 64 dims for speed; the fake
// embedder matches.
func testSettings() Settings {
	s := DefaultSettings()
	s.EmbeddingDimensions = 512
	return s
}

// newTestService returns a service with a deterministic fake
// embedder over an isolated scope set.
func newTestService(t *testing.T, db *store.DB, now time.Time, scopes ...string) *Service {
	t.Helper()
	opts := []Option{
		WithSettings(testSettings()),
		WithClock(func() time.Time { return now }),
		WithEmbedder(NewFakeEmbedder("fake-test", 512)),
	}
	if len(scopes) > 0 {
		opts = append(opts, WithScopes(scopes...))
	}
	return NewService(db, opts...)
}

func writeMessage(t *testing.T, db *store.DB, id, conv, scope, role, text string, at time.Time) {
	t.Helper()
	err := db.Write(context.Background(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO messages(id, conversation_id, role, text, scope_id, sent_at_ms, principal_id) VALUES(?,?,?,?,?,?,?)`,
			id, conv, role, text, scope, at.UnixMilli(), "")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// scriptDecider is a canned ambiguous-band judge for tests.
type scriptDecider struct {
	verdict string
	calls   int
}

func (d *scriptDecider) Classify(_ context.Context, _ Candidate, _ Memory) (string, error) {
	d.calls++
	return d.verdict, nil
}

// scriptExtractor returns canned candidates for review tests.
type scriptExtractor struct {
	cands []Candidate
}

func (x *scriptExtractor) Extract(_ context.Context, _ string, _ []Turn) ([]Candidate, error) {
	return x.cands, nil
}

func mustAdmit(t *testing.T, svc *Service, c Candidate) (Memory, AdmitOutcome) {
	t.Helper()
	outcome, err := svc.Admit(context.Background(), c)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	mem, err := svc.GetMemory(context.Background(), []string{c.Scope}, outcome.MemoryID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	return mem, outcome
}
