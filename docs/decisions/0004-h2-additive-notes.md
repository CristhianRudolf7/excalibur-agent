# 0004 — Additive schema and config notes for H2

Date: 2026-10-31. Status: accepted.

## What

Small additive choices the H2 storage work needed from shared
contracts (no breakage; everything below only adds):

1. `toml:` struct tags on every `internal/config` field (mirroring
   the existing `json:` names). `BurntSushi/toml` does not fold
   `snake_case` keys onto `CamelCase` fields without them, so
   layered TOML loading requires the tags. No field, type, default
   or validation changed.
2. Three storage-infrastructure tables beyond the Appendix A list,
   each required by its section and named here so reviewers see
   them: `tombstones` (the 4.7 deletion marker), `short_counters`
   (the 4.4 never-reused code allocator), `meta` (backend
   capabilities such as `vector_search`). `embedding_vectors` is
   the 10.8 shadow the architecture implies (rebuilds and model
   changes start from stored vectors, not from a virtual index).
3. `blob_refs.kind` / `blob_refs.run_id` columns so retention (4.7)
   can tell tool outputs from library originals without a second
   index.
4. `foreign_keys` stays off: relationship discipline lives in
   writers, so no test, migration or purge ordering trips on pragma
   enforcement.
5. `Store` itself is untouched. Storage conveniences live as new
   methods on the concrete `*DB` (`Checkpoint`, `CheckWAL`,
   `WALBytes`, `QueueLen`, `Stats`, `KNN`, `VecAvailable`,
   `RebuildFTS`, `QuickCheck`) and new package functions
   (`NextTaskCode` et al, `Backup`/`Restore`, `ApplyRetention`,
   `Purge*`); other agents program to `Store`/`Writer`/`ReadPool`
   and opt into these.

## Why

Wave-0 contracts are frozen except for additive, minimal, recorded
change. Each item above is forced by its architecture section and
changes no existing signature, file layout or wire byte.
