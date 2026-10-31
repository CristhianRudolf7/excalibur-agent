package store

import (
	"context"
	"errors"
	"io"
	"time"
)

// Sentinel errors returned by Store implementations.
var (
	// ErrBusy: a write could not acquire its lock after busy_timeout plus
	// bounded retries (Section 4.3). Never raised for porter-internal
	// contention, only for external lock holders.
	ErrBusy = errors.New("store: database is busy")
	// ErrDiskFull: disk full; background writes pause, interactive writes
	// continue while space lasts (Section 4.3).
	ErrDiskFull = errors.New("store: disk full")
	// ErrCorrupt: quick_check failed at boot (Section 4.3).
	ErrCorrupt = errors.New("store: database is corrupt")
	// ErrNetworkFS: the database lives on a network filesystem, so WAL
	// is disabled in favor of DELETE with a degradation notice.
	ErrNetworkFS = errors.New("store: network filesystem detected")
	// ErrBlobNotFound: no blob for this digest.
	ErrBlobNotFound = errors.New("store: blob not found")
	// ErrMigrationFailed: a migration left the schema untouched (4.6).
	ErrMigrationFailed = errors.New("store: migration failed")
	// ErrWriteCanceled: the caller's context was canceled before the
	// porter took the command; nothing was executed (Section 4.3).
	ErrWriteCanceled = errors.New("store: write canceled before execution")
)

// Priority lanes of the porter, highest first: interactive (messages,
// master turns) > journal (run journals) > normal > background
// (consolidation, indexing). Background writes yield (Section 4.3).
type Priority int

// Porter priorities.
const (
	PriorityInteractive Priority = iota
	PriorityJournal
	PriorityNormal
	PriorityBackground
)

// String renders the lane name.
func (p Priority) String() string {
	switch p {
	case PriorityInteractive:
		return "interactive"
	case PriorityJournal:
		return "journal"
	case PriorityNormal:
		return "normal"
	case PriorityBackground:
		return "background"
	default:
		return "unknown"
	}
}

// Result is the outcome of an Exec.
type Result interface {
	// RowsAffected reports modified rows.
	RowsAffected() (int64, error)
}

// Row scans a single row.
type Row interface {
	// Scan copies the row columns into dest.
	Scan(dest ...any) error
}

// Rows iterates a result set with explicit close.
type Rows interface {
	// Next advances to the next row.
	Next() bool
	// Scan copies the current row columns into dest.
	Scan(dest ...any) error
	// Err reports an iteration error.
	Err() error
	// Close releases the rows.
	Close() error
}

// Execer is the statement surface the porter hands to one write command.
// One command runs inside a savepoint of a group-commit transaction, so
// a constraint failure rolls back only that command (Section 4.3).
type Execer interface {
	Exec(ctx context.Context, query string, args ...any) (Result, error)
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
}

// Queryer is the read surface handed out by the read pool.
type Queryer interface {
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
}

// WriteFunc is one porter command. It must be quick and must not block
// on other store operations: the porter serializes writers.
type WriteFunc func(ctx context.Context, ex Execer) error

// ReadFunc is one pooled read. Transactions are short: read and release
// so WAL checkpoints are never starved (Section 4.3).
type ReadFunc func(ctx context.Context, q Queryer) error

// Writer is the porter: one dedicated writer goroutine per database
// file, fed by a bounded channel per priority lane (Section 4.3).
type Writer interface {
	// Write enqueues fn on lane p. It blocks the caller on backpressure
	// until there is room or ctx expires; commands canceled before the
	// writer takes them are discarded with ErrWriteCanceled. The porter
	// opens BEGIN IMMEDIATE, group-commits and answers per command.
	Write(ctx context.Context, p Priority, fn WriteFunc) error
}

// ReadPool is the pool of read connections with one lane reserved for
// the interactive path (Section 4.3).
type ReadPool interface {
	// Read runs fn on any pooled reader.
	Read(ctx context.Context, fn ReadFunc) error
	// ReadInteractive runs fn on the lane reserved for intake, the fast
	// lane, status and master turns.
	ReadInteractive(ctx context.Context, fn ReadFunc) error
}

// BlobInfo describes a stored blob.
type BlobInfo struct {
	Digest string
	Size   int64
}

// BlobStore is the content-addressed store (sha256, zstd) of Section
// 4.2: atomic write (temp + fsync + rename) before any row may
// reference the blob, counted references and daily GC of blobs that
// are unreferenced for over 24 hours.
type BlobStore interface {
	// Put stores data and returns its hex digest.
	Put(ctx context.Context, r io.Reader) (string, error)
	// Open returns the blob bytes for digest.
	Open(ctx context.Context, digest string) (io.ReadCloser, error)
	// Stat describes a stored blob.
	Stat(ctx context.Context, digest string) (BlobInfo, error)
	// AddRef records one more reference to digest.
	AddRef(ctx context.Context, digest string) error
	// Release drops one reference to digest.
	Release(ctx context.Context, digest string) error
	// CollectGarbage deletes unreferenced blobs older than olderThan and
	// reports reclaimed bytes.
	CollectGarbage(ctx context.Context, olderThan time.Duration) (int64, error)
}

// Migration is one numbered, transactional schema migration. Migrations
// follow expand-then-contract: a version only adds compatible tables or
// columns; removal lands in a later version so the previous binary can
// still read the migrated database (Section 4.6).
type Migration struct {
	Version int
	Name    string
	// Up applies the migration. The registry runs it in one transaction.
	Up WriteFunc
}

// MigrationRegistry holds the embedded, numbered migrations.
type MigrationRegistry interface {
	// Register adds a migration; versions must be unique and gapless.
	Register(m Migration) error
	// Ordered returns migrations sorted by version.
	Ordered() []Migration
	// Latest returns the highest registered version, or 0 if empty.
	Latest() int
	// Validate checks uniqueness and gaplessness.
	Validate() error
}

// Store is the full storage contract behind one database file family:
// porter writes, pooled reads, blobs and migrations.
type Store interface {
	Writer
	ReadPool
	// Blob returns the content-addressed store.
	Blob() BlobStore
	// Migrations returns the schema migration registry.
	Migrations() MigrationRegistry
	// Close shuts the writer down cleanly and closes the pools.
	Close() error
}
