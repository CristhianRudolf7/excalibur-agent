package types

import (
	"fmt"
	"strings"
)

// Scope kinds from Section 9.1.
type ScopeKind string

// Scope kinds.
const (
	ScopeGeneral      ScopeKind = "general"
	ScopeProject      ScopeKind = "project"
	ScopeConversation ScopeKind = "conversation"
	ScopeTask         ScopeKind = "task"
	ScopeApp          ScopeKind = "app"
)

// Scope is a data space that rows (memory, tasks, evidence, …) belong
// to. Every query carries a set of scopes and the filter is applied in
// SQL, never in the prompt (Section 9.5).
type Scope struct {
	Kind ScopeKind
	// ID is empty for the general scope and holds the project,
	// conversation, task or app id otherwise.
	ID string
}

// GeneralScope is the shared scope for preferences, identity and facts.
func GeneralScope() Scope { return Scope{Kind: ScopeGeneral} }

// ParseScope parses "general" or "<kind>:<id>".
func ParseScope(s string) (Scope, error) {
	if s == string(ScopeGeneral) {
		return GeneralScope(), nil
	}
	kind, id, ok := strings.Cut(s, ":")
	if !ok || id == "" {
		return Scope{}, fmt.Errorf("types: bad scope %q: want general or <kind>:<id>", s)
	}
	switch ScopeKind(kind) {
	case ScopeProject, ScopeConversation, ScopeTask, ScopeApp:
		return Scope{Kind: ScopeKind(kind), ID: id}, nil
	default:
		return Scope{}, fmt.Errorf("types: bad scope %q: unknown kind %q", s, kind)
	}
}

// String renders the canonical scope form.
func (s Scope) String() string {
	if s.Kind == ScopeGeneral {
		return string(ScopeGeneral)
	}
	return string(s.Kind) + ":" + s.ID
}

// Validate reports whether the scope is well-formed.
func (s Scope) Validate() error {
	switch s.Kind {
	case ScopeGeneral:
		if s.ID != "" {
			return fmt.Errorf("types: bad scope: general carries no id")
		}
		return nil
	case ScopeProject, ScopeConversation, ScopeTask, ScopeApp:
		if s.ID == "" {
			return fmt.Errorf("types: bad scope: kind %q requires an id", s.Kind)
		}
		return nil
	default:
		return fmt.Errorf("types: bad scope: unknown kind %q", s.Kind)
	}
}

// ScopeSet is the read or write set fixed for a run at creation time
// and enforced by the retriever and the porter (Section 9.5).
type ScopeSet []Scope

// Contains reports whether the set includes scope.
func (ss ScopeSet) Contains(scope Scope) bool {
	for _, s := range ss {
		if s == scope {
			return true
		}
	}
	return false
}

// Validate checks every scope in the set.
func (ss ScopeSet) Validate() error {
	for _, s := range ss {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return nil
}
