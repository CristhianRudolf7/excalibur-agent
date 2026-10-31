package memory_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

// Suite helpers for the Section 23.5 memory suites (mem-scope,
// mem-time, mem-conflict, mem-cite). Each suite plants deterministic
// fixtures and asserts backend behavior; embeddings use the fake so
// the suites run identically on both SQLite backends.

func openSuiteDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "suite.db"), opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

func suiteSettings() memory.Settings {
	s := memory.DefaultSettings()
	s.EmbeddingDimensions = 512
	return s
}

func newSuiteService(t *testing.T, db *store.DB, now time.Time, scopes ...string) *memory.Service {
	t.Helper()
	opts := []memory.Option{
		memory.WithSettings(suiteSettings()),
		memory.WithClock(func() time.Time { return now }),
		memory.WithEmbedder(memory.NewFakeEmbedder("suite-fake", 512)),
	}
	if len(scopes) > 0 {
		opts = append(opts, memory.WithScopes(scopes...))
	}
	return memory.NewService(db, opts...)
}

func suiteAdmit(t *testing.T, svc *memory.Service, c memory.Candidate) memory.Memory {
	t.Helper()
	outcome, err := svc.Admit(context.Background(), c)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	m, err := svc.GetMemory(context.Background(), []string{c.Scope}, outcome.MemoryID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	return m
}
