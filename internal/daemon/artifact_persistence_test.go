package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestArtifactsSurviveDatabaseRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "artifacts.db")
	db, err := store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	bus := telemetry.NewBus(nil)
	s := &artifactStore{journal: &journal.SQLStore{W: db, R: db}, bus: bus, blobs: gateway.StoreBlobs{Store: db.Blob()}}
	ref := artifactFixture(t, "durable")
	added, err := s.collect(ctx, "conv", "run", "render", []tools.ArtifactRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.journal.List(ctx, "run")
	if err != nil || len(entries) != 1 || entries[0].BlobRef == "" || len(entries[0].Payload) > 2048 {
		t.Fatalf("snapshot was not separated from journal metadata: %+v %v", entries, err)
	}
	turn := &artifactTurn{store: s, conversation: "conv", run: "run"}
	if err := turn.finish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ref.Path); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	newBus := telemetry.NewBus(nil)
	s = &artifactStore{journal: &journal.SQLStore{W: db, R: db}, bus: newBus, blobs: gateway.StoreBlobs{Store: db.Blob()}}
	backend := NewDaemonBackend(nil, nil, newBus)
	backend.artifacts = s
	gate := gateway.New(gateway.DefaultConfig(), backend, nil, newBus)
	res := artifactRPC(t, gate, api.MethodArtifactsList, api.ArtifactsListParams{ConversationID: "conv"})
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	var listed api.ArtifactsListResult
	if err := json.Unmarshal(res.Result, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Artifacts) != 1 || listed.Artifacts[0].ID != added[0].ID {
		t.Fatal(listed)
	}
	res = artifactRPC(t, gate, api.MethodArtifactsGet, api.ArtifactsGetParams{ConversationID: "conv", RunID: "run", ID: added[0].ID})
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	var got api.ArtifactsGetResult
	if err := json.Unmarshal(res.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Content != "<svg>durable</svg>" || got.Source != "graph TD; A-->B" {
		t.Fatal(got)
	}
	turn = &artifactTurn{store: s, conversation: "conv", run: "run"}
	if err := turn.finish(ctx); err != nil {
		t.Fatal(err)
	}
	if newBus.Len() != 0 {
		t.Fatal("restart duplicated ready announcement")
	}
}

func TestArtifactConcurrentCollectionAndFinish(t *testing.T) {
	ctx := context.Background()
	s := &artifactStore{journal: journal.NewMemStore(), bus: telemetry.NewBus(nil)}
	ref := artifactFixture(t, "concurrent")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		collect := func() {
			defer wg.Done()
			if _, err := s.collect(ctx, "conv", "run", "call", []tools.ArtifactRef{ref}); err != nil {
				t.Error(err)
			}
		}
		safego.Go("artifact-test-collect", nil, collect)
	}
	wg.Wait()
	turn := &artifactTurn{store: s, conversation: "conv", run: "run"}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		safego.Go("artifact-test-finish", nil, func() {
			defer wg.Done()
			if err := turn.finish(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	events := s.bus.Replay(types.Cursor{})
	if len(events) != 2 || events[0].Type != api.EventArtifactCreated || events[1].Type != api.EventArtifactReady {
		t.Fatal(events)
	}
}

type failingArtifactBlobs struct {
	gateway.BlobStore
}

func (b failingArtifactBlobs) Put(string, io.Reader) error { return errors.New("blob storage failed") }

func TestArtifactBlobFailures(t *testing.T) {
	ctx := context.Background()
	ref := artifactFixture(t, "unavailable")
	s := &artifactStore{journal: journal.NewMemStore(), bus: telemetry.NewBus(nil), blobs: failingArtifactBlobs{gateway.NewMemoryBlobs()}}
	if _, err := s.collect(ctx, "conv", "run", "call", []tools.ArtifactRef{ref}); err == nil {
		t.Fatal("blob write failure was ignored")
	}
	if s.bus.Len() != 0 {
		t.Fatal("unpersisted artifact was announced")
	}
	records, err := s.records(ctx, "conv", "run")
	if err != nil || len(records) != 0 {
		t.Fatalf("unpersisted artifact in journal: %+v %v", records, err)
	}
	s.blobs = gateway.NewMemoryBlobs()
	added, err := s.collect(ctx, "conv", "run", "call", []tools.ArtifactRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	// A missing snapshot must not cause fetch to read the original path.
	s.blobs = gateway.NewMemoryBlobs()
	b := NewDaemonBackend(nil, nil, nil)
	b.artifacts = s
	gate := gateway.New(gateway.DefaultConfig(), b, nil, nil)
	response := artifactRPC(t, gate, api.MethodArtifactsGet, api.ArtifactsGetParams{ConversationID: "conv", RunID: "run", ID: added[0].ID})
	if response.Error == nil || response.Error.Code != api.CodeInternal || len(response.Result) != 0 {
		t.Fatalf("missing snapshot was fetched: %+v", response)
	}
	response = artifactRPC(t, gate, api.MethodArtifactsGet, api.ArtifactsGetParams{ConversationID: "other-session", RunID: "run", ID: added[0].ID})
	if response.Error == nil || response.Error.Code != api.CodeUnknownTarget {
		t.Fatalf("wrong scope attempted to read snapshot: %+v", response)
	}
}
