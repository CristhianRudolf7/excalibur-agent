package wireengine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestEmbeddedVisionMemoryConstruction(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "memory.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	media := &tools.MediaDeps{Vision: &tools.MemoryVisionBackend{}}
	emb, err := BuildEmbedded(EmbeddedOptions{Media: media, MemoryStore: db})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	if emb.Library == nil {
		t.Fatal("embedded ingester not constructed")
	}
	if emb.Memory == nil {
		t.Fatal("embedded memory not constructed")
	}
	_, err = emb.Memory.Ingest(ctx, []string{"project:test"}, "page.png", "image/png", []byte("fake"), memory.IngestOptions{Origin: "page.png"})
	if err != nil {
		t.Fatalf("embedded memory vision: %v", err)
	}
	t.Setenv("EXCALIBUR_LIBRARY_INGEST", "0")
	disabled, err := BuildEmbedded(EmbeddedOptions{MemoryStore: db})
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close()
	if disabled.Library != nil {
		t.Fatal("disabled embedded ingestion started a worker")
	}
	// The shared option uses the same tool adapter in daemon and embedded memory.
	opt := VisionMemoryOption(media)
	if opt == nil {
		t.Fatal("missing memory option")
	}
	tool, err := emb.Registry.Get("vision_extract")
	if err != nil {
		t.Fatal(err)
	}
	got, err := (&memory.VisionToolAdapter{Tool: tool}).ExtractText(ctx, "page.png", []int{1})
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}
