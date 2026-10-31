package fake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// RecordedQuery is one observed statement with its arguments.
type RecordedQuery struct {
	Query string
	Args  []any
}

// ScriptedRows serves canned rows for one query string.
type ScriptedRows struct {
	Columns [][]any
	Err     error
}

// FakeResult is a scripted Exec outcome.
type FakeResult struct {
	N   int64
	Err error
}

// RowsAffected implements store.Result.
func (r FakeResult) RowsAffected() (int64, error) { return r.N, r.Err }

// FakeRow is a scripted single row.
type FakeRow struct {
	Values []any
	Err    error
}

// Scan implements store.Row.
func (r FakeRow) Scan(dest ...any) error {
	if r.Err != nil {
		return r.Err
	}
	if len(dest) != len(r.Values) {
		return fmt.Errorf("fake: Scan arity %d != %d", len(dest), len(r.Values))
	}
	for i, v := range r.Values {
		switch d := dest[i].(type) {
		case *string:
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("fake: value %d is not a string", i)
			}
			*d = s
		case *int64:
			n, ok := v.(int64)
			if !ok {
				return fmt.Errorf("fake: value %d is not an int64", i)
			}
			*d = n
		case *int:
			n, ok := v.(int)
			if !ok {
				return fmt.Errorf("fake: value %d is not an int", i)
			}
			*d = n
		case *any:
			*d = v
		default:
			return fmt.Errorf("fake: unsupported Scan target %T", dest[i])
		}
	}
	return nil
}

// FakeRows iterates scripted rows.
type FakeRows struct {
	rows [][]any
	pos  int
	err  error
}

// Next implements store.Rows.
func (r *FakeRows) Next() bool {
	r.pos++
	return r.pos <= len(r.rows)
}

// Scan implements store.Rows.
func (r *FakeRows) Scan(dest ...any) error {
	return FakeRow{Values: r.rows[r.pos-1]}.Scan(dest...)
}

// Err implements store.Rows.
func (r *FakeRows) Err() error { return r.err }

// Close implements store.Rows.
func (r *FakeRows) Close() error { return nil }

// FakeExecer records statements and serves scripted answers. The zero
// value is ready to use.
type FakeExecer struct {
	mu      sync.Mutex
	Queries []RecordedQuery
	Results map[string]FakeResult
	RowsMap map[string]ScriptedRows
	RowMap  map[string]FakeRow
}

// record appends one observed statement.
func (f *FakeExecer) record(query string, args []any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]any, len(args))
	copy(cp, args)
	f.Queries = append(f.Queries, RecordedQuery{Query: query, Args: cp})
}

// snapshot returns copies of the scripted answers for query.
func (f *FakeExecer) snapshot(query string) (FakeResult, ScriptedRows, FakeRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Results[query], f.RowsMap[query], f.RowMap[query]
}

// Exec implements store.Execer.
func (f *FakeExecer) Exec(_ context.Context, query string, args ...any) (store.Result, error) {
	f.record(query, args)
	res, _, _ := f.snapshot(query)
	return res, res.Err
}

// Query implements store.Execer and store.Queryer.
func (f *FakeExecer) Query(_ context.Context, query string, args ...any) (store.Rows, error) {
	f.record(query, args)
	_, scripted, _ := f.snapshot(query)
	if scripted.Err != nil {
		return nil, scripted.Err
	}
	rows := &FakeRows{}
	for _, cols := range scripted.Columns {
		cp := make([]any, len(cols))
		copy(cp, cols)
		rows.rows = append(rows.rows, cp)
	}
	return rows, nil
}

// QueryRow implements store.Execer and store.Queryer.
func (f *FakeExecer) QueryRow(_ context.Context, query string, args ...any) store.Row {
	f.record(query, args)
	_, _, row := f.snapshot(query)
	return row
}

// Recorded returns a copy of the recorded statements.
func (f *FakeExecer) Recorded() []RecordedQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RecordedQuery(nil), f.Queries...)
}

// FakeBlobStore keeps blobs in memory with reference counts.
type FakeBlobStore struct {
	mu   sync.Mutex
	data map[string][]byte
	refs map[string]int
}

// NewFakeBlobStore returns an empty blob store.
func NewFakeBlobStore() *FakeBlobStore {
	return &FakeBlobStore{data: map[string][]byte{}, refs: map[string]int{}}
}

// Put implements store.BlobStore.
func (b *FakeBlobStore) Put(_ context.Context, r io.Reader) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[digest] = append([]byte(nil), raw...)
	return digest, nil
}

// Open implements store.BlobStore.
func (b *FakeBlobStore) Open(_ context.Context, digest string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, ok := b.data[digest]
	if !ok {
		return nil, store.ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), raw...))), nil
}

// Stat implements store.BlobStore.
func (b *FakeBlobStore) Stat(_ context.Context, digest string) (store.BlobInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, ok := b.data[digest]
	if !ok {
		return store.BlobInfo{}, store.ErrBlobNotFound
	}
	return store.BlobInfo{Digest: digest, Size: int64(len(raw))}, nil
}

// AddRef implements store.BlobStore.
func (b *FakeBlobStore) AddRef(_ context.Context, digest string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.data[digest]; !ok {
		return store.ErrBlobNotFound
	}
	b.refs[digest]++
	return nil
}

// Release implements store.BlobStore.
func (b *FakeBlobStore) Release(_ context.Context, digest string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.refs[digest] <= 0 {
		return fmt.Errorf("fake: release of unreferenced blob")
	}
	b.refs[digest]--
	return nil
}

// CollectGarbage implements store.BlobStore: it drops unreferenced
// blobs regardless of age and reports reclaimed bytes.
func (b *FakeBlobStore) CollectGarbage(_ context.Context, _ time.Duration) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var freed int64
	for digest, raw := range b.data {
		if b.refs[digest] == 0 {
			freed += int64(len(raw))
			delete(b.data, digest)
		}
	}
	return freed, nil
}

// FakeMigrations is an in-memory store.MigrationRegistry.
type FakeMigrations struct {
	mu         sync.Mutex
	migrations []store.Migration
}

// Register implements store.MigrationRegistry.
func (m *FakeMigrations) Register(mig store.Migration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.migrations = append(m.migrations, mig)
	return m.ValidateLocked()
}

// Ordered implements store.MigrationRegistry.
func (m *FakeMigrations) Ordered() []store.Migration {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]store.Migration(nil), m.migrations...)
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// Latest implements store.MigrationRegistry.
func (m *FakeMigrations) Latest() int {
	ordered := m.Ordered()
	if len(ordered) == 0 {
		return 0
	}
	return ordered[len(ordered)-1].Version
}

// Validate implements store.MigrationRegistry: versions must be unique
// and gapless starting at 1.
func (m *FakeMigrations) Validate() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ValidateLocked()
}

// ValidateLocked validates with the lock held.
func (m *FakeMigrations) ValidateLocked() error {
	seen := map[int]bool{}
	max := 0
	for _, mig := range m.migrations {
		if mig.Version < 1 || seen[mig.Version] {
			return fmt.Errorf("fake: bad migration version %d", mig.Version)
		}
		seen[mig.Version] = true
		if mig.Version > max {
			max = mig.Version
		}
	}
	for v := 1; v <= max; v++ {
		if !seen[v] {
			return fmt.Errorf("fake: migration gap at version %d", v)
		}
	}
	return nil
}

// FakeStore is an in-memory store.Store. Writes run the command
// immediately against the shared FakeExecer; canceled contexts are
// discarded with store.ErrWriteCanceled, mirroring porter semantics.
type FakeStore struct {
	Execer     *FakeExecer
	blobs      *FakeBlobStore
	migrations *FakeMigrations
	closed     bool
	mu         sync.Mutex
}

// NewFakeStore returns an empty fake store.
func NewFakeStore() *FakeStore {
	return &FakeStore{
		Execer:     &FakeExecer{Results: map[string]FakeResult{}, RowsMap: map[string]ScriptedRows{}, RowMap: map[string]FakeRow{}},
		blobs:      NewFakeBlobStore(),
		migrations: &FakeMigrations{},
	}
}

// Write implements store.Writer.
func (s *FakeStore) Write(ctx context.Context, _ store.Priority, fn store.WriteFunc) error {
	select {
	case <-ctx.Done():
		return store.ErrWriteCanceled
	default:
	}
	return fn(ctx, s.Execer)
}

// Read implements store.ReadPool.
func (s *FakeStore) Read(ctx context.Context, fn store.ReadFunc) error {
	return fn(ctx, s.Execer)
}

// ReadInteractive implements store.ReadPool.
func (s *FakeStore) ReadInteractive(ctx context.Context, fn store.ReadFunc) error {
	return fn(ctx, s.Execer)
}

// Blob implements store.Store.
func (s *FakeStore) Blob() store.BlobStore { return s.blobs }

// Migrations implements store.Store.
func (s *FakeStore) Migrations() store.MigrationRegistry { return s.migrations }

// Close implements store.Store.
func (s *FakeStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// Closed reports whether Close ran.
func (s *FakeStore) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
