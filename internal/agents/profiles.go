package agents

import (
	"fmt"
	"sort"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ProfileName names one of the Section 15.2 role profiles.
type ProfileName string

// Built-in profiles of Section 15.2.
const (
	ProfileCoordinator ProfileName = "coordinator"
	ProfilePlanner     ProfileName = "planner"
	ProfileExplorer    ProfileName = "explorer"
	ProfileImplementer ProfileName = "implementer"
	ProfileVerifier    ProfileName = "verifier"
	ProfileReviewer    ProfileName = "reviewer"
	ProfileResearcher  ProfileName = "researcher"
	ProfileBrowser     ProfileName = "browser-operator"
	ProfileIntegrator  ProfileName = "integrator"
	ProfileAppOperator ProfileName = "app-operator"
	ProfileMaster      ProfileName = "master"
	ProfileCustom      ProfileName = "custom"
)

// Step budgets of Appendix B: master 8 steps per turn, child 60, parent
// 150. Only steps carry a profile default; tokens, cost and time come
// from the orchestrator configuration and the delegation card.
const (
	MasterDefaultSteps = 8
	ParentDefaultSteps = 150
	ChildDefaultSteps  = 60
)

// Context targets of Section 11.2: child context stays below 32K
// tokens, parent below 48K, master between 8K and 24K.
const (
	MasterContextTarget = 24_000
	ParentContextTarget = 48_000
	ChildContextTarget  = 32_000
)

// Core tool inventory of Section 13.2: every name the master or a
// subagent may hold before profile removals.
var coreTools = []string{
	"send_message", "ask_user", "leave_continuation_note", "ack_directive",
	"status", "clock", "settings",
	"read_file", "write_file", "edit_file", "apply_patch", "grep", "glob",
	"exec", "exec_io",
	"web_search", "web_read",
	"task_db", "memory_search", "memory_write", "memory_propose",
	"history_search", "library_search", "doc_read", "doc_edit",
	"get_tool_output", "pin_observation",
	"search_skills", "load_skill", "search_tools", "call_tool",
	"subagent", "reminders",
}

// MasterTools is the Section 13.2 master set: fast operations only, no
// file, terminal or navigation tools. The master delegates long work
// instead of doing it (Section 5.3, P5).
func MasterTools() []string {
	return []string{
		"send_message", "ask_user", "leave_continuation_note",
		"status", "clock", "settings",
		"web_search",
		"task_db", "memory_search", "memory_write", "memory_propose",
		"history_search", "library_search", "library_list", "library_view", "library_grep", "library_status", "doc_read", "doc_edit",
		"get_tool_output",
		"search_skills", "load_skill", "search_tools", "call_tool",
		"subagent", "reminders", "automations",
	}
}

// SubagentBaseTools is the Section 13.2 subagent set: every core tool
// except send_message and settings, plus ack_directive. Level-2 agents
// additionally lose subagent (Section 15.1).
func SubagentBaseTools() []string {
	out := []string{"ack_directive"}
	for _, t := range coreTools {
		if t == "send_message" || t == "settings" {
			continue
		}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Profile describes one Section 15.2 template: default tools expressed
// as removals from the level base set, model role, step budget and
// context target.
type Profile struct {
	Name ProfileName
	// Level is the hierarchy level agents of this profile take: 0 for
	// the master, 1 for coordinator-style parents, 2 for workers.
	Level int
	// Remove lists base-set tools this profile does not hold.
	Remove []string
	// Role is the model role the profile runs with.
	Role types.ModelRole
	// DefaultSteps is the Appendix B step budget for the profile.
	DefaultSteps int
	// ContextTarget is the Section 11.2 context target in tokens.
	ContextTarget int
	// CallToolFastOnly restricts call_tool to deferred tools marked
	// fast (master rule of Section 13.2).
	CallToolFastOnly bool
}

// ToolSet resolves the profile to its sorted tool list for a level:
// level 2 additionally drops subagent.
func (p Profile) ToolSet(level int) []string {
	base := map[string]bool{}
	if p.Name == ProfileMaster {
		for _, t := range MasterTools() {
			base[t] = true
		}
	} else {
		for _, t := range SubagentBaseTools() {
			base[t] = true
		}
	}
	for _, t := range p.Remove {
		delete(base, t)
	}
	if level >= 2 {
		delete(base, "subagent")
	}
	out := make([]string, 0, len(base))
	for t := range base {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Has reports whether the profile toolset at level contains tool.
func (p Profile) Has(tool string, level int) bool {
	for _, t := range p.ToolSet(level) {
		if t == tool {
			return true
		}
	}
	return false
}

// Builtins holds every Section 15.2 profile with its defaults.
var Builtins = map[ProfileName]Profile{
	ProfileMaster: {
		Name: ProfileMaster, Level: 0, Role: types.RoleInterlocutor,
		DefaultSteps: MasterDefaultSteps, ContextTarget: MasterContextTarget,
		CallToolFastOnly: true,
	},
	ProfileCoordinator: {
		Name: ProfileCoordinator, Level: 1, Role: types.RoleExecutor,
		DefaultSteps: ParentDefaultSteps, ContextTarget: ParentContextTarget,
	},
	ProfilePlanner: {
		Name: ProfilePlanner, Level: 2, Role: types.RolePlanner,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
		Remove: []string{"write_file", "edit_file", "apply_patch", "exec", "exec_io"},
	},
	ProfileExplorer: {
		Name: ProfileExplorer, Level: 2, Role: types.RoleExtractor,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
		Remove: []string{"write_file", "edit_file", "apply_patch", "exec", "exec_io"},
	},
	ProfileImplementer: {
		Name: ProfileImplementer, Level: 2, Role: types.RoleExecutor,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
	},
	ProfileVerifier: {
		Name: ProfileVerifier, Level: 2, Role: types.RoleVerifier,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
		// Read-only plus running checks (Section 15.2).
		Remove: []string{"write_file", "edit_file", "apply_patch"},
	},
	ProfileReviewer: {
		Name: ProfileReviewer, Level: 2, Role: types.RoleExpert,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
		Remove: []string{"write_file", "edit_file", "apply_patch", "exec", "exec_io"},
	},
	ProfileResearcher: {
		Name: ProfileResearcher, Level: 2, Role: types.RoleExtractor,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
		Remove: []string{"write_file", "edit_file", "apply_patch", "exec", "exec_io"},
	},
	ProfileBrowser: {
		Name: ProfileBrowser, Level: 2, Role: types.RoleExecutor,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
		// Operates the browser contract; no terminal (Section 15.2).
		Remove: []string{"exec", "exec_io"},
	},
	ProfileIntegrator: {
		Name: ProfileIntegrator, Level: 2, Role: types.RoleExecutor,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
	},
	ProfileAppOperator: {
		Name: ProfileAppOperator, Level: 2, Role: types.RoleExecutor,
		DefaultSteps: ChildDefaultSteps, ContextTarget: ChildContextTarget,
	},
}

// Lookup returns the profile by name, or an error for unknown names.
func Lookup(name ProfileName) (Profile, error) {
	if p, ok := Builtins[name]; ok {
		return p, nil
	}
	if c, ok := customProfiles[name]; ok {
		return c, nil
	}
	return Profile{}, fmt.Errorf("agents: unknown profile %q", name)
}

// customProfiles holds user-defined agents: profiles with their own
// identity document (Section 15.2, last paragraph).
var customProfiles = map[ProfileName]Profile{}

// CustomAgent registers a user-defined agent profile derived from a
// built-in base with an optional extra tool reduction. The toolset can
// only shrink relative to the base, never grow (Section 15.9).
func CustomAgent(name ProfileName, base ProfileName, remove []string, identityDoc string) (Profile, error) {
	if name == "" {
		return Profile{}, fmt.Errorf("agents: custom profile needs a name")
	}
	if _, ok := Builtins[name]; ok {
		return Profile{}, fmt.Errorf("agents: profile %q is built-in", name)
	}
	b, err := Lookup(base)
	if err != nil {
		return Profile{}, err
	}
	allowed := map[string]bool{}
	for _, t := range b.ToolSet(b.Level) {
		allowed[t] = true
	}
	for _, t := range remove {
		if !allowed[t] {
			return Profile{}, fmt.Errorf("agents: custom profile %q cannot remove %q: not in base %q", name, t, base)
		}
	}
	p := Profile{
		Name: name, Level: b.Level, Role: b.Role,
		DefaultSteps: b.DefaultSteps, ContextTarget: b.ContextTarget,
		Remove: append(append([]string(nil), b.Remove...), remove...),
	}
	if identityDoc == "" {
		return Profile{}, fmt.Errorf("agents: custom profile %q needs an identity document", name)
	}
	customProfiles[name] = p
	return p, nil
}

// ClampTools intersects requested with the profile toolset at level:
// a delegation card may reduce the tools, never expand them
// (Sections 15.4, 15.9). An empty requested keeps the full profile
// set. Unknown or out-of-profile tools are reported.
func ClampTools(p Profile, level int, requested []string) ([]string, error) {
	full := p.ToolSet(level)
	if len(requested) == 0 {
		return full, nil
	}
	have := map[string]bool{}
	for _, t := range full {
		have[t] = true
	}
	var out []string
	var outside []string
	for _, t := range requested {
		if have[t] {
			out = append(out, t)
		} else {
			outside = append(outside, t)
		}
	}
	if len(outside) > 0 {
		return nil, fmt.Errorf("agents: tools %v outside profile %q", outside, p.Name)
	}
	sort.Strings(out)
	return out, nil
}
