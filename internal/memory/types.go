package memory

import (
	"errors"
	"time"
)

// Sentinel errors returned by the memory service. Callers match them
// with errors.Is; tools translate them into input_error results and
// the review turns the scope ones into per-scope findings instead of
// aborting the whole night run.
var (
	// ErrNoScope is returned when a read or write carries no scope:
	// Section 9.5 forbids scopeless memory access.
	ErrNoScope = errors.New("memory: at least one scope is required")
	// ErrScopeDenied is returned when an operation targets a scope
	// outside the service scope set.
	ErrScopeDenied = errors.New("memory: scope outside the allowed set")
	// ErrNotFound is returned for unknown memory, entity, document,
	// fragment or chunk ids.
	ErrNotFound = errors.New("memory: not found")
	// ErrHashMismatch is returned by guarded fragment edits when
	// expected_hash differs; it carries the current fragment so the
	// agent can redo the edit on top of it (Section 10.2).
	ErrHashMismatch = errors.New("memory: fragment hash mismatch")
	// ErrEscalated is returned after consecutive hash mismatches on
	// the same fragment: the run must escalate to its parent or the
	// master instead of insisting (Section 10.2).
	ErrEscalated = errors.New("memory: repeated edit conflicts, escalate")
	// ErrNoSource is returned when a profile change lacks the
	// motivating user message required by Section 10.2.
	ErrNoSource = errors.New("memory: profile change needs provenance")
	// ErrExternalIdentity is returned when external-source content
	// would become a preference or an identity trait (Section 10.5).
	ErrExternalIdentity = errors.New("memory: external sources never become preference or identity")
	// ErrNoEmbedder is returned when semantic search is asked for
	// while no embedder is available; retrieval degrades to the
	// lexical, temporal and relational lists (Section 10.8).
	ErrNoEmbedder = errors.New("memory: no embedding provider available")
)

// Memory kind values (Section 10.3).
type Kind string

// Memory kinds.
const (
	KindFact       Kind = "fact"
	KindPreference Kind = "preference"
	KindEvent      Kind = "event"
	KindCommitment Kind = "commitment"
	KindBelief     Kind = "belief"
	KindRelation   Kind = "relation"
	KindProcedure  Kind = "procedure"
)

// Status values of a memory record (Section 10.3).
type Status string

// Memory statuses.
const (
	// StatusCandidate is a proposed fact visible only inside its own
	// conversation or task until consolidation admits it.
	StatusCandidate Status = "candidate"
	// StatusActive is an admitted, currently valid memory.
	StatusActive Status = "active"
	// StatusExpired passed its expires_at; the nightly review flips
	// the row, but readers already exclude it (Section 10.3).
	StatusExpired Status = "expired"
	// StatusSuperseded was replaced by a refinement.
	StatusSuperseded Status = "superseded"
	// StatusDisputed contradicts another active memory without a
	// decidable winner; both stay visible with a warning.
	StatusDisputed Status = "disputed"
	// StatusRetired lost its provenance or was withdrawn.
	StatusRetired Status = "retired"
)

// Trust values: where a memory comes from (Section 10.3).
type Trust string

// Memory trust values.
const (
	// TrustUser is content the user said or confirmed.
	TrustUser Trust = "user"
	// TrustAgent is content an agent derived or recorded.
	TrustAgent Trust = "agent"
	// TrustExternal is content extracted from outside pages, mail or
	// third-party documents. It never becomes a preference or an
	// identity trait (Section 10.5).
	TrustExternal Trust = "external"
)

// Source points at one backing evidence of a memory: a message, a
// tool call, a document chunk or a web page read.
type Source struct {
	// Kind is message, tool_call, document or page.
	Kind string `json:"kind"`
	// Ref is the msg_id, tool call id, chunk id or URL.
	Ref string `json:"ref"`
	// Conversation scopes message sources to their conversation.
	Conversation string `json:"conversation,omitempty"`
}

// Memory is one Section 10.3 record. Times are UTC; the zero value
// means unknown for ValidFrom/ValidTo/ExpiresAt.
type Memory struct {
	ID         string    `json:"id"`
	Scope      string    `json:"scope_id"`
	Kind       Kind      `json:"kind"`
	Statement  string    `json:"statement"`
	Entities   []string  `json:"entities"`
	ValidFrom  time.Time `json:"valid_from,omitempty"`
	ValidTo    time.Time `json:"valid_to,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	Status     Status    `json:"status"`
	Superseded string    `json:"superseded_by,omitempty"`
	Confidence float64   `json:"confidence"`
	Importance float64   `json:"importance"`
	Sources    []Source  `json:"sources"`
	Trust      Trust     `json:"trust"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	UseCount   int64     `json:"use_count"`
	ShortCode  string    `json:"short_code,omitempty"`
}

// Candidate is one fact proposed for admission (Section 10.5): by
// memory_propose during work, by batch extraction at segment close,
// or by the nightly review.
type Candidate struct {
	Scope      string    `json:"scope_id"`
	Kind       Kind      `json:"kind"`
	Statement  string    `json:"statement"`
	Entities   []string  `json:"entities,omitempty"`
	ValidFrom  time.Time `json:"valid_from,omitempty"`
	ValidTo    time.Time `json:"valid_to,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	Confidence float64   `json:"confidence"`
	Importance float64   `json:"importance"`
	Sources    []Source  `json:"sources"`
	Trust      Trust     `json:"trust"`
	// Conversation is the proposing conversation or task, used to
	// scope candidate visibility before consolidation.
	Conversation string `json:"conversation,omitempty"`
}

// AdmitOutcome describes what admission did with one candidate.
type AdmitOutcome struct {
	// Action is merged, admitted, refined, disputed or replaced.
	Action string `json:"action"`
	// MemoryID is the surviving or newly created row.
	MemoryID string `json:"memory_id,omitempty"`
	// Related names the older row involved in a merge, refinement,
	// replacement or dispute.
	Related string `json:"related,omitempty"`
	// NeedsUser is set when a low-priority user question was queued
	// (undecidable contradiction, Section 10.5).
	NeedsUser bool `json:"needs_user"`
}

// Entity is one Section 10.4 node with its alias lexicon.
type Entity struct {
	ID        string   `json:"id"`
	Scope     string   `json:"scope_id"`
	Type      string   `json:"type"`
	Canonical string   `json:"canonical"`
	Aliases   []string `json:"aliases"`
}

// RelationType is one entry of the per-user relation registry.
type RelationType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Domain      string `json:"domain"`
	Range       string `json:"range"`
	Inverse     string `json:"inverse"`
	// Origin is seed or learned.
	Origin string `json:"origin"`
	// Status is proposed, active or merged.
	Status   string `json:"status"`
	UseCount int64  `json:"use_count"`
}

// Edge is one memory-layer graph edge (Section 10.4): origin,
// relation type, destination, validity and provenance.
type Edge struct {
	ID         string            `json:"id"`
	FromType   string            `json:"from_type"`
	FromID     string            `json:"from_id"`
	Rel        string            `json:"rel"`
	ToType     string            `json:"to_type"`
	ToID       string            `json:"to_id"`
	Scope      string            `json:"scope_id"`
	Provenance map[string]string `json:"provenance"`
	ValidFrom  time.Time         `json:"valid_from,omitempty"`
	ValidTo    time.Time         `json:"valid_to,omitempty"`
}

// Fragment is one versioned piece of an identity document or the
// notebook (Section 10.2).
type Fragment struct {
	ID         string `json:"id"`
	Doc        string `json:"doc"`
	Heading    string `json:"heading_path"`
	Position   int    `json:"position"`
	Text       string `json:"text"`
	Version    int    `json:"version"`
	Hash       string `json:"hash"`
	Provenance string `json:"provenance,omitempty"`
	// Proposal marks an unconfirmed profile change waiting for
	// repeated evidence (Section 10.2).
	Proposal bool `json:"proposal"`
}

// IdentityDoc is one of the three logical documents: the agent
// profile, the user profile or the notebook.
type IdentityDoc struct {
	ID    string     `json:"id"`
	Scope string     `json:"scope_id"`
	Kind  string     `json:"kind"`
	Title string     `json:"title"`
	Frags []Fragment `json:"fragments,omitempty"`
}

// FragmentOp edits one fragment: replace its text, insert a fragment
// after it, or delete it.
type FragmentOp string

// Fragment operations.
const (
	OpReplace FragmentOp = "replace"
	OpInsert  FragmentOp = "insert"
	OpDelete  FragmentOp = "delete"
)

// DocEdit is one guarded fragment edit (Section 10.2).
type DocEdit struct {
	Doc          string     `json:"doc"`
	FragmentID   string     `json:"fragment_id"`
	ExpectedHash string     `json:"expected_hash"`
	Op           FragmentOp `json:"op"`
	// Text carries the replacement (replace) or the new fragment
	// (insert); Heading and Position place inserts.
	Text     string `json:"text,omitempty"`
	Heading  string `json:"heading_path,omitempty"`
	Position int    `json:"position,omitempty"`
	// Provenance is the motivating user message for profile changes.
	Provenance string `json:"provenance,omitempty"`
	// Explicit marks a change the user asked for directly; without
	// it profile changes land as visible proposals.
	Explicit bool `json:"explicit"`
}

// LibraryDoc is one versioned ingested document (Section 10.9): the
// original stays in the blob store, chunks carry the anchors.
type LibraryDoc struct {
	ID        string `json:"id"`
	Scope     string `json:"scope_id"`
	Version   int    `json:"version"`
	Digest    string `json:"blob_digest"`
	MIME      string `json:"mime"`
	Origin    string `json:"origin"`
	Title     string `json:"title"`
	Format    string `json:"format,omitempty"`
	Paginated bool   `json:"paginated,omitempty"`
}

// Chunk is one citable fragment with its location anchors.
type Chunk struct {
	PageLabel string `json:"page_label,omitempty"`
	Anchor    string `json:"anchor,omitempty"`
	ID        string `json:"id"`
	Doc       string `json:"doc_id"`
	Version   int    `json:"version"`
	Page      int    `json:"page"`
	Paragraph int    `json:"paragraph"`
	CharStart int    `json:"char_start"`
	CharEnd   int    `json:"char_end"`
	Section   string `json:"section_path"`
	Text      string `json:"text"`
	// Context is the one generated disambiguating sentence stored
	// only for marked library documents (Section 10.9).
	Context string `json:"context,omitempty"`
}

// Citation is the deterministic rendering of a chunk reference: the
// model cites the chunk id and the harness renders page and
// paragraph from metadata, never from model output (Section 10.9).
type Citation struct {
	PageLabel string `json:"page_label,omitempty"`
	ChunkID   string `json:"chunk_id"`
	DocID     string `json:"doc_id"`
	DocTitle  string `json:"doc_title"`
	Page      int    `json:"page"`
	Paragraph int    `json:"paragraph"`
	Section   string `json:"section_path"`
	// Verbatim renders the human citation string.
	Verbatim string `json:"verbatim"`
}

// SourceEvent records one web page read backing a Sources list, so
// answer source lists contain only pages actually read in the
// producing task tree (Section 10.9).
type SourceEvent struct {
	ID         string    `json:"id"`
	RunID      string    `json:"run_id"`
	Query      string    `json:"query"`
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	FetchedAt  time.Time `json:"fetched_at"`
	ContentSHA string    `json:"content_hash"`
}

// Query is one retriever call (Section 10.7). Scopes is mandatory.
type Query struct {
	Text   string   `json:"text"`
	Scopes []string `json:"scopes"`
	// Kinds optionally restricts memory kinds.
	Kinds []Kind `json:"kinds,omitempty"`
	// From/To optionally bound the validity window.
	From time.Time `json:"from,omitempty"`
	To   time.Time `json:"to,omitempty"`
	// KnownAt bounds the record time: only rows recorded at or
	// before it answer "what did we know at T", distinct from the
	// validity window answering "what was true at T"
	// (Section 10.3 bitemporal questions).
	KnownAt time.Time `json:"known_at,omitempty"`
	// Entities optionally restricts to memories linked to these ids.
	Entities []string `json:"entities,omitempty"`
	// Historical includes superseded, expired and retired rows when
	// the question is about the past.
	Historical bool `json:"historical"`
	// ActiveOnly excludes candidates; default unless Historical
	// or explicit kinds demand otherwise.
	ActiveOnly bool `json:"active_only"`
	// Conversation scopes candidate visibility: candidates surface
	// only inside their proposing conversation or task
	// (Section 10.5). Empty hides all candidates.
	Conversation string `json:"conversation,omitempty"`
	K            int    `json:"k,omitempty"`
	// MaxTokens caps the packaged context.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// ResultItem is one packaged hit: id, statement, validity window,
// provenance link and score parts for audit.
type ResultItem struct {
	ID        string    `json:"id"`
	Scope     string    `json:"scope_id"`
	Kind      Kind      `json:"kind"`
	Statement string    `json:"statement"`
	Status    Status    `json:"status"`
	ShortCode string    `json:"short_code,omitempty"`
	ValidFrom time.Time `json:"valid_from,omitempty"`
	ValidTo   time.Time `json:"valid_to,omitempty"`
	Sources   []Source  `json:"sources"`
	Score     float64   `json:"score"`
	Chunk     *Chunk    `json:"chunk,omitempty"`
}

// Notice carries retrieval uncertainty to the caller instead of a
// silent short list (Section 10.7 step 6).
type Notice struct {
	// Code is insufficient, contradiction or ambiguous_scope.
	Code string `json:"code"`
	// Detail names the memories or scopes involved.
	Detail string `json:"detail"`
}

// Retrieval is the packaged answer of the retriever.
type Retrieval struct {
	Items []ResultItem `json:"items"`
	// Notices carries insufficiency, contradiction and scope
	// ambiguity warnings.
	Notices []Notice `json:"notices"`
	// Semantic reports whether the semantic list participated; false
	// means degradation to lexical, temporal and relational search.
	Semantic bool `json:"semantic"`
}

// ReviewReport is the per-night, per-scope consolidation record the
// user can inspect (Section 10.6 step 9).
type ReviewReport struct {
	Scope     string    `json:"scope_id"`
	StartedAt time.Time `json:"started_at"`
	// Admitted, Merged, Expired, Retired and Disputed count rows
	// touched by each step.
	Admitted int `json:"admitted"`
	Merged   int `json:"merged"`
	Expired  int `json:"expired"`
	Retired  int `json:"retired"`
	Disputed int `json:"disputed"`
	// NotesRejected counts compaction rewrites refused for removing
	// more than 25% of the existing entries.
	NotesRejected int      `json:"notes_rejected"`
	Disputes      []string `json:"disputes,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

// Settings tunes the memory service. Zero Settings are valid:
// DefaultSettings fills every field with the Appendix B initial
// value. The service deliberately owns its settings struct instead
// of extending internal/config so milestones keep disjoint
// packages; the mapping to config.MemoryConfig thresholds is
// documented in docs/development/memory.md.
type Settings struct {
	// ExactDuplicateAt merges at/above this cosine with a shared
	// entity (Appendix B: 0.95).
	ExactDuplicateAt float64 `json:"exact_duplicate_at"`
	// AmbiguousBandLo opens the decider band (Appendix B: 0.80).
	AmbiguousBandLo float64 `json:"ambiguous_band_lo"`
	// RRFFusedK is the RRF constant (Appendix B: 60).
	RRFFusedK float64 `json:"rrf_k"`
	// CandidatesPerList caps each retrieval list (Appendix B: 50).
	CandidatesPerList int `json:"candidates_per_list"`
	// ConsolidationIdleDays gates full review (Appendix B: 5).
	ConsolidationIdleDays int `json:"consolidation_idle_days"`
	// EmbeddingDimensions sizes vectors (Appendix B: 512).
	EmbeddingDimensions int `json:"embedding_dimensions"`
	// EmbeddingModel is the provider embedding model id; the
	// default follows the operator credentials file.
	EmbeddingModel string `json:"embedding_model"`
	// Timezone names the user zone for review windows and temporal
	// filters (IANA, e.g. Europe/Madrid).
	Timezone string `json:"timezone"`
	// ActiveDays counts days with activity; the review picks the
	// quiet window once this reaches ConsolidationIdleDays.
	ActiveDays int `json:"active_days"`
}

// DefaultEmbeddingModel is the provider embedding model used when
// settings carry none.
const DefaultEmbeddingModel = "text-embedding-3-small"

// DefaultSettings returns the Appendix B initial values.
func DefaultSettings() Settings {
	return Settings{
		ExactDuplicateAt:      0.95,
		AmbiguousBandLo:       0.80,
		RRFFusedK:             60,
		CandidatesPerList:     50,
		ConsolidationIdleDays: 5,
		EmbeddingDimensions:   512,
		EmbeddingModel:        DefaultEmbeddingModel,
		Timezone:              "UTC",
	}
}
