package daemon

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

// LibraryService describes the shared memory service calls using its own DTOs.
// Generic parameters let this branch compile before the parallel memory changes.
type LibraryService[Info, Query, Chunk any] interface {
	ListDocuments(context.Context, []string, Query) ([]Info, string, error)
	GetDocument(context.Context, []string, string) (Info, error)
	DocumentPages(context.Context, []string, string, int, int, int) ([]docextract.Page, error)
	ChunkDetail(context.Context, []string, string, string) (Chunk, error)
	PurgeDocument(context.Context, string, string) error
}

// LibrarySubmitter is implemented by the ingester with its native request type.
type LibrarySubmitter[Info, Request any] interface {
	Submit(context.Context, Request) (Info, bool, error)
	Cancel(string) error
}

type libraryServiceBinding[Info, Query, Chunk, Request any] struct {
	service  LibraryService[Info, Query, Chunk]
	ingester LibrarySubmitter[Info, Request]
	request  func(api.LibraryIngestParams) Request
	query    func(api.LibraryListParams) Query
	document func(Info) api.DocumentInfo
	chunk    func(Chunk) api.ChunkDetail
}

// NewLibraryServiceBinding binds *memory.Service and *library.Ingester at the
// integration composition point. Converters explicitly map the native DTOs to
// the stable API (including Section -> section_path and the ingest metadata).
// All arguments are required. No background work is started by this adapter.
func NewLibraryServiceBinding[Info, Query, Chunk, Request any](
	service LibraryService[Info, Query, Chunk], ingester LibrarySubmitter[Info, Request],
	request func(api.LibraryIngestParams) Request, query func(api.LibraryListParams) Query,
	document func(Info) api.DocumentInfo, chunk func(Chunk) api.ChunkDetail,
) LibraryBackend {
	return &libraryServiceBinding[Info, Query, Chunk, Request]{service, ingester, request, query, document, chunk}
}
func (b *libraryServiceBinding[I, Q, C, R]) Submit(ctx context.Context, p api.LibraryIngestParams) (api.DocumentInfo, bool, error) {
	d, existing, err := b.ingester.Submit(ctx, b.request(p))
	if err != nil {
		return api.DocumentInfo{}, false, err
	}
	return b.document(d), existing, nil
}
func (b *libraryServiceBinding[I, Q, C, R]) Cancel(id string) error { return b.ingester.Cancel(id) }
func (b *libraryServiceBinding[I, Q, C, R]) ListDocuments(ctx context.Context, scopes []string, p api.LibraryListParams) ([]api.DocumentInfo, string, error) {
	docs, next, err := b.service.ListDocuments(ctx, scopes, b.query(p))
	if err != nil {
		return nil, "", err
	}
	out := make([]api.DocumentInfo, 0, len(docs))
	for _, d := range docs {
		out = append(out, b.document(d))
	}
	return out, next, nil
}
func (b *libraryServiceBinding[I, Q, C, R]) GetDocument(ctx context.Context, scopes []string, id string) (api.DocumentInfo, error) {
	d, err := b.service.GetDocument(ctx, scopes, id)
	if err != nil {
		return api.DocumentInfo{}, err
	}
	return b.document(d), nil
}
func (b *libraryServiceBinding[I, Q, C, R]) DocumentPages(ctx context.Context, scopes []string, id string, v, from, to int) ([]docextract.Page, error) {
	return b.service.DocumentPages(ctx, scopes, id, v, from, to)
}
func (b *libraryServiceBinding[I, Q, C, R]) ChunkDetail(ctx context.Context, scopes []string, id, chunk string) (api.ChunkDetail, error) {
	c, err := b.service.ChunkDetail(ctx, scopes, id, chunk)
	if err != nil {
		return api.ChunkDetail{}, err
	}
	return b.chunk(c), nil
}
func (b *libraryServiceBinding[I, Q, C, R]) PurgeDocument(ctx context.Context, id, reason string) error {
	return b.service.PurgeDocument(ctx, id, reason)
}

// LibraryServiceNotifier implements the ingester's Notifier for its native Info.
// Use Convert to map memory.DocumentInfo, then publish on the daemon bus.
type LibraryServiceNotifier[Info any] struct {
	Notifier LibraryNotifier
	Convert  func(Info) api.DocumentInfo
}

func (n LibraryServiceNotifier[I]) DocumentUpdated(info I) {
	n.Notifier.DocumentUpdated(n.Convert(info))
}
