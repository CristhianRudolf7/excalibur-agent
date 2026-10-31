//go:build cgo && sqlite_fts5

package store

import (
	"errors"
	"sync"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	sqlite3 "github.com/mattn/go-sqlite3"
)

// This file wires the production SQLite backend of Section 4.1:
// mattn/go-sqlite3 (cgo, FTS5 enabled by the sqlite_fts5 tag) with the
// sqlite-vec extension registered process-wide. It only compiles when
// cgo is available and the sqlite_fts5 tag is set; every other build
// (plain go test/vet, darwin, windows, CGO_ENABLED=0) uses
// driver_modernc.go, a pure-Go fallback behind the same Store
// interface (Section 3.4 note).

// sqliteDriverName is the database/sql driver name for this backend.
const sqliteDriverName = "sqlite3"

var vecAutoOnce sync.Once

// setupDriver performs one-time backend registration.
func setupDriver() {
	vecAutoOnce.Do(func() {
		// Every future connection in this process loads sqlite-vec.
		sqlitevec.Auto()
	})
}

// classifyError maps backend errors to porter behavior.
func classifyError(err error) errorClass {
	var serr sqlite3.Error
	if errors.As(err, &serr) {
		switch int(serr.Code) & 0xFF {
		case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED
			return classBusy
		case 13: // SQLITE_FULL
			return classFull
		case 19: // SQLITE_CONSTRAINT
			return classConstraint
		}
	}
	return classOther
}
