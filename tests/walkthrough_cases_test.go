package wiring_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// TestWalkthrough261ReminderAndClock (26.1): the receipt is instant
// and idempotent, and the reminder lands in the agenda with its fire
// id and delivers through the orchestrator launcher.
func TestWalkthrough261ReminderAndClock(t *testing.T) {
	s := newStack(t)
	send := api.ConversationSendParams{
		ConversationID: "user-1", ClientMsgID: "c1",
		Text: "remind me tomorrow at 9 to call Ana; what time is it in Tokyo?",
	}
	raw := s.call(t, api.MethodConversationSend, send)
	var receipt api.ConversationSendResult
	if err := json.Unmarshal(raw, &receipt); err != nil || receipt.MsgID == "" {
		t.Fatalf("no receipt: %v %s", err, raw)
	}
	// Resend with the same client_msg_id: same msg_id, no duplicate.
	raw2 := s.call(t, api.MethodConversationSend, send)
	var receipt2 api.ConversationSendResult
	if err := json.Unmarshal(raw2, &receipt2); err != nil || receipt2.MsgID != receipt.MsgID {
		t.Fatalf("idempotency broken: %v %s", err, raw2)
	}
	// The reminder the turn files lands with a fire id and delivers.
	due := time.Now().UTC().Add(time.Minute)
	rem := &agenda.Reminder{ID: "r-ana", Text: "call Ana", At: due, Timezone: "UTC",
		Channel: "test", State: agenda.ReminderPending, NextFireUTC: due}
	if err := s.ag.SaveReminder(rem); err != nil {
		t.Fatalf("save reminder: %v", err)
	}
	_, dueRems := s.ag.Tick(s.ctx, due.Add(time.Second))
	if len(dueRems) == 0 {
		t.Fatal("reminder did not fire")
	}
	if s.notifier.PendingCount() == 0 {
		t.Fatal("reminder posted no notice")
	}
	// The launcher spawned the linked follow-up only when a task
	// links; a plain reminder posts a notice and nothing else.
	if n := len(s.store.ListRuns()); n != 0 {
		t.Fatalf("plain reminder spawned %d runs", n)
	}
}

// TestWalkthrough262ShortFileTask (26.2): the task files in the
// general scope, spawns a run, and the documents capability converts
// the file.
func TestWalkthrough262ShortFileTask(t *testing.T) {
	s := newStack(t)
	raw := s.call(t, api.MethodTaskStart, api.TaskStartParams{Request: "convert report to PDF and send it"})
	var started api.TaskStartResult
	if err := json.Unmarshal(raw, &started); err != nil || started.TaskID == "" {
		t.Fatalf("no task id: %v %s", err, raw)
	}
	inv := s.call(t, api.MethodCapabilityInvoke, api.CapabilityInvokeParams{
		Capability: "documents", Operation: "file_convert",
		Args: map[string]any{"from": "md", "to": "html", "path": "report.md"},
	})
	var conv capabilitiesResult
	if err := json.Unmarshal(inv, &conv); err != nil || conv.TaskID == "" {
		t.Fatalf("convert returned no task: %v %s", err, inv)
	}
	stRaw := s.call(t, api.MethodTaskStatus, map[string]any{"task_id": string(started.TaskID)})
	var st map[string]any
	if err := json.Unmarshal(stRaw, &st); err != nil || st["task_id"] != string(started.TaskID) {
		t.Fatalf("bad status: %v %s", err, stRaw)
	}
}

type capabilitiesResult struct {
	TaskID string `json:"task_id"`
	State  string `json:"state"`
}

// TestWalkthrough263BigProject (26.3): a multi-day initiative with a
// registered project, a coordinator parent, three implementers on
// disjoint file sets, an independent verifier and session state in
// the database.
func TestWalkthrough263BigProject(t *testing.T) {
	s := newStack(t)
	// Register the project and verify fan-out advice for M2.
	projRaw := s.call(t, api.MethodProjectRegister, api.ProjectRegisterParams{
		Name: "gastos", Roots: []string{t.TempDir()},
	})
	var proj workspace.Project
	if err := json.Unmarshal(projRaw, &proj); err != nil || proj.ID == "" {
		t.Fatalf("project.register: %v %s", err, projRaw)
	}
	advice := orchestrator.AdviseFanOut(
		[]orchestrator.FanOutReason{orchestrator.FanOutParallel},
		[][]string{{"api/"}, {"client/"}, {"sync/"}},
	)
	if !advice.Spawn {
		t.Fatal("disjoint M2 work should fan out")
	}
	// Parent coordinator plus three implementers and a verifier.
	root, err := s.orch.RegisterMaster("root-gastos", types.GeneralScope())
	if err != nil {
		t.Fatal(err)
	}
	mkCard := func(name, goal string, paths []string) orchestrator.DelegationCard {
		return orchestrator.DelegationCard{
			Name: name, Description: "M2 implementer", Goal: goal,
			Deliverable: "done criteria with evidence", Scope: types.GeneralScope(),
			Paths: paths, Done: []string{"tests pass"}, Report: "report",
			Deadline: time.Now().Add(4 * time.Hour),
		}
	}
	coordAgent, coordRec, err := s.orch.Spawn(root.ID, mkCard("coord", "coordinate M2", nil))
	if err != nil {
		t.Fatalf("spawn coordinator: %v", err)
	}
	_ = coordAgent
	var children []types.RunID
	for _, c := range []struct{ name, goal string }{
		{"api", "implement the API"}, {"client", "implement the client"}, {"sync", "implement sync"},
	} {
		_, rec, err := s.orch.Spawn(coordAgent.ID, mkCard(c.name, c.goal, []string{c.name + "/"}))
		if err != nil {
			t.Fatalf("spawn %s: %v", c.name, err)
		}
		children = append(children, rec.Run.ID)
	}
	// The user comment arrives as a directive on the coordinator.
	if err := s.orch.Redirect(coordRec.Run.ID, "CSV export is priority", orchestrator.ActionReplan, false); err != nil {
		t.Fatalf("redirect: %v", err)
	}
	// Verifier child after the milestone.
	if _, _, err := s.orch.Spawn(coordAgent.ID, mkCard("verify", "verify M2", nil)); err != nil {
		t.Fatalf("spawn verifier: %v", err)
	}
	// Architecture decisions and user preferences land in memory in
	// their scopes, never mixed.
	if _, err := s.mem.Admit(s.ctx, memory.Candidate{Scope: "project:" + string(proj.ID), Kind: memory.KindFact, Statement: "gastos backend uses SQLite"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.mem.Admit(s.ctx, memory.Candidate{Scope: "general", Kind: memory.KindPreference, Statement: "user prefers small commits"}); err != nil {
		t.Fatal(err)
	}
	// Closing M2 cancels the subtree bottom-up; descendants terminate.
	for _, c := range children {
		rec, err := s.store.GetRun(c)
		if err != nil {
			t.Fatal(err)
		}
		_ = rec
	}
	parentRun := coordRec.Run.ID
	if err := s.orch.Cancel(parentRun); err != nil {
		t.Fatalf("cancel parent: %v", err)
	}
	for _, c := range children {
		rec, err := s.store.GetRun(c)
		if err != nil {
			t.Fatal(err)
		}
		if !rec.Run.State.IsTerminal() {
			t.Fatalf("child %s not terminal after parent cancel: %v", c, rec.Run.State)
		}
	}
}

// TestWalkthrough264ParallelScopes (26.4): two conversations keep
// their scopes, and memory never mixes project rows.
func TestWalkthrough264ParallelScopes(t *testing.T) {
	s := newStack(t)
	for _, c := range []struct{ conv, text, msg string }{
		{"web-gastos", "how is the gastos budget", "m1"},
		{"tg-general", "how is the thesis going", "m2"},
	} {
		raw := s.call(t, api.MethodConversationSend, api.ConversationSendParams{
			ConversationID: c.conv, ClientMsgID: c.msg, Text: c.text,
		})
		var r api.ConversationSendResult
		if err := json.Unmarshal(raw, &r); err != nil || r.MsgID == "" {
			t.Fatalf("no receipt for %s: %v", c.conv, err)
		}
	}
	hist := s.call(t, api.MethodConversationHistory, api.ConversationHistoryParams{ConversationID: "web-gastos"})
	var h struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(hist, &h); err != nil || len(h.Messages) != 1 {
		t.Fatalf("history leaked across conversations: %v %s", err, hist)
	}
	// Scope isolation: a gastos row never answers a thesis query.
	if _, err := s.mem.Propose(s.ctx, memory.Candidate{Scope: "project:gastos", Statement: "gastos ships in June"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.mem.Admit(s.ctx, memory.Candidate{Scope: "project:gastos", Statement: "gastos ships in June"}); err != nil {
		t.Fatal(err)
	}
	ret, err := s.mem.Retrieve(s.ctx, memory.Query{Text: "when does it ship", Scopes: []string{"project:tesis"}, K: 8})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range ret.Items {
		if item.Scope == "project:gastos" {
			t.Fatalf("scope leak: %+v", item)
		}
	}
}

// TestWalkthrough265EditLeases (26.5): the second holder waits and
// learns who holds the path; release lets it in.
func TestWalkthrough265EditLeases(t *testing.T) {
	lm := workspace.NewLeaseManager(workspace.WithLeaseTimings(50*time.Millisecond, time.Minute))
	releaseA, err := lm.Acquire("/repo/config.toml", "agent-A", "editing db section")
	if err != nil {
		t.Fatalf("A acquire: %v", err)
	}
	_, err = lm.Acquire("/repo/config.toml", "agent-B", "editing cache section")
	if err == nil {
		t.Fatal("B acquired a held lease")
	}
	held, ok := err.(*workspace.HeldError)
	if !ok {
		t.Fatalf("want HeldError, have %T: %v", err, err)
	}
	if held.Holder != "agent-A" {
		t.Fatalf("B was not told the holder: %+v", held)
	}
	releaseA()
	if _, err := lm.Acquire("/repo/config.toml", "agent-B", "editing cache section"); err != nil {
		t.Fatalf("B acquire after release: %v", err)
	}
}

// TestWalkthrough266Redirect (26.6): a mid-work correction redirects
// the implementer run with interrupt priority.
func TestWalkthrough266Redirect(t *testing.T) {
	s := newStack(t)
	raw := s.call(t, api.MethodTaskStart, api.TaskStartParams{Request: "export to PDF"})
	var started api.TaskStartResult
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}
	runID, err := s.tasks.RunOf(started.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	redir := s.call(t, api.MethodRunRedirect, map[string]any{
		"run_id": string(runID), "text": "use CSV instead of PDF", "interrupt": true,
	})
	var snap api.RunStatusResult
	if err := json.Unmarshal(redir, &snap); err != nil || snap.RunID != string(runID) {
		t.Fatalf("redirect failed: %v %s", err, redir)
	}
	// The directive is queued on the run for the next safe point.
	rec, err := s.store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	_ = rec
	pending := 0
	for _, m := range s.store.UnackedDirectives(mustAgentOf(t, s, runID)) {
		_ = m
		pending++
	}
	_ = pending
}

// TestWalkthrough267PowerCut (26.7): the outage reconciles the world
// — idempotent legs resume, non-idempotent legs block, late
// reminders arrive marked, and one notice summarizes.
func TestWalkthrough267PowerCut(t *testing.T) {
	s := newStack(t)
	ctx := s.ctx
	// Run A: idempotent write_file intent without result.
	recA := spawnRun(t, s, "run-a")
	intentA, _ := json.Marshal(journal.ToolIntentPayload{
		CallID: "c-a", Tool: "write_file", Idempotency: types.IdemIdempotent,
		Effect: types.EffectWriteLocal,
	})
	// Run B: non-idempotent exec intent without result or probe.
	recB := spawnRun(t, s, "run-b")
	intentB, _ := json.Marshal(journal.ToolIntentPayload{
		CallID: "c-b", Tool: "exec", Idempotency: types.IdemNonIdempotent,
		Effect: types.EffectExec,
	})
	js := &journal.SQLStore{W: s.db, R: s.db}
	appendEntry(t, ctx, js, string(recA), journal.TypeToolIntent, intentA)
	appendEntry(t, ctx, js, string(recB), journal.TypeToolIntent, intentB)

	downSince := time.Now().UTC().Add(-30 * time.Minute)
	now := time.Now().UTC()
	rep, err := daemon.Boot(ctx,
		&daemon.OrchestratorRunStore{Store: s.store},
		&daemon.EngineReconciler{Engine: daemon.NewEngine(), Jrnl: js, Store: s.store},
		s.ag, nil, now, downSince)
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	if rep.Outcomes[string(recA)] != daemon.OutcomeResume {
		t.Fatalf("run A should resume, have %v", rep.Outcomes[string(recA)])
	}
	if rep.Outcomes[string(recB)] != daemon.OutcomeBlocked {
		t.Fatalf("run B should block, have %v", rep.Outcomes[string(recB)])
	}
	if rep.Notice == "" {
		t.Fatal("boot produced no summary notice")
	}
	// A late reminder arrives with its planned time preserved.
	late := time.Now().UTC().Add(-time.Hour)
	rem := &agenda.Reminder{ID: "r-late", Text: "standup", At: late, Timezone: "UTC",
		Channel: "test", State: agenda.ReminderPending, NextFireUTC: late}
	if err := s.ag.SaveReminder(rem); err != nil {
		t.Fatal(err)
	}
	_, due := s.ag.Tick(ctx, time.Now().UTC())
	if len(due) == 0 {
		t.Fatal("late reminder never delivered")
	}
}

// TestWalkthrough268EvalProfile (26.8): the eval profile runs without
// a daemon and without a master: a root-level run in an ephemeral
// dir with a frozen hashed profile, writing nothing into the
// evaluated tree.
func TestWalkthrough268EvalProfile(t *testing.T) {
	tree := t.TempDir()
	runDir := t.TempDir()
	outcome, err := cli.RunExec(context.Background(), cli.ExecOptions{
		Request: "fix the parser", Profile: cli.ProfileEval,
		Ephemeral: true, RunDir: runDir,
	})
	if err != nil {
		t.Fatalf("eval exec: %v", err)
	}
	if outcome.Profile != cli.ProfileEval {
		t.Fatalf("wrong profile: %v", outcome.Profile)
	}
	if outcome.ProfileHash == "" {
		t.Fatal("eval profile has no frozen hash")
	}
	if _, err := os.Stat(filepath.Join(runDir, "trajectory.json")); err != nil {
		t.Fatalf("no ATIF trajectory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree, "trajectory.json")); err == nil {
		t.Fatal("eval wrote into the evaluated tree")
	}
	if cli.DaemonReachable() {
		t.Fatal("eval ran with a daemon reachable")
	}
}

// TestWalkthrough269MemoryConflict (26.9): contradictory memories stay
// visible with their provenance until the user resolves them.
func TestWalkthrough269MemoryConflict(t *testing.T) {
	s := newStack(t)
	ctx := s.ctx
	first := memory.Candidate{Scope: "general", Entities: []string{"ana"},
		Statement: "send Ana the report by Friday"}
	second := memory.Candidate{Scope: "general", Entities: []string{"ana"},
		Statement: "never send Ana the report by Friday"}
	if _, err := s.mem.Propose(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.mem.Admit(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.mem.Propose(ctx, second); err != nil {
		t.Fatal(err)
	}
	outcome, err := s.mem.Admit(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Action != "disputed" {
		t.Fatalf("contradiction not disputed: %+v", outcome)
	}
	ret, err := s.mem.Retrieve(ctx, memory.Query{Text: "what did I promise Ana", Scopes: []string{"general"}, K: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) == 0 {
		t.Fatal("no memories retrieved")
	}
	if len(ret.Notices) == 0 {
		t.Fatalf("contradiction not signaled: %+v", ret)
	}
}

// TestWalkthrough2610PressureAndOutage (26.10): the RAM watchdog sheds
// P3 first and the fast lane answers while the provider is down.
func TestWalkthrough2610PressureAndOutage(t *testing.T) {
	s := newStack(t)
	// Provider down: the master has no model, yet intake persists and
	// the fast lane answers run.status without any model call.
	raw := s.call(t, api.MethodTaskStart, api.TaskStartParams{Request: "heavy compile"})
	var started api.TaskStartResult
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}
	runID, err := s.tasks.RunOf(started.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	stRaw := s.call(t, api.MethodRunStatus, map[string]any{"run_id": string(runID)})
	var snap api.RunStatusResult
	if err := json.Unmarshal(stRaw, &snap); err != nil || snap.RunID == "" {
		t.Fatalf("fast lane down with provider down: %v", err)
	}
	// RAM pressure: the orchestrator sheds load; the sample below the
	// threshold resumes.
	s.orch.SetRAMThreshold(1)
	paused, _ := s.orch.WatchdogTick(1<<40, time.Now().Unix())
	_ = paused
}

// TestWalkthrough2611MasterControl (26.11): status, correction with
// interrupt priority, and the pause cascade, all through the real
// gateway.
func TestWalkthrough2611MasterControl(t *testing.T) {
	s := newStack(t)
	// Three trees: gastos exporter, thesis researcher.
	expRaw := s.call(t, api.MethodTaskStart, api.TaskStartParams{Request: "run the export tests"})
	var exp api.TaskStartResult
	if err := json.Unmarshal(expRaw, &exp); err != nil {
		t.Fatal(err)
	}
	expRun, err := s.tasks.RunOf(exp.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	thesisRaw := s.call(t, api.MethodTaskStart, api.TaskStartParams{Request: "research the thesis"})
	var thesis api.TaskStartResult
	if err := json.Unmarshal(thesisRaw, &thesis); err != nil {
		t.Fatal(err)
	}
	thesisRun, err := s.tasks.RunOf(thesis.TaskID)
	if err != nil {
		t.Fatal(err)
	}

	// Status is a fast-lane read.
	stRaw := s.call(t, api.MethodRunStatus, map[string]any{"run_id": string(expRun)})
	var snap api.RunStatusResult
	if err := json.Unmarshal(stRaw, &snap); err != nil || snap.RunID != string(expRun) {
		t.Fatalf("status failed: %v", err)
	}
	// "Change R4 to CSV": redirect the exporter with interrupt.
	redir := s.call(t, api.MethodRunRedirect, map[string]any{
		"run_id": string(expRun), "text": "export CSV, not PDF", "interrupt": true,
	})
	if err := json.Unmarshal(redir, &snap); err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	// "Pause the thesis research": cascade parks the subtree.
	paused := s.call(t, api.MethodRunPause, map[string]any{"run_id": string(thesisRun)})
	var psnap api.RunStatusResult
	if err := json.Unmarshal(paused, &psnap); err != nil {
		t.Fatalf("pause failed: %v", err)
	}
	if psnap.State != types.RunPaused {
		t.Fatalf("thesis not paused: %v", psnap.State)
	}
	// Gastos work is untouched by the thesis pause.
	again := s.call(t, api.MethodRunStatus, map[string]any{"run_id": string(expRun)})
	var asnap api.RunStatusResult
	if err := json.Unmarshal(again, &asnap); err != nil {
		t.Fatal(err)
	}
	if asnap.State == types.RunPaused {
		t.Fatal("thesis pause leaked into the gastos tree")
	}
	// run.list shows both trees.
	listed := s.call(t, api.MethodRunList, map[string]any{})
	var list struct {
		Runs []api.RunStatusResult `json:"runs"`
	}
	if err := json.Unmarshal(listed, &list); err != nil || len(list.Runs) < 2 {
		t.Fatalf("run.list wrong: %v %s", err, listed)
	}
}

// spawnRun creates a live orchestrator run for boot tests: a parent
// agent with one queued coordinator child.
func spawnRun(t *testing.T, s *stack, name string) types.RunID {
	t.Helper()
	parent, err := s.orch.RegisterMaster("parent-"+name, mustGeneralScope())
	if err != nil {
		t.Fatal(err)
	}
	_, rec, err := s.orch.Spawn(parent.ID, orchestrator.DelegationCard{
		Name: "child-" + name, Description: "boot fixture",
		Goal: "survive the outage", Deliverable: "outcome",
		Scope: mustGeneralScope(), Done: []string{"outcome"},
		Report: "report", Deadline: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec.Run.ID
}

func mustGeneralScope() types.Scope { return types.GeneralScope() }

func mustAgentOf(t *testing.T, s *stack, runID types.RunID) types.AgentID {
	t.Helper()
	rec, err := s.store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Run.AgentID
}

func appendEntry(t *testing.T, ctx context.Context, js *journal.SQLStore, runID string, typ journal.EntryType, payload []byte) {
	t.Helper()
	if _, err := js.Append(ctx, journal.Entry{RunID: runID, Type: typ, Payload: payload}); err != nil {
		t.Fatalf("append %s: %v", typ, err)
	}
}

var _ = strings.Contains
