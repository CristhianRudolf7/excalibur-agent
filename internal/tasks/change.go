package tasks

import (
	"encoding/json"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// changeSignature includes every criterion state, including failures that do
// not alter the aggregate satisfied count.
func (tx *txPlanning) changeSignature(id types.TaskID) string {
	t, err := tx.GetTask(id)
	if err != nil {
		return ""
	}
	states := []string{string(t.State)}
	for _, rid := range tx.TaskRequirements(id) {
		for _, c := range tx.ListCriteria(rid) {
			states = append(states, string(tx.clientCriterionState(c)))
			states = append(states, mustJSON(struct {
				ID         types.CriterionID
				State      types.CriterionState
				Statement  string
				Method     types.CriterionMethod
				Expected   string
				ImpactArea []string
			}{c.ID, c.State, c.Statement, c.Method, c.Expected, c.ImpactArea}))
		}
	}
	raw, _ := json.Marshal(states)
	return string(raw)
}
func (tx *txPlanning) trackTask(id types.TaskID) {
	if tx.changes == nil {
		return
	}
	if _, ok := tx.changes[id]; !ok {
		tx.changes[id] = tx.changeSignature(id)
	}
}
func (tx *txPlanning) trackRequirement(id types.RequirementID) {
	if tx.changes == nil {
		return
	}
	rows, err := tx.db.Query(bg(), `SELECT task_id FROM task_requirements WHERE requirement_id=?`, string(id))
	if err != nil {
		return
	}
	var ids []types.TaskID
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, types.TaskID(id))
		}
	}
	rows.Close()
	for _, id := range ids {
		tx.trackTask(id)
	}
}
func (tx *txPlanning) changedSummaries() []api.TaskSummary {
	var out []api.TaskSummary
	for id, before := range tx.changes {
		if before == tx.changeSignature(id) {
			continue
		}
		t, err := tx.GetTask(id)
		if err == nil {
			out = append(out, tx.summary(t))
		}
	}
	return out
}

// Freshness writes may affect multiple tasks sharing a criterion or scope.
func (tx *txPlanning) trackAllTasks() {
	if tx.changes == nil {
		return
	}
	rows, err := tx.db.Query(bg(), `SELECT id FROM tasks ORDER BY id`)
	if err != nil {
		return
	}
	var ids []types.TaskID
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, types.TaskID(id))
		}
	}
	rows.Close()
	for _, id := range ids {
		tx.trackTask(id)
	}
}
