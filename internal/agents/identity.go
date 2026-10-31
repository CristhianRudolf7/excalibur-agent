package agents

import (
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// MaxLevel is the deepest hierarchy level: master (0), parents (1),
// children (2). Three levels counting the master (Section 15.1).
const MaxLevel = 2

// NewIdentity creates the Section 15.2 identity of one subagent:
// readable name, agt_ id, function description, role profile, parent,
// level and scope. Level must be consistent with the parent: the
// master (level 0) and an embedded root (level 1) have no parent; any
// other agent sits exactly one level below its parent.
func NewIdentity(name, description string, profile Profile, parent *types.Agent, scope types.Scope) (types.Agent, error) {
	if strings.TrimSpace(name) == "" {
		return types.Agent{}, fmt.Errorf("agents: agent needs a readable name")
	}
	if strings.TrimSpace(description) == "" {
		return types.Agent{}, fmt.Errorf("agents: agent %q needs a function description", name)
	}
	if err := scope.Validate(); err != nil {
		return types.Agent{}, fmt.Errorf("agents: %w", err)
	}
	level := profile.Level
	if parent != nil {
		if parent.Level+1 != level {
			return types.Agent{}, fmt.Errorf("agents: profile %q is level %d, parent %q is level %d",
				profile.Name, level, parent.ID, parent.Level)
		}
		if level > MaxLevel {
			return types.Agent{}, fmt.Errorf("agents: level %d exceeds the three-level hierarchy", level)
		}
	} else if level != 0 && level != 1 {
		return types.Agent{}, fmt.Errorf("agents: level %d agent %q needs a parent", level, name)
	}
	now := time.Now().UTC()
	a := types.Agent{
		ID:          types.NewAgentID(),
		Name:        name,
		Description: description,
		Profile:     string(profile.Name),
		Level:       level,
		Scope:       scope,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if parent != nil {
		p := parent.ID
		a.ParentID = &p
	}
	return a, nil
}

// ValidateIdentity checks a stored identity against the hierarchy
// rules: known profile, level within range, parent linkage consistent.
func ValidateIdentity(a types.Agent) error {
	if err := a.ID.Validate(); err != nil {
		return fmt.Errorf("agents: %w", err)
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("agents: agent %q needs a readable name", a.ID)
	}
	if strings.TrimSpace(a.Description) == "" {
		return fmt.Errorf("agents: agent %q needs a function description", a.Name)
	}
	p, err := Lookup(ProfileName(a.Profile))
	if err != nil {
		return err
	}
	_ = p
	if a.Level < 0 || a.Level > MaxLevel {
		return fmt.Errorf("agents: agent %q level %d out of range", a.Name, a.Level)
	}
	if err := a.Scope.Validate(); err != nil {
		return fmt.Errorf("agents: %w", err)
	}
	if a.Level == 0 && a.ParentID != nil {
		return fmt.Errorf("agents: master agent %q must not have a parent", a.Name)
	}
	return nil
}

// MaySpawn reports whether an agent of this profile and level may
// create subagents: only the master and level-1 parents hold the
// subagent tool (Section 15.1). Level-2 workers never do.
func MaySpawn(p Profile, level int) bool {
	if level >= MaxLevel {
		return false
	}
	return p.Has("subagent", level)
}

// ChildLevel returns the level a spawned child takes under parent, or
// an error when the parent sits at the bottom of the hierarchy.
func ChildLevel(parent types.Agent) (int, error) {
	if parent.Level >= MaxLevel {
		return 0, fmt.Errorf("agents: level-%d agent %q cannot create subagents", parent.Level, parent.Name)
	}
	return parent.Level + 1, nil
}
