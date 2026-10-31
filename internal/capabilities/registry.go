package capabilities

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Registry is the daemon-side capability registry of Sections 14 and
// 14.12: it holds the named Capability implementations the gate
// serves through capability.* and the tool executor serves through
// deferred tools. Native deterministic capabilities (the Section
// 14.13-mandated stand-ins for live browsers, vision, voice, device,
// documents, apps, channels, nodes, GPU, store and GitHub backends)
// register here; MCP servers and loaded extensions register through
// Register as well, behind the lazy-dial and loader routes in
// internal/tools. The zero value is unusable; use NewRegistry.
//
// Concurrency: all methods are safe for concurrent use.
type Registry struct {
	mu   sync.RWMutex
	caps map[string]Capability
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{caps: map[string]Capability{}}
}

// Register adds or replaces the capability under its manifest id.
// The manifest must validate (Section 14.1).
func (r *Registry) Register(c Capability) error {
	m := c.Manifest()
	if err := m.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.caps[m.ID] = c
	return nil
}

// Unregister removes a capability. Unknown ids are a no-op so
// extension hot-unload stays idempotent (Section 14.12).
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.caps, id)
}

// Lookup returns the capability registered under id.
func (r *Registry) Lookup(id string) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.caps[id]
	return c, ok
}

// IDs lists registered capability ids in deterministic order.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.caps))
	for id := range r.caps {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Manifests returns every registered manifest in id order.
func (r *Registry) Manifests() []Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.caps))
	for id := range r.caps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Manifest, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.caps[id].Manifest())
	}
	return out
}

// Invoke runs one operation on the named capability. Unknown
// capability ids and unknown operations are permanent input errors,
// never retried blindly (Section 17.7).
func (r *Registry) Invoke(id, operation string, args json.RawMessage, key string) (TaskResult, error) {
	c, ok := r.Lookup(id)
	if !ok {
		return TaskResult{State: StateFailed, ErrClass: ErrInput},
			fmt.Errorf("capabilities: unknown capability %q", id)
	}
	return c.Invoke(operation, args, key)
}

// Poll resumes a working task on the named capability (17.3: the
// harness re-polls durable task ids after a restart).
func (r *Registry) Poll(id, taskID string) (TaskResult, error) {
	c, ok := r.Lookup(id)
	if !ok {
		return TaskResult{State: StateFailed, ErrClass: ErrInput},
			fmt.Errorf("capabilities: unknown capability %q", id)
	}
	return c.Poll(taskID)
}

// Cancel asks a working task to stop on the named capability.
func (r *Registry) Cancel(id, taskID string) (TaskResult, error) {
	c, ok := r.Lookup(id)
	if !ok {
		return TaskResult{State: StateFailed, ErrClass: ErrInput},
			fmt.Errorf("capabilities: unknown capability %q", id)
	}
	return c.Cancel(taskID)
}

// Update answers an input_required task on the named capability.
func (r *Registry) Update(id, taskID string, answer json.RawMessage) (TaskResult, error) {
	c, ok := r.Lookup(id)
	if !ok {
		return TaskResult{State: StateFailed, ErrClass: ErrInput},
			fmt.Errorf("capabilities: unknown capability %q", id)
	}
	return c.Update(taskID, answer)
}

// DefaultRegistry returns the daemon registry with every native
// capability wired to its deterministic implementation. Per Section
// 14.13 these fakes are the mandated stand-ins in ordinary builds:
// live search, vision, TTS and model backends stay behind the live
// build tag and never run under plain go test. The user-representation
// channel stays a permanent-refusal stub until the Section 20
// disclosure policy lands.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	native := []Capability{
		NewBrowserFake(),
		NewVisionFake(""),
		NewVoiceFake(),
		NewDeviceFake(),
		NewDocumentsFake(),
		NewMediaFake(),
		NewAppsFake(),
		NewMessagingFake(),
		NewWorkspaceFake(),
		NewMeetingFake(),
		NewNodeRegistry(),
		NewGPUFake(),
		NewStoreFake(),
		NewGitHubFake(),
		UserRepStub{},
	}
	for _, c := range native {
		if err := r.Register(c); err != nil {
			panic(fmt.Sprintf("capabilities: default registry: %v", err))
		}
	}
	return r
}
