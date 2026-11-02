package tasks

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ClientStatus maps the durable task lifecycle to the client contract.
func ClientStatus(state types.TaskState) string {
	switch state {
	case types.TaskInProgress:
		return "in_progress"
	case types.TaskPaused, types.TaskBlocked:
		return "blocked"
	case types.TaskCompleted, types.TaskCompletedPending:
		return "done"
	case types.TaskFailed:
		return "failed"
	case types.TaskCanceled:
		return "cancelled"
	default:
		return "pending"
	}
}
func criterionStatus(state types.CriterionState) string {
	switch state {
	case types.CriterionSatisfied:
		return "met"
	case types.CriterionFailed:
		return "failed"
	case types.CriterionNotApplicable:
		return "n/a"
	default:
		return "unverified"
	}
}
func (tx *txPlanning) summary(t types.Task) api.TaskSummary {
	title := t.Request.Interpretation
	if title == "" {
		title = t.Request.Original
	}
	level := "brief"
	if t.Level == types.TaskN2 {
		level = "project"
	}
	if t.Level == types.TaskN3 {
		level = "initiative"
	}
	out := api.TaskSummary{ID: string(t.ID), ShortCode: string(t.Code), Title: title, Status: ClientStatus(t.State), Level: level, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if t.Scope.Kind == types.ScopeProject {
		out.ProjectID = t.Scope.ID
	}
	if out.Status == "blocked" && t.RootRunID != nil {
		_ = tx.db.QueryRow(bg(), `SELECT CASE WHEN block_reason<>'' THEN block_reason ELSE reason END FROM runs WHERE id=?`, string(*t.RootRunID)).Scan(&out.BlockedReason)
	}

	for _, r := range tx.TaskRequirements(t.ID) {
		for _, c := range tx.ListCriteria(r) {
			out.CriteriaTotal++
			if c.UpdatedAt.After(out.UpdatedAt) {
				out.UpdatedAt = c.UpdatedAt
			}
			if tx.clientCriterionState(c) == types.CriterionSatisfied {
				out.CriteriaMet++
			}
		}
	}
	return out
}

// Query returns filtered tasks and counts before pagination, ordered by ID.
func (s *SQLStore) Query(ctx context.Context, p api.TasksQueryParams) (api.TasksQueryResult, error) {
	out := api.TasksQueryResult{Tasks: []api.TaskSummary{}, Counts: map[string]int{"pending": 0, "in_progress": 0, "blocked": 0, "done": 0, "failed": 0, "cancelled": 0}}
	if p.Limit < 0 || p.Limit > 200 {
		return out, &api.Error{Code: api.CodeInvalidParams, Message: "limit must be between 1 and 200"}
	}
	if p.Limit == 0 {
		p.Limit = 50
	}
	statuses := map[string]bool{}
	for _, v := range p.Status {
		if _, ok := out.Counts[v]; !ok {
			return out, &api.Error{Code: api.CodeInvalidParams, Message: "invalid task status"}
		}
		statuses[v] = true
	}
	after := ""
	if p.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(p.Cursor)
		if e != nil || len(b) == 0 {
			return out, &api.Error{Code: api.CodeInvalidParams, Message: "invalid cursor"}
		}
		after = string(b)
	}
	err := s.R.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		tx := &txPlanning{db: readOnly{q}}
		rows, err := q.Query(ctx, `SELECT `+taskCols+` FROM tasks ORDER BY id`)
		if err != nil {
			return err
		}
		var all []types.Task
		for rows.Next() {
			t, e := scanTask(rows.Scan)
			if e != nil {
				rows.Close()
				return e
			}
			all = append(all, t)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, t := range all {
			if err := ctx.Err(); err != nil {
				return err
			}
			summary := tx.summary(t)
			if p.ProjectID != "" && summary.ProjectID != p.ProjectID {
				continue
			}
			if p.Text != "" && !strings.Contains(strings.ToLower(summary.Title+" "+summary.ShortCode+" "+t.Request.Original), strings.ToLower(p.Text)) {
				continue
			}
			if len(statuses) > 0 && !statuses[summary.Status] {
				continue
			}
			out.Counts[summary.Status]++
			if summary.ID <= after {
				continue
			}
			if len(out.Tasks) < p.Limit {
				out.Tasks = append(out.Tasks, summary)
			} else if out.NextCursor == "" {
				out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(out.Tasks[len(out.Tasks)-1].ID))
			}
		}
		return nil
	})
	return out, err
}

// Detail reads the durable planning graph and historical runs for one task.
func (s *SQLStore) Detail(ctx context.Context, id string) (api.TaskDetail, error) {
	out := api.TaskDetail{Requirements: []api.TaskText{}, Criteria: []api.TaskCriterion{}, Evidence: []api.TaskEvidence{}, Plan: []api.TaskPlanItem{}, Runs: []api.TaskRun{}}
	err := s.R.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		tx := &txPlanning{db: readOnly{q}}
		t, err := tx.GetTask(types.TaskID(id))
		if err != nil {
			return err
		}
		out.TaskSummary = tx.summary(t)
		out.Request = t.Request.Original
		for _, rid := range tx.TaskRequirements(t.ID) {
			r, e := tx.GetRequirement(rid)
			if e != nil {
				return e
			}
			out.Requirements = append(out.Requirements, api.TaskText{ID: string(r.ID), Text: r.Text})
			for _, c := range tx.ListCriteria(rid) {
				out.Criteria = append(out.Criteria, api.TaskCriterion{TaskText: api.TaskText{ID: string(c.ID), Text: c.Statement}, State: criterionStatus(tx.clientCriterionState(c)), Method: string(c.Method)})
				for _, check := range tx.ListChecks(c.ID) {
					for _, rec := range tx.EvidenceHistory(check.ID) {
						e := rec.Evidence
						out.Evidence = append(out.Evidence, api.TaskEvidence{CriterionID: string(c.ID), Kind: string(c.Method), Summary: string(e.Result), At: e.CreatedAt, OK: e.Result == types.EvidencePass})
					}
				}
			}
		}
		for _, p := range tx.ListPlans(t.Scope, types.PlanType("")) {
			deps := tx.ListDependencies(p.ID)
			for _, item := range tx.ListPlanItems(p.ID) {
				if p.TaskID != t.ID && item.TaskID != t.ID {
					continue
				}
				v := api.TaskPlanItem{ID: item.ID, Title: item.Title, Status: string(item.State), DependsOn: []string{}}
				for _, d := range deps {
					if d.To == item.ID && d.Type == types.DependencyBlocks {
						v.DependsOn = append(v.DependsOn, d.From)
					}
				}
				out.Plan = append(out.Plan, v)
			}
		}
		noteRows, e := q.Query(ctx, `SELECT f.text FROM doc_fragments f JOIN identity_docs d ON d.id=f.doc_id WHERE d.kind='notebook' AND d.scope_id=? ORDER BY d.id,f.position,f.id`, "task:"+id)
		if e != nil {
			return e
		}
		var notes []string
		for noteRows.Next() {
			var text string
			if e := noteRows.Scan(&text); e != nil {
				noteRows.Close()
				return e
			}
			notes = append(notes, text)
		}
		noteErr := noteRows.Err()
		noteRows.Close()
		if noteErr != nil {
			return noteErr
		}
		out.Notes = strings.Join(notes, "\n\n")

		rows, e := q.Query(ctx, `SELECT id,status,created_at,updated_at FROM runs WHERE task_id=? ORDER BY created_at,id`, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var r api.TaskRun
			var start, end int64
			if e := rows.Scan(&r.ID, &r.State, &start, &end); e != nil {
				return e
			}
			r.StartedAt = fromMs(start)
			switch types.RunState(r.State) {
			case types.RunCompleted, types.RunCompletedPending, types.RunFailed, types.RunCanceled:
				at := fromMs(end)
				r.EndedAt = &at
			}
			out.Runs = append(out.Runs, r)
		}
		return rows.Err()
	})
	return out, err
}

func (tx *txPlanning) clientCriterionState(c types.Criterion) types.CriterionState {
	req, err := tx.GetRequirement(c.RequirementID)
	if err != nil {
		return types.CriterionUnverified
	}
	return statusOf(tx, req.Scope, c).State
}
