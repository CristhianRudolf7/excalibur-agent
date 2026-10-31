package envsnap

import (
	"context"
	"sync"
)

// Cached collects once and renders the same bytes for the whole run,
// keeping the L2 prefix stable across steps (Section 11.1 rule 3).
// It implements EnvProvider for the engine and CLI paths.
type Cached struct {
	collector *Collector
	cfg       Config
	once      sync.Once
	snap      Snapshot
	ready     bool
	mu        sync.Mutex
}

// NewCached returns a once-per-run provider over cfg.
func NewCached(cfg Config) *Cached {
	cfg = cfg.Normalize()
	return &Cached{collector: NewCollector(cfg), cfg: cfg}
}

// Snapshot collects on first call and returns the same snapshot after.
func (c *Cached) Snapshot(ctx context.Context) Snapshot {
	c.mu.Lock()
	if c.ready {
		snap := c.snap
		c.mu.Unlock()
		return snap
	}
	c.mu.Unlock()
	c.once.Do(func() {
		snap := c.collector.Collect(ctx)
		c.mu.Lock()
		c.snap = snap
		c.ready = true
		c.mu.Unlock()
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

// Ready reports whether the snapshot was collected.
func (c *Cached) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

// RenderCompact implements EnvProvider.
func (c *Cached) RenderCompact() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap.RenderCompact(c.cfg.MaxRenderBytes)
}

// SnapshotForTest installs a fixed snapshot (tests only).
func (c *Cached) SnapshotForTest(s Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snap = s
	c.ready = true
}
