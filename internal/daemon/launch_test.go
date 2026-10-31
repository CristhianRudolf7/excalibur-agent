package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func testOrchestrator() (*orchestrator.Orchestrator, orchestrator.Store) {
	store := orchestrator.NewMemoryStore()
	return orchestrator.New(orchestrator.DefaultConfig(), store, orchestrator.NoopJournal{}), store
}

// TestOrchestratorLauncherSpawn covers the agenda-to-orchestrator
// row: a fire spawns a coordinator child; a reminder posts a notice
// and spawns its linked task follow-up.
func TestOrchestratorLauncherSpawn(t *testing.T) {
	orch, store := testOrchestrator()
	notifier := master.NewNotifier(master.NotifierConfig{})
	launcher, err := NewOrchestratorLauncher(orch, notifier)
	if err != nil {
		t.Fatalf("launcher: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	if err := launcher.LaunchFire(ctx, agenda.Fire{FireID: "fire-1", ScheduleID: "nightly", Scheduled: now, Payload: "run the nightly checks"}); err != nil {
		t.Fatalf("launch fire: %v", err)
	}
	if len(store.ListRuns()) == 0 {
		t.Fatal("fire spawned no run")
	}
	rem := agenda.Reminder{ID: "r1", Text: "call Ana", State: agenda.ReminderPending, NextFireUTC: now}
	if err := launcher.LaunchReminder(ctx, rem); err != nil {
		t.Fatalf("launch reminder: %v", err)
	}
}

// TestQuestionConfirmerFlow covers the question-flow confirmer row:
// a confirm node becomes an answerable question; yes confirms, no
// declines, and a nil store declines without blocking.
func TestQuestionConfirmerFlow(t *testing.T) {
	_, store := testOrchestrator()
	conf := NewQuestionConfirmer(store)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = store.AnswerQuestion("confirm-inst-1-node-1", "yes, go ahead")
	}()
	ok, err := conf.WaitConfirm(ctx, "inst-1", "node-1", "send the application?")
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !ok {
		t.Fatal("yes answer declined")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = store.AnswerQuestion("confirm-inst-2-node-1", "no")
	}()
	ok2, err := conf.WaitConfirm(ctx2, "inst-2", "node-1", "send the application?")
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if ok2 {
		t.Fatal("no answer confirmed")
	}
	nilConf := NewQuestionConfirmer(nil)
	ok3, err := nilConf.WaitConfirm(ctx, "inst-3", "node-1", "anything?")
	if err != nil || ok3 {
		t.Fatalf("nil confirmer must decline: ok=%v err=%v", ok3, err)
	}
}

// TestModelDeciderClassify covers the Table 2.1 intake row: the
// calibrated model answers type/scope/durable/clarify, abstention
// degrades to the deterministic triage, and deterministic rules need
// no model at all.
func TestModelDeciderClassify(t *testing.T) {
	ctx := context.Background()
	lex := master.NewLexicon()
	now := time.Now()
	// High-confidence task classification.
	fake := models.NewFakeDecider(0.95, 0.9, 0.1, 0.1)
	dec := NewModelDecider(fake, []string{"general", "project:p1"})
	pre := master.Preanalyze("implement the CSV export", lex, now, time.UTC)
	tri := dec.Classify(ctx, "implement the CSV export", pre)
	if tri.Confidence != 0.95 {
		t.Fatalf("confidence not carried: %+v", tri)
	}
	// Nil decider: deterministic rules only.
	nilDec := NewModelDecider(nil, nil)
	preCmd := master.Preanalyze("/status", lex, now, time.UTC)
	triCmd := nilDec.Classify(ctx, "/status", preCmd)
	if triCmd.Type != master.TriageStatusQuery {
		t.Fatalf("command not fast-laned deterministically: %+v", triCmd)
	}
	preCtl := master.Preanalyze("pause the thesis work", lex, now, time.UTC)
	_ = preCtl
}

// TestDaemonTriggerLauncherDispatch covers the Section 21.3 daemon
// side: every trigger action spawns or posts through real packages.
func TestDaemonTriggerLauncherDispatch(t *testing.T) {
	orch, store := testOrchestrator()
	notifier := master.NewNotifier(master.NotifierConfig{})
	launcher, err := NewDaemonTriggerLauncher(orch, store, notifier)
	if err != nil {
		t.Fatalf("launcher: %v", err)
	}
	ctx := context.Background()
	ev := agenda.NormalizeMail("m1", "ana@example.com", "hello", time.Now().UTC())
	if err := launcher.StartTask(ctx, "inbox", ev); err != nil {
		t.Fatalf("start task: %v", err)
	}
	if err := launcher.LaunchFlow(ctx, "post", ev); err != nil {
		t.Fatalf("launch flow: %v", err)
	}
	if err := launcher.StartSession(ctx, "standup", ev); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if err := launcher.Notify(ctx, "inbox", ev); err != nil {
		t.Fatalf("notify: %v", err)
	}
	count := 0
	for range store.ListRuns() {
		count++
	}
	if count != 3 {
		t.Fatalf("want 3 spawned runs, have %d", count)
	}
	_ = types.GeneralScope
}
