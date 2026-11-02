package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/visionocr"
	"github.com/CristhianRudolf7/excalibur-agent/internal/library"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"io"
	"mime"
	"os"
	"strings"
)

// LibraryBackend is the client boundary. Implementations must deduplicate Submit
// by digest within scope and return typed api.Errors for domain failures.
// The adapter accepts callbacks because the memory/ingester types land separately.
type LibraryBackend interface {
	Submit(context.Context, api.LibraryIngestParams) (api.DocumentInfo, bool, error)
	ListDocuments(context.Context, []string, api.LibraryListParams) ([]api.DocumentInfo, string, error)
	GetDocument(context.Context, []string, string) (api.DocumentInfo, error)
	DocumentPages(context.Context, []string, string, int, int, int) ([]docextract.Page, error)
	ChunkDetail(context.Context, []string, string, string) (api.ChunkDetail, error)
	PurgeDocument(context.Context, string, string) error
	Cancel(string) error
}

// LibraryRoutes enforces server-configured scopes before every operation.
// Empty scopes deny access; clients cannot grant themselves a scope.
type LibraryRoutes struct {
	Backend      LibraryBackend
	Scopes       []string
	DefaultScope string
}

func libraryError(err error) error {
	if err == nil {
		return nil
	}
	var ae *api.Error
	if errors.As(err, &ae) {
		return ae
	}
	if errors.Is(err, memory.ErrScopeDenied) || errors.Is(err, memory.ErrNoScope) {
		return &api.Error{Code: api.CodeScopeDenied, Message: "library scope denied"}
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, os.ErrNotExist) || errors.Is(err, memory.ErrNotFound) {
		return &api.Error{Code: api.CodeUnknownTarget, Message: "document not found"}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &api.Error{Code: api.CodeConflict, Message: "library request canceled"}
	}
	return &api.Error{Code: api.CodeInternal, Message: "library operation failed"}
}
func strictLibraryParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return &api.Error{Code: api.CodeInvalidParams, Message: "expected parameter object"}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return &api.Error{Code: api.CodeInvalidParams, Message: "invalid library parameters"}
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return &api.Error{Code: api.CodeInvalidParams, Message: "invalid library parameters"}
	}
	return nil
}
func (r *LibraryRoutes) scope(scope string) ([]string, error) {
	if scope == "" {
		return append([]string(nil), r.Scopes...), nil
	}
	for _, s := range r.Scopes {
		if s == scope {
			return []string{s}, nil
		}
	}
	return nil, &api.Error{Code: api.CodeScopeDenied, Message: "library scope denied"}
}
func (r *LibraryRoutes) Op(ctx context.Context, method string, raw json.RawMessage) (result any, err error) {
	defer func() { err = libraryError(err) }()
	switch method {
	case api.MethodLibraryIngest, api.MethodLibraryList, api.MethodLibraryGet, api.MethodLibraryPages, api.MethodLibraryChunk, api.MethodLibraryDelete, api.MethodLibraryCancel:
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown library method"}
	}
	if strings.EqualFold(os.Getenv("EXCALIBUR_LIBRARY_API"), "false") || os.Getenv("EXCALIBUR_LIBRARY_API") == "0" {
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "library API disabled"}
	}
	if r == nil || r.Backend == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "library backend unavailable"}
	}
	if len(r.Scopes) == 0 {
		return nil, &api.Error{Code: api.CodeScopeDenied, Message: "library scope denied"}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	bad := func() error { return &api.Error{Code: api.CodeInvalidParams, Message: "invalid library parameters"} }
	switch method {
	case api.MethodLibraryIngest:
		var p api.LibraryIngestParams
		if err = strictLibraryParams(raw, &p); err != nil {
			return nil, err
		}
		p.BlobDigest = strings.ToLower(p.BlobDigest)
		digest, e := hex.DecodeString(p.BlobDigest)
		if e != nil || len(digest) != 32 || strings.TrimSpace(p.Name) == "" || len(p.Name) > 1024 || len(p.Title) > 4096 {
			return nil, bad()
		}
		if _, _, e = mime.ParseMediaType(p.MIME); e != nil || !strings.Contains(p.MIME, "/") {
			return nil, bad()
		}
		if p.ScopeID == "" {
			p.ScopeID = r.DefaultScope
			if p.ScopeID == "" {
				p.ScopeID = r.Scopes[0]
			}
		}
		if _, err = r.scope(p.ScopeID); err != nil {
			return nil, err
		}
		doc, existing, e := r.Backend.Submit(ctx, p)
		return api.LibraryIngestResult{Document: doc, Existing: existing}, e
	case api.MethodLibraryList:
		var p api.LibraryListParams
		if err = strictLibraryParams(raw, &p); err != nil {
			return nil, err
		}
		if p.Limit < 0 || p.Limit > 100 || len(p.Cursor) > 4096 || len(p.Query) > 4096 {
			return nil, bad()
		}
		if p.Limit == 0 {
			p.Limit = 50
		}
		switch p.Status {
		case "", "pending", "processing", "ready", "partial", "failed", "canceled":
		default:
			return nil, bad()
		}
		scopes, e := r.scope(p.ScopeID)
		if e != nil {
			return nil, e
		}
		docs, next, e := r.Backend.ListDocuments(ctx, scopes, p)
		if docs == nil {
			docs = []api.DocumentInfo{}
		}
		return api.LibraryListResult{Documents: docs, NextCursor: next}, e
	case api.MethodLibraryChunk:
		var p api.LibraryChunkParams
		if err = strictLibraryParams(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.DocumentID) == "" || strings.TrimSpace(p.ChunkID) == "" {
			return nil, bad()
		}
		if _, err = r.document(ctx, p.DocumentID); err != nil {
			return nil, err
		}
		chunk, e := r.Backend.ChunkDetail(ctx, r.Scopes, p.DocumentID, p.ChunkID)
		return api.LibraryChunkResult{Chunk: chunk}, e
	case api.MethodLibraryPages:
		var p api.LibraryPagesParams
		if err = strictLibraryParams(raw, &p); err != nil {
			return nil, err
		}
		if p.ID == "" || p.Version < 0 || p.From < 1 || p.To < p.From || p.To-p.From >= 20 {
			return nil, bad()
		}
		doc, e := r.document(ctx, p.ID)
		if e != nil {
			return nil, e
		}
		pages, e := r.Backend.DocumentPages(ctx, r.Scopes, p.ID, p.Version, p.From, p.To)
		if pages == nil {
			pages = []docextract.Page{}
		}
		return api.LibraryPagesResult{Document: doc, Pages: pages}, e
	default:
		var p api.LibraryIDParams
		if err = strictLibraryParams(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" {
			return nil, bad()
		}
		doc, e := r.document(ctx, p.ID)
		if e != nil {
			return nil, e
		}
		switch method {
		case api.MethodLibraryDelete:
			if e = r.Backend.PurgeDocument(ctx, p.ID, "client library.delete"); e != nil {
				return nil, e
			}
			return api.LibraryDeleteResult{Deleted: true, Notice: memory.BackupNotice}, nil
		case api.MethodLibraryCancel:
			if e = r.Backend.Cancel(p.ID); e != nil {
				return nil, e
			}
			doc, e = r.document(ctx, p.ID)
			return api.LibraryDocumentResult{Document: doc}, e
		default:
			return api.LibraryDocumentResult{Document: doc}, nil
		}
	}
}
func (r *LibraryRoutes) document(ctx context.Context, id string) (api.DocumentInfo, error) {
	d, e := r.Backend.GetDocument(ctx, r.Scopes, id)
	if e != nil {
		return d, e
	}
	if _, e = r.scope(d.ScopeID); e != nil {
		return api.DocumentInfo{}, e
	}
	if d.ScopeID == "" {
		return api.DocumentInfo{}, &api.Error{Code: api.CodeScopeDenied, Message: "document has no scope"}
	}
	return d, nil
}

// LibraryNotifier publishes progress and terminal metadata on the shared bus.
// Integration converts memory.DocumentInfo into the stable client DTO.
type LibraryNotifier struct{ Bus *telemetry.Bus }

func (n LibraryNotifier) DocumentUpdated(doc api.DocumentInfo) {
	if n.Bus != nil {
		_, _ = n.Bus.Publish(api.EventLibraryDocumentUpdated, nil, nil, doc)
	}
}

// LibraryOptions connects the document-library worker to daemon services. The
// registry hook is also the integration point for PDF/Office extractors.
type LibraryOptions struct {
	Media    *tools.MediaDeps
	DB       *store.DB
	Memory   *memory.Service
	Registry *docextract.Registry
	OCR      docextract.OCR
	Renderer docextract.Renderer
	Policy   library.OCRPolicy
	Updated  library.Notifier
	Notices  *master.Notifier
	Bus      *telemetry.Bus
}

// ConfigureLibrary prepares the worker before daemon Start. It fails explicitly
// when called without the CONTRACT section 2 implementation from library-core.
func (s *Services) ConfigureLibrary(opts LibraryOptions) error {
	if !library.Enabled() {
		return nil
	}
	if opts.DB == nil || opts.Memory == nil {
		return fmt.Errorf("daemon library: persistent store and memory are required")
	}
	mem, ok := any(opts.Memory).(library.Store)
	if !ok {
		return fmt.Errorf("daemon library: memory service does not implement the document-library contract")
	}
	reg := opts.Registry
	if reg == nil {
		reg = docextract.NewRegistry()
		if err := library.RegisterDefaultExtractors(reg); err != nil {
			return err
		}
	}
	if opts.Renderer == nil {
		opts.Renderer = pdf.New(pdf.Options{})
	}
	if opts.Policy == nil {
		opts.Policy = library.PDFPolicy{}
	}
	if opts.OCR == nil && opts.Media != nil {
		if backend, ok := opts.Media.Vision.(visionocr.Backend); ok {
			opts.OCR = visionocr.New(backend)
		}
	}
	if opts.Updated == nil {
		opts.Updated = LibraryServiceNotifier[memory.DocumentInfo]{Notifier: LibraryNotifier{Bus: opts.Bus}, Convert: LibraryDocumentInfo}
	}
	worker, err := library.New(library.Options{Store: mem, Blobs: opts.DB.Blob(), Queue: library.NewSQLQueue(opts.DB), Registry: reg, OCR: opts.OCR, Renderer: opts.Renderer, Policy: opts.Policy, Notifier: opts.Updated, Notices: libraryNoticeAdapter{notifier: opts.Notices, bus: opts.Bus}, Proposer: opts.Memory})
	if err != nil {
		return err
	}
	s.Library = worker
	opts.Memory.BindLibraryIngester(worker)
	return nil
}

type libraryNoticeAdapter struct {
	notifier *master.Notifier
	bus      *telemetry.Bus
}

func (a libraryNoticeAdapter) PostDocumentNotice(scope, id, content string) {
	if a.notifier != nil {
		a.notifier.Post(master.Notice{ID: "library:" + id, Class: types.NoticeNormal, Content: content})
	}
	if a.bus != nil {
		_, _ = a.bus.Publish(types.EventNoticePosted, nil, nil, api.NoticeEvent{Content: content})
	}
}

// LibraryDocumentInfo converts native metadata to the stable client contract.
func LibraryDocumentInfo(d memory.DocumentInfo) api.DocumentInfo { return api.DocumentInfo(d) }

// LibraryRoutesFor binds the real index and durable worker to authorized scopes.
func LibraryRoutesFor(mem *memory.Service, ingester *library.Ingester, scopes []string) *LibraryRoutes {
	if mem == nil || ingester == nil {
		return nil
	}
	binding := NewLibraryServiceBinding[memory.DocumentInfo, memory.ListQuery, memory.ChunkDetail, library.IngestRequest](
		mem, ingester,
		func(p api.LibraryIngestParams) library.IngestRequest {
			return library.IngestRequest{Scope: p.ScopeID, Meta: memory.DocMeta{BlobDigest: p.BlobDigest, SourceName: p.Name, MIME: p.MIME, Title: p.Title, Origin: "upload"}}
		},
		func(p api.LibraryListParams) memory.ListQuery {
			return memory.ListQuery{Query: p.Query, Status: p.Status, Limit: p.Limit, Cursor: p.Cursor}
		},
		LibraryDocumentInfo,
		func(c memory.ChunkDetail) api.ChunkDetail {
			return api.ChunkDetail{ChunkID: c.ChunkID, DocumentID: c.DocumentID, Version: c.Version, Page: c.Page, PageLabel: c.PageLabel, Paragraph: c.Paragraph, Section: c.Section, Anchor: c.Anchor, Text: c.Text, Boxes: c.Boxes}
		})
	return &LibraryRoutes{Backend: binding, Scopes: append([]string(nil), scopes...)}
}
