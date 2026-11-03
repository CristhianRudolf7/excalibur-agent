package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// chatDelegations owns child lifetimes independently of the short master turn.
// Admission remains with the orchestrator; queued children never call a model.
// Shutdown cancels and joins these workers before their stores are closed.
type chatDelegations struct {
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	stopped bool
	jobs    map[string]bool
	wg      sync.WaitGroup
	owner   *assistantTurns
}

func newChatDelegations(a *assistantTurns) *chatDelegations {
	ctx, cancel := context.WithCancel(context.Background())
	return &chatDelegations{ctx: ctx, cancel: cancel, jobs: map[string]bool{}, owner: a}
}

func (d *chatDelegations) stop() {
	d.mu.Lock()
	d.stopped = true
	d.cancel()
	d.mu.Unlock()
	d.wg.Wait()
}

func (d *chatDelegations) spawn(conv, inheritedDir string, args json.RawMessage, id string, spanish bool, app *api.AppContext, channels ...string) {
	app = api.CloneAppContext(app)
	d.mu.Lock()
	if d.stopped || d.jobs[id] {
		d.mu.Unlock()
		return
	}
	d.jobs[id] = true
	d.wg.Add(1)
	d.mu.Unlock()
	safego.Go("chat-subagent-"+id, func(_ string, recovered any, _ []byte) {
		defer d.wg.Done()
		d.finish(conv, id, types.RunFailed, fmt.Sprintf("Subagent failed: %v", recovered))
	}, func() {
		d.run(conv, inheritedDir, args, id, spanish, app, channels...)
		d.wg.Done()
	})
}

func (d *chatDelegations) run(conv, inheritedDir string, args json.RawMessage, id string, spanish bool, app *api.AppContext, channels ...string) {
	ctx, cancel := context.WithTimeout(whiteboard.WithConversation(d.ctx, conv), 30*time.Minute)
	defer cancel()
	channel := ""
	if len(channels) > 0 {
		channel = channels[0]
	}
	ctx = context.WithValue(ctx, credentialOriginKey{}, d.owner.credentialOrigin(ctx, conv, id, channel))
	if d.owner.services.Credentials != nil {
		defer d.owner.services.Credentials.EndRun(id)
	}
	if app != nil {
		ctx = api.WithAppContext(ctx, *app)
	}
	var input struct {
		Card struct {
			Goal        string    `json:"goal"`
			Deliverable string    `json:"deliverable"`
			Done        []string  `json:"done_criteria"`
			Pinned      []string  `json:"pinned_instructions"`
			Inputs      []string  `json:"inputs"`
			Report      string    `json:"report_contract"`
			Tools       []string  `json:"tools"`
			Paths       []string  `json:"paths"`
			Deadline    time.Time `json:"deadline"`
		} `json:"card"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		d.finish(conv, id, types.RunFailed, err.Error())
		return
	}
	if !input.Card.Deadline.IsZero() {
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithDeadline(ctx, input.Card.Deadline)
		defer deadlineCancel()
	}
	s := d.owner.services
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var rec orchestrator.RunRecord
	for {
		var err error
		rec, err = s.Store.GetRun(types.RunID(id))
		if err != nil {
			return
		}
		if rec.Run.State.IsTerminal() {
			d.deliver(ctx, conv, id, "Subagent "+string(rec.Run.State))
			return
		}
		if rec.Run.State == types.RunRunning {
			break
		}
		select {
		case <-ctx.Done():
			d.finish(conv, id, types.RunFailed, "Subagent stopped: "+ctx.Err().Error())
			return
		case <-ticker.C:
		}
	}
	agent, err := s.Store.GetAgent(rec.Run.AgentID)
	if err != nil {
		d.finish(conv, id, types.RunFailed, err.Error())
		return
	}
	profile, err := agents.Lookup(agents.ProfileName(agent.Profile))
	if err != nil {
		d.finish(conv, id, types.RunFailed, err.Error())
		return
	}
	dir := inheritedDir
	if rec.Scope.Kind == types.ScopeProject {
		project, err := s.Projects.Get(types.ProjectID(rec.Scope.ID))
		if err != nil {
			d.finish(conv, id, types.RunFailed, "Subagent project: "+err.Error())
			return
		}
		dir = project.Roots[0]
	} else if dir == "" {
		for _, project := range s.Projects.List() {
			for _, root := range project.Roots {
				for _, path := range input.Card.Paths {
					rel, err := filepath.Rel(root, path)
					if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
						dir = root
					}
				}
			}
		}
		if dir == "" && len(s.Projects.List()) == 1 {
			dir = s.Projects.List()[0].Roots[0]
		}
	}
	w := s.engineOptions
	w.Workdir, w.Root = dir, agent.ID
	_, dt, err := buildEngine(w)
	if err != nil {
		d.finish(conv, id, types.RunFailed, err.Error())
		return
	}
	allowed, err := agents.ClampTools(profile, agent.Level, input.Card.Tools)
	if err != nil {
		d.finish(conv, id, types.RunFailed, err.Error())
		return
	}
	runner := &assistantToolRunner{daemonToolRunner: daemonToolRunner{ex: dt.Exec, reg: dt.Registry, artifacts: &artifactTurn{store: d.owner.backend.artifacts, conversation: conv, run: id}}, allowed: map[string]bool{}, runID: id, root: agent.ID}
	defer d.owner.finishArtifacts(ctx, runner.artifacts)
	runner.spawn = func(args json.RawMessage, child string) { d.spawn(conv, dir, args, child, spanish, app, channel) }
	for _, name := range allowed {
		runner.allowed[name] = true
	}
	activation := s.chatToolActivation(input.Card.Goal, d.owner.modelID, app)
	// An explicit card grant remains authoritative. Otherwise matching deferred
	// tools extend the role's normal on-demand discovery surface for this run.
	if len(input.Card.Tools) == 0 {
		for _, def := range activation.Definitions {
			runner.allowed[def.Name] = true
		}
	} else {
		activation = wireengine.ToolActivation{}
	}
	// Generic deferred dispatch cannot widen a role's explicit tool grant.
	delete(runner.allowed, "call_tool")
	var names []string
	var defs []providers.ToolDefinition
	for _, name := range dt.Registry.Names() {
		if def, err := runner.Definition(name); err == nil {
			names = append(names, name)
			defs = append(defs, providers.ToolDefinition{Name: name, Description: def.Description, Schema: def.InputSchema})
		}
	}
	prompt := fmt.Sprintf("Goal: %s\nDeliverable: %s\nDone criteria: %v\nInputs: %v\nInstructions: %v\nPaths: %v\nWorking directory: %s\nReport: %s", input.Card.Goal, input.Card.Deliverable, input.Card.Done, input.Card.Inputs, input.Card.Pinned, input.Card.Paths, dir, input.Card.Report)
	language := "English"
	if spanish {
		language = "Spanish"
	}
	prompt += "\nAnswer in " + language + "."
	snap := ctxcompiler.Snapshot{Task: ctxcompiler.TaskView{SuggestedSkills: activation.Skills}, Role: "child", AgentProfile: "Answer in " + language + ". Complete the delegated work with the available tools. Verify the done criteria and report the result.", Activity: []ctxcompiler.ActivityEvent{{Role: providers.RoleUser, Content: prompt}}}
	if note := d.owner.appContextNote(ctx, app); note != "" {
		snap.Activity = append(snap.Activity, ctxcompiler.ActivityEvent{Role: providers.RoleSystem, Content: note})
	}
	capture := &automationEndpoint{ModelEndpoint: d.owner.endpoint}
	builder := &engine.DefaultBuilder{Config: ctxcompiler.DefaultConfig(), ModelID: d.owner.modelID, Snapshot: func(context.Context, *engine.Run) (ctxcompiler.Snapshot, error) {
		for _, message := range s.Store.PendingMessages(agent.ID) {
			if message.Type == orchestrator.MsgDirective {
				continue
			}
			snap.Activity = append(snap.Activity, ctxcompiler.ActivityEvent{Role: providers.RoleUser, Content: "Message from parent: " + message.Payload})
			_ = s.Store.AckMessage(message.ID, "included in child context")
		}
		return snap, nil
	}, ToolDefs: func(*engine.Run) []providers.ToolDefinition { return defs }}
	eng := engine.New(engine.Deps{Models: capture, Context: builder, Tools: runner, Gate: engine.GateFunc(func(*engine.Run) tasks.GateReport { return tasks.GateReport{OK: true} }), SelfVerify: &tasks.SelfVerifyConfig{}, LongHorizon: &tasks.LongHorizonConfig{}})
	budget := rec.Run.Budget
	run := engine.NewRun(id, string(rec.Run.TaskID), engine.Profile{Name: agent.Profile, Role: profile.Role, Tools: names, ContextRole: "child", Limit: tasks.Limit{MaxSteps: budget.MaxSteps, MaxTokens: budget.MaxTokens, MaxCostUSD: budget.MaxCostUSD, MaxSeconds: budget.MaxSeconds, MaxProcs: budget.MaxProcs}})
	run.Workdir = dir
	eng.AttachJournal(id, s.Jrnl)
	if err := activation.Record(ctx, s.Jrnl, id); err != nil {
		d.finish(conv, id, types.RunFailed, "Record tool activation: "+err.Error())
		return
	}
	s.Orch.AttachRunner(engine.NewRunner(eng, run))
	defer s.Orch.DetachRunner(types.RunID(id))
	err = eng.Admit(ctx, run)
	if err == nil {
		err = eng.Start(ctx, run)
	}
	if err == nil {
		err = driveChatChild(ctx, s.Orch, eng, run)
	}
	d.owner.finishArtifacts(ctx, runner.artifacts)
	_ = s.Store.RecordUsage(types.RunID(id), engine.NewRunner(eng, run).Snapshot().Usage)
	state, _ := s.Orch.RunState(types.RunID(id))
	if state.IsTerminal() {
		d.deliver(ctx, conv, id, "Subagent "+string(state))
		return
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil || run.State() != types.RunCompleted || strings.TrimSpace(capture.final) == "" {
		d.finish(conv, id, types.RunFailed, fmt.Sprintf("Subagent did not complete (%s): %v", run.State(), err))
		return
	}
	d.finish(conv, id, types.RunCompleted, capture.final)
}

func (d *chatDelegations) finish(conv, id string, state types.RunState, text string) {
	if d.ctx.Err() != nil {
		return
	} // shutdown leaves durable work for recovery
	orch := d.owner.services.Orch
	current, err := orch.RunState(types.RunID(id))
	if err != nil {
		return
	}
	if current.IsTerminal() {
		d.deliver(d.ctx, conv, id, "Subagent "+string(current))
		return
	}
	// A queue/deadline stop uses cancellation: queued and paused executions
	// have no failure edge in the state diagram.
	if state == types.RunFailed && (current == types.RunQueued || current == types.RunPaused || current == types.RunBlocked) {
		if err := orch.Cancel(types.RunID(id)); err != nil {
			return
		}
	} else if err := orch.Finish(types.RunID(id), state, text); err != nil {
		d.owner.notice(conv, "Subagent outcome could not be recorded: "+err.Error())
		return
	}
	d.deliver(d.ctx, conv, id, text)
}

func (d *chatDelegations) deliver(ctx context.Context, conv, id, text string) {
	// Initial acknowledgement must be persisted before a fast child follow-up.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for d.owner.backend.Master.Turns().Active(conv) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
	_, err := d.owner.backend.Master.SendAssistant(ctx, conv, text, []string{id})
	if err != nil {
		d.owner.notice(conv, "Subagent report could not be delivered: "+err.Error())
	}
}

// driveChatChild keeps a paused execution attached until resumed. Interrupts
// stop an engine call; the orchestrator remains authoritative for admission.
func driveChatChild(ctx context.Context, orch *orchestrator.Orchestrator, eng *engine.Engine, run *engine.Run) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := eng.Run(ctx, run)
		if err != nil || ctx.Err() != nil {
			return err
		}
		state, err := orch.RunState(types.RunID(run.ID))
		if err != nil || state.IsTerminal() {
			return err
		}
		if state == types.RunRunning && run.State() != types.RunRunning {
			return nil
		}
		if state != types.RunRunning && run.State() == types.RunRunning {
			if err := eng.RequestPause(ctx, run, types.PauseUser, ""); err != nil {
				return err
			}
		}
		for state != types.RunRunning {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
			state, err = orch.RunState(types.RunID(run.ID))
			if err != nil || state.IsTerminal() {
				return err
			}
		}
		if run.State() == types.RunPaused {
			if err := eng.Resume(ctx, run); err != nil {
				return err
			}
			if err := eng.Start(ctx, run); err != nil {
				return err
			}
		}
	}
}
