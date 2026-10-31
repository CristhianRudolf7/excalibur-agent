// SQL planning persistence: full PlanningStore round trip over a
// real temporary SQLite database, batch atomicity in one porter
// transaction, and crash/restart survival (Sections 8.10, 8.11,
// 17.8).
package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func openPlanningDB(t *testing.T, path string) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func planningScope() types.Scope {
	return types.Scope{Kind: types.ScopeProject, ID: "prj_sql"}
}

func TestSQLStorePlanningRoundTrip(t *testing.T) {
	db := openPlanningDB(t, filepath.Join(t.TempDir(), "plan.db"))
	s := OpenSQLStore(db)
	scope := planningScope()
	now := time.Now().UTC()

	if err := s.RegisterProject(Project{ID: "prj_sql", Name: "sql", Roots: []string{"/tmp"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterProject(Project{ID: "prj_sql", Name: "dup"}); err == nil {
		t.Fatal("expected duplicate project error")
	}
	got, err := s.GetProject("prj_sql")
	if err != nil || got.Name != "sql" {
		t.Fatalf("get project: %+v %v", got, err)
	}

	code := s.NextTaskCode(scope)
	if string(code) != "T1" {
		t.Fatalf("first task code = %q", code)
	}
	task := types.Task{ID: types.TaskID("tsk_" + types.NewULID()), Code: code, Scope: scope,
		Level: types.TaskN2, Request: types.RequestFile{Interpretation: "do it"},
		State: types.TaskOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(task); err == nil {
		t.Fatal("expected duplicate task error")
	}
	if back, err := s.TaskByCode(scope, code); err != nil || back.ID != task.ID {
		t.Fatalf("task by code: %+v %v", back, err)
	}
	bad := task
	bad.Request.Interpretation = "stale"
	if err := s.UpdateTask(bad, 999); err == nil {
		t.Fatal("expected version conflict")
	} else {
		// The porter wraps command errors; unwrap with errors.As.
		var vc *VersionConflict
		if !errors.As(err, &vc) {
			t.Fatalf("conflict type: %T", err)
		}
	}
	task.Request.Interpretation = "fresh"
	if err := s.UpdateTask(task, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskRun(task.ID, "run_1", types.TaskInProgress); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetTask(task.ID)
	if err != nil || stored.State != types.TaskInProgress || stored.RootRunID == nil {
		t.Fatalf("after setrun: %+v %v", stored, err)
	}
	if err := s.ReopenTask(task.ID, "run_2"); err == nil {
		t.Fatal("expected reopen of running task to fail")
	}
	if err := s.SetTaskRun(task.ID, "run_1", types.TaskFailed); err != nil {
		t.Fatal(err)
	}
	if err := s.ReopenTask(task.ID, "run_2"); err != nil {
		t.Fatal(err)
	}
	hist, err := s.TaskHistory(task.ID)
	if err != nil || len(hist) != 1 || hist[0].State != types.TaskFailed {
		t.Fatalf("task history: %+v %v", hist, err)
	}
	code2 := s.NextTaskCode(scope)
	if string(code2) != "T2" {
		t.Fatalf("second task code = %q, want T2", code2)
	}

	plan := Plan{ID: "pln_" + types.NewULID(), Scope: scope, Type: types.PlanMacro, Version: 1, State: PlanActive, CreatedAt: now, UpdatedAt: now}
	if err := s.CreatePlan(plan); err != nil {
		t.Fatal(err)
	}
	plan.State = PlanArchived
	if err := s.UpdatePlan(plan, 1); err != nil {
		t.Fatal(err)
	}
	vers := s.PlanVersions(plan.ID)
	if len(vers) != 2 || vers[1].Version != 2 || vers[1].State != PlanArchived {
		t.Fatalf("plan versions: %+v", vers)
	}
	item := PlanItem{ID: "pli_" + types.NewULID(), Code: "S1", PlanID: plan.ID, Title: "step", State: types.PlanItemPending, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.AddPlanItem(item); err != nil {
		t.Fatal(err)
	}
	other := PlanItem{ID: "pli_" + types.NewULID(), Code: "S2", PlanID: plan.ID, Title: "next", State: types.PlanItemPending, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.AddPlanItem(other); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependency(plan.ID, Dependency{From: item.ID, To: other.ID, Type: types.DependencyBlocks}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependency(plan.ID, Dependency{From: other.ID, To: item.ID, Type: types.DependencyBlocks}); err == nil {
		t.Fatal("expected cycle rejection")
	}
	if deps := s.ListDependencies(plan.ID); len(deps) != 1 {
		t.Fatalf("deps: %+v", deps)
	}
	mil := Milestone{ID: "mls_" + types.NewULID(), Code: s.NextMilestoneCode(plan.ID), PlanID: plan.ID, Title: "done", CreatedAt: now, UpdatedAt: now}
	if err := s.AddMilestone(mil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMilestoneDone(plan.ID, mil.Code, true); err != nil {
		t.Fatal(err)
	}
	if mils := s.ListMilestones(plan.ID); len(mils) != 1 || !mils[0].Done {
		t.Fatalf("milestones: %+v", mils)
	}

	rcode := s.NextRequirementCode(scope)
	req := types.Requirement{ID: types.NewRequirementID(), Code: rcode, Scope: scope, Text: "export", State: types.RequirementProposed, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.AddRequirement(req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequirementByCode(scope, rcode); err != nil {
		t.Fatal(err)
	}
	req.Text = "export pdf"
	if err := s.UpdateRequirement(req, 1); err != nil {
		t.Fatal(err)
	}
	ccode := s.NextCriterionCode(req.ID, req.Code.Number())
	crit := types.Criterion{ID: types.NewCriterionID(), Code: ccode, RequirementID: req.ID,
		Statement: "keeps headers", Method: types.CriterionTest, ImpactArea: []string{"a.go"},
		CreatedAt: now, UpdatedAt: now}
	if err := s.AddCriterion(crit); err != nil {
		t.Fatal(err)
	}
	if v, err := s.CriterionVersion(crit.ID); err != nil || v != 1 {
		t.Fatalf("criterion version: %d %v", v, err)
	}
	crit.Statement = "keeps headers v2"
	if err := s.UpdateCriterion(crit, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCriterion(crit, 1); err == nil {
		t.Fatal("expected criterion version conflict")
	}
	if back, err := s.CriterionByCode(scope, ccode.String()); err != nil || back.ID != crit.ID {
		t.Fatalf("criterion by code: %+v %v", back, err)
	}
	chk := types.Check{ID: types.NewCheckID(), CriterionID: crit.ID, Definition: "go test ./...", CreatedAt: now}
	if err := s.AddCheck(chk); err != nil {
		t.Fatal(err)
	}
	rec := EvidenceRecord{Evidence: types.Evidence{ID: types.NewEvidenceID(), CheckID: chk.ID, RunID: "run_1",
		Result: types.EvidencePass, CodeVersion: "abc", ToolCallID: "tc_1", Fingerprints: map[string]string{"a.go": "fp1"}, CreatedAt: now},
		InstructionRevSeen: 3}
	if err := s.RecordEvidence(rec); err != nil {
		t.Fatal(err)
	}
	latest, ok := s.LatestEvidence(chk.ID)
	if !ok || latest.InstructionRevSeen != 3 || latest.Evidence.Result != types.EvidencePass {
		t.Fatalf("latest evidence: %+v %v", latest, ok)
	}
	if h := s.EvidenceHistory(chk.ID); len(h) != 1 {
		t.Fatalf("evidence history: %d", len(h))
	}
	if err := s.AddAssumption(types.Assumption{ID: "asm_1", Text: "a", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDecision(types.Decision{ID: "dec_1", Statement: "d", Alternatives: []string{"x"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLog(ProjectLogEntry{ID: "log_1", ProjectID: "prj_sql", Kind: "note", Text: "hi", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if logs := s.ListLog("prj_sql"); len(logs) != 1 {
		t.Fatalf("logs: %+v", logs)
	}
	if err := s.LinkTaskRequirement(task.ID, req.ID); err != nil {
		t.Fatal(err)
	}
	if reqs := s.TaskRequirements(task.ID); len(reqs) != 1 || reqs[0] != req.ID {
		t.Fatalf("task requirements: %+v", reqs)
	}
	s.SetInstructionRev(scope, 7)
	if rev := s.InstructionRev(scope); rev != 7 {
		t.Fatalf("instruction rev = %d", rev)
	}
	s.RecordFileEdit("a.go", "fp2")
	if fp, ok := s.FileFingerprint("a.go"); !ok || fp != "fp2" {
		t.Fatalf("fingerprint: %q %v", fp, ok)
	}
}

func TestApplyBatchSQLAtomic(t *testing.T) {
	db := openPlanningDB(t, filepath.Join(t.TempDir(), "batch.db"))
	s := OpenSQLStore(db)
	scope := planningScope()

	_, err := ApplyBatch(s, scope, []OpCall{
		{Op: "create_task", Params: map[string]any{"title": "batch task"}},
		{Op: "add_requirement", Params: map[string]any{"text": "batch req"}},
		{Op: "no_such_op", Params: map[string]any{}},
	})
	if err == nil {
		t.Fatal("expected batch parse error")
	}
	if tasks := s.ListTasks(scope); len(tasks) != 0 {
		t.Fatalf("rejected batch left %d tasks", len(tasks))
	}
	// A failing check inside the transaction rolls everything back,
	// including the burned short code: the next task is still T1.
	res, err := ApplyBatch(s, scope, []OpCall{
		{Op: "create_task", Params: map[string]any{"title": "batch task", "requirements": []any{"R99"}}},
	})
	if err == nil || res != nil {
		t.Fatalf("expected reference failure, got %+v %v", res, err)
	}
	if tasks := s.ListTasks(scope); len(tasks) != 0 {
		t.Fatalf("rolled-back batch left %d tasks", len(tasks))
	}
	res, err = ApplyBatch(s, scope, []OpCall{
		{Op: "add_requirement", Params: map[string]any{"text": "batch req"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Code != "R1" {
		t.Fatalf("requirement code = %q", res[0].Code)
	}
	res, err = ApplyBatch(s, scope, []OpCall{
		{Op: "create_task", Params: map[string]any{"title": "good", "requirements": []any{"R1"}}},
		{Op: "query", Params: map[string]any{"entity": "tasks"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Code != "T1" {
		t.Fatalf("task code after rollback = %q, want T1", res[0].Code)
	}
	if len(res) != 2 || len(res[1].Rows) != 1 {
		t.Fatalf("batch query: %+v", res)
	}
	if reqs := s.TaskRequirements(types.TaskID(res[0].ID)); len(reqs) != 1 {
		t.Fatalf("batch link: %+v", reqs)
	}
}

// TestSQLStoreSurvivesReopen closes the database mid-work (kill -9)
// and reopens it: every confirmed row persists and new writes keep
// the counters moving forward.
func TestSQLStoreSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crash.db")
	db := openPlanningDB(t, path)
	s := OpenSQLStore(db)
	scope := planningScope()
	now := time.Now().UTC()

	if err := s.RegisterProject(Project{ID: "prj_c", Name: "c", Roots: []string{"/x"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	code := s.NextTaskCode(scope)
	if err := s.CreateTask(types.Task{ID: "tsk_crash1", Code: code, Scope: scope, Level: types.TaskN1, State: types.TaskOpen, Version: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openPlanningDB(t, path)
	s2 := OpenSQLStore(db2)
	if _, err := s2.GetTask("tsk_crash1"); err != nil {
		t.Fatalf("task lost across reopen: %v", err)
	}
	code2 := s2.NextTaskCode(scope)
	if string(code2) != "T2" {
		t.Fatalf("counter after reopen = %q, want T2", code2)
	}
	s2.SetInstructionRev(scope, 4)
	if err := db2.Close(); err != nil {
		t.Fatal(err)
	}
	db3 := openPlanningDB(t, path)
	s3 := OpenSQLStore(db3)
	if rev := s3.InstructionRev(scope); rev != 4 {
		t.Fatalf("instruction rev after second reopen = %d", rev)
	}
}
