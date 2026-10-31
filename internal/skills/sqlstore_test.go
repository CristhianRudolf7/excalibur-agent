// SQL skill index cache: entries, per-model stats, disabled groups
// and versions over a real temporary SQLite database, plus
// crash/restart survival (Sections 12.2, 12.4, 12.8).
package skills

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func openSkillsDB(t *testing.T, path string) *store.DB {
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

func TestSQLSkillCacheRoundTrip(t *testing.T) {
	db := openSkillsDB(t, filepath.Join(t.TempDir(), "skills.db"))
	c := OpenSkillCache(db)

	entry := &Entry{Name: "pdf", Description: "make PDFs", Group: "documents",
		Path: "/x/pdf", Root: RootInstalled, Hash: "abc", Version: "1.2.0",
		Tools: []string{"write"}, Headings: []string{"Usage"}, Body: "# pdf"}
	if err := c.SaveEntry(entry); err != nil {
		t.Fatal(err)
	}
	entries, err := c.LoadEntries()
	if err != nil || len(entries) != 1 || entries[0].Body != "# pdf" || entries[0].Version != "1.2.0" {
		t.Fatalf("entries: %+v %v", entries, err)
	}
	if err := c.DeleteEntry("pdf"); err != nil {
		t.Fatal(err)
	}
	if entries, err := c.LoadEntries(); err != nil || len(entries) != 0 {
		t.Fatalf("entries after delete: %+v %v", entries, err)
	}

	st := modelStats{WithTotal: 4, WithSuccess: 3, WithoutTotal: 2, WithoutSucc: 1}
	if err := c.SaveStats("pdf", "m1", st); err != nil {
		t.Fatal(err)
	}
	stats, err := c.LoadStats()
	if err != nil || stats["pdf"]["m1"].WithSuccess != 3 || stats["pdf"]["m1"].WithoutSucc != 1 {
		t.Fatalf("stats: %+v %v", stats, err)
	}
	if err := c.SetGroupDisabled("m1", "documents", true); err != nil {
		t.Fatal(err)
	}
	dis, err := c.LoadDisabled()
	if err != nil || !dis["m1"]["documents"] {
		t.Fatalf("disabled: %+v %v", dis, err)
	}
	if err := c.SetGroupDisabled("m1", "documents", false); err != nil {
		t.Fatal(err)
	}
	if dis, err := c.LoadDisabled(); err != nil || len(dis["m1"]) != 0 {
		t.Fatalf("disabled after release: %+v %v", dis, err)
	}
	if err := c.RecordVersion("pdf", "1.2.0", "abc", `{"name":"pdf"}`); err != nil {
		t.Fatal(err)
	}
	if v, h, ok := c.Version("pdf"); !ok || v != "1.2.0" || h != "abc" {
		t.Fatalf("version: %q %q %v", v, h, ok)
	}
	if _, _, ok := c.Version("nope"); ok {
		t.Fatal("unexpected version for unknown skill")
	}
}

func TestIndexSaveLoadCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	db := openSkillsDB(t, path)
	c := OpenSkillCache(db)

	ix := NewBuiltinIndex()
	ix.entries["pdf"] = &Entry{Name: "pdf", Group: "documents", Hash: "h1", Body: "b"}
	ix.RecordOutcome("pdf", "m1", true, true)
	if err := c.SetGroupDisabled("m1", "documents", true); err != nil {
		t.Fatal(err)
	}
	if err := ix.SaveCache(c); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openSkillsDB(t, path)
	c2 := OpenSkillCache(db2)
	ix2 := NewBuiltinIndex()
	// The built-in index is empty before Scan; the cache refills it.
	delete(ix2.entries, "pdf")
	if err := ix2.LoadCache(c2); err != nil {
		t.Fatal(err)
	}
	got, ok := ix2.Get("pdf")
	if !ok || got.Hash != "h1" {
		t.Fatalf("cached entry: %+v %v", got, ok)
	}
	if !ix2.SuggestDisabled("m1", "documents") {
		t.Fatal("disabled group lost across restart")
	}
	loaded, err := c2.LoadStats()
	if err != nil || loaded["pdf"]["m1"].WithTotal != 1 {
		t.Fatalf("stats across restart: %+v %v", loaded, err)
	}
}
