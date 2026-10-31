// Package store defines the storage contracts of Excalibur: one writer
// ("porter") per database file with priority lanes, a read pool with a
// reserved interactive lane, the content-addressed blob store and the
// migration registry.
//
// The interfaces are driver-agnostic on purpose: the SQLite porter of
// Section 4.3 is implemented behind Execer/Queryer in milestone H2, and
// unit tests use the in-memory fakes in internal/store/fake.
//
// Architecture reference: Sections 4.1-4.6. Implemented in milestones
// H0 (contracts, wave 0) and H2 (SQLite porter, blobs, migrations).
package store
