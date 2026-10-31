// Package migrations holds the embedded, numbered, transactional
// schema migrations of Section 4.6 (expand-then-contract): a version
// only adds compatible tables or columns, and removal lands in a
// later version so the previous binary can still read a database
// migrated by the next one. Populated in milestone H2; the Store,
// Migration and MigrationRegistry contracts live in internal/store.
//
// Layout: v1 conversation and work tables, v2 agents, v3 memory with
// FTS5, v4 agenda and files with vec0 (probed, skipped when the
// module is missing), plus the telemetry file set. Appendix A lists
// every table; Section 4.7 adds tombstones (the deletion marker) and
// Section 4.4 the short-code counters, both recorded in
// docs/decisions.
package migrations
