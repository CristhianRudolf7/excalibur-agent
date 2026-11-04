package agents

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func registryDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "agents.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
func TestRegistryMigrationKeepsHistoryAndSettingsName(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	legacy := migrations.New()
	for _, m := range migrations.Main().Ordered() {
		if m.Version < 11 {
			if err := legacy.Register(m); err != nil {
				t.Fatal(err)
			}
		}
	}
	db, err := store.Open(ctx, path, store.DefaultOptions(), legacy)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		for _, q := range []string{`INSERT INTO principals(id,name) VALUES('local','User')`, `INSERT INTO conversations(id,principal_id) VALUES('cnv_web_excalibur','local')`, `INSERT INTO messages(id,conversation_id,role,text,sent_at_ms) VALUES('msg_old','cnv_web_excalibur','assistant','Existing answer',1234567890000)`} {
			if _, e := ex.Exec(ctx, q); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r, err := OpenRegistry(ctx, db, "Merlin", nil)
	if err != nil {
		t.Fatal(err)
	}
	a := r.Favorite()
	if a.Name != "Merlin" || a.ConversationID != DefaultConversation || a.Unread != 0 || a.LastPreview != "Existing answer" {
		t.Fatalf("default: %+v", a)
	}
	r2, err := OpenRegistry(ctx, db, "Changed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Favorite().ID != a.ID || r2.Favorite().Name != "Merlin" {
		t.Fatal("reseeded default")
	}
	if err = db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var id, text string
		err := q.QueryRow(ctx, `SELECT id,text FROM messages WHERE conversation_id=?`, a.ConversationID).Scan(&id, &text)
		if id != "msg_old" || text != "Existing answer" {
			t.Fatal("history changed")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func TestRegistryLifecycleAndFavoriteRules(t *testing.T) {
	ctx := context.Background()
	r, err := OpenRegistry(ctx, registryDB(t), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := r.Favorite()
	if first.Name != "Excalibur" {
		t.Fatal(first.Name)
	}
	if _, err = r.Archive(ctx, first.ID); !errors.Is(err, ErrLastActive) {
		t.Fatal(err)
	}
	second, err := r.Create(ctx, CreateParams{Name: "Ada", Personality: "Precise"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := r.Create(ctx, CreateParams{Name: "Grace"})
	if err != nil {
		t.Fatal(err)
	}
	name := "Adelaide"
	empty := ""
	updated, err := r.Update(ctx, UpdateParams{ID: second.ID, Name: &name, Personality: &empty})
	if err != nil || updated.Name != name || updated.Personality != "" {
		t.Fatal(updated, err)
	}
	if err = r.ObserveMessage(ctx, third.ConversationID, "Recent", time.Now(), false); err != nil {
		t.Fatal(err)
	}
	favorite, err := r.Archive(ctx, first.ID)
	if err != nil || favorite != third.ID {
		t.Fatal(favorite, err)
	}
	if len(r.List(false)) != 2 || len(r.List(true)) != 3 {
		t.Fatal("archive filter")
	}
	if err = r.SetFavorite(ctx, first.ID); !errors.Is(err, ErrArchived) {
		t.Fatal(err)
	}
	restored, err := r.Restore(ctx, first.ID)
	if err != nil || restored.Favorite || restored.ConversationID != DefaultConversation {
		t.Fatal(restored, err)
	}
	if err = r.SetFavorite(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if r.Favorite().ID != second.ID {
		t.Fatal("favorite not changed")
	}
	for _, a := range r.List(false) {
		if a.ID != second.ID {
			if _, err = r.Archive(ctx, a.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = r.Archive(ctx, second.ID); !errors.Is(err, ErrLastActive) {
		t.Fatal(err)
	}
}
func TestRegistryFavoriteFallsBackToOldestAndValidation(t *testing.T) {
	ctx := context.Background()
	r, _ := OpenRegistry(ctx, nil, "", nil)
	first := r.Favorite()
	second, _ := r.Create(ctx, CreateParams{Name: "Ada"})
	third, _ := r.Create(ctx, CreateParams{Name: "Grace"})
	// Set distinct fixture times: Windows wall-clock ticks can give sequential
	// creations identical timestamps, where the registry correctly sorts by ID.
	r.mu.Lock()
	second.CreatedAt = time.Unix(1, 0).UTC()
	third.CreatedAt = second.CreatedAt.Add(time.Second)
	r.rows[second.ID], r.rows[third.ID] = second, third
	r.mu.Unlock()
	fav, err := r.Archive(ctx, first.ID)
	if err != nil || fav != second.ID {
		t.Fatal(fav, err)
	}
	for _, p := range []CreateParams{{Name: " "}, {Name: "Bad", Avatar: &AvatarSpec{Variant: "unsupported"}}} {
		if _, err = r.Create(ctx, p); err == nil {
			t.Fatal("accepted invalid agent")
		}
	}
	_, _ = r.Create(ctx, CreateParams{Name: "Ada"})
	if _, err = r.Resolve("ada"); !errors.Is(err, ErrAmbiguous) {
		t.Fatal(err)
	}
	if _, err = r.Resolve(second.ID); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = r.Create(canceled, CreateParams{Name: "Canceled"}); err == nil {
		t.Fatal("canceled write succeeded")
	}
}
func TestRegistryUnreadConcurrentAndPersistent(t *testing.T) {
	ctx := context.Background()
	db := registryDB(t)
	r, _ := OpenRegistry(ctx, db, "", nil)
	a := r.Favorite()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		safego.Go("agents-unread-test", nil, func() {
			defer wg.Done()
			if err := r.ObserveMessage(ctx, a.ConversationID, "An answer", time.Now(), true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	a, _ = r.Get(a.ID)
	if a.Unread != 20 {
		t.Fatal(a.Unread)
	}
	r2, err := OpenRegistry(ctx, db, "", nil)
	if err != nil || r2.Favorite().Unread != 20 {
		t.Fatal(err)
	}
	if err = r2.MarkRead(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if r2.Favorite().Unread != 0 {
		t.Fatal("mark read")
	}
}
