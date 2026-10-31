# H11 — Memory and documents

Contract and backend tests of the Section 10 area: memory layers and
the retriever, identity documents and the notebook, memory records
with valid time and expiry, adaptable entities and relations,
admission rules, the daily background review, fused retrieval with
uncertainty, embeddings via the provider, the document library with
exact citations, and the memory tools.

## Contract

`internal/memory` owns every Section 10 behavior behind `Service`,
which sits over `*store.DB` (porter writes, pooled reads, blobs) and
never touches another milestone's tables except `messages`, `runs`
and `journal` for episode reads and `short_counters` for per-scope
M-codes:

- **Records** (`Memory`): kind (fact, preference, event, commitment,
  belief, relation, procedure), atomic statement, entity links,
  valid_from/valid_to versus recorded_at (bitemporal), optional
  expires_at, status (candidate, active, expired, superseded,
  disputed, retired), superseded_by, confidence, importance, sources,
  trust (user, agent, external), use counters. Short codes M12, M40
  come from `short_counters` series `M` and never repeat.
- **Admission** (`Write` explicit-immediate, `Propose` pending,
  `Admit`): exact duplicates merge provenance; cosine >= 0.95 with a
  shared entity merges; 0.80-0.95 goes to the `Decider`
  (deterministic default; an LLM judge plugs in at H12);
  refinements replace with superseded_by and valid_to;
  contradictions replace on explicit temporal change ("now …")
  and otherwise leave both disputed with `NeedsUser`; below 0.80 a
  candidate is new. External content never becomes a preference.
- **Identity documents** (`identity_docs` + `doc_fragments`):
  agent profile, user profile, notebook, plus `persona:<name>`
  variants. `doc_read` serves index, section or FTS search without
  loading the whole document. `doc_edit` replaces, inserts or deletes
  one fragment behind an sha256 hash guard: a stale hash returns the
  current fragment, two consecutive mismatches escalate instead of
  insisting. Every version is kept (`doc_fragment_history`) with
  history and revert. Profile changes require provenance (the
  motivating user message); unrequested ones land as visible
  proposals that consolidation confirms on repeated evidence.
- **Retriever**: deterministic analysis (quoted phrases mandatory,
  bilingual relative/absolute dates in the user zone, entity-alias
  seeds, past-tense cues widen to non-active), four lists of up to 50
  (lexical FTS5 BM25 with diacritic folding, semantic KNN with
  scoped-partition fallback to exact shadow search, relational 1-2
  hops, temporal overlap), RRF fusion (k=60), adjustments (validity
  filter, per-kind half-life decay 7/90/365 days, confidence and
  importance blend), optional reranker only on a close top whose
  order is honored directly, sufficiency notices (insufficient,
  contradiction with M-codes, ambiguous_scope), scope-labeled
  packaging inside the token budget, usage touch. One rewrite fires
  only on zero results when a `Rewriter` is plugged.
- **Embeddings**: `Embedder` interface. `HTTPEmbedder` talks to any
  OpenAI-compatible `/embeddings` endpoint (default model
  `text-embedding-3-small` from `Settings.EmbeddingModel`),
  authenticates once, retries transient failures with backoff, and
  uses the fallback key a single time on quota/rate-limit errors.
  `FakeEmbedder` (word-hash, deterministic) serves tests.
  Vectors truncate to the Matryoshka prefix (512 default) and mirror
  into the `embedding_vectors` shadow plus vec0 when present. While
  the provider is down, writes queue in `pending_vectors` and
  retrieval degrades to lexical, temporal and relational search with
  the fact flagged; `DrainPending` backfills. `StartReindex`
  opens a new space, serves both during the backfill
  (`ReindexBatch`), and `FinishReindex` retires the old one.
- **Document library**: versioned blob originals, structure-aware
  chunks (page, paragraph, char offsets, section path), one generated
  context sentence for marked library documents, FTS5 plus vectors
  in scope, deterministic citations (`Title, p. N, ¶ M`, or section
  and paragraph without invented pages), validation of every
  `[[doc:id#chunk]]` ref against the answering run tree, and
  `source_events` so answer source lists hold only pages actually
  read. Vision formats extract through the `VisionExtractor` seam
  (`VisionToolAdapter` wraps the `vision_extract` tool).
- **Daily review**: `ShouldReview` (03:30 local fallback after two
  idle hours before day five; computed quiet window from the
  trailing-14-day histogram after), `NextWindow`, and `ReviewScope`
  (episodes since the watermark in checkpointed batches, extraction
  via the heuristic fallback or a plugged `Extractor`, admission,
  expirations, provenance recheck, contradiction resolution preferring
  marked uncertainty, mention-edge linking, lossless note compaction
  with the 25% safeguard, proposal confirmation, relation-need
  warnings, persisted consultable report). Runs on the background
  lane and yields on user activity, resuming from the last batch.
- **Tools** (`MemoryTools`, `WireMemoryTools`): `memory_write`,
  `memory_propose`, `memory_search`, `history_search`,
  `library_ingest` (new in H11; the H3 stub list predates it),
  `library_search` (records retrievals per run for citation
  validation), `doc_read`, `doc_edit`. Wiring fills the H3 stub
  providers or registers the real tools; the catalog prefix never
  changes.
- **Deletion** (`PurgeMemory`, `PurgeDocument` over
  `store.Purge*`): content, FTS, vectors, pending jobs and citation
  reads go; only the tombstone stays, with the backup-rotation
  notice.

No new Go dependencies: stdlib plus the existing module set.

## Backend tests

- `internal/memory`: admission (duplicate merge, band decider,
  dispute vs temporal-change replace, external-preference refusal,
  candidate visibility per conversation); retrieval (diacritic
  folding, required phrases, relative temporal windows in both
  languages, bitemporal validity vs record time, expiry exclusion
  before review, contradiction notices, relational seeds, RRF math,
  half-lives, usage touch); identity docs (guard cycle, escalation,
  history, revert, provenance, proposals, concurrent edits);
  library (unpaginated vs paginated chunking and citations,
  tree-scoped validation, context sentences, source events, purge
  with residue check); review (report flow, provenance retire,
  compaction safeguard, quiet window, fallback gate, cancel yield);
  embeddings (fake determinism, HTTP success/fallback/auth paths,
  pending queue drain); reindex (dual read, coverage,
  retire); scope isolation across all four paths; tools (wire
  stability, end-to-end invokes, mismatch payload).
- `tests/memory` (Section 23.5): `mem-scope`, `mem-time`,
  `mem-conflict`, `mem-cite` suites.
- Live (`-tags live`, `EXCALIBUR_LIVE=1`): one tiny embedding call
  through the provider; never under plain `go test`.

## Deferred (not in H11)

LLM-backed extractor, decider and rewriter roles (H12); the Agenda
driver calling `ShouldReview`/`NextWindow` weekly (H10); gate and CLI
wiring of `memory.*` methods (H9); local sidecar embedder and int8
quantization behind the shared schema (future).
