package ctxcompiler

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// This file implements Section 11.5: durable instructions are stored
// literally with scope, revision, origin and state, and the compiler
// renders them literally into L2 (project and general scopes) or L3
// (task and conversation scopes) on every call. Because they render
// from the database, they survive compaction, restart and delegation
// untouched by any summary.

// Instruction states.
const (
	// InstructionActive renders into the context.
	InstructionActive = "active"
	// InstructionWithdrawn stays stored but no longer renders.
	InstructionWithdrawn = "withdrawn"
)

// ScopeLayer returns the context layer of a scope kind: project and
// general instructions render in L2, task and conversation
// instructions in L3.
func ScopeLayer(kind types.ScopeKind) Layer {
	switch kind {
	case types.ScopeTask, types.ScopeConversation:
		return LayerTask
	default:
		return LayerScope
	}
}

// Views converts registry instructions to the compiler input,
// active only, in scope/id order.
func Views(ins []types.Instruction) []InstructionView {
	var active []types.Instruction
	for _, in := range ins {
		if in.State == InstructionActive {
			active = append(active, in)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		si, sj := active[i].Scope.String(), active[j].Scope.String()
		if si != sj {
			return si < sj
		}
		return active[i].ID < active[j].ID
	})
	out := make([]InstructionView, 0, len(active))
	for _, in := range active {
		out = append(out, InstructionView{
			Scope: in.Scope.String(),
			ID:    in.ID,
			Text:  in.Text,
		})
	}
	return out
}

// Registry stores durable instructions literally (11.5). Triage
// adds fragments on reception; the compiler renders the active ones
// verbatim on every call. It is safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	items map[string]types.Instruction
	seq   int
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]types.Instruction)}
}

// Add stores text literally under scope and returns the new active
// instruction at revision 1.
func (r *Registry) Add(scope types.Scope, text, origin string) types.Instruction {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	in := types.Instruction{
		ID:       fmt.Sprintf("ins_%04d", r.seq),
		Scope:    scope,
		Text:     text,
		Revision: 1,
		Origin:   origin,
		State:    InstructionActive,
	}
	r.items[in.ID] = in
	return in
}

// Revise replaces the text of id, bumps its revision and keeps it
// active. It reports whether id exists.
func (r *Registry) Revise(id, text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.items[id]
	if !ok {
		return false
	}
	in.Text = text
	in.Revision++
	in.State = InstructionActive
	r.items[id] = in
	return true
}

// Withdraw retires id without deleting it. It reports whether id
// exists.
func (r *Registry) Withdraw(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.items[id]
	if !ok {
		return false
	}
	in.State = InstructionWithdrawn
	in.Revision++
	r.items[id] = in
	return true
}

// Get returns the instruction with id.
func (r *Registry) Get(id string) (types.Instruction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	in, ok := r.items[id]
	return in, ok
}

// Active returns every active instruction in scope/id order.
func (r *Registry) Active() []types.Instruction {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []types.Instruction
	for _, in := range r.items {
		if in.State == InstructionActive {
			out = append(out, in)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := out[i].Scope.String(), out[j].Scope.String()
		if si != sj {
			return si < sj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ForScopes returns the active instructions of exactly the given
// scopes, in scope/id order.
func (r *Registry) ForScopes(scopes ...types.Scope) []types.Instruction {
	want := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		want[s.String()] = true
	}
	var out []types.Instruction
	for _, in := range r.Active() {
		if want[in.Scope.String()] {
			out = append(out, in)
		}
	}
	return out
}

// RenderText renders instructions literally, one per line, with no
// timestamps or counters: byte-identical across compaction, restart
// and delegation.
func RenderText(ins []types.Instruction) string {
	var b strings.Builder
	for _, in := range ins {
		fmt.Fprintf(&b, "[%s/%s r%d] %s\n", in.Scope.String(), in.ID, in.Revision, in.Text)
	}
	return b.String()
}

// Text renders the active instructions of scopes literally.
func (r *Registry) Text(scopes ...types.Scope) string {
	return RenderText(r.ForScopes(scopes...))
}

// DelegateCard renders the mandatory instructions transmitted
// explicitly in a delegation fiche (15.4): literals, never a skill
// suggestion.
func (r *Registry) DelegateCard(scopes ...types.Scope) string {
	ins := r.ForScopes(scopes...)
	var b strings.Builder
	b.WriteString("Mandatory instructions for the delegate:\n")
	b.WriteString(RenderText(ins))
	return b.String()
}

// snapshot is the Registry persistence form.
type snapshot struct {
	Items []types.Instruction `json:"items"`
	Seq   int                 `json:"seq"`
}

// Snapshot serializes the registry for restart persistence.
func (r *Registry) Snapshot() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]types.Instruction, 0, len(r.items))
	for _, in := range r.items {
		items = append(items, in)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return json.Marshal(snapshot{Items: items, Seq: r.seq})
}

// Restore replaces the registry contents from a Snapshot.
func (r *Registry) Restore(data []byte) error {
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	items := make(map[string]types.Instruction, len(s.Items))
	for _, in := range s.Items {
		items[in.ID] = in
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = items
	r.seq = s.Seq
	return nil
}
