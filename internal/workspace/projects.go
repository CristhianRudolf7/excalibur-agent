package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ProjectStatus tracks the lifecycle of a registered project
// (Section 9.2): active, paused or archived.
type ProjectStatus string

// Project statuses.
const (
	ProjectActive   ProjectStatus = "active"
	ProjectPaused   ProjectStatus = "paused"
	ProjectArchived ProjectStatus = "archived"
)

// ProjectSettings carries the build/test commands and LSP servers of
// a project (Section 9.2).
type ProjectSettings struct {
	Build []string `json:"build,omitempty"`
	Test  []string `json:"test,omitempty"`
	// LSP maps a language id to its server command and arguments.
	LSP map[string][]string `json:"lsp,omitempty"`
	// AutoCommit allows commits on the branch the user has checked
	// out in shared-tree mode (Section 13.7). Off by default.
	AutoCommit bool `json:"auto_commit,omitempty"`
}

// Project is one registered project: name, alias, description, one or
// more root directories, lifecycle status, VCS, pertinent skill
// groups, linked agents and settings (Section 9.2).
type Project struct {
	ID          types.ProjectID `json:"id"`
	Name        string          `json:"name"`
	Alias       string          `json:"alias,omitempty"`
	Description string          `json:"description,omitempty"`
	// Roots are canonical absolute directories; every file under them
	// belongs to the project.
	Roots       []string        `json:"roots"`
	Status      ProjectStatus   `json:"status"`
	VCS         string          `json:"vcs,omitempty"`
	SkillGroups []string        `json:"skill_groups,omitempty"`
	Agents      []string        `json:"agents,omitempty"`
	Settings    ProjectSettings `json:"settings,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// Scope returns the project scope id of the project.
func (p *Project) Scope() types.Scope {
	return types.Scope{Kind: types.ScopeProject, ID: string(p.ID)}
}

// Validate checks the project fields.
func (p *Project) Validate() error {
	if err := p.ID.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("workspace: project needs a name")
	}
	if len(p.Roots) == 0 {
		return fmt.Errorf("workspace: project %q needs at least one root", p.Name)
	}
	for _, r := range p.Roots {
		if !filepath.IsAbs(r) {
			return fmt.Errorf("workspace: project root %q is not absolute", r)
		}
		if r != filepath.Clean(r) {
			return fmt.Errorf("workspace: project root %q is not canonical", r)
		}
	}
	switch p.Status {
	case ProjectActive, ProjectPaused, ProjectArchived:
	default:
		return fmt.Errorf("workspace: bad project status %q", p.Status)
	}
	return nil
}

// Registry stores registered projects and resolves files to projects
// (Sections 9.2, 9.5). It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	projects map[types.ProjectID]*Project
	now      func() time.Time
	// persist is the optional SQL mirror (AttachProjectStore):
	// mutations write through, RestoreProjects reloads it.
	persist *SQLProjectStore
}

// NewRegistry returns an empty project registry.
func NewRegistry() *Registry {
	return &Registry{projects: map[types.ProjectID]*Project{}, now: time.Now}
}

// Register adds a project. Roots are canonicalized (symlinks resolved
// best-effort); names and aliases must be unique.
func (r *Registry) Register(p Project) (*Project, error) {
	if p.ID == "" {
		p.ID = types.NewProjectID()
	}
	roots := make([]string, 0, len(p.Roots))
	for _, root := range p.Roots {
		roots = append(roots, canonicalizeRoot(root))
	}
	p.Roots = roots
	if p.Status == "" {
		p.Status = ProjectActive
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, q := range r.projects {
		if strings.EqualFold(q.Name, p.Name) {
			return nil, fmt.Errorf("workspace: duplicate project name %q", p.Name)
		}
		if p.Alias != "" && q.Alias != "" && strings.EqualFold(q.Alias, p.Alias) {
			return nil, fmt.Errorf("workspace: duplicate project alias %q", p.Alias)
		}
	}
	cp := p
	cp.CreatedAt = now
	cp.UpdatedAt = now
	if r.persist != nil {
		if err := r.persist.Register(cp); err != nil {
			return nil, err
		}
	}
	r.projects[cp.ID] = &cp
	return &cp, nil
}

// Update replaces the mutable fields of a project (roots, status,
// settings, skill groups, agents, alias, description).
func (r *Registry) Update(p Project) (*Project, error) {
	if err := p.ID.Validate(); err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(p.Roots))
	for _, root := range p.Roots {
		roots = append(roots, canonicalizeRoot(root))
	}
	p.Roots = roots
	if err := p.Validate(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.projects[p.ID]
	if !ok {
		return nil, fmt.Errorf("workspace: unknown project %q", string(p.ID))
	}
	for _, q := range r.projects {
		if q.ID == p.ID {
			continue
		}
		if strings.EqualFold(q.Name, p.Name) {
			return nil, fmt.Errorf("workspace: duplicate project name %q", p.Name)
		}
		if p.Alias != "" && q.Alias != "" && strings.EqualFold(q.Alias, p.Alias) {
			return nil, fmt.Errorf("workspace: duplicate project alias %q", p.Alias)
		}
	}
	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	p.CreatedAt = cur.CreatedAt
	p.UpdatedAt = now
	if r.persist != nil {
		if err := r.persist.Update(p); err != nil {
			return nil, err
		}
	}
	r.projects[p.ID] = &p
	return &p, nil
}

// Archive marks a project archived.
func (r *Registry) Archive(id types.ProjectID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.projects[id]
	if !ok {
		return fmt.Errorf("workspace: unknown project %q", string(id))
	}
	p.Status = ProjectArchived
	if r.now != nil {
		p.UpdatedAt = r.now()
	} else {
		p.UpdatedAt = time.Now()
	}
	if r.persist != nil {
		if err := r.persist.Archive(id, p.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

// Get returns a copy of one project.
func (r *Registry) Get(id types.ProjectID) (*Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.projects[id]
	if !ok {
		return nil, fmt.Errorf("workspace: unknown project %q", string(id))
	}
	cp := *p
	cp.Roots = append([]string(nil), p.Roots...)
	return &cp, nil
}

// List returns all projects sorted by name.
func (r *Registry) List() []*Project {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Project, 0, len(r.projects))
	for _, p := range r.projects {
		cp := *p
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResolveOptions carries the tie-break context of Section 9.2: the
// project of the run touching the file and the project mentioned or
// touched in the last agent message.
type ResolveOptions struct {
	// RunProject is the project of the run working with the file.
	RunProject types.ProjectID
	// MentionedProject is the project mentioned or touched in the
	// last agent message of the conversation.
	MentionedProject types.ProjectID
}

// ResolveResult is the outcome of Resolve: the chosen project plus the
// full candidate list, so a client can open the right tree
// (Section 9.2 point 4).
type ResolveResult struct {
	Chosen     *Project
	Candidates []*Project
}

// Resolve maps a file to its project (Section 9.2, deterministic):
// canonicalize the path (resolving symlinks), find every registered
// root containing it, and break ties by run project, then mentioned
// project, then the most specific root. It always returns the full
// candidate list alongside the choice.
func (r *Registry) Resolve(path string, opts ResolveOptions) (*ResolveResult, error) {
	canon := canonicalizePath(path)
	r.mu.RLock()
	defer r.mu.RUnlock()
	var cands []*Project
	for _, p := range r.projects {
		if p.Status == ProjectArchived {
			continue
		}
		if rootContaining(p.Roots, canon) != "" {
			cp := *p
			cands = append(cands, &cp)
		}
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("workspace: no project contains %q", path)
	}
	// Deterministic candidate order: most specific root first, then
	// project id for stability.
	sort.Slice(cands, func(i, j int) bool {
		ri := rootContaining(cands[i].Roots, canon)
		rj := rootContaining(cands[j].Roots, canon)
		if len(ri) != len(rj) {
			return len(ri) > len(rj)
		}
		return string(cands[i].ID) < string(cands[j].ID)
	})
	chosen := cands[0]
	if opts.RunProject != "" {
		for _, c := range cands {
			if c.ID == opts.RunProject {
				chosen = c
				break
			}
		}
	} else if opts.MentionedProject != "" {
		for _, c := range cands {
			if c.ID == opts.MentionedProject {
				chosen = c
				break
			}
		}
	}
	return &ResolveResult{Chosen: chosen, Candidates: cands}, nil
}

// rootContaining returns the longest registered root containing path,
// or "" when none does.
func rootContaining(roots []string, path string) string {
	best := ""
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			if len(root) > len(best) {
				best = root
			}
		}
	}
	return best
}

// canonicalizePath resolves symlinks best-effort and cleans the path.
// When the file does not exist yet, it resolves the closest existing
// ancestor and joins the remainder, so new files still resolve.
func canonicalizePath(path string) string {
	abs := path
	if !filepath.IsAbs(abs) {
		if cwd, err := os.Getwd(); err == nil {
			abs = filepath.Join(cwd, abs)
		}
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	// Walk up to the closest existing ancestor.
	cur := abs
	var tail []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			if resolved, err := filepath.EvalSymlinks(cur); err == nil {
				cur = resolved
			}
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		tail = append([]string{filepath.Base(cur)}, tail...)
		cur = parent
	}
	joined := cur
	for _, t := range tail {
		joined = filepath.Join(joined, t)
	}
	return filepath.Clean(joined)
}

// canonicalizeRoot cleans a root and resolves symlinks when the
// directory exists.
func canonicalizeRoot(root string) string {
	clean := filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	return clean
}

// ConversationScopes tracks the active scope of every conversation
// (Section 9.3): general or one project. The zero value is usable.
type ConversationScopes struct {
	mu     sync.RWMutex
	active map[string]types.Scope
}

// SetActive switches a conversation to scope explicitly.
func (c *ConversationScopes) SetActive(conversationID string, scope types.Scope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		c.active = map[string]types.Scope{}
	}
	c.active[conversationID] = scope
	return nil
}

// Active returns the active scope of a conversation, defaulting to
// general.
func (c *ConversationScopes) Active(conversationID string) types.Scope {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if s, ok := c.active[conversationID]; ok {
		return s
	}
	return types.GeneralScope()
}

// Inference is the triage outcome for an implicit scope switch:
// which project the user seems to mean and with what confidence.
type Inference struct {
	Project    types.ProjectID
	Confidence float64
}

// SwitchDecision tells the master whether to switch for this turn or
// to ask first (Section 9.3).
type SwitchDecision struct {
	// Switch is true when the master applies the inferred scope for
	// this turn.
	Switch bool
	// Ask is true when confidence is low and the turn writes
	// something (tasks, files, memories): the master must ask before
	// writing.
	Ask      bool
	Scope    types.Scope
	Question string
}

// DecideSwitch applies the Section 9.3 rule: an explicit command
// always switches; a high-confidence inference switches for the turn;
// a low-confidence inference followed by a write asks first.
func DecideSwitch(explicit *types.Scope, inf *Inference, writes bool) SwitchDecision {
	if explicit != nil {
		return SwitchDecision{Switch: true, Scope: *explicit}
	}
	if inf == nil || inf.Project == "" {
		return SwitchDecision{}
	}
	scope := types.Scope{Kind: types.ScopeProject, ID: string(inf.Project)}
	if inf.Confidence >= 0.7 {
		return SwitchDecision{Switch: true, Scope: scope}
	}
	if writes {
		return SwitchDecision{
			Ask:      true,
			Scope:    scope,
			Question: fmt.Sprintf("did you mean project %q? Confirm before I write anything", string(inf.Project)),
		}
	}
	return SwitchDecision{Switch: true, Scope: scope}
}

// TaggedResult labels one retrieval hit with its project so answers
// over several projects never mix lists silently (Section 9.4).
type TaggedResult struct {
	ProjectName string
	Payload     string
}

// TagResults prefixes every payload with its project name.
func TagResults(projectName string, payloads []string) []TaggedResult {
	out := make([]TaggedResult, 0, len(payloads))
	for _, p := range payloads {
		out = append(out, TaggedResult{ProjectName: projectName, Payload: p})
	}
	return out
}

// RenderTagged renders tagged results as "[Project] payload" lines.
func RenderTagged(results []TaggedResult) string {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s] %s", r.ProjectName, r.Payload)
	}
	return b.String()
}

// DualScope is the explicit double read/write scope of a transversal
// task touching two projects ("unify the config of A and B",
// Section 9.4). It is recorded on the run.
type DualScope struct {
	A       types.Scope
	B       types.Scope
	Reason  string
	Created time.Time
}

// Validate checks both scopes.
func (d DualScope) Validate() error {
	if err := d.A.Validate(); err != nil {
		return fmt.Errorf("workspace: bad dual scope A: %w", err)
	}
	if err := d.B.Validate(); err != nil {
		return fmt.Errorf("workspace: bad dual scope B: %w", err)
	}
	if d.A == d.B {
		return fmt.Errorf("workspace: dual scope needs two distinct scopes")
	}
	return nil
}

// Scopes returns both scopes as a set.
func (d DualScope) Scopes() types.ScopeSet { return types.ScopeSet{d.A, d.B} }

// ScopeError is the deterministic rejection of an out-of-scope read
// or write (Section 9.5).
type ScopeError struct {
	Op     string
	Target types.Scope
	Run    []types.Scope
}

// Error implements error.
func (e *ScopeError) Error() string {
	return fmt.Sprintf("workspace: %s outside run scopes: target %s not in %v", e.Op, e.Target, e.Run)
}

// EnforceRead rejects reads outside the run read set. An empty target
// is rejected: every query carries a scope set (Section 9.5).
func EnforceRead(runScopes types.ScopeSet, target types.Scope) error {
	if err := target.Validate(); err != nil {
		return fmt.Errorf("workspace: read without scope: %w", err)
	}
	if runScopes.Contains(target) {
		return nil
	}
	return &ScopeError{Op: "read", Target: target, Run: runScopes}
}

// EnforceWrite rejects writes outside the run write set; by default a
// run writes only its project or task (Section 9.5).
func EnforceWrite(runScopes types.ScopeSet, target types.Scope) error {
	if err := target.Validate(); err != nil {
		return fmt.Errorf("workspace: write without scope: %w", err)
	}
	if runScopes.Contains(target) {
		return nil
	}
	return &ScopeError{Op: "write", Target: target, Run: runScopes}
}

// DefaultReadScopes returns the default read set of a run: general
// (preferences, identity, shared facts) plus its project
// (Section 9.5).
func DefaultReadScopes(project types.Scope) types.ScopeSet {
	return types.ScopeSet{types.GeneralScope(), project}
}

// CandidateMemory holds a memory fact extracted without a clear scope
// (Section 9.6): it waits in the conversation scope until the
// consolidation classifies it or the user clarifies.
type CandidateMemory struct {
	Statement      string
	ConversationID string
	CreatedAt      time.Time
}

// CandidateScope returns the holding scope of a candidate.
func (c CandidateMemory) CandidateScope() types.Scope {
	return types.Scope{Kind: types.ScopeConversation, ID: c.ConversationID}
}

// ClassifyCandidate assigns a final scope to a candidate, or keeps it
// in the conversation scope when the target is ambiguous.
func ClassifyCandidate(c CandidateMemory, target *types.Scope) types.Scope {
	if target == nil {
		return c.CandidateScope()
	}
	if err := target.Validate(); err != nil {
		return c.CandidateScope()
	}
	return *target
}
