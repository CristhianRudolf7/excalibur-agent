// Package library runs durable, cancellable background document ingestion.
package library

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

const MaxInputBytes = 64 << 20

// Store is the CONTRACT section 2 subset used by ingestion. Implementations
// enforce scope authorization and preserve document IDs across extraction.
type Store interface {
	CreatePendingDocument(context.Context, string, memory.DocMeta) (memory.DocumentInfo, bool, error)
	SetDocumentProgress(context.Context, string, string, int, int, string, string) error
	IngestExtracted(context.Context, string, string, docextract.Document) (memory.DocumentInfo, error)
	GetDocument(context.Context, []string, string) (memory.DocumentInfo, error)
}
type BlobStore interface {
	Put(context.Context, io.Reader) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
}
type Extractors interface {
	Get(string) (docextract.Extractor, error)
}
type Notifier interface{ DocumentUpdated(memory.DocumentInfo) }
type Notices interface {
	PostDocumentNotice(scope, id, content string)
}
type MemoryProposer interface {
	Propose(context.Context, memory.Candidate) (memory.Memory, error)
}

// OCRPolicy lets the PDF reader supply its richer page-quality decision.
type OCRPolicy interface{ PageNeedsOCR(docextract.Page) bool }

// PageCache persists completed OCR results before advancing the durable cursor.
type PageCache interface {
	LoadPage(context.Context, string, int) (docextract.Page, bool, error)
	SavePage(context.Context, string, docextract.Page) error
}

type IngestRequest struct {
	Scope string
	Meta  memory.DocMeta
	Path  string
}
type Options struct {
	Store    Store
	Blobs    BlobStore
	Queue    Queue
	Registry Extractors
	OCR      docextract.OCR
	Renderer docextract.Renderer
	Policy   OCRPolicy
	Notifier Notifier
	Notices  Notices
	Proposer MemoryProposer
	Workers  int
	// OCRMaxPages nil reads the environment; a pointer to zero disables OCR.
	OCRMaxPages *int
}

type Ingester struct {
	opts            Options
	workers, ocrMax int
	mu              sync.Mutex
	active          map[string]context.CancelFunc
	cancel          context.CancelFunc
	started         bool
	wake            chan struct{}
	done            chan struct{}
	wg              sync.WaitGroup
	submitMu        sync.Mutex
}

func New(opts Options) (*Ingester, error) {
	if opts.Store == nil || opts.Blobs == nil || opts.Queue == nil || opts.Registry == nil {
		return nil, errors.New("library: store, blobs, queue and registry are required")
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = envInt("EXCALIBUR_LIBRARY_WORKERS", 1, 1, 8)
	}
	if workers > 8 {
		workers = 8
	}
	max := envInt("EXCALIBUR_LIBRARY_OCR_MAX_PAGES", 50, 0, 2000)
	if opts.OCRMaxPages != nil {
		max = *opts.OCRMaxPages
	}
	if max < 0 {
		return nil, errors.New("library: negative OCR budget")
	}
	return &Ingester{opts: opts, workers: workers, ocrMax: max, active: map[string]context.CancelFunc{}, wake: make(chan struct{}, 1), done: make(chan struct{})}, nil
}
func envInt(name string, def, min, max int) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil || v < min || v > max {
		return def
	}
	return v
}
func Enabled() bool {
	return os.Getenv("EXCALIBUR_LIBRARY_INGEST") != "0" && !strings.EqualFold(os.Getenv("EXCALIBUR_LIBRARY_INGEST"), "false")
}

// Submit persists the original before referencing it and returns without running
// extraction. The request context belongs only to submission, never to the job.
func (i *Ingester) Submit(ctx context.Context, req IngestRequest) (memory.DocumentInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return memory.DocumentInfo{}, false, err
	}
	if !Enabled() {
		return memory.DocumentInfo{}, false, errors.New("library ingestion is disabled")
	}
	if req.Scope == "" {
		return memory.DocumentInfo{}, false, memory.ErrNoScope
	}
	if (req.Path == "") == (req.Meta.BlobDigest == "") {
		return memory.DocumentInfo{}, false, errors.New("library: provide exactly one of path or blob_digest")
	}
	if req.Path != "" {
		f, err := os.Open(req.Path)
		if err != nil {
			return memory.DocumentInfo{}, false, err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return memory.DocumentInfo{}, false, err
		}
		if !st.Mode().IsRegular() || st.Size() > MaxInputBytes {
			return memory.DocumentInfo{}, false, errors.New("library: input must be a regular file of at most 64 MiB")
		}
		raw, err := io.ReadAll(io.LimitReader(f, MaxInputBytes+1))
		if err != nil {
			return memory.DocumentInfo{}, false, err
		}
		if len(raw) > MaxInputBytes {
			return memory.DocumentInfo{}, false, errors.New("library: input exceeds 64 MiB")
		}
		req.Meta.BlobDigest, err = i.opts.Blobs.Put(ctx, bytes.NewReader(raw))
		if err != nil {
			return memory.DocumentInfo{}, false, err
		}
		req.Meta.Size = int64(len(raw))
		if req.Meta.SourceName == "" {
			req.Meta.SourceName = filepath.Base(req.Path)
		}
		req.Meta.Format = docextract.Sniff(raw, req.Meta.SourceName, req.Meta.MIME)
	} else {
		b, err := hex.DecodeString(req.Meta.BlobDigest)
		if err != nil || len(b) != 32 || strings.ToLower(req.Meta.BlobDigest) != req.Meta.BlobDigest {
			return memory.DocumentInfo{}, false, errors.New("library: invalid SHA-256 blob digest")
		}
	}
	if req.Meta.Title == "" {
		req.Meta.Title = req.Meta.SourceName
	}
	if req.Meta.Title == "" {
		req.Meta.Title = "Uploaded document"
	}
	i.submitMu.Lock()
	defer i.submitMu.Unlock()
	doc, exists, err := i.opts.Store.CreatePendingDocument(ctx, req.Scope, req.Meta)
	if err != nil {
		return doc, exists, err
	}
	if !exists || doc.Status == "pending" || doc.Status == "processing" {
		now := time.Now().UTC()
		err = i.opts.Queue.Put(ctx, Job{ID: doc.ID, DocID: doc.ID, ScopeID: req.Scope, BlobDigest: req.Meta.BlobDigest, State: "pending", NextPage: 1, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return doc, exists, err
		}
		i.signal()
	}
	return doc, exists, nil
}
func (i *Ingester) signal() {
	select {
	case i.wake <- struct{}{}:
	default:
	}
}

// Start resumes unfinished jobs. Canceling ctx stops workers, preserving their
// jobs for another process; Cancel explicitly cancels one document permanently.
func (i *Ingester) Start(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.started {
		return errors.New("library: ingester already started")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !Enabled() {
		return errors.New("library ingestion is disabled")
	}
	if err := i.opts.Queue.Recover(ctx); err != nil {
		return err
	}
	ctx, i.cancel = context.WithCancel(ctx)
	i.started = true
	for n := 0; n < i.workers; n++ {
		i.wg.Add(1)
		safego.Go("library-worker", nil, func() { defer i.wg.Done(); i.worker(ctx) })
	}
	safego.Go("library-join", nil, func() { i.wg.Wait(); close(i.done) })
	return nil
}
func (i *Ingester) Done() <-chan struct{} { return i.done }
func (i *Ingester) Close() error {
	i.mu.Lock()
	cancel := i.cancel
	started := i.started
	i.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if started {
		<-i.done
	}
	return nil
}

func (i *Ingester) Cancel(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Serialize against claim and worker completion so cancellation cannot race
	// a successful commit into a contradictory terminal state.
	i.mu.Lock()
	defer i.mu.Unlock()
	j, err := i.opts.Queue.Get(ctx, id)
	if err != nil {
		return err
	}
	if j.State == "ready" || j.State == "partial" || j.State == "failed" {
		return nil
	}
	if j.State == "canceled" {
		// A previous call may have committed the queue cancellation before
		// its document update failed. Retrying must finish that update.
		doc, err := i.opts.Store.GetDocument(ctx, []string{j.ScopeID}, id)
		if err != nil {
			return err
		}
		if doc.Status == "canceled" {
			return nil
		}
	} else {
		j.State = "canceled"
		if err = i.opts.Queue.Save(ctx, j); err != nil {
			return err
		}
	}
	if stop := i.active[id]; stop != nil {
		stop()
	}
	if err = i.opts.Store.SetDocumentProgress(ctx, id, "canceled", 0, 0, "", ""); err != nil {
		return err
	}
	i.notify(ctx, j)
	return nil
}
func (i *Ingester) worker(ctx context.Context) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		i.mu.Lock()
		j, err := i.opts.Queue.Claim(ctx)
		var jobCtx context.Context
		var cancel context.CancelFunc
		if err == nil && j != nil {
			jobCtx, cancel = context.WithCancel(ctx)
			i.active[j.DocID] = cancel
		}
		i.mu.Unlock()
		if err != nil || j == nil {
			select {
			case <-ctx.Done():
				return
			case <-i.wake:
			case <-tick.C:
			}
			continue
		}
		i.run(jobCtx, ctx, *j)
		cancel()
	}
}

// FailureStage lets ingestion preserve a chunk/embed/store error from the store.
type FailureStage interface{ FailureStage() string }

func (i *Ingester) run(ctx, parent context.Context, j Job) {
	doc, stage, err := i.safePipeline(ctx, j)
	i.mu.Lock()
	defer i.mu.Unlock()
	defer func() { delete(i.active, j.DocID) }()
	persist, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	current, getErr := i.opts.Queue.Get(persist, j.DocID)
	if getErr != nil {
		return
	}
	if current.State == "canceled" {
		// An atomic indexing commit may finish after Cancel persisted the
		// canceled status. Reconcile after the pipeline has stopped so that
		// commit cannot leave the document ready while its job is canceled.
		info, e := i.opts.Store.GetDocument(persist, []string{j.ScopeID}, j.DocID)
		if e == nil && info.Status != "canceled" {
			if e = i.opts.Store.SetDocumentProgress(persist, j.DocID, "canceled", 0, 0, "", ""); e == nil {
				i.notify(persist, j)
			}
		}
		return
	}
	j.NextPage = current.NextPage
	if parent.Err() != nil && err != nil {
		j.State = "pending"
		_ = i.opts.Queue.Save(persist, j)
		return
	}
	if err != nil {
		j.Attempts++
		j.Error = err.Error()
		if j.Attempts <= 2 {
			j.State = "pending"
			_ = i.opts.Queue.Save(persist, j)
			i.signal()
			return
		}
		j.State = "failed"
		if e := i.opts.Store.SetDocumentProgress(persist, j.DocID, "failed", 0, 0, stage, err.Error()); e != nil {
			return
		}
	} else {
		j.Error = ""
		j.State = doc.Status
		if j.State != "partial" {
			j.State = "ready"
		}
	}
	if err = i.opts.Queue.Save(persist, j); err != nil {
		return
	}
	doc = i.notify(persist, j)
	if i.opts.Notices != nil {
		message := fmt.Sprintf("Finished reading “%s” (%d pages)", doc.Title, doc.PageCount)
		if j.State == "partial" {
			message = fmt.Sprintf("Partially read “%s”: %s", doc.Title, strings.Join(doc.Warnings, "; "))
		}
		if j.State == "failed" {
			message = fmt.Sprintf("Failed reading “%s” at %s: %s", doc.Title, doc.FailureStage, doc.Error)
		}
		i.opts.Notices.PostDocumentNotice(j.ScopeID, j.DocID, message)
	}
	if i.opts.Proposer != nil && (j.State == "ready" || j.State == "partial") {
		_, _ = i.opts.Proposer.Propose(persist, memory.Candidate{Scope: j.ScopeID, Kind: memory.KindFact, Statement: fmt.Sprintf("The user uploaded the document %q on %s.", doc.Title, doc.UploadedAt.UTC().Format("2006-01-02")), Trust: memory.TrustAgent, Sources: []memory.Source{{Kind: "document", Ref: j.DocID}}, Conversation: "library:" + j.DocID, Confidence: 1})
	}
}
func (i *Ingester) notify(ctx context.Context, j Job) memory.DocumentInfo {
	doc, err := i.opts.Store.GetDocument(ctx, []string{j.ScopeID}, j.DocID)
	if err == nil && i.opts.Notifier != nil {
		i.opts.Notifier.DocumentUpdated(doc)
	}
	return doc
}

// safePipeline keeps a malformed dependency from retiring a pool worker and
// records the operation that failed for the terminal document and notice.
func (i *Ingester) safePipeline(ctx context.Context, j Job) (out memory.DocumentInfo, stage string, err error) {
	stage = "read"
	defer func() {
		if recovered := recover(); recovered != nil {
			out = memory.DocumentInfo{}
			err = fmt.Errorf("library: %s pipeline panicked: %v", stage, recovered)
		}
	}()
	fail := func(stage string, err error) (memory.DocumentInfo, string, error) {
		return memory.DocumentInfo{}, stage, err
	}
	info, err := i.opts.Store.GetDocument(ctx, []string{j.ScopeID}, j.DocID)
	if err != nil {
		return fail("read", err)
	}
	// Reconcile the narrow crash window after IngestExtracted committed.
	if info.Status == "ready" || info.Status == "partial" {
		return info, "", nil
	}
	stage = "store"
	if err = i.opts.Store.SetDocumentProgress(ctx, j.DocID, "processing", 0, 0, "", ""); err != nil {
		return fail("store", err)
	}
	stage = "read"
	rc, err := i.opts.Blobs.Open(ctx, j.BlobDigest)
	if err != nil {
		return fail("read", err)
	}
	data, err := io.ReadAll(io.LimitReader(rc, MaxInputBytes+1))
	closeErr := rc.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fail("read", err)
	}
	if len(data) > MaxInputBytes {
		return fail("read", errors.New("library: input exceeds 64 MiB"))
	}
	stage = "extract"
	mediaType := info.MIME
	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}
	format := docextract.Sniff(data, info.SourceName, mediaType)
	var doc docextract.Document
	var progressMu sync.Mutex
	last := time.Now()
	progress := func(done, total int) {
		progressMu.Lock()
		defer progressMu.Unlock()
		if time.Since(last) < time.Second {
			return
		}
		last = time.Now()
		if i.opts.Store.SetDocumentProgress(ctx, j.DocID, "processing", done, total, "", "") == nil {
			i.notify(ctx, j)
		}
	}
	ex, err := i.opts.Registry.Get(format)
	if err != nil {
		return fail("extract", err)
	}
	doc, err = ex.Extract(ctx, bytes.NewReader(data), int64(len(data)), docextract.Options{MaxPages: 2000, Progress: progress})
	if err != nil {
		return fail("extract", err)
	}

	if len(doc.Pages) > 2000 {
		return fail("extract", errors.New("library: too many pages"))
	}
	if doc.Title == "" {
		doc.Title = info.Title
	}
	doc.Format = format
	used := 0
	for n, p := range doc.Pages {
		stage = "ocr"
		if err = ctx.Err(); err != nil {
			return fail("ocr", err)
		}
		need := format == "png" || format == "jpeg" || format == "pdf" && (p.Method == docextract.MethodNone || p.Quality < 0.6)
		if format == "pdf" && i.opts.Policy != nil {
			need = i.opts.Policy.PageNeedsOCR(p)
		}
		if !need {
			continue
		}
		used++
		if used > i.ocrMax {
			doc.Warnings = append(doc.Warnings, "OCR page budget exceeded; confirmation needed to process remaining pages")
			break
		}
		cache, _ := i.opts.Queue.(PageCache)
		if cache != nil {
			stage = "store"
			cached, ok, e := cache.LoadPage(ctx, j.ID, p.Number)
			if e != nil {
				return fail("store", e)
			}
			if ok {
				doc.Pages[n] = cached
				continue
			}
		}
		stage = "ocr"
		if i.opts.OCR == nil {
			doc.Warnings = append(doc.Warnings, fmt.Sprintf("Page %d needs OCR, but OCR is unavailable", p.Number))
			continue
		}
		png := data
		if format == "pdf" {
			if i.opts.Renderer == nil {
				doc.Warnings = append(doc.Warnings, fmt.Sprintf("Page %d needs OCR, but rendering is unavailable", p.Number))
				continue
			}
			png, err = i.opts.Renderer.RenderPNG(ctx, bytes.NewReader(data), int64(len(data)), p.Number, 144)
			if err != nil {
				return fail("ocr", err)
			}
		}
		result, err := i.opts.OCR.OCRPage(ctx, png, p.Number)
		if err != nil {
			return fail("ocr", err)
		}
		result.Number = p.Number
		result.Label = p.Label
		if result.Width == 0 {
			result.Width = p.Width
		}
		if result.Height == 0 {
			result.Height = p.Height
		}
		stage = "store"
		if cache != nil {
			if err = cache.SavePage(ctx, j.ID, result); err != nil {
				return fail("store", err)
			}
		}
		doc.Pages[n] = result
		j.NextPage = p.Number + 1
		if err = i.opts.Queue.Save(ctx, j); err != nil {
			return fail("store", err)
		}
		progress(n+1, len(doc.Pages))
	}
	if err = ctx.Err(); err != nil {
		return fail("store", err)
	}
	stage = "store"
	result, err := i.opts.Store.IngestExtracted(ctx, j.ScopeID, j.DocID, doc)
	if err != nil {
		stage := "store"
		var staged FailureStage
		if errors.As(err, &staged) {
			switch staged.FailureStage() {
			case "chunk", "embed", "store":
				stage = staged.FailureStage()
			}
		}
		return fail(stage, err)
	}
	return result, "", nil
}

// SubmitLibrary adapts the memory tool binding without making memory import library.
func (i *Ingester) SubmitLibrary(ctx context.Context, scope string, meta memory.DocMeta, path string) (memory.DocumentInfo, bool, error) {
	return i.Submit(ctx, IngestRequest{Scope: scope, Meta: meta, Path: path})
}
