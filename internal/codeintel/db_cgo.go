//go:build cgo && sqlite_fts5

package codeintel

import (
	_ "github.com/mattn/go-sqlite3"
)

// Production graph backend (Section 4.1): mattn/go-sqlite3 with FTS5,
// selected with -tags sqlite_fts5. The pure-Go fallback in
// db_fallback.go serves every other build, including darwin/windows.
const graphDriverName = "sqlite3"
