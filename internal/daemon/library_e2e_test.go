package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf/testpdf"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/library"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// Exercise the production isolated PDF worker with generated local fixtures.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "internal" && os.Args[2] == "pdf-worker" {
		if err := pdf.ServeWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type libraryE2E struct {
	db       *store.DB
	mem      *memory.Service
	services *Services
	bus      *telemetry.Bus
	gate     *gateway.Server
}

func newLibraryE2E(t *testing.T, media *tools.MediaDeps, start bool) *libraryE2E {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "library.db"), opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	mem := memory.NewService(db, memory.WithScopes("general"))
	bus := telemetry.NewBus(nil)
	svc := &Services{}
	if err = svc.ConfigureLibrary(LibraryOptions{DB: db, Memory: mem, Media: media, Bus: bus}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Library.Close() })
	if start {
		if err = svc.Library.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}
	backend := NewDaemonBackend(nil, nil, bus)
	backend.AttachServices(BackendServices{Memory: mem, Library: LibraryRoutesFor(mem, svc.Library, []string{"general"})})
	cfg := gateway.DefaultConfig()
	cfg.AllowRemote = true
	cfg.DaemonBlobs = gateway.StoreBlobs{Store: db.Blob()}
	return &libraryE2E{db, mem, svc, bus, gateway.New(cfg, backend, gateway.NewMemoryBlobs(), bus)}
}
func (f *libraryE2E) rpc(t *testing.T, method string, params, result any) {
	t.Helper()
	req, err := api.NewRequest("library-e2e", method, params)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	f.gate.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(raw)))
	var response api.Response
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("%s: %v", method, response.Error)
	}
	if err = json.Unmarshal(response.Result, result); err != nil {
		t.Fatal(err)
	}
}
func (f *libraryE2E) upload(t *testing.T, name, mime string, data []byte) api.DocumentInfo {
	t.Helper()
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	w := httptest.NewRecorder()
	f.gate.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/blobs/"+digest, bytes.NewReader(data)))
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	var result api.LibraryIngestResult
	f.rpc(t, api.MethodLibraryIngest, api.LibraryIngestParams{BlobDigest: digest, Name: name, MIME: mime}, &result)
	return result.Document
}
func (f *libraryE2E) wait(t *testing.T, id, status string) api.DocumentInfo {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for {
		var result api.LibraryDocumentResult
		f.rpc(t, api.MethodLibraryGet, api.LibraryIDParams{ID: id}, &result)
		if result.Document.Status == status {
			return result.Document
		}
		if result.Document.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %+v", status, result.Document)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func (f *libraryE2E) pages(t *testing.T, id string) []docextract.Page {
	t.Helper()
	var result api.LibraryPagesResult
	f.rpc(t, api.MethodLibraryPages, api.LibraryPagesParams{ID: id, From: 1, To: 1}, &result)
	return result.Pages
}
func TestLibraryE2EPDFAndCitations(t *testing.T) {
	f := newLibraryE2E(t, nil, true)
	data := testpdf.Build([]testpdf.Page{{Lines: []testpdf.Line{{Text: "Robins nest in spring hedges and sing throughout the morning.", X: 50, Y: 700}}}}, testpdf.Options{Title: "Field guide", Labels: true})
	doc := f.upload(t, "guide.pdf", "application/pdf", data)
	f.wait(t, doc.ID, "ready")
	pages := f.pages(t, doc.ID)
	if len(pages) != 1 || pages[0].Label == "" || len(pages[0].Blocks) == 0 || len(pages[0].Blocks[0].Boxes) == 0 {
		t.Fatalf("page positions: %+v", pages)
	}
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for !found && time.Now().Before(deadline) {
		for _, event := range f.bus.Replay(types.Cursor{}) {
			if event.Type != api.EventLibraryDocumentUpdated {
				continue
			}
			var update api.DocumentInfo
			if err := json.Unmarshal(event.Payload, &update); err != nil {
				t.Fatal(err)
			}
			if update.ID == doc.ID && update.Status == "ready" {
				found = true
			}
		}
		if !found {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !found {
		t.Fatal("ready event missing")
	}

	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{Memory: f.mem})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	daemonTools, err := Assemble(AssembleOptions{Store: orchStore, Orch: orch, Memory: f.mem})
	if err != nil {
		t.Fatal(err)
	}
	for name, reg := range map[string]*tools.MapRegistry{"embedded": emb.Registry, "daemon": daemonTools.Tools} {
		t.Run(name, func(t *testing.T) {
			run := "run_library_" + name
			args, _ := json.Marshal(map[string]any{"query": "Robins", "scopes": []string{"general"}, "run_id": run})
			tool, err := reg.Get("library_search")
			if err != nil {
				t.Fatal(err)
			}
			result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: args})
			if err != nil || result.Status != tools.StatusOK {
				t.Fatalf("search: %+v %v", result, err)
			}
			var output struct {
				Hits []struct {
					Ref string `json:"ref"`
				} `json:"hits"`
			}
			if err = json.Unmarshal([]byte(result.Output), &output); err != nil {
				t.Fatal(err)
			}
			if len(output.Hits) == 0 || !strings.HasPrefix(output.Hits[0].Ref, "[[doc:") {
				t.Fatalf("missing refs: %s", result.Output)
			}
			_, chunkID, ok := memory.ParseCitationRef(output.Hits[0].Ref)
			if !ok {
				t.Fatal(output)
			}
			var chunk api.LibraryChunkResult
			f.rpc(t, api.MethodLibraryChunk, api.LibraryChunkParams{DocumentID: doc.ID, ChunkID: chunkID}, &chunk)
			if chunk.Chunk.PageLabel != pages[0].Label || len(chunk.Chunk.Boxes) == 0 {
				t.Fatalf("chunk positions: %+v", chunk)
			}
			text, sources, dropped := (memory.CiteFlow{Library: f.mem}).Process(context.Background(), "Robins nest in hedges "+output.Hits[0].Ref, []string{run})
			if !strings.Contains(text, "[1]") || len(dropped) != 0 || len(sources) != 1 || sources[0].PageLabel != pages[0].Label || len(sources[0].Boxes) == 0 {
				t.Fatalf("citation: %q %+v %v", text, sources, dropped)
			}
		})
	}
}

type libraryVisionFake struct{ calls atomic.Int32 }

func (*libraryVisionFake) Extract(context.Context, string, string, []int) (tools.VisionResult, error) {
	return tools.VisionResult{}, fmt.Errorf("unexpected file vision call")
}
func (v *libraryVisionFake) ExtractPNG(_ context.Context, data []byte, page int) (tools.VisionResult, error) {
	if !bytes.HasPrefix(data, []byte("\x89PNG")) && !bytes.HasPrefix(data, []byte{255, 216, 255}) {
		return tools.VisionResult{}, fmt.Errorf("expected rendered PNG")
	}
	v.calls.Add(1)
	return tools.VisionResult{Pages: []tools.VisionPage{{Page: page, Text: "Scanned robins build nests in hedges."}}}, nil
}
func TestLibraryE2EScannedAndBudget(t *testing.T) {
	for _, budget := range []string{"1", "0"} {
		t.Run(budget, func(t *testing.T) {
			t.Setenv("EXCALIBUR_LIBRARY_OCR_MAX_PAGES", budget)
			v := &libraryVisionFake{}
			f := newLibraryE2E(t, &tools.MediaDeps{Vision: v}, true)
			data := testpdf.Build([]testpdf.Page{{Image: true}}, testpdf.Options{Labels: true})
			doc := f.upload(t, "scan.pdf", "application/pdf", data)
			status := "ready"
			if budget == "0" {
				status = "partial"
			}
			info := f.wait(t, doc.ID, status)
			if budget == "1" {
				pages := f.pages(t, doc.ID)
				if v.calls.Load() != 1 || len(pages) != 1 || pages[0].Method != docextract.MethodVision {
					t.Fatalf("OCR: %d %+v", v.calls.Load(), pages)
				}
			} else if v.calls.Load() != 0 || !strings.Contains(strings.Join(info.Warnings, " "), "budget exceeded") {
				t.Fatalf("budget: %+v", info)
			}
		})
	}
}
func TestLibraryE2EOffice(t *testing.T) {
	f := newLibraryE2E(t, nil, true)
	for _, format := range []string{"docx", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			spec := create.Spec{Title: "Habitat", Blocks: []create.Block{{Kind: "paragraph", Text: "Robins nest in spring hedges."}}}
			mime := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			if format == "xlsx" {
				spec = create.Spec{Sheets: []create.Sheet{{Name: "Habitat", Cells: []create.Cell{{Ref: "A1", Value: "Robins nest in spring hedges."}}}}}
				mime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
			}
			pkg, err := create.Build(format, spec, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := pkg.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			doc := f.upload(t, "habitat."+format, mime, data)
			f.wait(t, doc.ID, "ready")
			hits, _, err := f.mem.LibrarySearch(context.Background(), []string{"general"}, "run_office_"+format, "Robins", 5, doc.ID)
			if err != nil || len(hits) == 0 {
				t.Fatalf("search: %+v %v", hits, err)
			}
			var chunk api.LibraryChunkResult
			f.rpc(t, api.MethodLibraryChunk, api.LibraryChunkParams{DocumentID: doc.ID, ChunkID: hits[0].Chunk.ID}, &chunk)
			if chunk.Chunk.Anchor == "" || !strings.Contains(chunk.Chunk.Text, "Robins") {
				t.Fatalf("anchor: %+v", chunk)
			}
		})
	}
}
func TestLibraryE2ERestartProcessing(t *testing.T) {
	f := newLibraryE2E(t, nil, false)
	doc := f.upload(t, "restart.txt", "text/plain", []byte("Robins survive a durable worker restart."))
	queue := library.NewSQLQueue(f.db)
	job, err := queue.Claim(context.Background())
	if err != nil || job == nil {
		t.Fatalf("claim: %+v %v", job, err)
	}
	if err = f.mem.SetDocumentProgress(context.Background(), doc.ID, "processing", 0, 1, "", ""); err != nil {
		t.Fatal(err)
	}
	if err = f.services.Library.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := &Services{}
	if err = restarted.ConfigureLibrary(LibraryOptions{DB: f.db, Memory: f.mem, Bus: f.bus}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Library.Close() })
	if err = restarted.Library.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.wait(t, doc.ID, "ready")
	jobState, err := queue.Get(context.Background(), job.ID)
	// Document indexing precedes the queue's terminal commit.
	deadline := time.Now().Add(3 * time.Second)
	for err == nil && jobState.State != "ready" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		jobState, err = queue.Get(context.Background(), job.ID)
	}
	if err != nil || jobState.State != "ready" || jobState.Attempts != 0 {
		t.Fatalf("recovered queue: %+v %v", jobState, err)
	}
}

// Images use the registered metadata reader before vision transcription so
// dimensions remain available to viewers and malformed originals are rejected.
func TestLibraryE2EImageFormats(t *testing.T) {
	vision := &libraryVisionFake{}
	f := newLibraryE2E(t, &tools.MediaDeps{Vision: vision}, true)
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			var data bytes.Buffer
			img := image.NewRGBA(image.Rect(0, 0, 3, 2))
			var err error
			if format == "png" {
				err = png.Encode(&data, img)
			} else {
				err = jpeg.Encode(&data, img, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			doc := f.upload(t, "image."+format, "image/"+format, data.Bytes())
			f.wait(t, doc.ID, "ready")
			pages := f.pages(t, doc.ID)
			if len(pages) != 1 || pages[0].Method != docextract.MethodVision || pages[0].Width != 3 || pages[0].Height != 2 {
				t.Fatalf("image metadata: %+v", pages)
			}
		})
	}
	if vision.calls.Load() != 2 {
		t.Fatalf("OCR calls: %d", vision.calls.Load())
	}
}
