//go:build !cgo || !sqlite_fts5

package codeintel

import (
	_ "modernc.org/sqlite"
)

// Pure-Go graph backend behind the same SQL (Section 3.4 note):
// modernc.org/sqlite. It serves plain builds (go test, go vet,
// darwin, windows, CGO_ENABLED=0); the cgo production backend lives in
// db_cgo.go and is selected with -tags sqlite_fts5.
const graphDriverName = "sqlite"
