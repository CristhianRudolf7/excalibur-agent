package orchestrator_test

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 15.10 row 4 (restart-safe mailbox is covered by the store
// contract; here the embedded half): at-least-once delivery without
// doubles, wakeups of waiting parents, and the escalation chain.

// A duplicate id is a redelivery: stored once, no second wake.
func TestMailboxDedupe(t *testing.T) {
	f := newFixture(t)
	if err := f.o.SetWaiting(f.prun.Run.ID, types.WaitSubagents); err != nil {
		t.Fatal(err)
	}
	id := types.NewMessageID()
	msg := orchestrator.Message{ID: id, From: f.parent.ID, To: f.parent.ID, Type: orchestrator.MsgReport, Payload: "half"}
	if err := f.o.SendMessage(msg); err != nil {
		t.Fatal(err)
	}
	if err := f.o.SendMessage(msg); err != nil {
		t.Fatal(err)
	}
	pending := f.store.PendingMessages(f.parent.ID)
	if len(pending) != 1 {
		t.Fatalf("duplicate delivery stored twice: %d", len(pending))
	}
	// First delivery woke the waiting parent exactly once.
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunRunning {
		t.Fatalf("waiting parent not woken, got %q", got)
	}
}

// Any child message wakes a waiting parent; questions climb to the
// first live ancestor unchanged, and past the root become a master
// notice.
func TestMailboxWakeAndEscalation(t *testing.T) {
	f := newFixture(t)
	child, crec := f.spawnOk(f.parent.ID, childCard("lena", agents.ProfileImplementer))
	_ = f.attach(crec)
	if err := f.o.SetWaiting(f.prun.Run.ID, types.WaitSubagents); err != nil {
		t.Fatal(err)
	}
	if err := f.o.SendMessage(orchestrator.Message{
		From: child.ID, To: f.parent.ID, Type: orchestrator.MsgCompletion, Payload: "done",
	}); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunRunning {
		t.Fatalf("completion did not wake parent, got %q", got)
	}
	// Finish the parent run; the next question from the child climbs
	// to the master notice (no live ancestor left).
	if err := f.o.Finish(f.prun.Run.ID, types.RunCompleted, "done"); err != nil {
		t.Fatal(err)
	}
	drained := 0
drain:
	for {
		select {
		case ev := <-f.o.Watch():
			_ = ev
			drained++
		default:
			break drain
		}
	}
	_ = drained
	if err := f.o.Escalate(child.ID, "which schema?"); err != nil {
		t.Fatal(err)
	}
	seen := false
	timeout := 0
	for !seen && timeout < 100 {
		select {
		case ev := <-f.o.Watch():
			if ev.Type == orchestrator.EventNotice && ev.Detail == "master notice: which schema?" {
				seen = true
			}
		default:
			timeout = 100
		}
		timeout++
	}
	if !seen {
		t.Fatal("orphan question never became a master notice")
	}
}

// Ask parks the run waiting on the user; the answer wakes it.
func TestAskAnswer(t *testing.T) {
	f := newFixture(t)
	q, err := f.o.AskQuestion(f.prun.Run.ID, "proceed?", true, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunWaiting {
		t.Fatalf("ask must park the run, got %q", got)
	}
	if err := f.o.AnswerQuestion(q.ID, "yes, proceed"); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunRunning {
		t.Fatalf("answer must wake the run, got %q", got)
	}
	if got := f.store.PendingQuestions(f.prun.Run.ID); len(got) != 0 {
		t.Fatal("answered question still pending")
	}
}

// A normal directive wakes a waiting run; ack records the seen
// revision and journals the application.
func TestDirectiveAck(t *testing.T) {
	f := newFixture(t)
	if err := f.o.SetWaiting(f.prun.Run.ID, types.WaitSubagents); err != nil {
		t.Fatal(err)
	}
	d, err := f.o.SendDirective(f.master.ID, f.parent.ID, orchestrator.ActionAdjust, "use CSV", "", orchestrator.PriorityNormal)
	if err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunRunning {
		t.Fatalf("any directive wakes a waiting run, got %q", got)
	}
	if got := f.o.PendingDirectives(f.parent.ID); len(got) != 1 {
		t.Fatalf("want 1 pending directive, got %d", len(got))
	}
	if err := f.o.AckDirective(f.prun.Run.ID, d.ID, "will do", 3); err != nil {
		t.Fatal(err)
	}
	if got := f.o.PendingDirectives(f.parent.ID); len(got) != 0 {
		t.Fatal("acked directive still pending")
	}
	rec, _ := f.store.GetRun(f.prun.Run.ID)
	if rec.Run.InstructionsRevSeen != 3 {
		t.Fatalf("seen revision not recorded: %d", rec.Run.InstructionsRevSeen)
	}
	found := false
	for _, typ := range f.jrnl.Types() {
		if typ == orchestrator.JournalDirectiveApplied {
			found = true
		}
	}
	if !found {
		t.Fatal("directive application left no journal row")
	}
}

// Unacked directives escalate after 2 steps; cooperative and
// after_completion tools stay pending with the maximum wait.
func TestDirectiveAckDeadline(t *testing.T) {
	f := newFixture(t)
	child, crec := f.spawnOk(f.parent.ID, childCard("coop", agents.ProfileImplementer))
	_ = child
	cr := f.attach(crec)
	cr.AutoAck = false
	_, err := f.o.SendDirective(f.parent.ID, crec.Run.AgentID, orchestrator.ActionReplan, "replan", "", orchestrator.PriorityNormal)
	if err != nil {
		t.Fatal(err)
	}
	// Tool with cooperative cancel: stays pending with max wait.
	cr.SetTool("browser_wait_ready", "{}", types.CancelCooperative, nil, 0, f.now)
	cr.Step(f.o)
	esc, pend := f.o.CheckDirectiveAcks(f.now)
	if len(esc) != 0 || len(pend) != 1 {
		t.Fatalf("cooperative tool must stay pending: esc=%v pend=%d", esc, len(pend))
	}
	// Ordinary tool: escalate after 2 steps of the addressee.
	cr.SetTool("exec", "make test", types.CancelGraceful, nil, 0, f.now)
	cr.Step(f.o)
	cr.Step(f.o)
	esc, _ = f.o.CheckDirectiveAcks(f.now)
	if len(esc) != 1 {
		t.Fatalf("want 1 escalation after 2 steps, got %v", esc)
	}
	if full, _ := cr.Interrupts(); full != 1 {
		t.Fatalf("escalation must interrupt the run, got %d", full)
	}
	if got := len(cr.Delivered()); got != 1 {
		t.Fatalf("escalation must redeliver with context, got %d", got)
	}
}
