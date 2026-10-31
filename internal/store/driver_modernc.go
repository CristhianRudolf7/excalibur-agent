//go:build !cgo || !sqlite_fts5

package store

import (
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// This file is the pure-Go fallback backend behind the Store interface
// (Section 3.4 note): modernc.org/sqlite, which bundles FTS5 without
// cgo. It compiles everywhere, so plain builds (go test ./..., go vet,
// GOOS=darwin/windows builds) always have a working store. The
// production backend (mattn/go-sqlite3 + sqlite-vec, cgo) lives in
// driver_sqlite3.go and is selected with -tags sqlite_fts5.
//
// Behavioral parity: identical SQL, same pragmas, FTS5 always on.
// Known gap: no vec0 module here, so vector tables are skipped and
// vector search reports unavailable (the degradation of Section 10.8).

// sqliteDriverName is the database/sql driver name for this backend.
const sqliteDriverName = "sqlite"

// setupDriver performs one-time backend registration (none needed).
func setupDriver() {}

// classifyError maps backend errors to porter behavior.
func classifyError(err error) errorClass {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		switch serr.Code() & 0xFF {
		case int(sqlite3.SQLITE_BUSY), int(sqlite3.SQLITE_LOCKED):
			return classBusy
		case int(sqlite3.SQLITE_FULL):
			return classFull
		case int(sqlite3.SQLITE_CONSTRAINT):
			return classConstraint
		}
	}
	return classOther
}
