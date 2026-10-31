package store

import (
	"runtime"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
)

// errorClass is the porter's reading of a backend error. The mapping
// itself is backend-specific (driver_*.go); the handling here is
// portable (Section 4.3).
type errorClass int

const (
	classOther errorClass = iota
	classBusy
	classFull
	classConstraint
)

// Options tunes one database file: writer behavior, durability and
// read pool sizing. Zero Options are valid: DefaultOptions fills in
// the Appendix B initial values.
type Options struct {
	// GroupCommitWait caps group-commit batching (Appendix B: 5ms).
	GroupCommitWait time.Duration
	// GroupCommitMaxOps caps one group commit (Appendix B: 200).
	GroupCommitMaxOps int
	// BusyTimeout is the SQLite busy handler ceiling (Appendix B: 5s).
	BusyTimeout time.Duration
	// BusyRetries bounds external-lock retries (Appendix B: 3).
	BusyRetries int
	// WALAlertBytes warns above this WAL size (Appendix B: 256MiB).
	WALAlertBytes int64
	// ReadPoolSize caps pooled readers; 0 means min(4, NumCPU).
	ReadPoolSize int
	// SynchronousFULL selects synchronous=FULL (durable files of
	// Section 4.2: excalibur.db, vault.db) versus NORMAL (telemetry,
	// code graphs).
	SynchronousFULL bool
	// QueueDepth bounds each priority lane; sends block past it
	// (backpressure, Section 4.3).
	QueueDepth int
	// BlobDir overrides the content-addressed store directory
	// (default: <dbdir>/blobs, Section 4.2).
	BlobDir string
	// InMemory opens a shared-cache in-memory database (tests only).
	InMemory bool
	// InMemoryName names the shared-cache memory database.
	InMemoryName string
	// ForceJournalMode overrides journal-mode selection ("WAL" or
	// "DELETE"); empty means automatic (WAL, DELETE on network
	// filesystems).
	ForceJournalMode string
	// ForceNetworkFS pretends the database lives on a network
	// filesystem (tests for Section 4.3 degradation).
	ForceNetworkFS bool
	// AutoRestore restores the latest backup when quick_check fails at
	// open (Section 4.3). Defaults to true.
	AutoRestore bool
	// AutoRestoreSet records whether AutoRestore was set explicitly.
	AutoRestoreSet bool
	// WriterCacheKB sizes the writer page cache in KiB (Section 4.3:
	// 8 MiB initial).
	WriterCacheKB int
	// ReaderCacheKB sizes each reader page cache in KiB (Section 4.3:
	// 2 MiB initial).
	ReaderCacheKB int
	// MMapBytes sizes the memory-mapped I/O window (Section 4.3:
	// configurable, 0 disables). Applied per connection on open.
	MMapBytes int64
	// OnWALAlert fires on the false->true transition of the WAL size
	// alert (Section 4.3 watcher).
	OnWALAlert func(walBytes int64)
	// OnDiskFull fires once when SQLITE_FULL is observed (Section 4.3:
	// pause background work, tell the master).
	OnDiskFull func()
	// OnWriterPanic fires if the writer goroutine panics. The default
	// exits the process so the supervisor restarts the daemon
	// (Sections 3.2, 4.3: a failed writer is critical).
	OnWriterPanic func(owner string, recovered any, stack []byte)
}

// DefaultOptions returns Appendix B values for a durable database.
func DefaultOptions() Options {
	return Options{
		GroupCommitWait:   5 * time.Millisecond,
		GroupCommitMaxOps: 200,
		BusyTimeout:       5 * time.Second,
		BusyRetries:       3,
		WALAlertBytes:     256 << 20,
		ReadPoolSize:      0,
		SynchronousFULL:   true,
		QueueDepth:        1024,
		WriterCacheKB:     8 << 10,
		ReaderCacheKB:     2 << 10,
		AutoRestore:       true,
	}
}

// TelemetryOptions returns options for telemetry.db: losing the last
// confirmations after a power cut is acceptable (Section 4.2), so
// synchronous=NORMAL.
func TelemetryOptions() Options {
	o := DefaultOptions()
	o.SynchronousFULL = false
	return o
}

// OptionsFromPorter converts the shared harness config (Appendix B,
// porter.*) into store options.
func OptionsFromPorter(p config.PorterConfig, fullSync bool) Options {
	o := DefaultOptions()
	o.GroupCommitWait = p.GroupCommitWait
	o.GroupCommitMaxOps = p.GroupCommitMaxOps
	o.BusyTimeout = p.BusyTimeout
	o.BusyRetries = p.BusyRetries
	o.WALAlertBytes = p.WALAlertBytes
	o.ReadPoolSize = p.ReadPoolSize
	o.SynchronousFULL = fullSync
	return o
}

// readPoolSize resolves 0 to min(4, NumCPU) (Appendix B).
func (o Options) readPoolSize() int {
	if o.ReadPoolSize > 0 {
		return o.ReadPoolSize
	}
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

// withDefaults fills zero values that have non-zero defaults.
func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.GroupCommitWait <= 0 {
		o.GroupCommitWait = d.GroupCommitWait
	}
	if o.GroupCommitMaxOps <= 0 {
		o.GroupCommitMaxOps = d.GroupCommitMaxOps
	}
	if o.BusyTimeout <= 0 {
		o.BusyTimeout = d.BusyTimeout
	}
	if o.BusyRetries < 0 {
		o.BusyRetries = d.BusyRetries
	}
	if o.WALAlertBytes <= 0 {
		o.WALAlertBytes = d.WALAlertBytes
	}
	if o.QueueDepth <= 0 {
		o.QueueDepth = d.QueueDepth
	}
	if o.WriterCacheKB <= 0 {
		o.WriterCacheKB = d.WriterCacheKB
	}
	if o.ReaderCacheKB <= 0 {
		o.ReaderCacheKB = d.ReaderCacheKB
	}
	if !o.AutoRestoreSet {
		o.AutoRestore = true
	}
	return o
}
