package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestLibraryToolsInvokeThroughEmbeddedAndDaemon(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "library.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	mem := memory.NewService(db, memory.WithScopes("general"), memory.WithEmbedder(memory.NewFakeEmbedder("test", 512)))
	doc, _, err := mem.CreatePendingDocument(ctx, "general", memory.DocMeta{Title: "Sample document", SourceName: "sample.txt", BlobDigest: "sample"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err = mem.IngestExtracted(ctx, "general", doc.ID, docextract.Document{Format: "txt", Pages: []docextract.Page{{Number: 1, Method: "native", Quality: 1, Blocks: []docextract.Block{{Kind: "paragraph", Text: "Robins sing in the garden."}}}}})
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := mem.ListChunks(ctx, nil, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{Memory: mem})
	if err != nil {
		t.Fatal(err)
	}
	os := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), os, orchestrator.NoopJournal{})
	daemon, err := Assemble(AssembleOptions{Store: os, Orch: orch, Memory: mem})
	if err != nil {
		t.Fatal(err)
	}
	paths := []struct {
		name string
		reg  *tools.MapRegistry
		cat  *tools.DeferredCatalog
	}{{"embedded", emb.Registry, emb.Catalog}, {"daemon", daemon.Tools, daemon.Catalog}}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			exec := tools.NewExecutor(path.reg)
			tools.WireSearchCall(path.reg, path.cat, exec)
			found, err := exec.Execute(ctx, "search_tools", json.RawMessage(`{"query":"library_list"}`), tools.ExecuteOptions{})
			if err != nil || !strings.Contains(found.Output, "library_list") {
				t.Fatalf("discovery: %+v %v", found, err)
			}
			calls := []struct {
				name string
				args map[string]any
				want string
			}{
				{"library_list", map[string]any{}, doc.ID},
				{"library_status", map[string]any{"document_id": doc.ID}, "ready"},
				{"library_view", map[string]any{"document_id": doc.ID, "pages": "1"}, "Robins"},
				{"library_view", map[string]any{"document_id": doc.ID, "chunk_id": chunks[0].ID}, "Robins"},
				{"library_grep", map[string]any{"pattern": "Robins", "limit": 1}, "Robins"},
				{"library_search", map[string]any{"query": "Robins", "document_ids": []string{doc.ID}, "run_id": "run-tool"}, "[[doc:" + doc.ID + "#"},
			}
			for _, call := range calls {
				raw, _ := json.Marshal(call.args)
				name := call.name
				if name != "library_search" {
					if !path.cat.IsDeferred(name) {
						t.Fatalf("%s not deferred", name)
					}
					raw, _ = json.Marshal(map[string]any{"name": name, "arguments": call.args})
					name = "call_tool"
				}
				res, err := exec.Execute(ctx, name, raw, tools.ExecuteOptions{})
				if err != nil || res.Status != tools.StatusOK || !strings.Contains(res.Output, call.want) || !strings.Contains(res.Output, "untrusted") {
					t.Fatalf("%s: %+v %v", call.name, res, err)
				}
			}
		})
	}
}
