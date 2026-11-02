package tasks

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestClientTaskMappingPaginationFiltersCounts(t *testing.T) {
	ctx := context.Background()
	s := OpenSQLStore(openPlanningDB(t, filepath.Join(t.TempDir(), "query.db")))
	states := []types.TaskState{types.TaskOpen, types.TaskInProgress, types.TaskPaused, types.TaskBlocked, types.TaskCompleted, types.TaskCompletedPending, types.TaskFailed, types.TaskCanceled}
	want := []string{"pending", "in_progress", "blocked", "blocked", "done", "done", "failed", "cancelled"}
	for i, state := range states {
		if ClientStatus(state) != want[i] {
			t.Fatalf("mapping %s", state)
		}
		now := time.Now().UTC()
		err := s.CreateTask(types.Task{ID: types.TaskID(fmt.Sprintf("tsk_%d", i)), Code: types.TaskRef(fmt.Sprintf("T%d", i)), Scope: planningScope(), State: state, Level: types.TaskN2, Request: types.RequestFile{Original: "original request", Interpretation: fmt.Sprintf("job %d", i)}, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.Query(ctx, api.TasksQueryParams{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 3 || page.NextCursor == "" || page.Counts["blocked"] != 2 || page.Counts["done"] != 2 {
		t.Fatalf("page: %+v", page)
	}
	var ids []string
	for {
		for _, item := range page.Tasks {
			ids = append(ids, item.ID)
		}
		if page.NextCursor == "" {
			break
		}
		page, err = s.Query(ctx, api.TasksQueryParams{Limit: 3, Cursor: page.NextCursor})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(ids) != 8 {
		t.Fatalf("pagination: %v", ids)
	}
	for i, id := range ids {
		if id != fmt.Sprintf("tsk_%d", i) {
			t.Fatal(ids)
		}
	}
	filtered, err := s.Query(ctx, api.TasksQueryParams{Status: []string{"done"}, ProjectID: "prj_sql", Text: "JOB"})
	if err != nil || len(filtered.Tasks) != 2 || filtered.Counts["done"] != 2 {
		t.Fatalf("filters %+v %v", filtered, err)
	}
	empty, err := s.Query(ctx, api.TasksQueryParams{ProjectID: "missing"})
	if err != nil || len(empty.Tasks) != 0 || empty.Counts["pending"] != 0 {
		t.Fatal(empty, err)
	}
	for _, p := range []api.TasksQueryParams{{Limit: 201}, {Limit: -1}, {Status: []string{"open"}}, {Cursor: "!"}} {
		if _, err := s.Query(ctx, p); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Query(canceled, api.TasksQueryParams{}); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestTaskDetailAndPostCommitChanges(t *testing.T) {
	ctx := context.Background()
	db := openPlanningDB(t, filepath.Join(t.TempDir(), "detail.db"))
	s := OpenSQLStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := types.Task{ID: "tsk_detail", Code: "T1", Scope: planningScope(), Level: types.TaskN3, State: types.TaskOpen, Version: 1, Request: types.RequestFile{Original: "build something", Interpretation: "Build"}, CreatedAt: now, UpdatedAt: now}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.CreateTask(task))
	must(s.AddRequirement(types.Requirement{ID: "req_1", Code: "R1", Scope: planningScope(), Text: "works", Version: 1}))
	must(s.LinkTaskRequirement(task.ID, "req_1"))
	var updates []api.TaskSummary
	s.OnTaskUpdated = func(v api.TaskSummary) {
		updates = append(updates, v)
		if _, err := s.GetTask(types.TaskID(v.ID)); err != nil {
			t.Fatal("event before commit", err)
		}
	}
	c := types.Criterion{ID: "crt_1", RequirementID: "req_1", Statement: "tests pass", Method: types.CriterionTest, State: types.CriterionUnverified}
	must(s.AddCriterion(c))
	if len(updates) != 1 || updates[0].CriteriaTotal != 1 {
		t.Fatalf("criterion event %+v", updates)
	}
	must(s.AddCheck(types.Check{ID: "chk_1", CriterionID: c.ID, Definition: "go test"}))
	must(s.RecordEvidence(EvidenceRecord{Evidence: types.Evidence{ID: "ev_1", CheckID: "chk_1", Result: types.EvidencePass, CreatedAt: now}}))
	if len(updates) != 2 || updates[1].CriteriaMet != 1 {
		t.Fatal(updates)
	}

	must(s.SetTaskRun(task.ID, "run_1", types.TaskBlocked))
	if len(updates) != 3 || updates[2].Status != "blocked" {
		t.Fatal(updates)
	}
	must(s.SetTaskRun(task.ID, "run_1", types.TaskBlocked))
	if len(updates) != 3 {
		t.Fatal("no-op event")
	}
	if err := s.UpdateTask(task, 999); err == nil {
		t.Fatal("expected conflict")
	}
	if len(updates) != 3 {
		t.Fatal("failed write emitted event")
	}
	must(s.CreatePlan(Plan{ID: "plan_1", Scope: planningScope(), Type: types.PlanType("micro"), TaskID: task.ID, Version: 1, State: PlanActive}))
	must(s.AddPlanItem(PlanItem{ID: "step_1", PlanID: "plan_1", Title: "first", Version: 1}))
	must(s.AddPlanItem(PlanItem{ID: "step_2", PlanID: "plan_1", Title: "second", Version: 1}))
	must(s.AddDependency("plan_1", Dependency{From: "step_1", To: "step_2", Type: types.DependencyBlocks}))
	must(db.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO runs(id,task_id,status,created_at,updated_at) VALUES('run_1',?,'completed',?,?)`, string(task.ID), now.UnixMilli(), now.Add(time.Minute).UnixMilli())
		return err
	}))

	must(db.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `INSERT INTO identity_docs(id,scope_id,kind) VALUES('doc_task','task:tsk_detail','notebook'),('doc_other','general','notebook')`); err != nil {
			return err
		}
		_, err := ex.Exec(ctx, `INSERT INTO doc_fragments(id,doc_id,text) VALUES('frag_task','doc_task','Task notes'),('frag_other','doc_other','Unrelated notes')`)
		return err
	}))
	detail, err := s.Detail(ctx, string(task.ID))
	must(err)
	if detail.Notes != "Task notes" || detail.Request != "build something" || detail.Level != "initiative" || len(detail.Requirements) != 1 || len(detail.Criteria) != 1 || detail.Criteria[0].State != "met" || len(detail.Evidence) != 1 || !detail.Evidence[0].OK || len(detail.Plan) != 2 || len(detail.Plan[1].DependsOn) != 1 || len(detail.Runs) != 1 || detail.Runs[0].EndedAt == nil {
		t.Fatalf("detail: %+v", detail)
	}
	if _, err := s.Detail(ctx, "missing"); err == nil {
		t.Fatal("missing task accepted")
	}

	must(s.RecordEvidence(EvidenceRecord{Evidence: types.Evidence{ID: "ev_fail", CheckID: "chk_1", Result: types.EvidenceFail, CreatedAt: now.Add(time.Minute)}}))
	detail, err = s.Detail(ctx, string(task.ID))
	must(err)
	if detail.Criteria[0].State != "failed" || detail.CriteriaMet != 0 || len(updates) != 4 {
		t.Fatalf("failure not reflected: %+v %+v", detail, updates)
	}
	s.SetInstructionRev(planningScope(), 1)
	detail, err = s.Detail(ctx, string(task.ID))
	must(err)
	if detail.Criteria[0].State != "unverified" || len(updates) != 5 {
		t.Fatalf("stale evidence not reflected: %+v %+v", detail, updates)
	}

}
