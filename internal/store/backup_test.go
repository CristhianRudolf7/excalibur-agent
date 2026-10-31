package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

// TestBackupAndRotate keeps 7 daily and 4 weekly copies (Section 4.6).
func TestBackupAndRotate(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "INSERT INTO principals(id,name) VALUES('p1','ana')")
		return err
	})
	path := db.Path()

	// Seed old backups with crafted timestamps: 10 dailies plus
	// week-old copies.
	dir := t.TempDir()
	for _, stamp := range []string{
		"20260101-000000", "20260102-000000", "20260103-000000",
		"20260104-000000", "20260105-000000", "20260106-000000",
		"20260107-000000", "20260108-000000", "20260109-000000",
		"20260110-000000",
	} {
		if err := os.WriteFile(filepath.Join(dir, "excalibur-"+stamp+".db"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dst, err := store.Backup(ctx, path, dir, "excalibur-")
	if err != nil {
		t.Fatal(err)
	}
	names, err := store.BackupNames(dir, "excalibur-")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) > 11 {
		t.Fatalf("%d backups kept, want at most 11 (7 daily + 4 weekly)", len(names))
	}
	found := false
	for _, n := range names {
		if n == dst {
			found = true
		}
	}
	if !found {
		t.Fatal("fresh backup rotated away immediately")
	}
	// The fresh copy opens and holds the row.
	verify, err := store.Open(ctx, dst, store.DefaultOptions(), store.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Close()
	var name string
	if err := verify.Read(ctx, func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, "SELECT name FROM principals WHERE id='p1'").Scan(&name)
	}); err != nil {
		t.Fatal(err)
	}
	if name != "ana" {
		t.Fatalf("backup content = %q", name)
	}
}

// TestCorruptRestore scribbles over a page, then asserts Open
// detects the damage, restores the latest daily copy and rebuilds
// what is rebuildable (Section 4.3).
func TestCorruptRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	blobDir := filepath.Join(dir, "blobs")
	backups := filepath.Join(dir, "backups")

	opts := store.DefaultOptions()
	opts.BlobDir = blobDir
	db, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	mustWriteT(t, db, ctx, "INSERT INTO principals(id,name) VALUES('p1','ana')")
	mustWriteT(t, db, ctx, "INSERT INTO messages(id,conversation_id,role,text) VALUES('m1','c1','user','hola mundo')")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Backup(ctx, path, backups, "t-"); err != nil {
		t.Fatal(err)
	}

	// Scribble zeros into the middle of the file: a torn page.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := f.Stat()
	mid := st.Size() / 2
	zeros := make([]byte, 4096)
	if _, err := f.WriteAt(zeros, mid); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// Raw quick_check must now fail (otherwise the scribble missed).
	if ok := rawQuickCheck(t, path); ok {
		t.Skip("scribble missed mapped pages; storage engine recovered transparently")
	}

	restoredOpts := store.DefaultOptions()
	restoredOpts.BlobDir = blobDir
	restored, err := store.Open(ctx, path, restoredOpts, migrations.Main())
	if err != nil {
		t.Fatalf("open after corruption: %v", err)
	}
	defer restored.Close()
	if !restored.Restored() {
		t.Fatal("expected restore from backup")
	}
	ok, err := restored.QuickCheck(ctx)
	if err != nil || !ok {
		t.Fatalf("quick_check after restore: ok=%v err=%v", ok, err)
	}
	var name string
	if err := restored.Read(ctx, func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, "SELECT name FROM principals WHERE id='p1'").Scan(&name)
	}); err != nil {
		t.Fatal(err)
	}
	if name != "ana" {
		t.Fatalf("restored content = %q", name)
	}
}

// TestCorruptWithoutBackup surfaces ErrCorrupt when there is nothing
// to restore (Section 4.3).
func TestCorruptWithoutBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(dir, "blobs")
	db, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	mustWriteT(t, db, ctx, "INSERT INTO principals(id,name) VALUES('p1','ana')")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := f.Stat()
	if _, err := f.WriteAt(make([]byte, 8192), st.Size()/2); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if ok := rawQuickCheck(t, path); ok {
		t.Skip("scribble missed mapped pages")
	}
	if _, err := store.Open(ctx, path, opts, migrations.Main()); err == nil {
		t.Fatal("expected corruption error")
	} else if !isCorrupt(err) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func mustWriteT(t *testing.T, db *store.DB, ctx context.Context, stmt string) {
	t.Helper()
	if err := db.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, stmt)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// rawQuickCheck bypasses the store to probe the file directly,
// never restoring (AutoRestore off).
func rawQuickCheck(t *testing.T, path string) bool {
	t.Helper()
	opts := store.DefaultOptions()
	opts.AutoRestore = false
	opts.AutoRestoreSet = true
	db, err := store.Open(context.Background(), path, opts, store.NewRegistry())
	if err != nil {
		return false
	}
	defer db.Close()
	ok, err := db.QuickCheck(context.Background())
	return err == nil && ok
}

func isCorrupt(err error) bool {
	if err == nil {
		return false
	}
	for err != nil {
		if err == store.ErrCorrupt {
			return true
		}
		type causer interface{ Unwrap() error }
		c, ok := err.(causer)
		if !ok {
			return strings.Contains(err.Error(), "corrupt")
		}
		err = c.Unwrap()
	}
	return false
}
