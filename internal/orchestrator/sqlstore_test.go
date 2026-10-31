// SQL orchestrator persistence: full Store round trip over a real
// temporary SQLite database plus crash/restart survival (Sections
// 15.5, 15.10, 17.8).
package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func openOrchestratorDB(t *testing.T, path string) *store.DB {
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

func TestSQLStoreRoundTrip(t *testing.T) {
	db := openOrchestratorDB(t, filepath.Join(t.TempDir(), "orch.db"))
	s := OpenSQLStore(db)
	scope := types.Scope{Kind: types.ScopeProject, ID: "prj_o"}

	parent := types.Agent{ID: "agt_parent", Name: "worker", Profile: "sub", Level: 1, Scope: scope, CreatedAt: time.Now().UTC()}
	if err := s.SaveAgent(parent); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAgent(parent); err == nil {
		t.Fatal("expected duplicate agent error")
	}
	child := types.Agent{ID: "agt_child", Name: "worker", Profile: "sub", Level: 2, Scope: scope, CreatedAt: time.Now().UTC()}
	child.ParentID = &parent.ID
	if err := s.SaveAgent(child); err != nil {
		t.Fatal(err)
	}
	if kids := s.Children(parent.ID); len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("children: %+v", kids)
	}
	if found := s.FindAgentsByName("worker"); len(found) != 2 {
		t.Fatalf("find by name: %+v", found)
	}

	wait := types.WaitLease
	rec := RunRecord{
		Run: types.Run{ID: "run_1", AgentID: child.ID, TaskID: "tsk_1", State: types.RunRunning,
			WaitReason: &wait, PausedBy: "user", QueuePosition: 2,
			Budget: types.Budget{MaxSteps: 10}, HeartbeatAt: time.Now().UTC(),
			LastProgress: "half", InstructionsRevSeen: 4,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
		Lane:  LaneP2,
		Scope: scope,
	}
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	back, err := s.GetRun("run_1")
	if err != nil {
		t.Fatal(err)
	}
	if back.Lane != LaneP2 || back.Scope != scope || back.Run.WaitReason == nil || *back.Run.WaitReason != types.WaitLease {
		t.Fatalf("run round trip: %+v", back)
	}
	if back.Run.Budget.MaxSteps != 10 || back.Run.LastProgress != "half" {
		t.Fatalf("run fields lost: %+v", back.Run)
	}
	rec.Run.State = types.RunPaused
	pause := types.PauseUser
	rec.Run.PauseReason = &pause
	rec.Run.WaitReason = nil
	if err := s.UpdateRun(rec); err != nil {
		t.Fatal(err)
	}
	paused, err := s.GetRun("run_1")
	if err != nil || paused.Run.State != types.RunPaused || paused.Run.PauseReason == nil {
		t.Fatalf("run update: %+v %v", paused, err)
	}
	if runs := s.RunsByState(types.RunPaused); len(runs) != 1 {
		t.Fatalf("runs by state: %+v", runs)
	}

	for _, m := range []Message{
		{ID: "msg_1", From: parent.ID, To: child.ID, Type: MsgDirective, Priority: PriorityNormal, Payload: "adjust", CreatedAt: time.Now().UTC()},
		{ID: "msg_2", From: parent.ID, To: child.ID, Type: MsgQuestion, Priority: PriorityInterrupt, Payload: "?", CreatedAt: time.Now().UTC()},
	} {
		if err := s.AppendMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	// Redelivery of msg_1 is at-least-once without doubles.
	if err := s.AppendMessage(Message{ID: "msg_1", From: parent.ID, To: child.ID, Type: MsgDirective, Payload: "adjust"}); err != nil {
		t.Fatal(err)
	}
	pending := s.PendingMessages(child.ID)
	if len(pending) != 2 || pending[0].ID != "msg_1" || pending[1].ID != "msg_2" {
		t.Fatalf("pending order: %+v", pending)
	}
	if dirs := s.UnackedDirectives(child.ID); len(dirs) != 1 || dirs[0].Priority != PriorityNormal {
		t.Fatalf("directives: %+v", dirs)
	}
	if err := s.AckMessage("msg_1", "applied"); err != nil {
		t.Fatal(err)
	}
	acked, err := s.GetMessage("msg_1")
	if err != nil || !acked.Acked || acked.AckNote != "applied" {
		t.Fatalf("ack: %+v %v", acked, err)
	}
	if pending := s.PendingMessages(child.ID); len(pending) != 1 {
		t.Fatalf("pending after ack: %+v", pending)
	}
	if err := s.AckMessage("msg_nope", "x"); err == nil {
		t.Fatal("expected unknown message error")
	}

	ins := types.Instruction{ID: "ins_1", Scope: scope, Text: "be kind", Revision: 1, State: "active", CreatedAt: time.Now().UTC()}
	if err := s.SaveInstruction(ins); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveInstruction(ins); err == nil {
		t.Fatal("expected stale revision error")
	}
	ins2 := ins
	ins2.ID = "ins_2"
	ins2.Revision = 2
	ins2.Text = "be kinder"
	if err := s.SaveInstruction(ins2); err != nil {
		t.Fatal(err)
	}
	latest, ok := s.LatestInstruction(scope)
	if !ok || latest.Revision != 2 || latest.Text != "be kinder" {
		t.Fatalf("latest instruction: %+v %v", latest, ok)
	}
	if text, ok := s.InstructionText(scope, 1); !ok || text != "be kind" {
		t.Fatalf("instruction text: %q %v", text, ok)
	}

	q := types.Question{ID: "q_1", RunID: "run_1", Kind: "approval", Content: "ship?", Blocking: true, State: "open", CreatedAt: time.Now().UTC()}
	if err := s.SaveQuestion(q); err != nil {
		t.Fatal(err)
	}
	if open := s.PendingQuestions("run_1"); len(open) != 1 {
		t.Fatalf("pending questions: %+v", open)
	}
	if err := s.AnswerQuestion("q_1", "yes"); err != nil {
		t.Fatal(err)
	}
	if open := s.PendingQuestions("run_1"); len(open) != 0 {
		t.Fatalf("questions after answer: %+v", open)
	}
	answered, err := s.GetQuestion("q_1")
	if err != nil || answered.Answer != "yes" || answered.State != "answered" || !answered.Blocking {
		t.Fatalf("answered: %+v %v", answered, err)
	}

	rep := DeliveryReport{RunID: "run_1", AgentID: child.ID, State: types.RunCompleted,
		Reason: "ok", Summary: "shipped", Deliverables: []string{"pdf"}, Pending: []string{"none"},
		Usage: Usage{Steps: 3, CostUSD: 0.5}, CreatedAt: time.Now().UTC()}
	if err := s.SaveReport(rep); err != nil {
		t.Fatal(err)
	}
	rep.Summary = "shipped v2"
	if err := s.SaveReport(rep); err != nil {
		t.Fatal(err)
	}
	latestRep, ok := s.LatestReport("run_1")
	if !ok || latestRep.Summary != "shipped v2" || len(latestRep.Deliverables) != 1 {
		t.Fatalf("latest report: %+v %v", latestRep, ok)
	}

	if err := s.RecordUsage("run_1", Usage{Steps: 2, InputTokens: 100, OutputTokens: 50, CostUSD: 0.1, ToolCalls: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage("run_1", Usage{Steps: 3, InputTokens: 200, OutputTokens: 60, CostUSD: 0.2, ToolCalls: 2}); err != nil {
		t.Fatal(err)
	}
	u := s.GetUsage("run_1")
	if u.Steps != 5 || u.InputTokens != 300 || u.OutputTokens != 110 || u.ToolCalls != 3 || u.CostUSD < 0.29 || u.CostUSD > 0.31 {
		t.Fatalf("usage accumulation: %+v", u)
	}
}

func TestSQLStoreSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orch-crash.db")
	db := openOrchestratorDB(t, path)
	s := OpenSQLStore(db)

	if err := s.SaveAgent(types.Agent{ID: "agt_1", Name: "a", Level: 1, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(RunRecord{Run: types.Run{ID: "run_c", AgentID: "agt_1", State: types.RunRunning, CreatedAt: time.Now().UTC()}, Lane: LaneP1}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(Message{ID: "msg_c", From: "agt_1", To: "agt_1", Type: MsgDirective, Payload: "fix", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage("run_c", Usage{Steps: 7}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openOrchestratorDB(t, path)
	s2 := OpenSQLStore(db2)
	if _, err := s2.GetAgent("agt_1"); err != nil {
		t.Fatalf("agent lost: %v", err)
	}
	if _, err := s2.GetRun("run_c"); err != nil {
		t.Fatalf("run lost: %v", err)
	}
	if pending := s2.PendingMessages("agt_1"); len(pending) != 1 {
		t.Fatalf("mailbox lost: %+v", pending)
	}
	if u := s2.GetUsage("run_c"); u.Steps != 7 {
		t.Fatalf("usage lost: %+v", u)
	}
	// The reopened store keeps serving: ack lands on the old message.
	if err := s2.AckMessage("msg_c", "done"); err != nil {
		t.Fatal(err)
	}
	if pending := s2.PendingMessages("agt_1"); len(pending) != 0 {
		t.Fatalf("ack after reopen: %+v", pending)
	}
}
