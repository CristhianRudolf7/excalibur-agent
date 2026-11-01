package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// TestProjectRegistrySurvivesRestart registers and archives projects,
// closes the database as a daemon restart does, and checks the reopened
// registry holds the same projects with the same status and roots.
func TestProjectRegistrySurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "excalibur.db")
	root := t.TempDir()

	db, err := store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := OpenProjectRegistry(db)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := reg.Register(workspace.Project{Name: "kept", Alias: "k", Roots: []string{root}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	archived, err := reg.Register(workspace.Project{Name: "old", Roots: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Archive(archived.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	again, err := OpenProjectRegistry(db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.Get(kept.ID)
	if err != nil {
		t.Fatalf("project lost across restart: %v", err)
	}
	if got.Name != "kept" || got.Alias != "k" || len(got.Roots) != 1 {
		t.Fatalf("restored project = %+v", got)
	}
	gone, err := again.Get(archived.ID)
	if err != nil {
		t.Fatalf("archived project lost across restart: %v", err)
	}
	if gone.Status != workspace.ProjectArchived {
		t.Fatalf("archive status not persisted: %q", gone.Status)
	}
	if n := len(again.List()); n != 2 {
		t.Fatalf("restored %d projects, want 2", n)
	}
	// The restored registry keeps writing through: a new project is
	// unique against the restored names.
	if _, err := again.Register(workspace.Project{Name: "KEPT", Roots: []string{t.TempDir()}}); err == nil {
		t.Fatal("restored registry must keep name uniqueness")
	}
}

// TestProjectRegistryWithoutDatabaseIsInMemory keeps the embedded and
// test default: no database, no mirror, no error.
func TestProjectRegistryWithoutDatabaseIsInMemory(t *testing.T) {
	reg, err := OpenProjectRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Register(workspace.Project{Name: "mem", Roots: []string{t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
}
