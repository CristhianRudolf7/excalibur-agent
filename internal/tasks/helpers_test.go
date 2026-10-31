// Shared fakes and seeds for the H5 tests.
package tasks

import (
	"fmt"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// MemRunTree is the in-memory RunTree: a map of nodes with pause,
// resume and cancel semantics mirroring Section 15.8. Tests drive
// the pause/resume/cancel cascade through it.
type MemRunTree struct {
	Nodes     map[string]*RunNode
	RootOf    map[types.TaskID]string
	Questions map[string][]string
	Canceled  []string
	Order     []string
}

// NewMemRunTree returns an empty tree.
func NewMemRunTree() *MemRunTree {
	return &MemRunTree{
		Nodes:     map[string]*RunNode{},
		RootOf:    map[types.TaskID]string{},
		Questions: map[string][]string{},
	}
}

// Add inserts a node; parent "" marks a root candidate.
func (m *MemRunTree) Add(n RunNode) {
	cp := n
	m.Nodes[n.ID] = &cp
	m.Order = append(m.Order, n.ID)
}

// SetRoot binds a task to its root run.
func (m *MemRunTree) SetRoot(task types.TaskID, run string) {
	m.RootOf[task] = run
}

// RootRun implements RunTree.
func (m *MemRunTree) RootRun(task types.TaskID) (RunNode, error) {
	id, ok := m.RootOf[task]
	if !ok {
		return RunNode{}, fmt.Errorf("tasks: no root run for task %q", task)
	}
	n, ok := m.Nodes[id]
	if !ok {
		return RunNode{}, fmt.Errorf("tasks: unknown run %q", id)
	}
	return *n, nil
}

// Children implements RunTree.
func (m *MemRunTree) Children(run string) []RunNode {
	var out []RunNode
	for _, id := range m.Order {
		if n := m.Nodes[id]; n.Parent == run {
			out = append(out, *n)
		}
	}
	return out
}

// SetPause implements RunTree.
func (m *MemRunTree) SetPause(run, reason, pausedBy string) error {
	n, ok := m.Nodes[run]
	if !ok {
		return fmt.Errorf("tasks: unknown run %q", run)
	}
	n.State = types.RunPaused
	n.Reason = reason
	n.PausedBy = pausedBy
	return nil
}

// SetResume implements RunTree.
func (m *MemRunTree) SetResume(run string) error {
	n, ok := m.Nodes[run]
	if !ok {
		return fmt.Errorf("tasks: unknown run %q", run)
	}
	n.State = types.RunQueued
	n.PausedBy = ""
	return nil
}

// SetCancel implements RunTree: bottom-up is driven by the cascade;
// the node itself goes canceled.
func (m *MemRunTree) SetCancel(run string) error {
	n, ok := m.Nodes[run]
	if !ok {
		return fmt.Errorf("tasks: unknown run %q", run)
	}
	n.State = types.RunCanceled
	m.Canceled = append(m.Canceled, run)
	return nil
}

// BlockingQuestions implements RunTree.
func (m *MemRunTree) BlockingQuestions(run string) []string {
	return append([]string(nil), m.Questions[run]...)
}

// seedScope returns the general scope used by most tests.
func seedScope() types.Scope { return types.GeneralScope() }

// seedRequirement adds one accepted requirement with the next code.
func seedRequirement(store *MemoryStore, scope types.Scope, text string) types.Requirement {
	code := store.NextRequirementCode(scope)
	now := time.Now().UTC()
	r := types.Requirement{
		ID: types.NewRequirementID(), Code: code, Scope: scope, Text: text,
		State: types.RequirementAccepted, Priority: types.PriorityMust,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.AddRequirement(r); err != nil {
		panic(err)
	}
	return r
}

// seedCriterion adds one criterion to a requirement with the next
// C-code, plus one check.
func seedCriterion(store *MemoryStore, req types.Requirement, reqNum int, method types.CriterionMethod, impact ...string) (types.Criterion, types.Check) {
	code := store.NextCriterionCode(req.ID, reqNum)
	now := time.Now().UTC()
	c := types.Criterion{
		ID: types.NewCriterionID(), Code: code, RequirementID: req.ID,
		Statement: "criterion " + code.String(), Method: method,
		ImpactArea: impact, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.AddCriterion(c); err != nil {
		panic(err)
	}
	chk := types.Check{
		ID: types.NewCheckID(), CriterionID: c.ID,
		Definition: "run check for " + code.String(),
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := store.AddCheck(chk); err != nil {
		panic(err)
	}
	return c, chk
}

// seedEvidence records one evidence for a check.
func seedEvidence(store *MemoryStore, chk types.Check, result types.EvidenceResult, cv CodeVersion, prints map[string]string, revSeen int) EvidenceRecord {
	rec := EvidenceRecord{
		Evidence: types.Evidence{
			ID: types.NewEvidenceID(), CheckID: chk.ID,
			Result: result, CodeVersion: cv.String(),
			Fingerprints: prints, CreatedAt: time.Now().UTC(),
		},
		InstructionRevSeen: revSeen,
	}
	if err := store.RecordEvidence(rec); err != nil {
		panic(err)
	}
	return rec
}

// sortedCodes renders criterion codes for stable failure messages.
func sortedCodes(crits []types.Criterion) []string {
	var out []string
	for _, c := range crits {
		out = append(out, c.Code.String())
	}
	sort.Strings(out)
	return out
}
