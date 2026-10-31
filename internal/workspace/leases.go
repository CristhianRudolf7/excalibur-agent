package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Lease timings from Appendix B: wait up to 30 s for a busy file,
// expire 10 min without a heartbeat.
const (
	DefaultLeaseWaitMax  = 30 * time.Second
	DefaultLeaseExpireIn = 10 * time.Minute
)

// HeldError reports a busy file with its holder and what the holder
// is doing, so the agent can work on something else or ask the parent
// to arbitrate (Section 16.2).
type HeldError struct {
	Path     string
	Holder   string
	Activity string
	Waited   time.Duration
}

// Error implements error.
func (e *HeldError) Error() string {
	what := e.Activity
	if what == "" {
		what = "working"
	}
	return fmt.Sprintf("workspace: %s is leased by %s (%s)", e.Path, e.Holder, what)
}

// leaseEntry is one held write lease: canonical path, holder run,
// what the holder is doing, and the heartbeat deadline.
type leaseEntry struct {
	holder   string
	activity string
	expires  time.Time
}

// LeaseSnapshot is a point-in-time copy of the lease table for tests
// and evidence (Section 16.6 keeps the table over time).
type LeaseSnapshot struct {
	Path     string
	Holder   string
	Activity string
	Expires  time.Time
	At       time.Time
}

// LeaseManager serializes writers per canonical path (Section 16.2):
// a SQLite table plus an in-RAM fast-path map. In this package the
// table is the Manager itself; the store adapter persists the same
// rows through the porter. Cross-process coordination additionally
// uses platform.FileLock when configured.
type LeaseManager struct {
	mu        sync.Mutex
	leases    map[string]leaseEntry
	waitMax   time.Duration
	expireIn  time.Duration
	now       func() time.Time
	fileLock  platform.FileLock
	osHeld    map[string]platform.Release
	runStates map[string]types.RunState
	// leaseSQL is the optional SQL mirror (AttachLeaseStore): grants
	// write through, RestoreLeases reloads live rows.
	leaseSQL *SQLLeaseStore
}

// LeaseOption tunes a LeaseManager.
type LeaseOption func(*LeaseManager)

// WithLeaseTimings overrides the wait and expiry durations (tests use
// short values; production uses Appendix B).
func WithLeaseTimings(waitMax, expireIn time.Duration) LeaseOption {
	return func(m *LeaseManager) {
		m.waitMax = waitMax
		m.expireIn = expireIn
	}
}

// WithLeaseClock overrides the clock (tests).
func WithLeaseClock(now func() time.Time) LeaseOption {
	return func(m *LeaseManager) { m.now = now }
}

// WithFileLock enables the advisory cross-process lock behind
// platform.FileLock (Section 16.2, consultative).
func WithFileLock(fl platform.FileLock) LeaseOption {
	return func(m *LeaseManager) { m.fileLock = fl }
}

// NewLeaseManager returns a manager with Section 16.2 timings unless
// overridden.
func NewLeaseManager(opts ...LeaseOption) *LeaseManager {
	m := &LeaseManager{
		leases:    map[string]leaseEntry{},
		waitMax:   DefaultLeaseWaitMax,
		expireIn:  DefaultLeaseExpireIn,
		now:       time.Now,
		osHeld:    map[string]platform.Release{},
		runStates: map[string]types.RunState{},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Acquire takes the write lease for path on behalf of holder, waiting
// up to waitMax. The activity string describes what the holder is
// doing and is reported to waiters. The returned release frees the
// lease; it is idempotent and only releases when still held by the
// same holder.
func (m *LeaseManager) Acquire(path, holder, activity string) (release func(), err error) {
	return m.AcquireContext(context.Background(), path, holder, activity)
}

// AcquireContext is Acquire honoring ctx cancellation.
func (m *LeaseManager) AcquireContext(ctx context.Context, path, holder, activity string) (func(), error) {
	canon := filepath.Clean(path)
	start := m.now()
	deadline := start.Add(m.waitMax)
	var osRel platform.Release
	if m.fileLock != nil {
		rel, err := m.lockWithContext(ctx, canon, deadline)
		if err != nil {
			return nil, err
		}
		osRel = rel
	}
	timer := time.NewTimer(m.waitMax)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		e, ok := m.leases[canon]
		if !ok || !m.now().Before(e.expires) {
			m.leases[canon] = leaseEntry{holder: holder, activity: activity, expires: m.now().Add(m.expireIn)}
			m.mu.Unlock()
			m.persistLease(canon, holder)
			return func() {
				m.Release(canon, holder)
				if osRel != nil {
					_ = osRel()
				}
			}, nil
		}
		held := e
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			if osRel != nil {
				_ = osRel()
			}
			return nil, fmt.Errorf("workspace: acquire %s: %w", canon, ctx.Err())
		case <-ticker.C:
			if !m.now().Before(deadline) {
				if osRel != nil {
					_ = osRel()
				}
				return nil, &HeldError{Path: canon, Holder: held.holder, Activity: held.activity, Waited: m.now().Sub(start)}
			}
		}
	}
}

// ctxLocker is the context-aware variant some FileLock
// implementations offer (LinuxFileLock.AcquireContext polls with
// LOCK_NB so a canceled wait never leaves an orphaned holder behind).
type ctxLocker interface {
	AcquireContext(ctx context.Context, path string) (platform.Release, error)
}

// lockWithContext takes the advisory OS lock, bounded by deadline and
// ctx. A nil FileLock (other platforms without an implementation) is a
// no-op.
func (m *LeaseManager) lockWithContext(ctx context.Context, path string, deadline time.Time) (platform.Release, error) {
	if cl, ok := m.fileLock.(ctxLocker); ok {
		bound, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		rel, err := cl.AcquireContext(bound, path)
		if err != nil {
			if h, ok := m.Holding(path); ok {
				return nil, &HeldError{Path: path, Holder: h.Holder, Activity: h.Activity, Waited: m.waitMax}
			}
			return nil, &HeldError{Path: path, Holder: "another process", Waited: m.waitMax}
		}
		return rel, nil
	}
	// Fallback for purely blocking backends: coordinate through a
	// done channel so ctx cancellation and the wait budget both
	// apply. A wait that times out while blocked in the kernel may
	// leave the backend holding; backends should offer
	// AcquireContext instead (see above). The helper runs under
	// safego.Go: a backend panic is logged, not a daemon crash
	// (the wait then ends at the budget with a HeldError).
	type res struct {
		rel platform.Release
		err error
	}
	ch := make(chan res, 1)
	safego.Go("workspace/os-lock-acquire", nil, func() {
		rel, err := m.fileLock.Acquire(path)
		ch <- res{rel: rel, err: err}
	})
	// Bound the blocking acquire by the remaining budget.
	remain := time.Until(deadline)
	if remain < 0 {
		remain = 0
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("workspace: acquire os lock %s: %w", path, ctx.Err())
	case r := <-ch:
		if r.err == nil {
			return r.rel, nil
		}
		return nil, fmt.Errorf("workspace: acquire os lock %s: %w", path, r.err)
	case <-time.After(remain):
		if h, ok := m.Holding(path); ok {
			return nil, &HeldError{Path: path, Holder: h.Holder, Activity: h.Activity, Waited: m.waitMax}
		}
		return nil, &HeldError{Path: path, Holder: "another process", Waited: m.waitMax}
	}
}

// Holding reports the current holder of path, if any.
func (m *LeaseManager) Holding(path string) (LeaseSnapshot, bool) {
	canon := filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.leases[canon]
	if !ok || !m.now().Before(e.expires) {
		return LeaseSnapshot{}, false
	}
	return LeaseSnapshot{Path: canon, Holder: e.holder, Activity: e.activity, Expires: e.expires, At: m.now()}, true
}

// Release frees the lease when held by holder.
func (m *LeaseManager) Release(path, holder string) {
	canon := filepath.Clean(path)
	m.mu.Lock()
	_, ok := m.leases[canon]
	if e, has := m.leases[canon]; has && e.holder == holder {
		delete(m.leases, canon)
	} else {
		ok = false
	}
	sql := m.leaseSQL
	m.mu.Unlock()
	if ok && sql != nil {
		_ = sql.ReleaseRow(canon, holder)
	}
}

// ReleaseAll frees every lease held by holder: run end, pause, block
// or human-paced wait (Section 16.2).
func (m *LeaseManager) ReleaseAll(holder string) int {
	m.mu.Lock()
	n := 0
	for p, e := range m.leases {
		if e.holder == holder {
			delete(m.leases, p)
			n++
		}
	}
	sql := m.leaseSQL
	m.mu.Unlock()
	if n > 0 && sql != nil {
		_, _ = sql.ReleaseHolder(holder)
	}
	return n
}

// Heartbeat renews every lease of holder: the orchestrator calls it
// with the run heartbeat while the run is running, waiting on a tool
// or waiting on subagents, including during long tools
// (Section 16.2). It returns the renewed count.
func (m *LeaseManager) Heartbeat(holder string) int {
	m.mu.Lock()
	n := 0
	var latest time.Time
	for p, e := range m.leases {
		if e.holder == holder {
			e.expires = m.now().Add(m.expireIn)
			m.leases[p] = e
			latest = e.expires
			n++
		}
	}
	sql := m.leaseSQL
	m.mu.Unlock()
	if n > 0 && sql != nil {
		_, _ = sql.HeartbeatRow(holder, latest)
	}
	return n
}

// NotifyRunState applies the Section 16.2 release rules to a run
// state transition: paused and blocked release their leases (so files
// are not immobilized for hours); waiting on the user releases too;
// terminal states release everything. Running, waiting on a tool or
// on subagents keep and renew their leases.
func (m *LeaseManager) NotifyRunState(holder string, state types.RunState, waitReason types.WaitReason) {
	m.mu.Lock()
	m.runStates[holder] = state
	m.mu.Unlock()
	switch state {
	case types.RunPaused, types.RunBlocked:
		m.ReleaseAll(holder)
	case types.RunWaiting:
		if waitReason == types.WaitUser {
			m.ReleaseAll(holder)
		} else {
			m.Heartbeat(holder)
		}
	case types.RunCompleted, types.RunCompletedPending, types.RunFailed, types.RunCanceled:
		m.ReleaseAll(holder)
	default:
		m.Heartbeat(holder)
	}
}

// Snapshot returns the current lease table sorted by path.
func (m *LeaseManager) Snapshot() []LeaseSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	out := make([]LeaseSnapshot, 0, len(m.leases))
	for p, e := range m.leases {
		if !now.Before(e.expires) {
			continue
		}
		out = append(out, LeaseSnapshot{Path: p, Holder: e.holder, Activity: e.activity, Expires: e.expires, At: now})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Expire sweeps leases past their deadline without a heartbeat. It
// returns the expired count. Production calls it on a timer; tests
// call it after advancing the clock.
func (m *LeaseManager) Expire() int {
	m.mu.Lock()
	now := m.now()
	n := 0
	for p, e := range m.leases {
		if !now.Before(e.expires) {
			delete(m.leases, p)
			n++
		}
	}
	sql := m.leaseSQL
	m.mu.Unlock()
	if n > 0 && sql != nil {
		_, _ = sql.ExpireRows(now)
	}
	return n
}
