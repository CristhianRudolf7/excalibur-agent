package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// OrchControl adapts the H8 orchestrator to the fast-lane control
// surface (Sections 6.1, 15.6, 15.8). Snapshots come from the
// orchestrator status; orders serialize in its actor.
type OrchControl struct {
	Orch  *orchestrator.Orchestrator
	Store orchestrator.Store
}

// Snapshot implements master.StatusResolver: one deterministic
// Section 15.6 snapshot.
func (c *OrchControl) Snapshot(runID string) (api.RunStatusResult, error) {
	if c.Orch == nil {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeUnknownTarget, Message: "no orchestrator"}
	}
	st, err := c.Orch.Status(types.RunID(runID))
	if err != nil {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeUnknownTarget, Message: fmt.Sprintf("unknown run %q", runID)}
	}
	return st.ToAPI(), nil
}

// ListRuns implements master.StatusResolver.
func (c *OrchControl) ListRuns() []api.RunStatusResult {
	if c.Store == nil {
		return nil
	}
	var out []api.RunStatusResult
	for _, rec := range c.Store.ListRuns() {
		if rec.Run.State.IsTerminal() {
			continue
		}
		if c.Orch != nil {
			if st, err := c.Orch.Status(rec.Run.ID); err == nil {
				out = append(out, st.ToAPI())
				continue
			}
		}
		out = append(out, api.RunStatusResult{RunID: string(rec.Run.ID), State: rec.Run.State})
	}
	return out
}

// FindRunsByAgentName implements master.StatusResolver: every live
// run of agents with this visible name (ambiguity surfaces all).
func (c *OrchControl) FindRunsByAgentName(name string) []api.RunStatusResult {
	if c.Store == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(name))
	var out []api.RunStatusResult
	for _, a := range c.Store.ListAgents() {
		if strings.ToLower(a.Name) != key && !strings.Contains(strings.ToLower(a.Name), key) {
			continue
		}
		for _, rec := range c.Store.ListRuns() {
			if rec.Run.AgentID != a.ID || rec.Run.State.IsTerminal() {
				continue
			}
			if c.Orch != nil {
				if st, err := c.Orch.Status(rec.Run.ID); err == nil {
					out = append(out, st.ToAPI())
					continue
				}
			}
			out = append(out, api.RunStatusResult{RunID: string(rec.Run.ID), State: rec.Run.State, ToolInFlight: a.Name})
		}
	}
	return out
}

// Pause implements master.ControlRunner.
func (c *OrchControl) Pause(runID string) (api.RunStatusResult, error) {
	if err := c.Orch.Pause(types.RunID(runID), types.PauseUser, ""); err != nil {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeConflict, Message: err.Error()}
	}
	return c.Snapshot(runID)
}

// Resume implements master.ControlRunner.
func (c *OrchControl) Resume(runID string) (api.RunStatusResult, error) {
	if err := c.Orch.Resume(types.RunID(runID), ""); err != nil {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeConflict, Message: err.Error()}
	}
	return c.Snapshot(runID)
}

// Cancel implements master.ControlRunner.
func (c *OrchControl) Cancel(runID string) (api.RunStatusResult, error) {
	if err := c.Orch.Cancel(types.RunID(runID)); err != nil {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeConflict, Message: err.Error()}
	}
	return c.Snapshot(runID)
}

// Interrupt implements master.ControlRunner.
func (c *OrchControl) Interrupt(runID string, toolOnly bool) (api.RunStatusResult, error) {
	if err := c.Orch.Interrupt(types.RunID(runID), toolOnly); err != nil {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeConflict, Message: err.Error()}
	}
	return c.Snapshot(runID)
}

// Redirect implements master.ControlRunner.
func (c *OrchControl) Redirect(runID, text string, interrupt bool) (api.RunStatusResult, error) {
	if err := c.Orch.Redirect(types.RunID(runID), text, orchestrator.ActionReplan, interrupt); err != nil {
		return api.RunStatusResult{}, &api.Error{Code: api.CodeConflict, Message: err.Error()}
	}
	return c.Snapshot(runID)
}

// PauseAll implements master.ControlRunner: every non-terminal run.
func (c *OrchControl) PauseAll() []api.RunStatusResult {
	var out []api.RunStatusResult
	for _, rec := range c.ListRuns() {
		if snap, err := c.Pause(rec.RunID); err == nil {
			out = append(out, snap)
		}
	}
	return out
}

// AnswerQuestion implements master.ControlRunner.
func (c *OrchControl) AnswerQuestion(questionID, answer string) error {
	if c.Store == nil {
		return fmt.Errorf("daemon: no store")
	}
	return c.Store.AnswerQuestion(questionID, answer)
}

// ApprovePlan implements master.ControlRunner.
func (c *OrchControl) ApprovePlan(_ string) error {
	return fmt.Errorf("daemon: no plan awaiting approval")
}

// DenyPlan implements master.ControlRunner.
func (c *OrchControl) DenyPlan(_, _ string) error {
	return fmt.Errorf("daemon: no plan awaiting approval")
}

// SetScope implements master.ControlRunner.
func (c *OrchControl) SetScope(_, _ string) error { return nil }

// DaemonBackend routes the gate methods to the master (conversation),
// the H8 orchestrator (runs) and the embedded memory backend for the
// task delegation, files and passthrough families (Section 3.6).
// AttachServices installs the real daemon-side backends; without them
// every family degrades to the embedded backend.
type DaemonBackend struct {
	credentials    *CredentialRegistry
	classroomTurns classroomTurns
	Master         *master.Master
	Ctrls          *OrchControl
	Bus            *telemetry.Bus
	activity       activityState
	artifacts      *artifactStore
	embed          *gateway.MemoryBackend
	// Services holds the real backends; nil keeps embedded behavior.
	Services *daemonServices
}

// NewDaemonBackend returns the composed backend.
func NewDaemonBackend(m *master.Master, ctrls *OrchControl, bus *telemetry.Bus) *DaemonBackend {
	b := &DaemonBackend{Master: m, Ctrls: ctrls, Bus: bus, embed: gateway.NewMemoryBackend(bus)}
	b.configureAssistantOutput()
	return b
}

// ConversationSend implements gateway.Backend through master intake:
// idempotent on client_msg_id, receipt in milliseconds.
func (b *DaemonBackend) ConversationSend(ctx context.Context, clientID, clientMsgID, conversationID, text string) (types.MessageID, bool, error) {
	if b.Master == nil {
		return b.embed.ConversationSend(ctx, clientID, clientMsgID, conversationID, text)
	}
	if err := b.checkAgentConversation(conversationID); err != nil {
		return "", false, err
	}
	// Duplicate probe first: the master dedupes, but flag it.
	id, err := b.Master.ReceiveDeferred(ctx, clientID, clientMsgID, conversationID, text, nil, b.fastReply(ctx, conversationID))
	if err != nil {
		return "", false, err
	}
	return id, false, nil
}

// ConversationHistory implements gateway.Backend: master intake owns
// persistence (Section 6.1), so history reads the master store when
// attached and the embedded backend otherwise.
func (b *DaemonBackend) ConversationHistory(_ context.Context, conversationID string, before string, limit int) ([]gateway.StoredMessage, error) {
	if b.Master != nil {
		var out []gateway.StoredMessage
		for _, h := range b.Master.History(conversationID, 0) {
			if before != "" && string(h.ID) >= before {
				continue
			}
			out = append(out, gateway.StoredMessage{
				ID: h.ID, ConversationID: types.ConversationID(conversationID),
				RelayedBy: h.RelayedBy, CreatedAt: h.CreatedAt, Role: h.Role, Text: h.Text, Sources: h.Sources, Changes: h.Changes,
			})
		}
		if limit > 0 && len(out) > limit {
			out = out[len(out)-limit:]
		}
		if out == nil {
			out = []gateway.StoredMessage{}
		}
		return out, nil
	}
	return b.embed.ConversationHistory(context.Background(), conversationID, before, limit)
}

// RunControl implements gateway.Backend over the orchestrator.
func (b *DaemonBackend) RunControl(_ context.Context, method, runID string, interrupt bool, text string) (api.RunStatusResult, error) {
	if b.Ctrls == nil || b.Ctrls.Orch == nil {
		return b.embed.RunControl(context.Background(), method, runID, interrupt, text)
	}
	switch method {
	case api.MethodRunStatus:
		return b.Ctrls.Snapshot(runID)
	case api.MethodRunPause:
		return b.Ctrls.Pause(runID)
	case api.MethodRunResume:
		return b.Ctrls.Resume(runID)
	case api.MethodRunInterrupt:
		return b.Ctrls.Interrupt(runID, interrupt)
	case api.MethodRunCancel:
		return b.Ctrls.Cancel(runID)
	case api.MethodRunRedirect:
		return b.Ctrls.Redirect(runID, text, interrupt)
	default:
		return api.RunStatusResult{}, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}

// RunList implements gateway.Backend.
func (b *DaemonBackend) RunList(_ context.Context) ([]api.RunStatusResult, error) {
	if b.Ctrls == nil {
		return b.embed.RunList(context.Background())
	}
	return b.Ctrls.ListRuns(), nil
}

// Bridge forwards orchestrator bus events to the gate bus and posts
// run notices to the master notifier (Sections 5.4, 15.5, 22.1). One
// safego.Go goroutine; panics cannot take the daemon down.
func Bridge(ctx context.Context, orch *orchestrator.Orchestrator, bus *telemetry.Bus, notify *master.Notifier) {
	if orch == nil {
		return
	}
	safego.Go("daemon-bridge", nil, func() {
		ch := orch.Watch()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				forwardEvent(bus, notify, ev)
			}
		}
	})
}

func forwardEvent(bus *telemetry.Bus, notify *master.Notifier, ev orchestrator.Event) {
	runID := types.RunID(ev.RunID)
	var runPtr *types.RunID
	if ev.RunID != "" {
		runPtr = &runID
	}
	if bus != nil {
		switch ev.Type {
		case orchestrator.EventStateChanged:
			_, _ = bus.Publish(types.EventRunStateChanged, runPtr, nil, api.RunProgressEvent{RunID: runID})
		case orchestrator.EventNotice:
			_, _ = bus.Publish(types.EventNoticePosted, runPtr, nil, api.NoticeEvent{Content: ev.Detail})
		}
	}
	if notify != nil && ev.Type == orchestrator.EventNotice {
		notify.Post(master.Notice{
			RunID:   runID,
			Class:   types.NoticeNormal,
			Content: ev.Detail,
		})
	}
}

// ConversationSendAttachments is the additive transport seam for document ids.
// The text-only Backend contract continues through ConversationSend.
func (b *DaemonBackend) ConversationSendAttachments(ctx context.Context, clientID, clientMsgID, conversationID, text string, attachments []string) (types.MessageID, bool, error) {
	return b.ConversationSendContext(ctx, clientID, clientMsgID, conversationID, text, attachments, nil)
}

// ConversationSendContext preserves app context and attachments in one intake.
func (b *DaemonBackend) ConversationSendContext(ctx context.Context, clientID, clientMsgID, conversationID, text string, attachments []string, app *api.AppContext) (types.MessageID, bool, error) {
	if err := app.Validate(); err != nil {
		return "", false, err
	}
	if b.Master == nil {
		if app != nil {
			return "", false, &api.Error{Code: api.CodeInvalidParams, Message: "app context requires the daemon master"}
		}
		return b.ConversationSend(ctx, clientID, clientMsgID, conversationID, text)
	}
	if err := b.checkAgentConversation(conversationID); err != nil {
		return "", false, err
	}
	id, err := b.Master.ReceiveDeferredWithContext(ctx, clientID, clientMsgID, conversationID, text, attachments, app, b.fastReply(ctx, conversationID))
	return id, false, err
}

// fastReply posts a fast-lane answer after the receipt has already returned.
func (b *DaemonBackend) fastReply(ctx context.Context, conversationID string) func(string) {
	return func(reply string) {
		pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = b.Master.SendAssistant(pctx, conversationID, reply, nil)
	}
}
