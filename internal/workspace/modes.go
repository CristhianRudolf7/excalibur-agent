package workspace

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// WorkMode selects how the agents of one project share the tree
// (Section 16.1).
type WorkMode string

// Work modes.
const (
	// ModeShared is the default: every agent works in the user tree
	// with per-file leases and read fingerprints.
	ModeShared WorkMode = "shared"
	// ModeWorktrees gives every child its branch and worktree; the
	// parent integrates through the queue.
	ModeWorktrees WorkMode = "worktrees"
)

// ModeRequest carries the inputs of the Section 16.1 decision.
type ModeRequest struct {
	// ParallelImplementers is the number of concurrent implementers
	// planned for the task.
	ParallelImplementers int
	// HasGit reports whether the project root is inside a git repo.
	HasGit bool
	// HasSubmodules reports git submodules: git worktree support for
	// them is incomplete, so such projects always use the shared
	// tree (Section 16.4 point 1).
	HasSubmodules bool
	// BriefTask forces the shared tree for short tasks even with
	// several implementers.
	BriefTask bool
}

// ModeFor applies the Section 16.1 table: shared by default (short
// tasks, a single implementer, projects without git); worktrees for
// several parallel implementers in a git project. Submodules always
// force the shared tree.
func ModeFor(req ModeRequest) WorkMode {
	if req.HasSubmodules {
		return ModeShared
	}
	if req.BriefTask {
		return ModeShared
	}
	if req.ParallelImplementers > 1 && req.HasGit {
		return ModeWorktrees
	}
	return ModeShared
}

// ModeStore keeps a per-project mode override. The zero value uses
// ModeFor everywhere.
type ModeStore struct {
	mu        sync.RWMutex
	overrides map[string]WorkMode
}

// SetOverride pins the mode of one project.
func (s *ModeStore) SetOverride(projectID string, mode WorkMode) error {
	if mode != ModeShared && mode != ModeWorktrees {
		return fmt.Errorf("workspace: bad work mode %q", mode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.overrides == nil {
		s.overrides = map[string]WorkMode{}
	}
	s.overrides[projectID] = mode
	return nil
}

// ForProject returns the pinned mode of a project, if any.
func (s *ModeStore) ForProject(projectID string) (WorkMode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.overrides[projectID]
	return m, ok
}

// Resolve returns the override when pinned, else ModeFor(req).
func (s *ModeStore) Resolve(projectID string, req ModeRequest) WorkMode {
	if m, ok := s.ForProject(projectID); ok {
		// An override to worktrees on a project with submodules is
		// refused: submodules always use the shared tree.
		if m == ModeWorktrees && req.HasSubmodules {
			return ModeShared
		}
		if m == ModeWorktrees && !req.HasGit {
			return ModeShared
		}
		return m
	}
	return ModeFor(req)
}

// Assignment gives one child its file set (Section 16.3).
type Assignment struct {
	ChildID string
	Files   []string
}

// OverlapNotice is the warning the parent receives when two file sets
// overlap (Section 16.3). The parent then serializes those subtasks,
// assigns the shared file to a single child, or moves to worktrees.
type OverlapNotice struct {
	Files []string
	// Children lists the child ids sharing each file, in
	// "child:file" form sorted for determinism.
	Children    []string
	Suggestion  string
	Assignments []Assignment
}

// ParentNotifier receives overlap warnings.
type ParentNotifier interface {
	NotifyOverlap(n OverlapNotice)
}

// FuncNotifier adapts a function to ParentNotifier.
type FuncNotifier func(n OverlapNotice)

// NotifyOverlap implements ParentNotifier.
func (f FuncNotifier) NotifyOverlap(n OverlapNotice) { f(n) }

// AssignFileSets splits files across children following the code
// graph: modules with their direct dependencies stay together. The
// modules map names a module and its files; deps names its direct
// dependencies. Children take modules round-robin; unclaimed files go
// to the least loaded child. Overlapping claims produce a notice.
func AssignFileSets(children []string, modules map[string][]string, deps map[string][]string, notifier ParentNotifier) ([]Assignment, *OverlapNotice) {
	if len(children) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	// Expand every module with its direct dependencies.
	expanded := map[string][]string{}
	for _, name := range names {
		seen := map[string]bool{}
		var files []string
		for _, f := range modules[name] {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
		for _, d := range deps[name] {
			for _, f := range modules[d] {
				if !seen[f] {
					seen[f] = true
					files = append(files, f)
				}
			}
		}
		sort.Strings(files)
		expanded[name] = files
	}
	assign := make([]Assignment, len(children))
	childSeen := make([]map[string]bool, len(children))
	for i, c := range children {
		assign[i] = Assignment{ChildID: c}
		childSeen[i] = map[string]bool{}
	}
	for i, name := range names {
		k := i % len(children)
		for _, f := range expanded[name] {
			if !childSeen[k][f] {
				childSeen[k][f] = true
				assign[k].Files = append(assign[k].Files, f)
			}
		}
	}
	for i := range assign {
		sort.Strings(assign[i].Files)
	}
	owners := map[string][]string{}
	for _, a := range assign {
		for _, f := range a.Files {
			owners[f] = append(owners[f], a.ChildID)
		}
	}
	var overlapFiles []string
	var pairs []string
	for f, ids := range owners {
		if len(ids) > 1 {
			overlapFiles = append(overlapFiles, f)
			for _, id := range ids {
				pairs = append(pairs, id+":"+f)
			}
		}
	}
	if len(overlapFiles) == 0 {
		return assign, nil
	}
	sort.Strings(overlapFiles)
	sort.Strings(pairs)
	notice := &OverlapNotice{
		Files:       overlapFiles,
		Children:    pairs,
		Suggestion:  "serialize those subtasks, assign the shared file to a single child, or move to worktrees",
		Assignments: append([]Assignment(nil), assign...),
	}
	if notifier != nil {
		notifier.NotifyOverlap(*notice)
	}
	return assign, notice
}

// SharedFileOwner picks the single owner of a shared file: the child
// whose set contains the module the file was claimed through, or the
// first claimant for determinism (Section 16.3 single-owner option).
func SharedFileOwner(file string, assign []Assignment) string {
	for _, a := range assign {
		for _, f := range a.Files {
			if f == file {
				return a.ChildID
			}
		}
	}
	return ""
}

// DescribeAssignment renders an assignment for parent messages.
func DescribeAssignment(assign []Assignment) string {
	var b strings.Builder
	for i, a := range assign {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s", a.ChildID, strings.Join(a.Files, ", "))
	}
	return b.String()
}
