package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestAssistantCitationsPersistAndPublish(t *testing.T) {
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "on")
	b := citationTestBackend(t)
	ctx := context.Background()
	doc, err := b.Services.Memory.Ingest(ctx, []string{"general"}, "guide.txt", "text/plain", []byte("The robin nests in spring hedges."), memory.IngestOptions{Title: "Field guide"})
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := b.Services.Memory.LibrarySearch(ctx, []string{"general"}, "run_child", "robin nests", 1)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search %v %+v", err, hits)
	}
	ref := "[[doc:" + doc.ID + "#" + hits[0].Chunk.ID + "]]"
	// Run delivery derives the child tree from trusted orchestrator records.
	parent, child, other := types.AgentID("agt_parent"), types.AgentID("agt_child"), types.AgentID("agt_other")
	for _, a := range []types.Agent{{ID: parent}, {ID: child, ParentID: &parent}, {ID: other}} {
		if err := b.Ctrls.Store.SaveAgent(a); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []types.Run{{ID: "run_parent", AgentID: parent}, {ID: "run_child", AgentID: child}, {ID: "run_other", AgentID: other}} {
		if err := b.Ctrls.Store.CreateRun(orchestrator.RunRecord{Run: r}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Services.Memory.RecordSourceEvent(ctx, "run_child", "robin", "https://example.test/robin", "Robin", "hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Services.Memory.RecordSourceEvent(ctx, "run_other", "other", "https://example.test/other", "Other", "hash"); err != nil {
		t.Fatal(err)
	}
	// Master delivery receives the trusted run tree, including child retrieval.
	ev, err := b.SendAssistant(ctx, "cnv_citations", "Robin nests "+ref+". Unknown [[doc:missing#bad]].", "run_parent")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Text != "Robin nests [1]. Unknown. [unsourced]" || len(ev.Sources) != 2 || ev.Sources[0].Title != "Field guide" || ev.Sources[0].MIME != "text/plain" || ev.Sources[0].Excerpt == "" {
		t.Fatalf("answer %+v", ev)
	}
	history, err := b.ConversationHistory(ctx, "cnv_citations", "", 20)
	if err != nil || len(history) != 1 || history[0].Text != ev.Text || len(history[0].Sources) != 2 {
		t.Fatalf("history %+v %v", history, err)
	}
	raw, _ := json.Marshal(history[0])
	if !strings.Contains(string(raw), `"sources":[{"n":1,"kind":"document"`) {
		t.Fatalf("history JSON %s", raw)
	}
	var message api.MessageEvent
	var dropped float64
	for _, event := range b.Bus.Replay(types.Cursor{}) {
		switch event.Type {
		case types.EventMessageCreated:
			if err := json.Unmarshal(event.Payload, &message); err != nil {
				t.Fatal(err)
			}
		case types.EventMetricRecorded:
			var metric api.MetricEvent
			_ = json.Unmarshal(event.Payload, &metric)
			if metric.Name == "citations_dropped" {
				dropped += metric.Value
			}
		}
	}
	if message.Text != ev.Text || len(message.Sources) != 2 || dropped != 1 {
		t.Fatalf("message %+v dropped %v", message, dropped)
	}
	if ev.Sources[1].Kind != "web" || ev.Sources[1].URL != "https://example.test/robin" {
		t.Fatalf("web sources %+v", ev.Sources)
	}
	// Another task's read cannot authorize this claim.
	ev, err = b.SendAssistant(ctx, "cnv_citations", "Other "+ref+".", "run_other")
	if err != nil || len(ev.Sources) != 1 || ev.Sources[0].Kind != "web" || !strings.Contains(ev.Text, "[unsourced]") {
		t.Fatalf("cross-tree %+v %v", ev, err)
	}
}

func TestAssistantCitationsSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "off")
	b := citationTestBackend(t)
	input := "Claim [doc:missing#c1]."
	ev, err := b.SendAssistant(context.Background(), "cnv_off", input, "")
	if err != nil || ev.Text != input || len(ev.Sources) != 0 {
		t.Fatalf("off %+v %v", ev, err)
	}
	for _, e := range b.Bus.Replay(types.Cursor{}) {
		if e.Type == types.EventMetricRecorded {
			t.Fatal("off path recorded drops")
		}
	}
}

// Keep citation tests independent of vault key derivation and unrelated services.
func citationTestBackend(t *testing.T) *DaemonBackend {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "memory.db"), opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	svc := memory.NewService(db, memory.WithEmbedder(memory.NewFakeEmbedder("citation-test", 64)), memory.WithScopes("general"))
	ctrl := &OrchControl{Store: orchestrator.NewMemoryStore()}
	m := master.New(master.Config{}, nil, nil, nil, nil, nil, nil)
	b := NewDaemonBackend(m, ctrl, telemetry.NewBus(nil))
	b.AttachServices(BackendServices{Memory: svc})
	return b
}
