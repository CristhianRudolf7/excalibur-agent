// Package store defines the storage contracts of Excalibur and, since
// milestone H2, their SQLite implementation: one writer ("porter")
// per database file with four priority lanes and group commit, a WAL
// read pool with a reserved interactive lane, a crash-ordered
// content-addressed blob store, and the migration framework.
//
// Architecture reference: Section 4 (all rows of 4.8 covered by Go
// tests). Implemented in milestones H0 (contracts, wave 0) and H2
// (SQLite porter, blobs, migrations, backups, retention, purge).
package store
