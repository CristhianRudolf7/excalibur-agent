package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/library"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeLibrary struct {
	doc        api.DocumentInfo
	submitted  bool
	deleted    bool
	err        error
	listParams api.LibraryListParams
}

func (f *fakeLibrary) Submit(_ context.Context, p api.LibraryIngestParams) (api.DocumentInfo, bool, error) {
	existing := f.submitted
	f.submitted = true
	f.doc.BlobDigest = p.BlobDigest
	return f.doc, existing, f.err
}
func (f *fakeLibrary) ListDocuments(_ context.Context, _ []string, p api.LibraryListParams) ([]api.DocumentInfo, string, error) {
	f.listParams = p
	return []api.DocumentInfo{f.doc}, "next", f.err
}
func (f *fakeLibrary) GetDocument(_ context.Context, _ []string, id string) (api.DocumentInfo, error) {
	if id == "missing" {
		return api.DocumentInfo{}, os.ErrNotExist
	}
	return f.doc, f.err
}
func (f *fakeLibrary) DocumentPages(context.Context, []string, string, int, int, int) ([]docextract.Page, error) {
	return []docextract.Page{{Number: 1}}, f.err
}
func (f *fakeLibrary) ChunkDetail(context.Context, []string, string, string) (api.ChunkDetail, error) {
	return api.ChunkDetail{DocumentID: f.doc.ID, ChunkID: "c"}, f.err
}
func (f *fakeLibrary) PurgeDocument(context.Context, string, string) error {
	f.deleted = true
	return f.err
}
func (f *fakeLibrary) Cancel(string) error { f.doc.Status = "canceled"; return f.err }
func libraryFixture() (*LibraryRoutes, *fakeLibrary) {
	f := &fakeLibrary{doc: api.DocumentInfo{ID: "doc", ScopeID: "general", Status: "pending"}}
	return &LibraryRoutes{Backend: f, Scopes: []string{"general"}}, f
}
func TestLibraryRoutes(t *testing.T) {
	t.Setenv("EXCALIBUR_LIBRARY_API", "1")
	r, f := libraryFixture()
	ctx := context.Background()
	ingest := json.RawMessage(`{"blob_digest":"` + strings.Repeat("a", 64) + `","name":"x.pdf","mime":"application/pdf"}`)
	for i := 0; i < 2; i++ {
		v, e := r.Op(ctx, api.MethodLibraryIngest, ingest)
		if e != nil || v.(api.LibraryIngestResult).Existing != (i == 1) {
			t.Fatalf("ingest %v %v", v, e)
		}
	}
	for _, tc := range []struct{ method, params string }{
		{api.MethodLibraryList, `{}`}, {api.MethodLibraryGet, `{"id":"doc"}`},
		{api.MethodLibraryPages, `{"id":"doc","from":1,"to":20}`},
		{api.MethodLibraryChunk, `{"document_id":"doc","chunk_id":"c"}`},
		{api.MethodLibraryCancel, `{"id":"doc"}`},
		{api.MethodLibraryDelete, `{"id":"doc"}`},
	} {
		v, e := r.Op(ctx, tc.method, json.RawMessage(tc.params))
		if e != nil {
			t.Fatalf("%s: %v", tc.method, e)
		}
		if tc.method == api.MethodLibraryDelete && (v.(api.LibraryDeleteResult).Notice != memory.BackupNotice || !f.deleted) {
			t.Fatal(v)
		}
	}
	if f.listParams.Limit != 50 {
		t.Fatal("missing default limit")
	}
	if f.doc.Status != "canceled" {
		t.Fatal("cancel not applied")
	}
}
func TestLibraryInvalidAndDenied(t *testing.T) {
	for _, tc := range []struct {
		method, params string
		code           int
	}{
		{api.MethodLibraryList, `{"limit":101}`, api.CodeInvalidParams},
		{api.MethodLibraryList, `{"status":"bogus"}`, api.CodeInvalidParams},
		{api.MethodLibraryList, `{"scope_id":"secret"}`, api.CodeScopeDenied},
		{api.MethodLibraryGet, `{"id":"missing"}`, api.CodeUnknownTarget},
		{api.MethodLibraryGet, `{"id":"doc","extra":1}`, api.CodeInvalidParams},
		{api.MethodLibraryGet, `null`, api.CodeInvalidParams},
		{api.MethodLibraryGet, `[]`, api.CodeInvalidParams},
		{api.MethodLibraryGet, `{} {}`, api.CodeInvalidParams},
		{api.MethodLibraryGet, `{`, api.CodeInvalidParams},
		{api.MethodLibraryGet, `{}`, api.CodeInvalidParams},
		{api.MethodLibraryPages, `{"id":"doc","from":1,"to":21}`, api.CodeInvalidParams},
		{api.MethodLibraryPages, `{"id":"doc","from":0,"to":1}`, api.CodeInvalidParams},
		{api.MethodLibraryChunk, `{"document_id":"doc"}`, api.CodeInvalidParams},
		{api.MethodLibraryIngest, `{"blob_digest":"bad","name":"x","mime":"text/plain"}`, api.CodeInvalidParams},
		{"library.unknown", `{}`, api.CodeMethodNotFound},
	} {
		t.Run(tc.method+tc.params, func(t *testing.T) {
			r, _ := libraryFixture()
			_, err := r.Op(context.Background(), tc.method, json.RawMessage(tc.params))
			var ae *api.Error
			if !errors.As(err, &ae) || ae.Code != tc.code {
				t.Fatalf("got %v, want %d", err, tc.code)
			}
		})
	}
	r, f := libraryFixture()
	f.doc.ScopeID = "secret"
	_, err := r.Op(context.Background(), api.MethodLibraryDelete, json.RawMessage(`{"id":"doc"}`))
	if err == nil || f.deleted {
		t.Fatal("cross-scope delete")
	}
	r.Scopes = nil
	_, err = r.Op(context.Background(), api.MethodLibraryList, nil)
	if err == nil {
		t.Fatal("empty scopes admitted")
	}
	t.Setenv("EXCALIBUR_LIBRARY_API", "0")
	_, err = r.Op(context.Background(), api.MethodLibraryList, nil)
	if err.(*api.Error).Code != api.CodeMethodNotFound {
		t.Fatal(err)
	}
}
func TestLibraryGatewayDispatchAndEvents(t *testing.T) {
	r, _ := libraryFixture()
	backend := NewDaemonBackend(nil, nil, nil)
	backend.AttachServices(BackendServices{Library: r})
	cfg := gateway.DefaultConfig()
	cfg.AllowRemote = true
	g := gateway.New(cfg, backend, nil, nil)
	for _, method := range []string{api.MethodLibraryGet, "library.unknown"} {
		req, _ := api.NewRequest("1", method, api.LibraryIDParams{ID: "doc"})
		raw, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest("POST", "/rpc", bytes.NewReader(raw)))
		var resp api.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if method == api.MethodLibraryGet && resp.Error != nil {
			t.Fatal(resp.Error)
		}
		if method == "library.unknown" && (resp.Error == nil || resp.Error.Code != api.CodeMethodNotFound) {
			t.Fatal(resp)
		}
	}
	bus := telemetry.NewBus(nil)
	LibraryNotifier{Bus: bus}.DocumentUpdated(api.DocumentInfo{ID: "doc"})
	events := bus.Replay(types.Cursor{})
	if len(events) != 1 || events[0].Type != api.EventLibraryDocumentUpdated {
		t.Fatal(events)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Op(ctx, api.MethodLibraryGet, json.RawMessage(`{"id":"doc"}`)); err == nil {
		t.Fatal("canceled request succeeded")
	}
}

func TestLibraryServiceBinding(t *testing.T) {
	_, f := libraryFixture()
	b := NewLibraryServiceBinding[api.DocumentInfo, api.LibraryListParams, api.ChunkDetail, api.LibraryIngestParams](
		f, f, func(p api.LibraryIngestParams) api.LibraryIngestParams { return p },
		func(p api.LibraryListParams) api.LibraryListParams { return p },
		func(d api.DocumentInfo) api.DocumentInfo { return d }, func(c api.ChunkDetail) api.ChunkDetail { return c })
	r := &LibraryRoutes{Backend: b, Scopes: []string{"general"}}
	v, err := r.Op(context.Background(), api.MethodLibraryList, json.RawMessage(`{"limit":3}`))
	if err != nil || len(v.(api.LibraryListResult).Documents) != 1 || f.listParams.Limit != 3 {
		t.Fatalf("binding %v %v", v, err)
	}
	for _, tc := range []struct{ method, params string }{
		{api.MethodLibraryIngest, `{"blob_digest":"` + strings.Repeat("a", 64) + `","name":"a","mime":"text/plain"}`},
		{api.MethodLibraryGet, `{"id":"doc"}`}, {api.MethodLibraryPages, `{"id":"doc","from":1,"to":1}`},
		{api.MethodLibraryChunk, `{"document_id":"doc","chunk_id":"c"}`}, {api.MethodLibraryCancel, `{"id":"doc"}`}, {api.MethodLibraryDelete, `{"id":"doc"}`},
	} {
		if _, err := r.Op(context.Background(), tc.method, json.RawMessage(tc.params)); err != nil {
			t.Fatal(err)
		}
	}
	f.err = errors.New("private storage detail")
	if _, err := r.Op(context.Background(), api.MethodLibraryList, nil); err.(*api.Error).Code != api.CodeInternal || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
}

func TestLibraryGatewayAllMethods(t *testing.T) {
	routes, _ := libraryFixture()
	backend := NewDaemonBackend(nil, nil, nil)
	backend.AttachServices(BackendServices{Library: routes})
	cfg := gateway.DefaultConfig()
	cfg.AllowRemote = true
	gate := gateway.New(cfg, backend, nil, nil)
	for _, tc := range []struct{ method, params string }{
		{api.MethodLibraryIngest, `{"blob_digest":"` + strings.Repeat("a", 64) + `","name":"a","mime":"text/plain"}`},
		{api.MethodLibraryList, `{}`}, {api.MethodLibraryGet, `{"id":"doc"}`},
		{api.MethodLibraryPages, `{"id":"doc","from":1,"to":1}`},
		{api.MethodLibraryChunk, `{"document_id":"doc","chunk_id":"c"}`},
		{api.MethodLibraryCancel, `{"id":"doc"}`}, {api.MethodLibraryDelete, `{"id":"doc"}`},
	} {
		for _, invalid := range []bool{false, true} {
			params := json.RawMessage(tc.params)
			if invalid {
				params = json.RawMessage(`{"unknown":true}`)
			}
			req, _ := api.NewRequest("request", tc.method, params)
			raw, _ := json.Marshal(req)
			w := httptest.NewRecorder()
			gate.ServeHTTP(w, httptest.NewRequest("POST", "/rpc", bytes.NewReader(raw)))
			var resp api.Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if invalid {
				if resp.Error == nil || resp.Error.Code != api.CodeInvalidParams {
					t.Fatalf("%s invalid: %+v", tc.method, resp)
				}
			} else if resp.Error != nil {
				t.Fatalf("%s: %v", tc.method, resp.Error)
			}
		}
	}
}

type idleLibraryStore struct{}

func (idleLibraryStore) CreatePendingDocument(context.Context, string, memory.DocMeta) (memory.DocumentInfo, bool, error) {
	return memory.DocumentInfo{}, false, errors.New("unused")
}
func (idleLibraryStore) SetDocumentProgress(context.Context, string, string, int, int, string, string) error {
	return errors.New("unused")
}
func (idleLibraryStore) IngestExtracted(context.Context, string, string, docextract.Document) (memory.DocumentInfo, error) {
	return memory.DocumentInfo{}, errors.New("unused")
}
func (idleLibraryStore) GetDocument(context.Context, []string, string) (memory.DocumentInfo, error) {
	return memory.DocumentInfo{}, errors.New("unused")
}

type idleLibraryBlobs struct{}

func (idleLibraryBlobs) Put(context.Context, io.Reader) (string, error) {
	return "", errors.New("unused")
}
func (idleLibraryBlobs) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}

type idleLibraryQueue struct {
	recovered atomic.Int32
	err       error
}

func (q *idleLibraryQueue) Recover(context.Context) error             { q.recovered.Add(1); return q.err }
func (*idleLibraryQueue) Claim(context.Context) (*library.Job, error) { return nil, nil }
func (*idleLibraryQueue) Put(context.Context, library.Job) error      { return nil }
func (*idleLibraryQueue) Save(context.Context, library.Job) error     { return nil }
func (*idleLibraryQueue) Get(context.Context, string) (library.Job, error) {
	return library.Job{}, errors.New("unused")
}
func TestLibraryDaemonLifecycle(t *testing.T) {
	q := &idleLibraryQueue{}
	i, err := library.New(library.Options{Store: idleLibraryStore{}, Blobs: idleLibraryBlobs{}, Queue: q, Registry: docextract.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	d := New(Config{}, Deps{Services: &Services{Library: i}})
	if _, err = d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.recovered.Load() != 1 {
		t.Fatal("library was not started")
	}
	if err = d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-i.Done():
	case <-time.After(time.Second):
		t.Fatal("library worker outlived daemon")
	}
}
func TestLibraryStartFailureAndOff(t *testing.T) {
	q := &idleLibraryQueue{err: errors.New("unavailable queue")}
	i, err := library.New(library.Options{Store: idleLibraryStore{}, Blobs: idleLibraryBlobs{}, Queue: q, Registry: docextract.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	d := New(Config{}, Deps{Services: &Services{Library: i}})
	if _, err = d.Start(context.Background()); err == nil {
		t.Fatal("startup failure hidden")
	}
	t.Setenv("EXCALIBUR_LIBRARY_INGEST", "0")
	svc := &Services{}
	if err = svc.ConfigureLibrary(LibraryOptions{}); err != nil || svc.Library != nil {
		t.Fatal(err)
	}
}
