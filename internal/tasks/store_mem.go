// Planning persistence behind an interface (Sections 8.2, 8.10).
//
// The SQLite porters belong to the storage milestone (H2): this
// package defines the PlanningStore interface the task_db tool, the
// lifecycle and the queries run against, and ships a MemoryStore used
// by unit tests and by the embedded eval profile. A SQL adapter can
// implement the same interface later without touching the logic; the
// batch atomicity contract is documented on ApplyBatch.
//
// Every mutating update on tasks, plans, requirements and criteria
// carries optimistic concurrency: the caller passes the version it
// read, and a mismatch rejects the write with VersionConflict
// carrying the current row (Section 8.10).
package tasks

import (
	"fmt"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// VersionConflict rejects a stale optimistic-concurrency write. It
// carries the current version so the agent can decide on fresh data
// instead of overwriting a concurrent change (Section 8.10).
type VersionConflict struct {
	Entity  string
	ID      string
	Have    int
	Want    int
	Current any
}

// Error implements error.
func (e *VersionConflict) Error() string {
	return fmt.Sprintf("tasks: %s %q changed: have version %d, want %d", e.Entity, e.ID, e.Have, e.Want)
}

// EvidenceRecord wraps stored evidence with the instruction revision
// the producing execution had seen, which freshness needs (Section
// 8.3): evidence goes stale when an applicable instruction revision
// lands after it.
type EvidenceRecord struct {
	Evidence types.Evidence
	// InstructionRevSeen is the applicable instruction revision at
	// record time.
	InstructionRevSeen int
}

// PlanningStore is the persistence contract for planning rows. All
// methods are safe for concurrent use; multi-operation batches must
// be atomic (see TaskDB.ApplyBatch).
type PlanningStore interface {
	// Projects.
	RegisterProject(p Project) error
	GetProject(id types.ProjectID) (Project, error)
	UpdateProject(p Project) error
	ListProjects(includeArchived bool) []Project
	// Tasks.
	CreateTask(t types.Task) error
	GetTask(id types.TaskID) (types.Task, error)
	UpdateTask(t types.Task, wantVersion int) error
	SetTaskRun(id types.TaskID, run types.RunID, state types.TaskState) error
	ReopenTask(id types.TaskID, run types.RunID) error
	ListTasks(scope types.Scope) []types.Task
	// Plans.
	CreatePlan(p Plan) error
	GetPlan(id string) (Plan, error)
	UpdatePlan(p Plan, wantVersion int) error
	PlanVersions(planID string) []Plan
	ListPlans(scope types.Scope, typ types.PlanType) []Plan
	// Plan items.
	AddPlanItem(item PlanItem) error
	GetPlanItem(id string) (PlanItem, error)
	UpdatePlanItem(item PlanItem, wantVersion int) error
	ListPlanItems(planID string) []PlanItem
	// Dependencies.
	AddDependency(planID string, d Dependency) error
	ListDependencies(planID string) []Dependency
	// Milestones.
	AddMilestone(m Milestone) error
	SetMilestoneDone(planID string, code types.MilestoneRef, done bool) error
	ListMilestones(planID string) []Milestone
	// Requirements.
	AddRequirement(r types.Requirement) error
	GetRequirement(id types.RequirementID) (types.Requirement, error)
	UpdateRequirement(r types.Requirement, wantVersion int) error
	ListRequirements(scope types.Scope) []types.Requirement
	// Criteria.
	AddCriterion(c types.Criterion) error
	GetCriterion(id types.CriterionID) (types.Criterion, error)
	UpdateCriterion(c types.Criterion, wantVersion int) error
	ListCriteria(req types.RequirementID) []types.Criterion
	// Checks.
	AddCheck(c types.Check) error
	GetCheck(id types.CheckID) (types.Check, error)
	ListChecks(criterion types.CriterionID) []types.Check
	// Evidence.
	RecordEvidence(rec EvidenceRecord) error
	LatestEvidence(check types.CheckID) (EvidenceRecord, bool)
	EvidenceHistory(check types.CheckID) []EvidenceRecord
	// Assumptions, decisions, logbook.
	AddAssumption(a types.Assumption) error
	AddDecision(d types.Decision) error
	AppendLog(e ProjectLogEntry) error
	ListLog(project types.ProjectID) []ProjectLogEntry
	// Task-requirement edges (Section 8.2, edges graph).
	LinkTaskRequirement(task types.TaskID, req types.RequirementID) error
	TaskRequirements(task types.TaskID) []types.RequirementID
	// Instruction revisions per scope (Section 8.3 freshness).
	InstructionRev(scope types.Scope) int
	SetInstructionRev(scope types.Scope, rev int)
	// File fingerprints for impact-area freshness (Section 8.3).
	RecordFileEdit(path, fingerprint string)
	FileFingerprint(path string) (string, bool)
	// Code allocation helpers: short codes never repeat (Section 4.4).
	NextTaskCode(scope types.Scope) types.TaskRef
	NextRequirementCode(scope types.Scope) types.RequirementRef
	NextCriterionCode(req types.RequirementID, reqNum int) types.CriterionRef
	NextMilestoneCode(planID string) types.MilestoneRef
	// Short-code resolution for task_db parameters (Section 8.10):
	// operations name requirements, criteria and tasks by their
	// short codes ("R4", "C12.3", "T45").
	RequirementByCode(scope types.Scope, code types.RequirementRef) (types.Requirement, error)
	TaskByCode(scope types.Scope, code types.TaskRef) (types.Task, error)
	CriterionByCode(scope types.Scope, code string) (types.Criterion, error)
}

// MemoryStore is the in-memory PlanningStore: a mutex plus maps, with
// counters that only move forward so short codes never repeat. It is
// the store unit tests use; it is not durable.
type MemoryStore struct {
	mu            sync.Mutex
	projects      map[types.ProjectID]Project
	tasks         map[types.TaskID]types.Task
	taskHistory   map[types.TaskID][]types.Task
	plans         map[string][]Plan
	items         map[string]PlanItem
	itemPlans     map[string][]string
	deps          map[string][]Dependency
	milestones    map[string][]Milestone
	requirements  map[types.RequirementID]types.Requirement
	criteria      map[types.CriterionID]types.Criterion
	checks        map[types.CheckID]types.Check
	evidence      map[types.CheckID][]EvidenceRecord
	assumptions   map[string]types.Assumption
	decisions     map[string]types.Decision
	logs          map[types.ProjectID][]ProjectLogEntry
	taskReqs      map[types.TaskID][]types.RequirementID
	instrRevs     map[string]int
	filePrints    map[string]string
	taskNums      map[string]int
	reqNums       map[string]int
	milestoneNums map[string]int
	criterionNums map[types.RequirementID]int
	// criterionVers tracks optimistic-concurrency versions for
	// criteria, whose shared struct carries no version column.
	criterionVers map[types.CriterionID]int
	// Short-code indexes for task_db resolution.
	taskCodes  map[string]types.TaskID
	reqCodes   map[string]types.RequirementID
	reqNumbers map[types.RequirementID]int
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projects:      map[types.ProjectID]Project{},
		tasks:         map[types.TaskID]types.Task{},
		taskHistory:   map[types.TaskID][]types.Task{},
		plans:         map[string][]Plan{},
		items:         map[string]PlanItem{},
		itemPlans:     map[string][]string{},
		deps:          map[string][]Dependency{},
		milestones:    map[string][]Milestone{},
		requirements:  map[types.RequirementID]types.Requirement{},
		criteria:      map[types.CriterionID]types.Criterion{},
		checks:        map[types.CheckID]types.Check{},
		evidence:      map[types.CheckID][]EvidenceRecord{},
		assumptions:   map[string]types.Assumption{},
		decisions:     map[string]types.Decision{},
		logs:          map[types.ProjectID][]ProjectLogEntry{},
		taskReqs:      map[types.TaskID][]types.RequirementID{},
		instrRevs:     map[string]int{},
		filePrints:    map[string]string{},
		taskNums:      map[string]int{},
		reqNums:       map[string]int{},
		milestoneNums: map[string]int{},
		criterionNums: map[types.RequirementID]int{},
		criterionVers: map[types.CriterionID]int{},
		taskCodes:     map[string]types.TaskID{},
		reqCodes:      map[string]types.RequirementID{},
		reqNumbers:    map[types.RequirementID]int{},
	}
}

// RegisterProject implements PlanningStore.
func (m *MemoryStore) RegisterProject(p Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[p.ID]; ok {
		return fmt.Errorf("tasks: project %q already registered", p.ID)
	}
	m.projects[p.ID] = p
	return nil
}

// GetProject implements PlanningStore.
func (m *MemoryStore) GetProject(id types.ProjectID) (Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return Project{}, fmt.Errorf("tasks: unknown project %q", id)
	}
	return p, nil
}

// UpdateProject implements PlanningStore.
func (m *MemoryStore) UpdateProject(p Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[p.ID]; !ok {
		return fmt.Errorf("tasks: unknown project %q", p.ID)
	}
	m.projects[p.ID] = p
	return nil
}

// ListProjects implements PlanningStore.
func (m *MemoryStore) ListProjects(includeArchived bool) []Project {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Project
	for _, p := range m.projects {
		if p.Archived && !includeArchived {
			continue
		}
		out = append(out, p)
	}
	return out
}

// CreateTask implements PlanningStore.
func (m *MemoryStore) CreateTask(t types.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[t.ID]; ok {
		return fmt.Errorf("tasks: task %q already exists", t.ID)
	}
	m.tasks[t.ID] = t
	m.taskCodes[t.Scope.String()+"\x00"+string(t.Code)] = t.ID
	return nil
}

// GetTask implements PlanningStore.
func (m *MemoryStore) GetTask(id types.TaskID) (types.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return types.Task{}, fmt.Errorf("tasks: unknown task %q", id)
	}
	return t, nil
}

// UpdateTask implements PlanningStore with optimistic concurrency.
func (m *MemoryStore) UpdateTask(t types.Task, wantVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.tasks[t.ID]
	if !ok {
		return fmt.Errorf("tasks: unknown task %q", t.ID)
	}
	if cur.Version != wantVersion {
		return &VersionConflict{Entity: "task", ID: string(t.ID), Have: cur.Version, Want: wantVersion, Current: cur}
	}
	t.Version = cur.Version + 1
	m.tasks[t.ID] = t
	return nil
}

// SetTaskRun implements PlanningStore: it records the new root run
// and the derived task state (derived by the caller via
// types.DeriveTaskState; the model never writes it).
func (m *MemoryStore) SetTaskRun(id types.TaskID, run types.RunID, state types.TaskState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return fmt.Errorf("tasks: unknown task %q", id)
	}
	t.RootRunID = &run
	t.State = state
	t.Version++
	t.UpdatedAt = time.Now().UTC()
	m.tasks[id] = t
	return nil
}

// ReopenTask implements PlanningStore: the finished task keeps its
// history and restarts from the database state with a fresh root run.
func (m *MemoryStore) ReopenTask(id types.TaskID, run types.RunID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return fmt.Errorf("tasks: unknown task %q", id)
	}
	switch t.State {
	case types.TaskCompleted, types.TaskCompletedPending, types.TaskFailed, types.TaskCanceled:
		m.taskHistory[id] = append(m.taskHistory[id], t)
		t.RootRunID = &run
		t.State = types.TaskOpen
		t.Version++
		t.UpdatedAt = time.Now().UTC()
		m.tasks[id] = t
		return nil
	default:
		return fmt.Errorf("tasks: task %q is %q, only finished tasks reopen", id, t.State)
	}
}

// TaskHistory returns the previous incarnations of a reopened task.
func (m *MemoryStore) TaskHistory(id types.TaskID) []types.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]types.Task(nil), m.taskHistory[id]...)
}

// ListTasks implements PlanningStore.
func (m *MemoryStore) ListTasks(scope types.Scope) []types.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.Task
	for _, t := range m.tasks {
		if t.Scope == scope {
			out = append(out, t)
		}
	}
	return out
}

// CreatePlan implements PlanningStore.
func (m *MemoryStore) CreatePlan(p Plan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[p.ID] = append(m.plans[p.ID], p)
	return nil
}

// GetPlan implements PlanningStore: it returns the latest version.
func (m *MemoryStore) GetPlan(id string) (Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vers, ok := m.plans[id]
	if len(vers) == 0 || !ok {
		return Plan{}, fmt.Errorf("tasks: unknown plan %q", id)
	}
	return vers[len(vers)-1], nil
}

// UpdatePlan implements PlanningStore with optimistic concurrency:
// the update lands as a new version, keeping the previous one
// queryable (Sections 8.4, 8.5).
func (m *MemoryStore) UpdatePlan(p Plan, wantVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	vers, ok := m.plans[p.ID]
	if !ok || len(vers) == 0 {
		return fmt.Errorf("tasks: unknown plan %q", p.ID)
	}
	cur := vers[len(vers)-1]
	if cur.Version != wantVersion {
		return &VersionConflict{Entity: "plan", ID: p.ID, Have: cur.Version, Want: wantVersion, Current: cur}
	}
	p.Version = cur.Version + 1
	p.UpdatedAt = time.Now().UTC()
	m.plans[p.ID] = append(vers, p)
	return nil
}

// PlanVersions implements PlanningStore.
func (m *MemoryStore) PlanVersions(planID string) []Plan {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Plan(nil), m.plans[planID]...)
}

// ListPlans implements PlanningStore.
func (m *MemoryStore) ListPlans(scope types.Scope, typ types.PlanType) []Plan {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Plan
	for _, vers := range m.plans {
		if len(vers) == 0 {
			continue
		}
		p := vers[len(vers)-1]
		if p.Scope == scope && (typ == "" || p.Type == typ) {
			out = append(out, p)
		}
	}
	return out
}

// AddPlanItem implements PlanningStore.
func (m *MemoryStore) AddPlanItem(item PlanItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[item.ID]; ok {
		return fmt.Errorf("tasks: plan item %q already exists", item.ID)
	}
	m.items[item.ID] = item
	m.itemPlans[item.PlanID] = append(m.itemPlans[item.PlanID], item.ID)
	return nil
}

// GetPlanItem implements PlanningStore.
func (m *MemoryStore) GetPlanItem(id string) (PlanItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return PlanItem{}, fmt.Errorf("tasks: unknown plan item %q", id)
	}
	return it, nil
}

// UpdatePlanItem implements PlanningStore with optimistic concurrency.
func (m *MemoryStore) UpdatePlanItem(item PlanItem, wantVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.items[item.ID]
	if !ok {
		return fmt.Errorf("tasks: unknown plan item %q", item.ID)
	}
	if cur.Version != wantVersion {
		return &VersionConflict{Entity: "plan_item", ID: item.ID, Have: cur.Version, Want: wantVersion, Current: cur}
	}
	item.Version = cur.Version + 1
	item.UpdatedAt = time.Now().UTC()
	m.items[item.ID] = item
	return nil
}

// ListPlanItems implements PlanningStore.
func (m *MemoryStore) ListPlanItems(planID string) []PlanItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []PlanItem
	for _, id := range m.itemPlans[planID] {
		out = append(out, m.items[id])
	}
	return out
}

// AddDependency implements PlanningStore: endpoints must exist and
// the edge must not close a cycle (Section 8.2).
func (m *MemoryStore) AddDependency(planID string, d Dependency) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[d.From]; !ok {
		return fmt.Errorf("tasks: dependency from unknown item %q", d.From)
	}
	if _, ok := m.items[d.To]; !ok {
		return fmt.Errorf("tasks: dependency to unknown item %q", d.To)
	}
	nodes := append([]string(nil), m.itemPlans[planID]...)
	if WouldCycle(nodes, m.deps[planID], d.From, d.To) {
		return fmt.Errorf("tasks: dependency %q -> %q would close a cycle", d.From, d.To)
	}
	m.deps[planID] = append(m.deps[planID], d)
	return nil
}

// ListDependencies implements PlanningStore.
func (m *MemoryStore) ListDependencies(planID string) []Dependency {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Dependency(nil), m.deps[planID]...)
}

// AddMilestone implements PlanningStore.
func (m *MemoryStore) AddMilestone(mil Milestone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.milestones[mil.PlanID] = append(m.milestones[mil.PlanID], mil)
	return nil
}

// SetMilestoneDone implements PlanningStore.
func (m *MemoryStore) SetMilestoneDone(planID string, code types.MilestoneRef, done bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, mil := range m.milestones[planID] {
		if mil.Code == code {
			mil.Done = done
			mil.UpdatedAt = time.Now().UTC()
			m.milestones[planID][i] = mil
			return nil
		}
	}
	return fmt.Errorf("tasks: unknown milestone %q in plan %q", code, planID)
}

// ListMilestones implements PlanningStore.
func (m *MemoryStore) ListMilestones(planID string) []Milestone {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Milestone(nil), m.milestones[planID]...)
}

// AddRequirement implements PlanningStore.
func (m *MemoryStore) AddRequirement(r types.Requirement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.requirements[r.ID]; ok {
		return fmt.Errorf("tasks: requirement %q already exists", r.ID)
	}
	m.requirements[r.ID] = r
	m.reqCodes[r.Scope.String()+"\x00"+string(r.Code)] = r.ID
	if _, ok := m.reqNumbers[r.ID]; !ok {
		m.reqNumbers[r.ID] = r.Code.Number()
	}
	return nil
}

// GetRequirement implements PlanningStore.
func (m *MemoryStore) GetRequirement(id types.RequirementID) (types.Requirement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requirements[id]
	if !ok {
		return types.Requirement{}, fmt.Errorf("tasks: unknown requirement %q", id)
	}
	return r, nil
}

// UpdateRequirement implements PlanningStore with optimistic
// concurrency.
func (m *MemoryStore) UpdateRequirement(r types.Requirement, wantVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.requirements[r.ID]
	if !ok {
		return fmt.Errorf("tasks: unknown requirement %q", r.ID)
	}
	if cur.Version != wantVersion {
		return &VersionConflict{Entity: "requirement", ID: string(r.ID), Have: cur.Version, Want: wantVersion, Current: cur}
	}
	r.Version = cur.Version + 1
	r.UpdatedAt = time.Now().UTC()
	m.requirements[r.ID] = r
	return nil
}

// ListRequirements implements PlanningStore.
func (m *MemoryStore) ListRequirements(scope types.Scope) []types.Requirement {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.Requirement
	for _, r := range m.requirements {
		if r.Scope == scope {
			out = append(out, r)
		}
	}
	return out
}

// AddCriterion implements PlanningStore.
func (m *MemoryStore) AddCriterion(c types.Criterion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.criteria[c.ID]; ok {
		return fmt.Errorf("tasks: criterion %q already exists", c.ID)
	}
	m.criteria[c.ID] = c
	m.criterionVers[c.ID] = 1
	return nil
}

// GetCriterion implements PlanningStore.
func (m *MemoryStore) GetCriterion(id types.CriterionID) (types.Criterion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.criteria[id]
	if !ok {
		return types.Criterion{}, fmt.Errorf("tasks: unknown criterion %q", id)
	}
	return c, nil
}

// UpdateCriterion implements PlanningStore with optimistic
// concurrency.
func (m *MemoryStore) UpdateCriterion(c types.Criterion, wantVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.criteria[c.ID]
	if !ok {
		return fmt.Errorf("tasks: unknown criterion %q", c.ID)
	}
	curVer := m.criterionVers[c.ID]
	// A criterion created outside AddCriterion starts at version 0;
	// treat a missing entry as version 1 for the first update only
	// when the caller passes 0? No: keep it strict and simple — new
	// rows start at 1, like every other table.
	if curVer == 0 {
		curVer = 1
	}
	if curVer != wantVersion {
		return &VersionConflict{Entity: "criterion", ID: string(c.ID), Have: curVer, Want: wantVersion, Current: cur}
	}
	m.criterionVers[c.ID] = curVer + 1
	c.UpdatedAt = time.Now().UTC()
	m.criteria[c.ID] = c
	return nil
}

// ListCriteria implements PlanningStore.
func (m *MemoryStore) ListCriteria(req types.RequirementID) []types.Criterion {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.Criterion
	for _, c := range m.criteria {
		if c.RequirementID == req {
			out = append(out, c)
		}
	}
	return out
}

// AddCheck implements PlanningStore.
func (m *MemoryStore) AddCheck(c types.Check) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.checks[c.ID]; ok {
		return fmt.Errorf("tasks: check %q already exists", c.ID)
	}
	m.checks[c.ID] = c
	return nil
}

// GetCheck implements PlanningStore.
func (m *MemoryStore) GetCheck(id types.CheckID) (types.Check, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.checks[id]
	if !ok {
		return types.Check{}, fmt.Errorf("tasks: unknown check %q", id)
	}
	return c, nil
}

// ListChecks implements PlanningStore.
func (m *MemoryStore) ListChecks(criterion types.CriterionID) []types.Check {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.Check
	for _, c := range m.checks {
		if c.CriterionID == criterion {
			out = append(out, c)
		}
	}
	return out
}

// RecordEvidence implements PlanningStore.
func (m *MemoryStore) RecordEvidence(rec EvidenceRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.checks[rec.Evidence.CheckID]; !ok {
		return fmt.Errorf("tasks: evidence for unknown check %q", rec.Evidence.CheckID)
	}
	m.evidence[rec.Evidence.CheckID] = append(m.evidence[rec.Evidence.CheckID], rec)
	return nil
}

// LatestEvidence implements PlanningStore.
func (m *MemoryStore) LatestEvidence(check types.CheckID) (EvidenceRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	recs := m.evidence[check]
	if len(recs) == 0 {
		return EvidenceRecord{}, false
	}
	return recs[len(recs)-1], true
}

// EvidenceHistory implements PlanningStore: the per-criterion
// history shows when a check went from passing to failing and with
// which change (Section 8.6).
func (m *MemoryStore) EvidenceHistory(check types.CheckID) []EvidenceRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]EvidenceRecord(nil), m.evidence[check]...)
}

// AddAssumption implements PlanningStore.
func (m *MemoryStore) AddAssumption(a types.Assumption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assumptions[a.ID] = a
	return nil
}

// AddDecision implements PlanningStore.
func (m *MemoryStore) AddDecision(d types.Decision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisions[d.ID] = d
	return nil
}

// AppendLog implements PlanningStore.
func (m *MemoryStore) AppendLog(e ProjectLogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs[e.ProjectID] = append(m.logs[e.ProjectID], e)
	return nil
}

// ListLog implements PlanningStore.
func (m *MemoryStore) ListLog(project types.ProjectID) []ProjectLogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ProjectLogEntry(nil), m.logs[project]...)
}

// LinkTaskRequirement implements PlanningStore (edges graph,
// task -> requirement).
func (m *MemoryStore) LinkTaskRequirement(task types.TaskID, req types.RequirementID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[task]; !ok {
		return fmt.Errorf("tasks: unknown task %q", task)
	}
	if _, ok := m.requirements[req]; !ok {
		return fmt.Errorf("tasks: unknown requirement %q", req)
	}
	for _, q := range m.taskReqs[task] {
		if q == req {
			return nil
		}
	}
	m.taskReqs[task] = append(m.taskReqs[task], req)
	return nil
}

// TaskRequirements implements PlanningStore.
func (m *MemoryStore) TaskRequirements(task types.TaskID) []types.RequirementID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]types.RequirementID(nil), m.taskReqs[task]...)
}

// InstructionRev implements PlanningStore.
func (m *MemoryStore) InstructionRev(scope types.Scope) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.instrRevs[scope.String()]
}

// SetInstructionRev implements PlanningStore.
func (m *MemoryStore) SetInstructionRev(scope types.Scope, rev int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instrRevs[scope.String()] = rev
}

// RecordFileEdit implements PlanningStore.
func (m *MemoryStore) RecordFileEdit(path, fingerprint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filePrints[path] = fingerprint
}

// FileFingerprint implements PlanningStore.
func (m *MemoryStore) FileFingerprint(path string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fp, ok := m.filePrints[path]
	return fp, ok
}

// NextTaskCode implements PlanningStore.
func (m *MemoryStore) NextTaskCode(scope types.Scope) types.TaskRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.taskNums[scope.String()]++
	return types.FormatTaskRef(m.taskNums[scope.String()])
}

// NextRequirementCode implements PlanningStore.
func (m *MemoryStore) NextRequirementCode(scope types.Scope) types.RequirementRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqNums[scope.String()]++
	return types.FormatRequirementRef(m.reqNums[scope.String()])
}

// NextCriterionCode implements PlanningStore.
func (m *MemoryStore) NextCriterionCode(req types.RequirementID, reqNum int) types.CriterionRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.criterionNums[req]++
	return types.CriterionRef{Requirement: reqNum, Criterion: m.criterionNums[req]}
}

// NextMilestoneCode implements PlanningStore.
func (m *MemoryStore) NextMilestoneCode(planID string) types.MilestoneRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.milestoneNums[planID]++
	return types.FormatMilestoneRef(m.milestoneNums[planID])
}

// RequirementByCode implements PlanningStore.
func (m *MemoryStore) RequirementByCode(scope types.Scope, code types.RequirementRef) (types.Requirement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.reqCodes[scope.String()+"\x00"+string(code)]
	if !ok {
		return types.Requirement{}, fmt.Errorf("tasks: unknown requirement %q in scope %q", code, scope.String())
	}
	return m.requirements[id], nil
}

// TaskByCode implements PlanningStore.
func (m *MemoryStore) TaskByCode(scope types.Scope, code types.TaskRef) (types.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.taskCodes[scope.String()+"\x00"+string(code)]
	if !ok {
		return types.Task{}, fmt.Errorf("tasks: unknown task %q in scope %q", code, scope.String())
	}
	return m.tasks[id], nil
}

// CriterionByCode implements PlanningStore: "C12.3" resolves the
// requirement with code R12 in the scope, then its criterion 3.
func (m *MemoryStore) CriterionByCode(scope types.Scope, code string) (types.Criterion, error) {
	ref, err := types.ParseCriterionRef(code)
	if err != nil {
		return types.Criterion{}, fmt.Errorf("tasks: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	reqID, ok := m.reqCodes[scope.String()+"\x00"+string(types.FormatRequirementRef(ref.Requirement))]
	if !ok {
		return types.Criterion{}, fmt.Errorf("tasks: unknown requirement %q in scope %q", types.FormatRequirementRef(ref.Requirement), scope.String())
	}
	for _, c := range m.criteria {
		if c.RequirementID == reqID && c.Code == ref {
			return c, nil
		}
	}
	return types.Criterion{}, fmt.Errorf("tasks: unknown criterion %q in scope %q", code, scope.String())
}
