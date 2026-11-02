package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

type fakeStore struct {
	mu          sync.Mutex
	docs        map[string]memory.DocumentInfo
	ingestCalls int
	ingestErr   error
	extracted   docextract.Document
}

func newFakeStore() *fakeStore { return &fakeStore{docs: map[string]memory.DocumentInfo{}} }
func (s *fakeStore) CreatePendingDocument(ctx context.Context, scope string, m memory.DocMeta) (memory.DocumentInfo, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.docs {
		if d.ScopeID == scope && d.BlobDigest == m.BlobDigest {
			return d, true, nil
		}
	}
	id := fmt.Sprint(len(s.docs) + 1)
	d := memory.DocumentInfo{ID: id, ScopeID: scope, Title: m.Title, SourceName: m.SourceName, MIME: m.MIME, BlobDigest: m.BlobDigest, Status: "pending", UploadedAt: time.Now(), Version: 1}
	s.docs[id] = d
	return d, false, nil
}
func (s *fakeStore) SetDocumentProgress(ctx context.Context, id, state string, done, total int, stage, errText string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.docs[id]
	d.Status = state
	d.ProgressPages = done
	d.TotalPages = total
	d.FailureStage = stage
	d.Error = errText
	s.docs[id] = d
	return nil
}
func (s *fakeStore) GetDocument(ctx context.Context, scopes []string, id string) (memory.DocumentInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[id]
	if !ok {
		return d, memory.ErrNotFound
	}
	if len(scopes) != 1 || scopes[0] != d.ScopeID {
		return d, memory.ErrScopeDenied
	}
	return d, nil
}
func (s *fakeStore) IngestExtracted(ctx context.Context, scope, id string, doc docextract.Document) (memory.DocumentInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ingestCalls++
	if err := ctx.Err(); err != nil {
		return memory.DocumentInfo{}, err
	}
	if s.ingestErr != nil {
		return memory.DocumentInfo{}, s.ingestErr
	}
	d := s.docs[id]
	d.Status = "ready"
	if len(doc.Warnings) > 0 {
		d.Status = "partial"
	}
	d.PageCount = len(doc.Pages)
	d.ProgressPages = len(doc.Pages)
	d.TotalPages = len(doc.Pages)
	d.Warnings = doc.Warnings
	s.docs[id] = d
	s.extracted = doc
	return d, nil
}

type fakeBlobs struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (b *fakeBlobs) Put(ctx context.Context, r io.Reader) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	id := hex.EncodeToString(sum[:])
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[id] = raw
	return id, nil
}
func (b *fakeBlobs) Open(ctx context.Context, id string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, ok := b.data[id]
	if !ok {
		return nil, errors.New("missing blob")
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

type fakeExtractor struct {
	fn func(context.Context, docextract.Options) (docextract.Document, error)
}

func (fakeExtractor) Formats() []string { return []string{"pdf"} }
func (e fakeExtractor) Extract(ctx context.Context, _ io.ReaderAt, _ int64, o docextract.Options) (docextract.Document, error) {
	return e.fn(ctx, o)
}

type fakeOCR struct {
	fn func(context.Context, int) (docextract.Page, error)
}

func (o fakeOCR) OCRPage(ctx context.Context, _ []byte, p int) (docextract.Page, error) {
	return o.fn(ctx, p)
}

type fakeRenderer struct{}

func (fakeRenderer) RenderPNG(ctx context.Context, _ io.ReaderAt, _ int64, p, dpi int) ([]byte, error) {
	return []byte("image"), ctx.Err()
}

type fakeNotifications struct {
	updates    chan memory.DocumentInfo
	mu         sync.Mutex
	notices    []string
	candidates []memory.Candidate
}

func (n *fakeNotifications) DocumentUpdated(d memory.DocumentInfo) { n.updates <- d }
func (n *fakeNotifications) PostDocumentNotice(scope, id, content string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notices = append(n.notices, scope+":"+id+":"+content)
}
func (n *fakeNotifications) Propose(ctx context.Context, c memory.Candidate) (memory.Memory, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.candidates = append(n.candidates, c)
	return memory.Memory{}, nil
}
func openQueue(t *testing.T, path string) (*SQLQueue, func()) {
	t.Helper()
	ctx := context.Background()
	reg := migrations.New()
	migration := migrations.LibraryJobs()
	migration.Version = 1
	if err := reg.Register(migration); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, path, store.DefaultOptions(), reg)
	if err != nil {
		t.Fatal(err)
	}
	return NewSQLQueue(db), func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}
}
func setup(t *testing.T) (Options, *fakeStore, *fakeBlobs, *fakeNotifications) {
	t.Helper()
	q, closeDB := openQueue(t, filepath.Join(t.TempDir(), "test.db"))
	t.Cleanup(closeDB)
	s := newFakeStore()
	b := &fakeBlobs{data: map[string][]byte{}}
	n := &fakeNotifications{updates: make(chan memory.DocumentInfo, 100)}
	reg := docextract.NewRegistry()
	if err := RegisterDefaultExtractors(reg); err != nil {
		t.Fatal(err)
	}
	return Options{Store: s, Queue: q, Blobs: b, Registry: reg, Notifier: n, Notices: n, Proposer: n}, s, b, n
}
func newIngester(t *testing.T, o Options) *Ingester {
	t.Helper()
	i, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = i.Close() })
	return i
}
func submit(t *testing.T, i *Ingester, b *fakeBlobs, data string) memory.DocumentInfo {
	t.Helper()
	digest, err := b.Put(context.Background(), strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := i.Submit(context.Background(), IngestRequest{Scope: "project:one", Meta: memory.DocMeta{BlobDigest: digest, Title: "Guide", SourceName: "guide.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func start(t *testing.T, i *Ingester) {
	t.Helper()
	if err := i.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func waitState(t *testing.T, s *fakeStore, id, state string) memory.DocumentInfo {
	t.Helper()
	deadline := time.After(4 * time.Second)
	tick := time.NewTicker(time.Millisecond * 5)
	defer tick.Stop()
	for {
		d, _ := s.GetDocument(context.Background(), []string{"project:one"}, id)
		if d.Status == state {
			return d
		}
		select {
		case <-deadline:
			t.Fatalf("want %s, got %+v", state, d)
		case <-tick.C:
		}
	}
}
func TestSubmitDedupNoticesAndProposal(t *testing.T) {
	o, s, b, n := setup(t)
	i := newIngester(t, o)
	d := submit(t, i, b, "hello world")
	if d.Status != "pending" {
		t.Fatal(d)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := i.Submit(ctx, IngestRequest{Scope: d.ScopeID, Meta: memory.DocMeta{BlobDigest: d.BlobDigest}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled submission: %v", err)
	}
	duplicate, existing, err := i.Submit(context.Background(), IngestRequest{Scope: d.ScopeID, Meta: memory.DocMeta{BlobDigest: d.BlobDigest}})
	if err != nil || !existing || duplicate.ID != d.ID {
		t.Fatalf("%+v %v %v", duplicate, existing, err)
	}
	start(t, i)
	waitState(t, s, d.ID, "ready")
	_ = i.Close()
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.notices) != 1 || !strings.Contains(n.notices[0], "Finished reading “Guide” (1 pages)") {
		t.Fatal(n.notices)
	}
	if len(n.candidates) != 1 {
		t.Fatal(n.candidates)
	}
	c := n.candidates[0]
	if c.Scope != d.ScopeID || c.Trust != memory.TrustAgent || c.Sources[0].Ref != d.ID || !strings.Contains(c.Statement, "Guide") {
		t.Fatal(c)
	}
}
func TestPathAndValidation(t *testing.T) {
	o, _, _, _ := setup(t)
	i := newIngester(t, o)
	path := filepath.Join(t.TempDir(), "local.md")
	if err := os.WriteFile(path, []byte("# Header"), 0600); err != nil {
		t.Fatal(err)
	}
	d, existing, err := i.Submit(context.Background(), IngestRequest{Scope: "project:one", Path: path})
	if err != nil || existing || d.BlobDigest == "" || d.SourceName != "local.md" {
		t.Fatalf("%+v %v %v", d, existing, err)
	}
	for _, r := range []IngestRequest{{Scope: "project:one"}, {Scope: "project:one", Path: path, Meta: memory.DocMeta{BlobDigest: d.BlobDigest}}, {Scope: "project:one", Meta: memory.DocMeta{BlobDigest: "bad"}}, {Path: path}} {
		if _, _, err := i.Submit(context.Background(), r); err == nil {
			t.Fatal(r)
		}
	}
	t.Setenv("EXCALIBUR_LIBRARY_INGEST", "0")
	if _, _, err := i.Submit(context.Background(), IngestRequest{Scope: "project:one", Path: path}); err == nil {
		t.Fatal("disabled")
	}
}

type failingCancelStore struct {
	*fakeStore
	fail bool
}

func (s *failingCancelStore) SetDocumentProgress(ctx context.Context, id, state string, done, total int, stage, errText string) error {
	if state == "canceled" && s.fail {
		s.fail = false
		return errors.New("document store temporarily unavailable")
	}
	return s.fakeStore.SetDocumentProgress(ctx, id, state, done, total, stage, errText)
}

func TestCancelRetryRepairsDocument(t *testing.T) {
	o, s, b, n := setup(t)
	o.Store = &failingCancelStore{fakeStore: s, fail: true}
	i := newIngester(t, o)
	d := submit(t, i, b, "cancel before workers start")
	if err := i.Cancel(d.ID); err == nil {
		t.Fatal("expected the document update to fail")
	}
	j, err := o.Queue.Get(context.Background(), d.ID)
	if err != nil || j.State != "canceled" {
		t.Fatalf("cancellation was not durable: %+v %v", j, err)
	}
	// A new instance must also be able to finish the cancellation after the
	// queue commit succeeded but the document update failed.
	resumed := newIngester(t, o)
	if err := resumed.Cancel(d.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDocument(context.Background(), []string{d.ScopeID}, d.ID)
	if err != nil || got.Status != "canceled" {
		t.Fatalf("retry did not repair cancellation: %+v %v", got, err)
	}
	select {
	case update := <-n.updates:
		if update.ID != d.ID || update.Status != "canceled" {
			t.Fatalf("unexpected cancellation update: %+v", update)
		}
	default:
		t.Fatal("missing cancellation update")
	}
	if err := resumed.Cancel(d.ID); err != nil {
		t.Fatal(err)
	}
	if len(n.updates) != 0 {
		t.Fatal("repeated cancellation emitted another update")
	}
	start(t, resumed)
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = o.Queue.Get(context.Background(), d.ID)
	if err != nil || j.State != "canceled" || j.Attempts != 0 {
		t.Fatalf("canceled job changed: %+v %v", j, err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.notices) != 0 || len(n.candidates) != 0 {
		t.Fatalf("cancellation emitted completion effects: %v %v", n.notices, n.candidates)
	}
}

func TestCancel(t *testing.T) {
	o, s, b, n := setup(t)
	entered := make(chan struct{})
	reg := docextract.NewRegistry()
	_ = reg.Register(fakeExtractor{fn: func(ctx context.Context, _ docextract.Options) (docextract.Document, error) {
		close(entered)
		<-ctx.Done()
		return docextract.Document{}, ctx.Err()
	}})
	o.Registry = reg
	i := newIngester(t, o)
	d := submit(t, i, b, "%PDF scanned")
	start(t, i)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	if err := i.Cancel(d.ID); err != nil {
		t.Fatal(err)
	}
	_ = i.Close()
	waitState(t, s, d.ID, "canceled")
	j, err := o.Queue.Get(context.Background(), d.ID)
	if err != nil || j.State != "canceled" || j.Attempts != 0 {
		t.Fatalf("%+v %v", j, err)
	}
	resumed := newIngester(t, o)
	start(t, resumed)
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = o.Queue.Get(context.Background(), d.ID)
	if err != nil || j.State != "canceled" || j.Attempts != 0 {
		t.Fatalf("canceled job changed during recovery: %+v %v", j, err)
	}
	if pending, err := o.Queue.Claim(context.Background()); err != nil || pending != nil {
		t.Fatalf("canceled job became claimable: %+v %v", pending, err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.candidates) != 0 {
		t.Fatal(n.candidates)
	}
}

// committingStore models an atomic indexing transaction that finishes its
// commit despite the request context being canceled during that commit.
type committingStore struct {
	*fakeStore
	entered chan struct{}
	release chan struct{}
}

func (s *committingStore) IngestExtracted(_ context.Context, scope, id string, doc docextract.Document) (memory.DocumentInfo, error) {
	close(s.entered)
	<-s.release
	return s.fakeStore.IngestExtracted(context.Background(), scope, id, doc)
}

func TestCancelDuringIndexCommit(t *testing.T) {
	o, s, b, n := setup(t)
	committing := &committingStore{fakeStore: s, entered: make(chan struct{}), release: make(chan struct{})}
	o.Store = committing
	i := newIngester(t, o)
	// Release the transaction even if an earlier assertion fails, before Close
	// waits for the worker in the cleanup registered by newIngester.
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(committing.release) }) })
	d := submit(t, i, b, "an atomic indexing commit")
	start(t, i)
	select {
	case <-committing.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("indexing did not start")
	}
	if err := i.Cancel(d.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, d.ID, "canceled")
	release.Do(func() { close(committing.release) })
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDocument(context.Background(), []string{d.ScopeID}, d.ID)
	if err != nil || got.Status != "canceled" {
		t.Fatalf("index commit overwrote cancellation: %+v %v", got, err)
	}
	j, err := o.Queue.Get(context.Background(), d.ID)
	if err != nil || j.State != "canceled" || j.Attempts != 0 {
		t.Fatalf("canceled queue entry changed: %+v %v", j, err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.notices) != 0 || len(n.candidates) != 0 {
		t.Fatalf("canceled indexing emitted completion effects: %v %v", n.notices, n.candidates)
	}
	for len(n.updates) > 0 {
		if update := <-n.updates; update.Status != "canceled" {
			t.Fatalf("canceled indexing emitted status %s", update.Status)
		}
	}
}

func TestRetriesAndFailureStages(t *testing.T) {
	for _, stage := range []string{"extract", "ocr", "chunk", "embed", "store", "read"} {
		t.Run(stage, func(t *testing.T) {
			o, s, b, n := setup(t)
			reg := docextract.NewRegistry()
			_ = reg.Register(fakeExtractor{fn: func(ctx context.Context, _ docextract.Options) (docextract.Document, error) {
				if stage == "extract" {
					return docextract.Document{}, errors.New("broken input")
				}
				return scanned(1), nil
			}})
			o.Registry = reg
			o.Renderer = fakeRenderer{}
			o.OCR = fakeOCR{fn: func(ctx context.Context, p int) (docextract.Page, error) {
				if stage == "ocr" {
					return docextract.Page{}, errors.New("OCR failed")
				}
				return native(p), nil
			}}
			if stage == "chunk" || stage == "embed" || stage == "store" {
				s.ingestErr = testStageError(stage)
			}
			i := newIngester(t, o)
			d := submit(t, i, b, "%PDF test")
			if stage == "read" {
				b.mu.Lock()
				delete(b.data, d.BlobDigest)
				b.mu.Unlock()
			}
			start(t, i)
			got := waitState(t, s, d.ID, "failed")
			_ = i.Close()
			j, err := o.Queue.Get(context.Background(), d.ID)
			if err != nil || j.Attempts != 3 || got.FailureStage != stage {
				t.Fatalf("%+v %+v %v", got, j, err)
			}
			n.mu.Lock()
			defer n.mu.Unlock()
			if len(n.notices) != 1 || !strings.Contains(n.notices[0], "Failed reading “Guide” at "+stage+": "+got.Error) {
				t.Fatalf("failed ingestion notice: %v", n.notices)
			}
			if len(n.candidates) != 0 {
				t.Fatalf("failed ingestion proposed a memory: %+v", n.candidates)
			}
		})
	}
}

type testStageError string

func (e testStageError) Error() string        { return string(e) + " failed" }
func (e testStageError) FailureStage() string { return string(e) }
func native(p int) docextract.Page {
	return docextract.Page{Number: p, Method: docextract.MethodVision, Quality: 1, Blocks: []docextract.Block{{Kind: docextract.KindParagraph, Text: "Recovered page"}}}
}
func scanned(n int) docextract.Document {
	d := docextract.Document{Format: "pdf", Paginated: true, PageCount: n}
	for p := 1; p <= n; p++ {
		d.Pages = append(d.Pages, docextract.Page{Number: p, Method: docextract.MethodNone})
	}
	return d
}
func TestOCRBudgetAndSelectivePages(t *testing.T) {
	o, s, b, n := setup(t)
	budget := 1
	o.OCRMaxPages = &budget
	reg := docextract.NewRegistry()
	_ = reg.Register(fakeExtractor{fn: func(ctx context.Context, _ docextract.Options) (docextract.Document, error) {
		d := scanned(3)
		d.Pages[0] = native(1)
		return d, nil
	}})
	o.Registry = reg
	o.Renderer = fakeRenderer{}
	var pages []int
	o.OCR = fakeOCR{fn: func(ctx context.Context, p int) (docextract.Page, error) {
		pages = append(pages, p)
		return native(p), nil
	}}
	i := newIngester(t, o)
	d := submit(t, i, b, "%PDF scanned")
	start(t, i)
	got := waitState(t, s, d.ID, "partial")
	_ = i.Close()
	if len(pages) != 1 || pages[0] != 2 || len(got.Warnings) == 0 || !strings.Contains(got.Warnings[0], "confirmation") {
		t.Fatalf("%v %+v", pages, got)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.notices) != 1 || !strings.Contains(n.notices[0], "Partially read “Guide”: "+got.Warnings[0]) {
		t.Fatalf("partial ingestion notice: %v", n.notices)
	}
	if len(n.candidates) != 1 || n.candidates[0].Scope != got.ScopeID || n.candidates[0].Sources[0].Ref != got.ID {
		t.Fatalf("partial ingestion upload provenance: %+v", n.candidates)
	}
}

type testOCRPolicy func(docextract.Page) bool

func (p testOCRPolicy) PageNeedsOCR(page docextract.Page) bool { return p(page) }

func TestPDFOCRSelectionPolicy(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprintf("custom=%t", custom), func(t *testing.T) {
			o, s, b, _ := setup(t)
			reg := docextract.NewRegistry()
			if err := reg.Register(fakeExtractor{fn: func(context.Context, docextract.Options) (docextract.Document, error) {
				d := scanned(4)
				d.Pages[0].Quality = 1 // MethodNone still needs OCR.
				d.Pages[1] = native(2)
				d.Pages[1].Quality = 0.59
				d.Pages[2] = native(3)
				d.Pages[2].Quality = 0.6 // The threshold is exclusive.
				d.Pages[3] = native(4)
				return d, nil
			}}); err != nil {
				t.Fatal(err)
			}
			o.Registry, o.Renderer = reg, fakeRenderer{}
			var pages, checked []int
			if custom {
				o.Policy = testOCRPolicy(func(p docextract.Page) bool {
					checked = append(checked, p.Number)
					return p.Number == 4
				})
			}
			o.OCR = fakeOCR{fn: func(_ context.Context, p int) (docextract.Page, error) {
				pages = append(pages, p)
				return native(p), nil
			}}
			i := newIngester(t, o)
			d := submit(t, i, b, "%PDF selective OCR")
			start(t, i)
			waitState(t, s, d.ID, "ready")
			if err := i.Close(); err != nil {
				t.Fatal(err)
			}
			want := "[1 2]"
			if custom {
				want = "[4]"
				if fmt.Sprint(checked) != "[1 2 3 4]" {
					t.Fatalf("PDF policy did not inspect every page: %v", checked)
				}
			}
			if fmt.Sprint(pages) != want {
				t.Fatalf("OCR pages %v, want %s", pages, want)
			}
		})
	}
}

func TestImageZeroOCRBudget(t *testing.T) {
	t.Setenv("EXCALIBUR_LIBRARY_OCR_MAX_PAGES", "0")
	o, s, b, _ := setup(t)
	called := false
	o.OCR = fakeOCR{fn: func(_ context.Context, p int) (docextract.Page, error) {
		called = true
		return native(p), nil
	}}
	i := newIngester(t, o)
	d := submit(t, i, b, imageFixture(t))
	start(t, i)
	got := waitState(t, s, d.ID, "partial")
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	if called || len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "confirmation needed") {
		t.Fatalf("zero OCR budget: called=%t document=%+v", called, got)
	}
}
func TestRestartDurabilityReusesOCR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	q, closeDB := openQueue(t, path)
	s := newFakeStore()
	b := &fakeBlobs{data: map[string][]byte{}}
	reg := docextract.NewRegistry()
	_ = reg.Register(fakeExtractor{fn: func(ctx context.Context, _ docextract.Options) (docextract.Document, error) { return scanned(2), nil }})
	entered := make(chan struct{})
	var calls []int
	o := Options{Store: s, Queue: q, Blobs: b, Registry: reg, Renderer: fakeRenderer{}, OCR: fakeOCR{fn: func(ctx context.Context, p int) (docextract.Page, error) {
		calls = append(calls, p)
		if p == 2 {
			close(entered)
			<-ctx.Done()
			return docextract.Page{}, ctx.Err()
		}
		return native(p), nil
	}}}
	i := newIngester(t, o)
	d := submit(t, i, b, "%PDF restart")
	start(t, i)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	_ = i.Close()
	closeDB()
	q, closeAgain := openQueue(t, path)
	defer closeAgain()
	o.Queue = q
	o.OCR = fakeOCR{fn: func(ctx context.Context, p int) (docextract.Page, error) {
		calls = append(calls, p)
		return native(p), nil
	}}
	resumed := newIngester(t, o)
	start(t, resumed)
	waitState(t, s, d.ID, "ready")
	_ = resumed.Close()
	if fmt.Sprint(calls) != "[1 2 2]" {
		t.Fatal(calls)
	}
	j, err := q.Get(context.Background(), d.ID)
	if err != nil || j.Attempts != 0 {
		t.Fatalf("%+v %v", j, err)
	}
}
func TestPendingQueueRecovery(t *testing.T) {
	o, s, b, _ := setup(t)
	i := newIngester(t, o)
	d := submit(t, i, b, "durable queued input")
	j, err := o.Queue.Claim(context.Background())
	if err != nil || j == nil {
		t.Fatal(j, err)
	}
	resumed := newIngester(t, o)
	start(t, resumed)
	waitState(t, s, d.ID, "ready")
	_ = resumed.Close()
}

func TestRestartPreservesRetryBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retry-restart.db")
	q, closeDB := openQueue(t, path)
	s := newFakeStore()
	b := &fakeBlobs{data: map[string][]byte{}}
	reg := docextract.NewRegistry()
	entered := make(chan struct{})
	calls := 0
	if err := reg.Register(fakeExtractor{fn: func(ctx context.Context, _ docextract.Options) (docextract.Document, error) {
		calls++
		if calls == 3 {
			close(entered)
			<-ctx.Done()
			return docextract.Document{}, ctx.Err()
		}
		return docextract.Document{}, errors.New("extraction unavailable")
	}}); err != nil {
		t.Fatal(err)
	}
	o := Options{Store: s, Blobs: b, Queue: q, Registry: reg}
	i := newIngester(t, o)
	d := submit(t, i, b, "%PDF retry budget")
	start(t, i)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("last permitted attempt did not start")
	}
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	j, err := q.Get(context.Background(), d.ID)
	if err != nil || j.State != "pending" || j.Attempts != 2 {
		t.Fatalf("shutdown spent or reset retry budget: %+v %v", j, err)
	}
	closeDB()
	q, closeAgain := openQueue(t, path)
	defer closeAgain()
	o.Queue = q
	resumed := newIngester(t, o)
	start(t, resumed)
	got := waitState(t, s, d.ID, "failed")
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = q.Get(context.Background(), d.ID)
	if err != nil || j.State != "failed" || j.Attempts != 3 || calls != 4 || got.FailureStage != "extract" {
		t.Fatalf("recovery granted extra retries: job=%+v document=%+v calls=%d error=%v", j, got, calls, err)
	}
}

func TestRecoveryReconcilesCommittedDocument(t *testing.T) {
	for _, state := range []string{"ready", "partial"} {
		t.Run(state, func(t *testing.T) {
			o, s, b, n := setup(t)
			i := newIngester(t, o)
			d := submit(t, i, b, "committed before the job completed")
			if j, err := o.Queue.Claim(context.Background()); err != nil || j == nil {
				t.Fatalf("claim before simulated crash: %+v %v", j, err)
			}
			extracted := docextract.Document{Pages: []docextract.Page{native(1)}, PageCount: 1}
			if state == "partial" {
				extracted.Warnings = []string{"one figure could not be read"}
			}
			if _, err := s.IngestExtracted(context.Background(), d.ScopeID, d.ID, extracted); err != nil {
				t.Fatal(err)
			}
			// The original is deliberately unavailable: recovery must use the
			// committed document rather than repeat extraction and indexing.
			b.mu.Lock()
			delete(b.data, d.BlobDigest)
			b.mu.Unlock()
			resumed := newIngester(t, o)
			start(t, resumed)
			select {
			case got := <-n.updates:
				if got.Status != state {
					t.Fatalf("recovered status: %+v", got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("committed document was not reconciled")
			}
			if err := resumed.Close(); err != nil {
				t.Fatal(err)
			}
			j, err := o.Queue.Get(context.Background(), d.ID)
			if err != nil || j.State != state || j.Attempts != 0 || s.ingestCalls != 1 {
				t.Fatalf("committed document replayed: %+v calls=%d error=%v", j, s.ingestCalls, err)
			}
			n.mu.Lock()
			defer n.mu.Unlock()
			if len(n.notices) != 1 || len(n.candidates) != 1 {
				t.Fatalf("recovered terminal effects: notices=%v candidates=%v", n.notices, n.candidates)
			}
		})
	}
}

func TestProgressThrottled(t *testing.T) {
	o, s, b, n := setup(t)
	reg := docextract.NewRegistry()
	_ = reg.Register(fakeExtractor{fn: func(ctx context.Context, opts docextract.Options) (docextract.Document, error) {
		for k := 0; k < 1000; k++ {
			opts.Progress(k, 1000)
		}
		return scanned(0), nil
	}})
	o.Registry = reg
	i := newIngester(t, o)
	d := submit(t, i, b, "%PDF progress")
	start(t, i)
	waitState(t, s, d.ID, "ready")
	_ = i.Close()
	if len(n.updates) != 1 {
		t.Fatalf("%d progress updates", len(n.updates))
	}
}

func TestImageOCRAndDefaults(t *testing.T) {
	o, s, b, _ := setup(t)
	var called int
	o.OCR = fakeOCR{fn: func(ctx context.Context, p int) (docextract.Page, error) { called++; return native(p), nil }}
	i := newIngester(t, o)
	d := submit(t, i, b, imageFixture(t))
	start(t, i)
	waitState(t, s, d.ID, "ready")
	_ = i.Close()
	if called != 1 {
		t.Fatal(called)
	}
	t.Setenv("EXCALIBUR_LIBRARY_WORKERS", "10000")
	t.Setenv("EXCALIBUR_LIBRARY_OCR_MAX_PAGES", "-1")
	fallback := newIngester(t, o)
	if fallback.workers != 1 || fallback.ocrMax != 50 {
		t.Fatal(fallback.workers, fallback.ocrMax)
	}
}
func TestTransientFailureRecovers(t *testing.T) {
	o, s, b, _ := setup(t)
	calls := 0
	reg := docextract.NewRegistry()
	_ = reg.Register(fakeExtractor{fn: func(ctx context.Context, opts docextract.Options) (docextract.Document, error) {
		calls++
		if calls == 1 {
			return docextract.Document{}, errors.New("transient extraction error")
		}
		return docextract.Document{Format: "pdf", Pages: []docextract.Page{native(1)}, PageCount: 1}, nil
	}})
	o.Registry = reg
	i := newIngester(t, o)
	d := submit(t, i, b, "%PDF recover")
	start(t, i)
	waitState(t, s, d.ID, "ready")
	_ = i.Close()
	if calls != 2 {
		t.Fatal(calls)
	}
	j, err := o.Queue.Get(context.Background(), d.ID)
	if err != nil || j.State != "ready" || j.Attempts != 1 || j.Error != "" {
		t.Fatalf("successful retry retained a failure: %+v %v", j, err)
	}
}

func TestRecoveredSuccessClearsDurableError(t *testing.T) {
	for _, state := range []string{"ready", "partial"} {
		t.Run(state, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "recovered-error.db")
			q, closeDB := openQueue(t, path)
			s := newFakeStore()
			b := &fakeBlobs{data: map[string][]byte{}}
			reg := docextract.NewRegistry()
			n := &fakeNotifications{updates: make(chan memory.DocumentInfo, 1)}
			i := newIngester(t, Options{Store: s, Blobs: b, Queue: q, Registry: reg, Notifier: n})
			d := submit(t, i, b, "already indexed")
			j, err := q.Get(context.Background(), d.ID)
			if err != nil {
				t.Fatal(err)
			}
			j.State, j.Attempts, j.Error = "processing", 1, "temporary extraction failure"
			if err := q.Save(context.Background(), j); err != nil {
				t.Fatal(err)
			}
			// Simulate a crash after indexing commits but before queue completion.
			s.mu.Lock()
			d.Status = state
			s.docs[d.ID] = d
			s.mu.Unlock()
			start(t, i)
			select {
			case <-time.After(3 * time.Second):
				t.Fatal("recovery did not finish")
			case got := <-n.updates:
				if got.Status != state {
					t.Fatalf("recovered status = %s, want %s", got.Status, state)
				}
			}
			_ = i.Close()
			closeDB()
			q, closeAgain := openQueue(t, path)
			defer closeAgain()
			j, err = q.Get(context.Background(), d.ID)
			if err != nil || j.State != state || j.Attempts != 1 || j.Error != "" {
				t.Fatalf("recovered success retained a durable failure: %+v %v", j, err)
			}
		})
	}
}

func TestConfiguredWorkersBoundConcurrentJobs(t *testing.T) {
	t.Setenv("EXCALIBUR_LIBRARY_WORKERS", "2")
	o, s, b, _ := setup(t)
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	var mu sync.Mutex
	active, peak := 0, 0
	reg := docextract.NewRegistry()
	if err := reg.Register(fakeExtractor{fn: func(ctx context.Context, _ docextract.Options) (docextract.Document, error) {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return docextract.Document{}, ctx.Err()
		case <-release:
			return docextract.Document{Pages: []docextract.Page{native(1)}, PageCount: 1}, nil
		}
	}}); err != nil {
		t.Fatal(err)
	}
	o.Registry = reg
	i := newIngester(t, o)
	docs := make([]memory.DocumentInfo, 3)
	for k := range docs {
		docs[k] = submit(t, i, b, fmt.Sprintf("%%PDF worker %d", k))
	}
	start(t, i)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for k := 0; k < 2; k++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("configured workers did not run concurrently")
		}
	}
	states := map[string]int{}
	for _, d := range docs {
		j, err := o.Queue.Get(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		states[j.State]++
	}
	if states["processing"] != 2 || states["pending"] != 1 {
		t.Fatalf("worker pool exceeded its limit: %v", states)
	}
	close(release)
	for _, d := range docs {
		waitState(t, s, d.ID, "ready")
	}
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if peak != 2 || active != 0 {
		t.Fatalf("peak=%d active=%d", peak, active)
	}
}

func TestRealMemoryContractWhenIntegrated(t *testing.T) {
	if _, ok := any(memory.NewService(nil)).(Store); !ok {
		t.Skip("library-core CONTRACT section 2 is provided at integration")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "real.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := NewSQLQueue(db)
	if err = db.Write(ctx, store.PriorityNormal, migrations.LibraryJobs().Up); err != nil {
		t.Fatal(err)
	}
	svc := memory.NewService(db, memory.WithScopes("project:one"))
	mem := any(svc).(Store)
	if err := memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	reg := docextract.NewRegistry()
	if err := RegisterDefaultExtractors(reg); err != nil {
		t.Fatal(err)
	}
	i := newIngester(t, Options{Store: mem, Queue: q, Blobs: q.DB.Blob(), Registry: reg, Proposer: svc})
	path := filepath.Join(t.TempDir(), "real.txt")
	if err := os.WriteFile(path, []byte("A document with exact citation text."), 0600); err != nil {
		t.Fatal(err)
	}
	d, _, err := i.Submit(ctx, IngestRequest{Scope: "project:one", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	start(t, i)
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		doc, err := mem.GetDocument(ctx, []string{"project:one"}, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Status == "ready" {
			break
		}
		if doc.Status == "failed" {
			t.Fatal(doc)
		}
		select {
		case <-deadline:
			t.Fatal("real ingestion timed out")
		case <-tick.C:
		}
	}
	if err = i.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractorPanicDoesNotRetireWorker(t *testing.T) {
	o, s, b, _ := setup(t)
	reg := docextract.NewRegistry()
	_ = reg.Register(fakeExtractor{fn: func(context.Context, docextract.Options) (docextract.Document, error) { panic("bad extractor") }})
	o.Registry = reg
	i := newIngester(t, o)
	d := submit(t, i, b, "%PDF panic")
	start(t, i)
	failed := waitState(t, s, d.ID, "failed")
	_ = i.Close()
	if failed.FailureStage != "extract" {
		t.Fatal(failed)
	}
	j, err := o.Queue.Get(context.Background(), d.ID)
	if err != nil || j.Attempts != 3 {
		t.Fatalf("%+v %v", j, err)
	}
}

type panicReadBlobs struct{ BlobStore }

func (panicReadBlobs) Open(context.Context, string) (io.ReadCloser, error) {
	panic("blob reader failed")
}

type panicRenderer struct{}

func (panicRenderer) RenderPNG(context.Context, io.ReaderAt, int64, int, int) ([]byte, error) {
	panic("renderer failed")
}

type panicIndexStore struct{ Store }

func (panicIndexStore) IngestExtracted(context.Context, string, string, docextract.Document) (memory.DocumentInfo, error) {
	panic("indexer failed")
}

func TestPipelinePanicPreservesFailureStage(t *testing.T) {
	for _, component := range []string{"read", "renderer", "ocr", "store"} {
		t.Run(component, func(t *testing.T) {
			o, s, b, n := setup(t)
			reg := docextract.NewRegistry()
			if err := reg.Register(fakeExtractor{fn: func(context.Context, docextract.Options) (docextract.Document, error) {
				return scanned(1), nil
			}}); err != nil {
				t.Fatal(err)
			}
			o.Registry = reg
			o.Renderer = fakeRenderer{}
			o.OCR = fakeOCR{fn: func(_ context.Context, p int) (docextract.Page, error) { return native(p), nil }}
			stage := component
			switch component {
			case "read":
				o.Blobs = panicReadBlobs{o.Blobs}
			case "renderer":
				stage = "ocr"
				o.Renderer = panicRenderer{}
			case "ocr":
				o.OCR = fakeOCR{fn: func(context.Context, int) (docextract.Page, error) { panic("OCR failed") }}
			case "store":
				o.Store = panicIndexStore{o.Store}
			}
			i := newIngester(t, o)
			d := submit(t, i, b, "%PDF panic "+component)
			start(t, i)
			failed := waitState(t, s, d.ID, "failed")
			if err := i.Close(); err != nil {
				t.Fatal(err)
			}
			j, err := o.Queue.Get(context.Background(), d.ID)
			if err != nil || j.State != "failed" || j.Attempts != 3 || failed.FailureStage != stage {
				t.Fatalf("document=%+v job=%+v err=%v; want stage %s", failed, j, err, stage)
			}
			n.mu.Lock()
			defer n.mu.Unlock()
			if len(n.notices) != 1 || !strings.Contains(n.notices[0], "at "+stage+":") || len(n.candidates) != 0 {
				t.Fatalf("notices=%v proposals=%v", n.notices, n.candidates)
			}
		})
	}
}

func imageFixture(t *testing.T) string {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
