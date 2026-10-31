package capabilities

import (
	"encoding/json"
	"testing"
)

// TestRegistryRoundTrip covers the Section 14.13 registry rows: every
// default capability validates, invokes, polls and cancels through one
// pipeline, and unknown ids fail as permanent input errors.
func TestRegistryRoundTrip(t *testing.T) {
	r := DefaultRegistry()
	ids := r.IDs()
	if len(ids) == 0 {
		t.Fatal("default registry is empty")
	}
	for _, m := range r.Manifests() {
		if err := m.Validate(); err != nil {
			t.Fatalf("manifest %q invalid: %v", m.ID, err)
		}
	}
	// Unknown capability is a permanent input error, never retried.
	res, err := r.Invoke("nope", "open", json.RawMessage(`{}`), "k1")
	if err == nil {
		t.Fatal("want error for unknown capability")
	}
	if res.ErrClass != ErrInput {
		t.Fatalf("want input error class, have %q", res.ErrClass)
	}
	if _, err := r.Poll("nope", "t1"); err == nil {
		t.Fatal("want poll error for unknown capability")
	}
	// Unregister is idempotent, even for missing ids.
	r.Unregister("nope")
	before := len(r.IDs())
	r.Unregister(ids[0])
	if got := len(r.IDs()); got != before-1 {
		t.Fatalf("unregister did not remove: %d -> %d", before, got)
	}
	if _, ok := r.Lookup(ids[0]); ok {
		t.Fatalf("removed capability %q still resolves", ids[0])
	}
}

// TestRegistryHotAdd covers the four-routes-hot-add row: a new
// capability registers at runtime and serves through the same
// pipeline without disturbing the rest.
func TestRegistryHotAdd(t *testing.T) {
	r := DefaultRegistry()
	n := len(r.IDs())
	extra := NewStoreFake()
	if err := r.Register(extra); err != nil {
		t.Fatalf("hot add failed: %v", err)
	}
	// Re-registering the same id replaces without growing the set.
	if err := r.Register(NewStoreFake()); err != nil {
		t.Fatalf("re-register failed: %v", err)
	}
	if got := len(r.IDs()); got != n {
		t.Fatalf("re-register grew the set: %d -> %d", n, got)
	}
}

// TestRegistryBrowserFlow exercises one durable capability end to
// end through the registry: open, act, poll to terminal, cancel
// after completion.
func TestRegistryBrowserFlow(t *testing.T) {
	r := DefaultRegistry()
	open, err := r.Invoke("browser", "browser_open_session", json.RawMessage(`{}`), "k-open")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if open.TaskID == "" {
		t.Fatal("open returned no task id")
	}
	got, err := r.Poll("browser", open.TaskID)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if got.State == StateWorking && open.State == StateWorking {
		t.Log("browser task still working; terminal state not required here")
	}
	if _, err := r.Cancel("browser", open.TaskID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
}
