# H6 — Context and cache (`internal/ctxcompiler`)

Contract and backend tests of the milestone implementing Section 11:
layered context compiler with stable prefix and per-layer hashes
(11.1), small active context budgets (11.2), batched observation
masking (11.3), compaction with the 500 000-token ceiling and the
artifact trail (11.4), literal durable instructions (11.5),
per-provider cache adapters and breakpoint planning (11.6), and
token counting (11.7).

## Contract

The package depends only on `internal/types`, the provider request
types of `internal/providers`, and its own small source interfaces
(`MemorySource`, `SkillSource`, `ToolSource` in `sources.go`);
production producers stay behind those interfaces and tests use the
fakes in `internal/ctxcompiler/fake`. `Config` mirrors the
`ContextConfig` Appendix B values without importing
`internal/config` (a parity test pins the two together).

- **Compiler** (`compiler.go`): `Snapshot` in, six rendered layers
  L0-L5 out with SHA-256 hashes each. Rules enforced: tools
  name-ordered; clock, request IDs and counters only in L5; history
  append-only (extending history only appends L4 bytes); task changes
  travel as versioned deltas (`FormatDelta`/`AppendDelta`), L3
  re-renders only at compaction; suggested skills capped at 3;
  `ToChatRequest` projects tools, system (L0-L3), messages, volatile
  tail with per-role fixed reasoning settings. `RoleTarget`,
  `CheckBudget` and `CatalogBudget` report the 11.2 ranges
  (master 8-24K, child <32K, parent <48K; catalog min(2%, 4K)).
- **Masking** (`masking.go`): `Masker` batches per 11.3 — newest 10,
  pinned and error observations stay literal; above 24K unmasked
  tokens (estimate + 10% margin) the oldest candidates mask down
  toward 12K; resume past the cache TTL advances the batch early.
  Markers carry call ID, label, line count and content hash;
  `ContentStore` keeps exact bytes (`get_tool_output` equivalent).
- **Compaction** (`compaction.go`): `CompactionThreshold` implements
  `min(ceiling, 0.85*window-reserve)` with the 500K default and an
  optional per-model ceiling; `StructuredState` renders the
  deterministic state incl. the exact artifact trail; the summary is
  incremental (discarded segment + previous summary) and
  mechanically repaired so every open ID appears; without a summary
  the block still compacts on state alone; `SelectTail` keeps the
  post-compaction 20K literal window without splitting call/result
  pairs; `CompactionRecord` points at the discarded span, which stays
  queryable through `HistoryStore`. `ConversationCompactor` manages
  the master long history: 12K literal, recalculation only at
  session boundaries (past 24K, or past TTL with >12K literal),
  archiving to the history store; facts still go to memory.
- **Instructions** (`instructions.go`): `Registry` stores literals
  with scope, revision, origin and state; renders byte-identical
  text across compaction, restart (JSON snapshot) and delegation
  (`DelegateCard` carries mandatory instructions explicitly, never
  as skill suggestions). Task/conversation scopes render in L3,
  project/general in L2.
- **Cache** (`cache.go`): `Plan` wires each provider family —
  Anthropic (4 `cache_control` marks incl. sliding L4 tail, TTL
  1h human / 5min machine, leader-first prewarm), OpenAI (auto
  cache from 1024 tokens, stable `prompt_cache_key` per
  role/scope, fixed reasoning, allowlist restriction), Gemini
  (implicit + 2050-token warning), Qwen (explicit marks),
  economic/local/generic (stable prefix only). `DriftDetector`
  flags L0/L1 changes inside a run as harness defects;
  `TheoreticalStableLayers` and `CacheStats.HitRate` feed the cache
  suite comparison of real vs theoretical hits.
- **Tokens** (`tokens.go`): `Counter` seam; `Estimator` calibrates
  chars-per-token per model with a moving average from response
  usage (10% margin via `WithMargin`); `Tokenizer` accepts an exact
  local tokenizer when one exists without network. Decision
  0003 records why tiktoken-go is not bundled (runtime BPE
  download); decision 0004 records the Section 11 readings.

## Backend tests

All rows of the 11.8 table, hermetic (no network, no paid APIs):

- `compiler_test.go`: determinism incl. serialize/restore round
  trip; clock confined to L5; tool ordering; append-only L4;
  deltas keep L0-L2 hashes with zero drift over a 20-call session;
  skill cap; request shape; role budgets and catalog cap.
- `masking_test.go`: 30-observation batch drops 30K toward 12K
  keeping the newest 10 and a pinned entry; errors conserved;
  silence below trigger; early batch on resume; marker rewrite
  with call identity and exact store round trip.
- `compaction_test.go`: ceiling/clamp/fraction/per-model
  thresholds and margined trigger; deterministic state with exact
  trail; open-ID check and mechanical repair (closed states
  excluded); incremental summary over the discarded segment only;
  fallback with failing and nil summarizers; pair-safe tail;
  master history (12K cap, boundary-only recalculation, archived
  retrieval, summary survival of summarizer failure, segment-only
  inputs).
- `instructions_test.go`: byte-identical literals across
  compaction render, registry snapshot/restore and delegation
  card; lifecycle; L2/L3 scope layering.
- `cache_test.go`: per-family plans (marks, TTLs, key stability,
  2050-token note); classification; prewarm predicate; 20-call
  zero-drift session then a planted L0 break firing exactly one
  drift; theoretical-prefix counts; hit-rate math.
- `tokens_test.go`: calibration of a 6.0 factor from 4.0 within
  the 10% band after 30 samples; margin math; tokenizer seam;
  message costing; defaults parity with `internal/config`.
- `fake/fake_test.go`: budget-capped recall, suggestion cap,
  summary fusion, exact store bytes, substring history search.
- `live_test.go` (`//go:build live`, `EXCALIBUR_LIVE=1` only): one
  tiny chat call checks the compiled shape against the real API
  and the uncalibrated estimate within a 2x smoke bound; keys
  travel in the Authorization header only. Never runs under
  plain `go test ./...`.

## Deferred

Nothing of Section 11 is deferred. Consumers land with their
milestones: the engine feeds snapshots and journal activity (H3),
memory/skills/tools implement the source interfaces (H7/H8/H11),
and the `cache`/`compaction` suites of 23.5 measure real hit rates
against the theoretical prefix computed here.
