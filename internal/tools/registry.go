package tools

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is the in-memory tool catalog: definitions, validation and
// lookup. All catalog sources (built-ins, MCP, capabilities, packages)
// register here; deferred tools resolve through it (Section 13.3).
type Registry interface {
	// Register adds a tool; names must be unique catalog-wide.
	Register(t Tool) error
	// Get returns the tool with name.
	Get(name string) (Tool, error)
	// Names returns catalog names in deterministic (sorted) order:
	// providers ask for deterministic tools/list ordering to protect
	// the input cache (Section 13.1).
	Names() []string
}

// MapRegistry is the in-memory Registry implementation.
type MapRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewMapRegistry returns an empty registry.
func NewMapRegistry() *MapRegistry {
	return &MapRegistry{tools: map[string]Tool{}}
}

// Register implements Registry.
func (r *MapRegistry) Register(t Tool) error {
	def := t.Definition()
	if err := def.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[def.Name]; ok {
		return fmt.Errorf("tools: duplicate tool %q", def.Name)
	}
	r.tools[def.Name] = t
	return nil
}

// Unregister removes a tool from the catalog. It exists for the
// extension kit (Section 14.12): disabling or replacing an extension
// retires its deferred tools without touching the fixed prefix. Core
// tools are never unregistered.
func (r *MapRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// Get implements Registry.
func (r *MapRegistry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tools: unknown tool %q", name)
	}
	return t, nil
}

// Names implements Registry.
func (r *MapRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
