package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// ErrClosed is returned when the database is used after Close.
var ErrClosed = errors.New("store: database is closed")

// command is one queued porter write.
type command struct {
	prio Priority
	fn   WriteFunc
	resp chan error
	ctx  context.Context
	// enqueuedAt measures backpressure latency (Section 4.8 evidence).
	enqueuedAt time.Time
}

// PorterStats is a snapshot of writer behavior for tests and evidence.
type PorterStats struct {
	// Executed counts committed commands per lane.
	Executed [4]int64
	// Failed counts per-command failures per lane.
	Failed [4]int64
	// Batches counts group commits.
	Batches int64
	// BusyRetries counts external-lock retries.
	BusyRetries int64
	// BusyErrors counts batches failed with ErrBusy.
	BusyErrors int64
	// FullErrors counts batches failed with ErrDiskFull.
	FullErrors int64
	// Canceled counts commands discarded before execution.
	Canceled int64
	// QueueWait is the total enqueue-to-take latency per lane.
	QueueWait [4]time.Duration
	// ExecTime is the total execution latency per lane.
	ExecTime [4]time.Duration
}

// DB is one SQLite file behind the Store contract: a single writer
// goroutine (the porter of Section 4.3) fed by four priority lanes, a
// WAL read pool with a reserved interactive lane, and a
// content-addressed blob store beside the file.
type DB struct {
	path   string
	opts   Options
	driver string

	writerDB        *sql.DB
	writerConn      *sql.Conn
	readDB          *sql.DB
	readConns       chan *sql.Conn
	interactiveDB   *sql.DB
	interactiveConn *sql.Conn
	interactiveMu   sync.Mutex

	queues [4]chan command
	stop   chan struct{}
	done   chan struct{}

	// notifyChan wakes the writer when new work arrives (capacity 1,
	// coalesced; the writer always rescans every lane).
	notifyChan chan struct{}

	mu     sync.Mutex
	closed bool

	diskFull   atomic.Bool
	diskFullTx atomic.Bool // edge trigger for OnDiskFull
	walAlerted atomic.Bool

	degraded    bool
	journalMode string
	restored    bool

	registry MigrationRegistry
	blobs    *blobStore

	statsMu sync.Mutex
	stats   PorterStats
}

// Open opens (creating) one database file and starts its porter.
// Migrations in opts are applied before the first write is accepted.
// The registry behind opts is exposed via Migrations.
func Open(ctx context.Context, path string, opts Options, reg MigrationRegistry) (*DB, error) {
	opts = opts.withDefaults()
	setupDriver()

	db := &DB{
		path:       path,
		opts:       opts,
		driver:     sqliteDriverName,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		notifyChan: make(chan struct{}, 1),
	}
	for i := range db.queues {
		db.queues[i] = make(chan command, opts.QueueDepth)
	}
	if reg == nil {
		reg = NewRegistry()
	}
	db.registry = reg

	if !opts.InMemory {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := ensureFilePerm(path); err != nil {
			return nil, err
		}
		if opts.ForceNetworkFS || detectNetworkFS(filepath.Dir(path)) {
			db.degraded = true
		}
	}

	dsn := path
	if opts.InMemory {
		name := opts.InMemoryName
		if name == "" {
			name = "excalibur"
		}
		dsn = "file:" + name + "?mode=memory&cache=shared"
	}

	writerDB, err := sql.Open(db.driver, dsn)
	if err != nil {
		return nil, err
	}
	writerDB.SetMaxOpenConns(1)
	db.writerDB = writerDB

	conn, err := writerDB.Conn(ctx)
	if err != nil {
		_ = writerDB.Close()
		return nil, err
	}
	db.writerConn = conn

	if err := db.pragmaWriter(ctx); err != nil {
		_ = conn.Close()
		_ = writerDB.Close()
		return nil, err
	}

	// Crash recovery: quick_check first (Section 4.3). A failure with
	// a backup available restores the latest daily copy and rebuilds
	// what is rebuildable (FTS indexes) before migrations run.
	if !opts.InMemory {
		ok, qerr := quickCheck(ctx, conn)
		if qerr != nil || !ok {
			if !opts.AutoRestore {
				_ = conn.Close()
				_ = writerDB.Close()
				return nil, ErrCorrupt
			}
			restored, rerr := restoreLatest(ctx, path, BackupsDirFor(path))
			if rerr != nil {
				_ = conn.Close()
				_ = writerDB.Close()
				return nil, rerr
			}
			if !restored {
				_ = conn.Close()
				_ = writerDB.Close()
				return nil, ErrCorrupt
			}
			// Reopen on the restored copy: the old handle may cache
			// the corrupt image.
			_ = conn.Close()
			_ = writerDB.Close()
			writerDB2, err := sql.Open(db.driver, dsn)
			if err != nil {
				return nil, err
			}
			writerDB2.SetMaxOpenConns(1)
			conn2, err := writerDB2.Conn(ctx)
			if err != nil {
				_ = writerDB2.Close()
				return nil, err
			}
			db.writerDB = writerDB2
			db.writerConn = conn2
			if err := db.pragmaWriter(ctx); err != nil {
				_ = conn2.Close()
				_ = writerDB2.Close()
				return nil, err
			}
			if ok2, qerr2 := quickCheck(ctx, conn2); qerr2 != nil || !ok2 {
				_ = conn2.Close()
				_ = writerDB2.Close()
				return nil, ErrCorrupt
			}
			db.restored = true
		}
	}

	if err := applyMigrations(ctx, db.writerConn, reg); err != nil {
		_ = db.writerConn.Close()
		_ = db.writerDB.Close()
		return nil, err
	}
	if db.restored {
		if err := rebuildFTS(ctx, connExecer{conn: db.writerConn}); err != nil {
			_ = db.writerConn.Close()
			_ = db.writerDB.Close()
			return nil, err
		}
	}

	if err := db.openReads(ctx); err != nil {
		_ = db.writerConn.Close()
		_ = db.writerDB.Close()
		return nil, err
	}

	blobDir := optsBlobDir(path, opts)
	db.blobs = newBlobStore(blobDir, db)
	// Crash leftovers (.tmp-*) are never referenced; drop them.
	db.blobs.cleanTemps()

	onFail := opts.OnWriterPanic
	if onFail == nil {
		onFail = defaultWriterPanic
	}
	safego.Go("store-porter-"+filepath.Base(path), func(owner string, recovered any, stack []byte) {
		onFail(owner, recovered, stack)
	}, db.run)

	return db, nil
}

// ensureFilePerm creates the file with 0600 when missing and leaves
// existing permissions untouched.
func ensureFilePerm(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return f.Close()
}

// pragmas applied to the writer connection.
func (db *DB) pragmaWriter(ctx context.Context) error {
	mode := db.opts.ForceJournalMode
	if mode == "" {
		mode = "WAL"
		if db.degraded {
			mode = "DELETE"
		}
	}
	db.journalMode = mode
	busyMS := int(db.opts.BusyTimeout / time.Millisecond)
	if busyMS < 0 {
		busyMS = 0
	}
	syncMode := "NORMAL"
	if db.opts.SynchronousFULL {
		syncMode = "FULL"
	}
	stmts := []string{
		fmt.Sprintf("PRAGMA busy_timeout=%d", busyMS),
		"PRAGMA journal_mode=" + mode,
		"PRAGMA synchronous=" + syncMode,
		fmt.Sprintf("PRAGMA cache_size=%d", -db.opts.WriterCacheKB),
		fmt.Sprintf("PRAGMA mmap_size=%d", db.opts.MMapBytes),
	}
	for _, s := range stmts {
		if _, err := db.writerConn.ExecContext(ctx, s); err != nil {
			// journal_mode on in-memory databases reports "memory";
			// keep going, the effective mode is recorded below.
			if strings.HasPrefix(s, "PRAGMA journal_mode") && db.opts.InMemory {
				continue
			}
			return fmt.Errorf("store: pragma %q: %w", s, err)
		}
	}
	// Record the effective journal mode (WAL, DELETE, memory...).
	var effective string
	if err := db.writerConn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&effective); err == nil && effective != "" {
		db.journalMode = strings.ToUpper(effective)
		if db.journalMode == "MEMORY" && db.degraded {
			db.journalMode = "MEMORY"
		}
	}
	return nil
}

// openReads builds the shared pool and the reserved interactive lane.
// Each connection is pragma'd once and held for life, so settings can
// never drift when the pool recycles handles (Section 4.3).
func (db *DB) openReads(ctx context.Context) error {
	size := db.opts.readPoolSize()
	shared := size - 1
	if shared < 1 {
		shared = 1
	}
	if db.opts.InMemory {
		// Shared-cache memory databases share one image; one held
		// connection per lane keeps semantics identical to files.
		shared = 1
	}

	mkDB := func() (*sql.DB, error) {
		dsn := db.path
		if db.opts.InMemory {
			name := db.opts.InMemoryName
			if name == "" {
				name = "excalibur"
			}
			dsn = "file:" + name + "?mode=memory&cache=shared"
		}
		pool, err := sql.Open(db.driver, dsn)
		if err != nil {
			return nil, err
		}
		// The pool only ever serves the handles we check out below
		// and hold for life; sizing it exactly prevents surprise
		// handles without pragmas.
		return pool, nil
	}
	pragmaRead := func(ctx context.Context, c *sql.Conn) error {
		busyMS := int(db.opts.BusyTimeout / time.Millisecond)
		if busyMS < 0 {
			busyMS = 0
		}
		syncMode := "NORMAL"
		if db.opts.SynchronousFULL {
			syncMode = "FULL"
		}
		for _, s := range []string{
			fmt.Sprintf("PRAGMA busy_timeout=%d", busyMS),
			"PRAGMA query_only=ON",
			fmt.Sprintf("PRAGMA cache_size=%d", -db.opts.ReaderCacheKB),
			"PRAGMA synchronous=" + syncMode,
			fmt.Sprintf("PRAGMA mmap_size=%d", db.opts.MMapBytes),
		} {
			if _, err := c.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("store: read pragma %q: %w", s, err)
			}
		}
		return nil
	}

	readDB, err := mkDB()
	if err != nil {
		return err
	}
	readDB.SetMaxOpenConns(shared)
	readDB.SetMaxIdleConns(shared)
	db.readDB = readDB
	db.readConns = make(chan *sql.Conn, shared)
	for range shared {
		c, err := readDB.Conn(ctx)
		if err != nil {
			_ = readDB.Close()
			return err
		}
		if err := pragmaRead(ctx, c); err != nil {
			_ = c.Close()
			_ = readDB.Close()
			return err
		}
		db.readConns <- c
	}

	interactiveDB, err := mkDB()
	if err != nil {
		_ = readDB.Close()
		return err
	}
	interactiveDB.SetMaxOpenConns(1)
	interactiveDB.SetMaxIdleConns(1)
	db.interactiveDB = interactiveDB
	ic, err := interactiveDB.Conn(ctx)
	if err != nil {
		_ = readDB.Close()
		_ = interactiveDB.Close()
		return err
	}
	if err := pragmaRead(ctx, ic); err != nil {
		_ = ic.Close()
		_ = readDB.Close()
		_ = interactiveDB.Close()
		return err
	}
	db.interactiveConn = ic
	return nil
}

// Path returns the database file path.
func (db *DB) Path() string { return db.path }

// Degraded reports network-filesystem degradation (DELETE instead of
// WAL, Section 4.3).
func (db *DB) Degraded() bool { return db.degraded }

// JournalMode returns the effective journal mode.
func (db *DB) JournalMode() string { return db.journalMode }

// Restored reports whether Open restored this file from a backup.
func (db *DB) Restored() bool { return db.restored }

// QueueLen reports queued (not yet taken) commands on lane p.
func (db *DB) QueueLen(p Priority) int {
	if p < PriorityInteractive || p > PriorityBackground {
		return 0
	}
	return len(db.queues[p])
}

// Stats snapshots porter counters.
func (db *DB) Stats() PorterStats {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	return db.stats
}

// Migrations implements Store.
func (db *DB) Migrations() MigrationRegistry { return db.registry }

// Blob implements Store.
func (db *DB) Blob() BlobStore { return db.blobs }

// Read implements Store: one pooled reader, released when fn returns
// so WAL checkpoints are never starved (Section 4.3).
func (db *DB) Read(ctx context.Context, fn ReadFunc) error {
	db.mu.Lock()
	closed := db.closed
	db.mu.Unlock()
	if closed {
		return ErrClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c := <-db.readConns:
		defer func() { db.readConns <- c }()
		return fn(ctx, queryerConn{conn: c})
	case <-db.stop:
		return ErrClosed
	}
}

// ReadInteractive implements Store: the lane reserved for intake, the
// fast lane, status and master turns (Section 4.3).
func (db *DB) ReadInteractive(ctx context.Context, fn ReadFunc) error {
	db.mu.Lock()
	closed := db.closed
	db.mu.Unlock()
	if closed {
		return ErrClosed
	}
	// The interactive lane serializes briefly rather than waiting
	// behind background readers; turns are short by contract.
	db.interactiveMu.Lock()
	defer db.interactiveMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case <-db.stop:
		return ErrClosed
	default:
	}
	return fn(ctx, queryerConn{conn: db.interactiveConn})
}

// Close drains queued writes, checkpoints, and releases everything.
func (db *DB) Close() error {
	db.mu.Lock()
	if db.closed {
		db.mu.Unlock()
		return nil
	}
	db.closed = true
	close(db.stop)
	db.mu.Unlock()

	<-db.done

	// Best-effort final checkpoint so the next open is cheap.
	_ = db.checkpoint("PASSIVE")

	for _, c := range db.drainConns() {
		_ = c.Close()
	}
	if db.interactiveConn != nil {
		_ = db.interactiveConn.Close()
	}
	if db.readDB != nil {
		_ = db.readDB.Close()
	}
	if db.interactiveDB != nil {
		_ = db.interactiveDB.Close()
	}
	if db.writerConn != nil {
		_ = db.writerConn.Close()
	}
	if db.writerDB != nil {
		_ = db.writerDB.Close()
	}
	return nil
}

func (db *DB) drainConns() []*sql.Conn {
	var out []*sql.Conn
	for {
		select {
		case c := <-db.readConns:
			out = append(out, c)
		default:
			return out
		}
	}
}
