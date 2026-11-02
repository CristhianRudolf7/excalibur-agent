package memory

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Service owns every Section 10 behavior behind one store handle.
// Embedder, Decider and Extract are seams: tests and later milestones
// replace them without touching the SQL below.
//
// Concurrency: the service holds no mutable state besides its seams
// and the clock. All rows live in the store; the porter serializes
// writers and the read pool serves concurrent readers (Section 4.3).
type Service struct {
	db *store.DB

	librarySubmitter atomic.Pointer[librarySubmitterBinding]
	// tele optionally receives source-event mirrors; nil disables it.
	tele *store.DB

	settings Settings
	scopes   []string
	now      func() time.Time
	// prio is the porter lane of ctxWrite; Background twins run it
	// on the background lane (Section 10.6).
	prio store.Priority

	decider  Decider
	extract  Extractor
	rerank   Reranker
	rewriter Rewriter
	vision   VisionExtractor
	// embeds holds the active embedder plus the retiring one while
	// a model change reindexes: both spaces answer until the new
	// one is complete (Section 10.8 dual read). The pointer swaps
	// atomically so readers never race the swap.
	embeds atomic.Pointer[embedSet]
}

// Option configures a Service.
type Option func(*Service)

// WithSettings overrides the default Appendix B settings.
func WithSettings(s Settings) Option {
	return func(svc *Service) { svc.settings = s }
}

// WithScopes fixes the read/write scope set. Every service carries
// its run scope set from creation (Section 9.5); empty means every
// call must pass explicit scopes that the service then enforces.
func WithScopes(scopes ...string) Option {
	return func(svc *Service) { svc.scopes = append([]string(nil), scopes...) }
}

// WithClock fixes the clock; tests use it to freeze time.
func WithClock(now func() time.Time) Option {
	return func(svc *Service) { svc.now = now }
}

// WithEmbedder plugs the embedding provider (or its fake).
func WithEmbedder(e Embedder) Option {
	return func(svc *Service) { svc.embeds.Store(&embedSet{cur: e}) }
}

// WithDecider plugs the ambiguous-band judge.
func WithDecider(d Decider) Option {
	return func(svc *Service) { svc.decider = d }
}

// WithExtractor plugs batch fact extraction (LLM in production,
// scripted in tests).
func WithExtractor(x Extractor) Option {
	return func(svc *Service) { svc.extract = x }
}

// WithReranker plugs the optional small reranker.
func WithReranker(r Reranker) Option {
	return func(svc *Service) { svc.rerank = r }
}

// WithVision plugs page/image text extraction for library ingest.
func WithVision(v VisionExtractor) Option {
	return func(svc *Service) { svc.vision = v }
}

// WithTelemetry mirrors source events into a telemetry database.
func WithTelemetry(tele *store.DB) Option {
	return func(svc *Service) { svc.tele = tele }
}

// NewService opens the Section 10 behavior over db. Scopes passed
// here are the default read/write set; calls may narrow them but
// never widen them.
func NewService(db *store.DB, opts ...Option) *Service {
	svc := &Service{
		db:       db,
		settings: DefaultSettings(),
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, o := range opts {
		o(svc)
	}
	if svc.embeds.Load() == nil {
		svc.embeds.Store(&embedSet{})
	}
	if svc.decider == nil {
		svc.decider = DeterministicDecider{}
	}
	return svc
}

// embedSet is the atomic embedder generation: the active model plus
// the retiring one served during a model-change reindex.
type embedSet struct {
	cur       Embedder
	prev      Embedder
	prevSpace string
}

// curEmbed returns the active embedder, or nil when none is
// configured (lexical/temporal/relational degradation).
func (s *Service) curEmbed() Embedder {
	set := s.embeds.Load()
	if set == nil {
		return nil
	}
	return set.cur
}

// embedSnapshot returns the active and retiring embedders for
// dual-read retrieval.
func (s *Service) embedSnapshot() (cur, prev Embedder, prevSpace string) {
	set := s.embeds.Load()
	if set == nil {
		return nil, nil, ""
	}
	return set.cur, set.prev, set.prevSpace
}

// Settings returns the effective settings.
func (s *Service) Settings() Settings { return s.settings }

// Scopes returns the fixed default scope set, if any.
func (s *Service) Scopes() []string { return append([]string(nil), s.scopes...) }

// resolveScopes enforces Section 9.5: every operation carries at
// least one scope, and a fixed service set bounds what calls may ask
// for. An empty service set means per-call scopes with no bound.
func (s *Service) resolveScopes(want []string) ([]string, error) {
	if len(want) == 0 {
		if len(s.scopes) == 0 {
			return nil, ErrNoScope
		}
		return append([]string(nil), s.scopes...), nil
	}
	for _, w := range want {
		if strings.TrimSpace(w) == "" {
			return nil, ErrNoScope
		}
		var sc types.Scope
		var err error
		if w == "general" {
			sc = types.GeneralScope()
		} else {
			sc, err = types.ParseScope(w)
			if err != nil {
				return nil, fmt.Errorf("memory: bad scope %q: %w", w, err)
			}
		}
		if err := sc.Validate(); err != nil {
			return nil, fmt.Errorf("memory: bad scope %q: %w", w, err)
		}
		if len(s.scopes) > 0 {
			allowed := false
			for _, a := range s.scopes {
				if a == w {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, fmt.Errorf("%w: %q", ErrScopeDenied, w)
			}
		}
	}
	out := append([]string(nil), want...)
	return out, nil
}

// scopePlaceholders renders "?,…" for an IN filter and appends the
// scope values to args. The filter always lands in SQL, never in the
// prompt (Section 9.5).
func scopePlaceholders(scopes []string, args []any) (string, []any) {
	ph := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		ph = append(ph, "?")
		args = append(args, sc)
	}
	return strings.Join(ph, ","), args
}

// ms converts a time to epoch milliseconds; zero stays zero so
// "unknown" survives the round trip.
func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

// fromMS converts epoch milliseconds back; zero means unknown.
func fromMS(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

// idPrefixes for memory-owned rows. Memories reuse the shared mem_
// prefix via types.NewMemoryID; subordinate rows use memory-local
// prefixes over types.NewULID so internal/types stays untouched.
const (
	entityPrefix   = "ent_"
	edgePrefix     = "edg_"
	relTypePrefix  = "rlt_"
	identityPrefix = "idd_"
	fragPrefix     = "frg_"
	docPrefix      = "doc_"
	chunkPrefix    = "chn_"
	sourcePrefix   = "sev_"
	reportPrefix   = "rep_"
)

func newEntityID() string   { return entityPrefix + types.NewULID() }
func newEdgeID() string     { return edgePrefix + types.NewULID() }
func newRelTypeID() string  { return relTypePrefix + types.NewULID() }
func newIdentityID() string { return identityPrefix + types.NewULID() }
func newFragmentID() string { return fragPrefix + types.NewULID() }
func newDocID() string      { return docPrefix + types.NewULID() }
func newChunkID() string    { return chunkPrefix + types.NewULID() }
func newSourceID() string   { return sourcePrefix + types.NewULID() }

// ctxWrite runs fn on the service lane (normal, or background for
// review twins).
func (s *Service) ctxWrite(ctx context.Context, fn store.WriteFunc) error {
	return s.db.Write(ctx, s.writePrio(), fn)
}

// read runs fn on the pooled reader.
func (s *Service) read(ctx context.Context, fn store.ReadFunc) error {
	return s.db.Read(ctx, fn)
}

// nowMS is the service clock in epoch milliseconds.
func (s *Service) nowMS() int64 { return s.now().UTC().UnixMilli() }

// location resolves the user timezone for temporal filters and the
// review window; unknown names fall back to UTC.
func (s *Service) location() *time.Location {
	if s.settings.Timezone == "" || s.settings.Timezone == "UTC" {
		return time.UTC
	}
	loc, err := time.LoadLocation(s.settings.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Decider judges one ambiguous-band candidate pair (Section 10.5,
// 2.1): duplicate, refinement, contradiction or genuinely new. The
// deterministic default decides by structure; an LLM-backed decider
// plugs in here when calibrated (Section 19.4, H12).
type Decider interface {
	// Classify returns one of duplicate, refinement, contradiction
	// or new for candidate against an active memory.
	Classify(ctx context.Context, c Candidate, old Memory) (string, error)
}

// DeterministicDecider applies the structural rules: near-identical
// statements with a shared entity are duplicates; explicit temporal
// change markers make contradictions replacements handled by the
// caller; otherwise the band resolves by statement overlap.
type DeterministicDecider struct{}

// DeterministicDecider applies the structural rules: exact
// statements with a shared entity duplicate; a negation flip over a
// shared core contradicts (and outranks substring refinement);
// substring containment with a shared entity refines. Everything
// else is new: below-band similarities never merge without judgment
// (Section 10.5), and paraphrase merges belong to the calibrated
// judge (Section 19.4, H12), not to fixed overlap cutoffs.
func (DeterministicDecider) Classify(_ context.Context, c Candidate, old Memory) (string, error) {
	cn, on := normStatement(c.Statement), normStatement(old.Statement)
	if cn == on {
		return "duplicate", nil
	}
	// Contradiction outranks refinement: an extension that flips
	// the shared claim ("X, not Y", "now never X") disputes it
	// rather than refining it.
	if sharesEntity(c.Entities, old.Entities) && contradicts(cn, on) {
		return "contradiction", nil
	}
	if sharesEntity(c.Entities, old.Entities) && (isRefinement(cn, on) || isRefinement(on, cn)) {
		return "refinement", nil
	}
	return "new", nil
}

// Extractor turns fresh episode turns into atomic candidates with
// msg_id sources (Section 10.5 step 3). Production plugs the
// cheapest model role; tests plug scripted extraction.
type Extractor interface {
	// Extract returns candidates for turns since the scope watermark.
	Extract(ctx context.Context, scope string, turns []Turn) ([]Candidate, error)
}

// Turn is one fresh episode unit fed to batch extraction: the
// conversation turn is the most effective storage unit (Section
// 10.5, LongMemEval).
type Turn struct {
	MsgID        string    `json:"msg_id"`
	Conversation string    `json:"conversation_id"`
	Text         string    `json:"text"`
	At           time.Time `json:"at"`
}

// Reranker optionally reorders fused hits. It runs only when the top
// results sit close together and budget remains; its order is used
// directly, never re-fused (Section 10.7).
type Reranker interface {
	// Rerank reorders items for query, returning the same slice in
	// the reranked order.
	Rerank(ctx context.Context, query string, items []ResultItem) ([]ResultItem, error)
}

// VisionExtractor renders page/image text for library ingest
// (Section 10.9 step 2): scanned PDFs and images go through the
// vision model, other formats through native conversion.
type VisionExtractor interface {
	// ExtractText returns per-page text blocks for input.
	ExtractText(ctx context.Context, input string, pages []int) ([][]string, error)
}
