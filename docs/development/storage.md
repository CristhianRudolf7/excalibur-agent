# H2 — Base storage and layered configuration

Contract and backend tests of the H2 area: the SQLite porter, the
per-user file family, blobs, migrations, backups, retention, purge,
short codes, and layered configuration. Wave 0 owns the shared
contracts (`internal/store` interfaces, `internal/config` types);
this document owns their H2 behavior.

## Contract

H2 implements Section 4 entirely behind the `Store` interfaces, plus
the layered config loading of Section 25.2 point 1.

- **Backend** (Section 4.1): `mattn/go-sqlite3` with FTS5
  (`sqlite_fts5` tag) and `sqlite-vec` (cgo bindings), selected with
  `-tags sqlite_fts5`. Without the tag (plain builds, darwin,
  windows, `CGO_ENABLED=0`) a pure-Go fallback (`modernc.org/sqlite`)
  serves the same SQL behind the same `Store` interface (Section 3.4
  note). FTS5 exists on both; vec0 only on the cgo path, where the
  migration probes and otherwise records `vector_search=unavailable`
  (the Section 10.8 degradation).
- **Files** (Section 4.2): `Layout` — `excalibur.db` (`synchronous=FULL`),
  `telemetry.db` (`NORMAL`), `projects/<id>/codegraph.db` (path helper;
  the graph belongs to a later milestone), `blobs/` (sha256 sharded,
  zstd). `OpenFamily` opens the whole set, each file with its own
  porter.
- **Porter** (Section 4.3): one writer goroutine per file (via
  `safego.Go`) fed by four bounded lanes
  (interactive > journal > normal > background). `BEGIN IMMEDIATE`
  only, group commit up to 5 ms / 200 ops of same-or-higher priority,
  one savepoint per command (constraint failures roll back only that
  command), one reply per command after the commit decision.
  Cancellation before the writer takes a command discards it with
  `ErrWriteCanceled`; a full lane blocks the sender (backpressure).
  `busy_timeout` 5 s plus 3 jittered retries, then typed `ErrBusy`
  with nothing applied. `SQLITE_FULL` enters disk-full mode:
  background writes fail fast, the interactive path keeps trying, and
  the episode clears on the next clean commit. `PRAGMA quick_check`
  at open; on failure the latest `VACUUM INTO` backup restores and FTS
  rebuilds. Network filesystems degrade to `journal_mode=DELETE`
  with a `Degraded()` flag. `PASSIVE` checkpoints when idle,
  `TRUNCATE` on demand; the WAL watch fires `OnWALAlert` on the
  false-to-true crossing of 256 MiB. Read pool of `min(4, NumCPU)`
  with one connection reserved for the interactive lane; every
  connection is pragma'd once (`query_only`, cache sizes, synchronous
  mode) and held for life.
- **Identifiers** (Section 4.4): ULIDs via `internal/types`; short
  codes (`T45`, `R12`, `C12.3`, `M2`) from per-scope counters in
  `short_counters`, allocated inside porter transactions so they
  never move backwards and deleted codes are never reused.
- **Schema** (Section 4.5, Appendix A): embedded, numbered,
  transactional migrations in `internal/store/migrations` (v1
  conversation/work, v2 agents/runs, v3 memory with FTS5 unicode61
  folding plus trigram paths, v4 agenda/files/vec0). Every Appendix A
  table exists; `blob_refs` carries `kind`/`run_id` for retention,
  `embedding_vectors` is the portable vector shadow behind the vec0
  partition-keyed tables (`scope_id TEXT PARTITION KEY`, KNN via
  `embedding MATCH ?` with mandatory `scope_id IN (...)`). Telemetry
  has its own one-version registry.
- **Copies** (Section 4.6): `Backup` (`VACUUM INTO` off the porter
  path) with 7-daily + 4-weekly rotation; `Restore` replaces files
  with the daemon stopped; expand-then-contract only (a newer
  database stays readable and writable by the older binary).
- **Retention and purge** (Section 4.7): `ApplyRetention` compacts
  terminal run journals older than 30 days into one summary blob
  (runs marked `keep_full_journal` keep everything), releases
  tool-output blob references older than 30 days unless evidence,
  documents, file versions or journal markers keep them, and trims
  raw telemetry past 90 days. `PurgeMemory`, `PurgeConversation`,
  `PurgeDocument`, `PurgeProject` remove content rows, FTS entries,
  vectors, blobs (dereferenced for GC) and telemetry traces, and
  leave one tombstone; `BackupPurgeNotice` is the confirmation text
  callers must show (backups keep the data until they rotate).
- **Config** (Section 25.2.1): `config.Load` merges
  defaults < system (`/etc/excalibur/config.toml`) < user (XDG) <
  project (nearest `.excalibur/config.toml` upward) < scope file, all
  TOML, then `EXCALIBUR_SECTION__FIELD` environment variables, then
  explicit dotted overrides; durations accept strings (`"5s"`) in
  every layer. `config.WriteFile` writes TOML for install.

Durability defaults: `foreign_keys` off (relationships are enforced
by writers, so test and migration ordering never trips on pragma
enforcement); writer cache 8 MiB, readers 2 MiB, mmap off unless
`MMapBytes` is set.

## Backend tests

Every row of the Section 4.8 table has a Go test; `vec0` rows run on
the cgo path and record the degradation elsewhere.

- `porter_test.go`: lane priority order behind a blocked writer;
  group commit (fewer batches than commands, exact row count);
  savepoint isolation (constraint fails alone); pre-queue cancel
  (`ErrWriteCanceled`, never runs); backpressure (full lane + expired
  context); close drains queued work.
- `load_test.go`: 16 writers on 4 lanes x 50 plus 4 readers (one on
  the interactive lane): zero `SQLITE_BUSY`, all 800 rows, interactive
  mean ~10 ms under load; long reader never blocks the writer;
  interactive lane answers with the shared pool saturated.
- `busy_test.go`: a second handle holds `RESERVED` past the whole
  busy budget: typed `ErrBusy` after the configured retries, no
  half-applied rows, recovery afterwards.
- `blob_test.go`: deterministic addressing, round trip, refcounting,
  `ErrBlobNotFound`, GC of only old unreferenced blobs with the
  no-dangling-rows check.
- `crash_test.go`: `SIGKILL` mid-group-commit and mid-blob-write,
  then `quick_check` passes, the pre-storm blob survives, and no row
  points at a missing blob.
- `backup_test.go`: `VACUUM INTO` opens with identical content;
  rotation keeps at most 11 copies with the freshest safe; torn-page
  corruption restores from backup (`Restored()`, content and
  `quick_check` green); corruption without backup is `ErrCorrupt`.
- `health_test.go`: network FS forces `DELETE` with the degraded
  flag; explicit override without the flag; `FULL`/`NORMAL`
  synchronous modes; `query_only` rejects writes on both read lanes;
  a pinned WAL crosses a small alert threshold exactly once and
  `TRUNCATE` reclaims it.
- `diskfull_test.go`: real `SQLITE_FULL` via `max_page_count` —
  typed error, background fails fast without running, the callback
  fires once, recovery resumes the lane.
- `migration_test.go`: every Appendix A table plus FTS/trigram
  indexes at the latest version; diacritic folding (`accion` finds
  `acción`); deleted rows leave the index; vec0 KNN round trip with
  scope isolation (`project:other` matches nothing) and shadow
  rebuild; forward compatibility (N opens and writes an N+1
  database); a failing migration aborts with `ErrMigrationFailed`
  and the file stays at its version.
- `purge_test.go`: purged text absent from content tables, all FTS
  indexes, the journal and telemetry; one tombstone; the backup
  notice text ships with the result.
- `retention_test.go`: 30-day journal compaction (marker + summary
  blob, kept/recent/active runs untouched); tool-blob release that
  spares evidenced blobs; 90-day telemetry trim with aggregates kept.
- `ids_test.go`: delete-and-reallocate never reuses codes; 200
  concurrent allocators produce exactly `T1..T200`.
- `networkfs_test.go` (internal): mount parsing, longest-prefix
  match, network fs classification.
- `migrations/registry_test.go`: registries validate, versions are
  unique and gapless.
- `config/loader_test.go`: empty layers equal defaults; precedence
  system < user < project < scope < env < flags; duration strings in
  TOML and env; project discovery upward; invalid layers rejected;
  missing files skipped; write/read round trip preserves everything.

Run the backend both ways: plain `go test ./internal/store/
./internal/config/` (pure-Go fallback) and
`go test -tags sqlite_fts5 ./internal/store/` (production cgo path
with sqlite-vec). The 256 MiB WAL threshold is exercised through a
small `WALAlertBytes` override; the threshold itself is unchanged.

## Deferred (not in H2)

No journal/agenda/telemetry semantics (H3/H9/H10 own the content;
H2 only stores and rotates it), no retriever or ranking (H6/H11),
no code-graph indexing (later milestone), no multi-space embedding
sharding (single 512-dim space; reindexing starts from the shadow),
no installer or supervisor wiring (H10; `WriteFile` and `Degraded()`
are the hooks), no live paid-provider checks.
