package orchestrator

import (
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Mailbox and directives of Section 15.5: agent_messages rows with
// at-least-once delivery, ack and dedupe by id, plus an in-memory
// wake so addressees and waiting parents resume promptly. Safe
// points belong to the engine: normal directives apply at the next
// one, interrupt directives cancel the in-flight call first.

// directiveWatch tracks an unacked directive for the Section 15.7
// ack deadline: 2 steps of the addressee or 120 s running.
type directiveWatch struct {
	run       types.RunID
	sentStep  int
	sentAt    time.Time
	escalated bool
}

// sendDirectiveLocked stores a directive, journals it, pushes it to a
// live interrupt runner, and wakes a waiting addressee: any directive
// wakes (Section 7.2). Normal directives to paused runs stay queued
// until resume (Section 15.8).
func (o *Orchestrator) sendDirectiveLocked(from, to types.AgentID, action DirectiveAction, text, summary string, priority DirectivePriority) (Directive, error) {
	now := o.cfg.Clock()
	msg := Message{
		ID:        types.NewMessageID(),
		From:      from,
		To:        to,
		Type:      MsgDirective,
		Priority:  priority,
		Payload:   string(action) + ": " + text,
		CreatedAt: now,
	}
	if err := o.store.AppendMessage(msg); err != nil {
		return Directive{}, err
	}
	d := Directive{ID: msg.ID, Action: action, Text: text, Summary: summary}
	o.journal(o.agentRunLocked(to), JournalDirectiveReceived, string(priority)+" "+string(action))
	sentStep := 0
	if rec, ok := o.agentLiveRunLocked(to); ok {
		if runner, ok := o.runners.Lookup(rec.Run.ID); ok {
			snap := runner.Snapshot()
			sentStep = snap.StepsDone
			if priority == PriorityInterrupt {
				runner.RequestInterrupt(false)
				runner.ApplyDirective(d)
			}
		}
		if rec.Run.State == types.RunWaiting {
			rec.Run.WaitReason = nil
			_ = o.store.UpdateRun(rec)
			_ = o.transitionLocked(rec.Run.ID, types.RunRunning, "wake: directive")
		}
	}
	o.directives[msg.ID] = directiveWatch{run: o.agentRunLocked(to), sentStep: sentStep, sentAt: now}
	o.emit(Event{Type: EventMessageDelivered, RunID: o.agentRunLocked(to), From: from, To: to, Detail: "directive " + string(priority)})
	return d, nil
}

// SendDirective stores a directive from one agent to another.
func (o *Orchestrator) SendDirective(from, to types.AgentID, action DirectiveAction, text, summary string, priority DirectivePriority) (Directive, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sendDirectiveLocked(from, to, action, text, summary, priority)
}

// PendingDirectives lists the unacked directives for an agent in send
// order: what the engine applies at its next safe point.
func (o *Orchestrator) PendingDirectives(to types.AgentID) []Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.store.UnackedDirectives(to)
}

// AckDirective records the ack of a directive at a safe point (or via
// the ack_directive tool) with the instruction revision the run has
// seen, and journals its application (Section 15.7).
func (o *Orchestrator) AckDirective(runID types.RunID, directiveID types.MessageID, note string, seenRev int) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.store.AckMessage(directiveID, note); err != nil {
		return err
	}
	if rec, err := o.store.GetRun(runID); err == nil {
		rec.Run.InstructionsRevSeen = seenRev
		_ = o.store.UpdateRun(rec)
	}
	delete(o.directives, directiveID)
	o.journal(runID, JournalDirectiveApplied, string(directiveID))
	o.emit(Event{Type: EventDirectiveAcked, RunID: runID, Detail: string(directiveID)})
	return nil
}

// PendingAckInfo describes an unacked directive past its grace for
// cooperative tools: it stays pending with its maximum wait.
type PendingAckInfo struct {
	Directive Message
	Run       types.RunID
	MaxWait   time.Duration
}

// CheckDirectiveAcks escalates unacked directives past the deadline:
// after 2 steps of the addressee, or 120 s running, the orchestrator
// interrupts the run and resumes it with the directive in context
// (Section 15.7). Directives inside after_completion or cooperative
// tools stay pending; the caller learns the maximum wait, which the
// master reports (the tool timeout).
func (o *Orchestrator) CheckDirectiveAcks(now time.Time) (escalated []types.MessageID, pending []PendingAckInfo) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for id, w := range o.directives {
		msg, err := o.store.GetMessage(id)
		if err != nil || msg.Acked || w.escalated {
			if err == nil && msg.Acked {
				delete(o.directives, id)
			}
			continue
		}
		rec, err := o.store.GetRun(w.run)
		if err != nil || rec.Run.State.IsTerminal() {
			delete(o.directives, id)
			continue
		}
		runner, ok := o.runners.Lookup(w.run)
		if !ok {
			continue
		}
		snap := runner.Snapshot()
		if snap.AckedDirectives[string(id)] {
			_ = o.AckDirectiveNoLock(w.run, id, "implicit in next step", snap.InstructionsSeen)
			continue
		}
		if snap.HasTool && (snap.ToolCancel == types.CancelAfterCompletion || snap.ToolCancel == types.CancelCooperative) {
			maxWait := snap.ToolTimeout
			if since := now.Sub(snap.ToolStarted); maxWait > since {
				maxWait -= since
			} else {
				maxWait = 0
			}
			pending = append(pending, PendingAckInfo{Directive: msg, Run: w.run, MaxWait: maxWait})
			continue
		}
		over := snap.StepsDone-w.sentStep >= o.cfg.DirectiveAckSteps
		if rec.Run.State == types.RunRunning && now.Sub(w.sentAt).Seconds() >= float64(o.cfg.DirectiveAckSecs) {
			over = true
		}
		if !over {
			continue
		}
		runner.RequestInterrupt(false)
		runner.ApplyDirective(Directive{ID: msg.ID, Text: msg.Payload})
		w.escalated = true
		o.directives[id] = w
		o.journal(w.run, JournalDirectiveReceived, "escalated "+string(msg.ID))
		o.emit(Event{Type: EventDirectiveEscalated, RunID: w.run, To: msg.To, Detail: string(msg.ID)})
		escalated = append(escalated, id)
	}
	return escalated, pending
}

// AckDirectiveNoLock is the lock-held variant for internal callers.
func (o *Orchestrator) AckDirectiveNoLock(runID types.RunID, directiveID types.MessageID, note string, seenRev int) error {
	if err := o.store.AckMessage(directiveID, note); err != nil {
		return err
	}
	if rec, err := o.store.GetRun(runID); err == nil {
		rec.Run.InstructionsRevSeen = seenRev
		_ = o.store.UpdateRun(rec)
	}
	delete(o.directives, directiveID)
	o.journal(runID, JournalDirectiveApplied, string(directiveID))
	o.emit(Event{Type: EventDirectiveAcked, RunID: runID, Detail: string(directiveID)})
	return nil
}

// SendMessage stores one non-directive mailbox row with dedupe by id
// and wakes a waiting parent: child completion, question, blocked and
// report rows wake (Section 15.5). When the addressee agent has no
// live run, the message climbs unchanged to the next ancestor; at the
// top it becomes a master notice (Section 15.5 chain).
func (o *Orchestrator) SendMessage(msg Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if msg.ID == "" {
		msg.ID = types.NewMessageID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = o.cfg.Clock()
	}
	before, _ := o.store.GetMessage(msg.ID)
	if err := o.store.AppendMessage(msg); err != nil {
		return err
	}
	if before.ID != "" {
		// Redelivery of a known id: no second wake, no second notice.
		return nil
	}
	o.emit(Event{Type: EventMessageDelivered, From: msg.From, To: msg.To, Detail: string(msg.Type)})
	o.wakeForMessageLocked(msg.To)
	return nil
}

// wakeForMessageLocked moves a waiting addressee to running, or
// climbs the chain when the addressee has no live run.
func (o *Orchestrator) wakeForMessageLocked(to types.AgentID) {
	if rec, ok := o.agentLiveRunLocked(to); ok {
		if rec.Run.State == types.RunWaiting {
			rec.Run.WaitReason = nil
			_ = o.store.UpdateRun(rec)
			_ = o.transitionLocked(rec.Run.ID, types.RunRunning, "wake: child message")
		}
		return
	}
	agent, err := o.store.GetAgent(to)
	if err != nil || agent.ParentID == nil {
		o.emit(Event{Type: EventNotice, To: to, Detail: "master notice: orphan message"})
		return
	}
	o.wakeForMessageLocked(*agent.ParentID)
}

// Escalate forwards an unresolvable question up unchanged: the first
// ancestor with a live run gets it; above the root it becomes a
// master notice (Section 15.5 chain).
func (o *Orchestrator) Escalate(from types.AgentID, content string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	cur := from
	for {
		agent, err := o.store.GetAgent(cur)
		if err != nil {
			return err
		}
		if agent.ParentID == nil {
			o.emit(Event{Type: EventNotice, From: from, Detail: "master notice: " + content})
			return nil
		}
		parent := *agent.ParentID
		if _, ok := o.agentLiveRunLocked(parent); ok {
			return o.store.AppendMessage(Message{
				ID: types.NewMessageID(), From: from, To: parent,
				Type: MsgQuestion, Payload: content, CreatedAt: o.cfg.Clock(),
			})
		}
		cur = parent
	}
}

// AskQuestion records a blocking or non-blocking question from a run
// and parks the run waiting for the user when it was running.
func (o *Orchestrator) AskQuestion(runID types.RunID, content string, blocking bool, def string) (types.Question, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(runID)
	if err != nil {
		return types.Question{}, err
	}
	now := o.cfg.Clock()
	q := types.Question{
		ID: string(types.NewMessageID()), RunID: runID, Kind: "user",
		Content: content, Blocking: blocking, Default: def,
		State: "open", CreatedAt: now, UpdatedAt: now,
	}
	if err := o.store.SaveQuestion(q); err != nil {
		return types.Question{}, err
	}
	if rec.Run.State == types.RunRunning {
		reason := types.WaitUser
		rec.Run.WaitReason = &reason
		_ = o.store.UpdateRun(rec)
		_ = o.transitionLocked(runID, types.RunWaiting, "waiting user")
	}
	o.emit(Event{Type: EventNotice, RunID: runID, Detail: "question: " + content})
	return q, nil
}

// AnswerQuestion stores the answer and wakes a run waiting on it.
func (o *Orchestrator) AnswerQuestion(questionID, answer string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	q, err := o.store.GetQuestion(questionID)
	if err != nil {
		return err
	}
	if err := o.store.AnswerQuestion(questionID, answer); err != nil {
		return err
	}
	_ = o.store.AppendMessage(Message{
		ID: types.NewMessageID(), To: agentOf(q.RunID, o), Type: MsgAnswer,
		Payload: answer, CreatedAt: o.cfg.Clock(),
	})
	if rec, err := o.store.GetRun(q.RunID); err == nil && rec.Run.State == types.RunWaiting {
		rec.Run.WaitReason = nil
		_ = o.store.UpdateRun(rec)
		_ = o.transitionLocked(q.RunID, types.RunRunning, "wake: answer")
	}
	return nil
}

func agentOf(run types.RunID, o *Orchestrator) types.AgentID {
	if rec, err := o.store.GetRun(run); err == nil {
		return rec.Run.AgentID
	}
	return ""
}

// agentLiveRunLocked returns the newest non-terminal run of an agent.
func (o *Orchestrator) agentLiveRunLocked(agent types.AgentID) (RunRecord, bool) {
	return o.parentRunLocked(agent)
}

// agentRunLocked returns the newest run of an agent, terminal or not,
// for journal attribution. Empty when the agent never ran.
func (o *Orchestrator) agentRunLocked(agent types.AgentID) types.RunID {
	rec, ok := o.agentLiveRunLocked(agent)
	if ok {
		return rec.Run.ID
	}
	var best RunRecord
	found := false
	for _, r := range o.store.ListRuns() {
		if r.Run.AgentID != agent {
			continue
		}
		if !found || r.Run.CreatedAt.After(best.Run.CreatedAt) {
			best, found = r, true
		}
	}
	if !found {
		return ""
	}
	return best.Run.ID
}
