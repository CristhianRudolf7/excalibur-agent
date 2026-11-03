package migrations_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func TestV12PreservesExistingPrincipalTimezone(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "profile.db")
	old := migrations.New()
	for _, m := range migrations.Main().Ordered() {
		if m.Version < 12 {
			if err := old.Register(m); err != nil {
				t.Fatal(err)
			}
		}
	}
	db, err := store.Open(ctx, path, store.DefaultOptions(), old)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO principals(id,name,timezone,language) VALUES('local','Example','Europe/Madrid','es')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var zone, source, language string
		if err := q.QueryRow(ctx, `SELECT timezone,timezone_source,language FROM principals WHERE id='local'`).Scan(&zone, &source, &language); err != nil {
			return err
		}
		if zone != "Europe/Madrid" || source != "default" || language != "es" {
			t.Fatalf("migration changed profile: %s %s %s", zone, source, language)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
