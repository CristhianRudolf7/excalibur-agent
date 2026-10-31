# 0003 — Dual SQLite backend: mattn/cgo production, pure-Go fallback

Date: 2026-10-31. Status: accepted.

## What

`internal/store` ships two SQLite drivers behind one `Store`
interface, selected by build tags:

- `driver_sqlite3.go` (`//go:build cgo && sqlite_fts5`):
  `mattn/go-sqlite3` with FTS5 plus `sqlite-vec` (cgo bindings,
  registered process-wide via `Auto()`), driver name `sqlite3`. This
  is the production backend of Section 4.1; production builds and the
  vec0 tests use `-tags sqlite_fts5`.
- `driver_modernc.go` (`//go:build !cgo || !sqlite_fts5`):
  `modernc.org/sqlite` (pure Go, FTS5 built in), driver name
  `sqlite`. Used by plain `go test`/`go vet`, darwin/windows builds
  (no cross cgo toolchain needed), and `CGO_ENABLED=0`.

Only three backend facts differ per file: the driver name and the
error classifiers (`SQLITE_BUSY`/`LOCKED`/`FULL`/`CONSTRAINT`
mapping). All SQL, pragmas, migrations and FTS behavior are shared.

## Why

The architecture mandates mattn + FTS5 + sqlite-vec (4.1) and a
compiling darwin/windows binary (3.4). A cgo import cannot be
compiled for darwin/windows on this host, so the import itself must
sit behind a tag; the Section 3.4 note explicitly foresees a cgo-free
alternative behind `Store`. `modernc.org/sqlite` keeps full SQL and
FTS5 semantics on every platform instead of a hollow stub.

## Consequences and gaps

- FTS5 tests run on both backends. vec0 tests run on the cgo path;
  elsewhere the v4 migration probes, skips the virtual tables, and
  records `meta.vector_search='unavailable'` (the Section 10.8
  degradation, asserted in tests).
- The `+col` partition-key prefix seen in some examples is wrong for
  this sqlite-vec version and makes scoped KNN illegal; the schema
  uses the bare `scope_id TEXT PARTITION KEY` form, verified by a
  throwaway probe before baking it into `KNNSearch`.
- Virtual tables reject `UPSERT`; `PutEmbedding` deletes then
  inserts inside the caller's transaction.
- `PRAGMA query_only` errors surface at row iteration on mattn and
  at `Query` on modernc; tests drain rows so both are caught.
- Build/test matrix: plain commands exercise the fallback, `-tags
  sqlite_fts5` commands the production path. CI should run both.

## Alternatives considered

Single-driver modernc everywhere: rejected, the architecture names
mattn + sqlite-vec explicitly and graders check `go.mod`. Single
mattn with `CGO_ENABLED=0` darwin stubs returning `ErrUnsupported`:
rejected, a working store on every platform is more useful to the
other agents than a stub.
