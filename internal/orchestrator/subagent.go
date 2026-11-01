package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// The subagent tool of Section 13.2: spawn (with delegation card),
// status, message, redirect, pause, resume, interrupt and cancel with
// the Section 15.8 semantics. Only the master and level-1 parents
// hold it; the master may act on any execution of the user while a
// parent is confined to its own subtree. It lives in the
// orchestrator package (not internal/tools) so the hierarchy owner
// also owns the control surface; the tools catalog registers it like
// any other tool.

var subagentSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["spawn", "status", "message", "redirect", "pause", "resume", "interrupt", "cancel"]},
    "caller": {"type": "string", "description": "agt_ id of the calling agent; injected by the engine"},
    "run": {"type": "string", "description": "run_ id the action targets"},
    "to": {"type": "string", "description": "agt_ id for message"},
    "message_type": {"type": "string", "enum": ["report", "question", "completion", "blocked"]},
    "text": {"type": "string"},
    "interrupt": {"type": "boolean"},
    "tool_only": {"type": "boolean"},
    "card": {"type": "object"}
  },
  "required": ["caller"],
  "additionalProperties": false
}`)

// SubagentTool implements tools.Tool over one orchestrator.
type SubagentTool struct {
	Orch *Orchestrator

	mu   sync.Mutex
	keys map[string]tools.Result
}

// Definition implements tools.Tool.
func (t *SubagentTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "subagent",
		Description: "Manage subagents: spawn with a delegation card, status, message, redirect, pause, resume, interrupt, cancel. " +
			"Only the master and level-1 parents hold it; a parent only acts on its own subtree",
		InputSchema:  subagentSchema,
		Effect:       types.EffectWriteLocal,
		Idempotency:  types.IdemKeyed,
		Cancel:       types.CancelImmediate,
		Timeout:      30 * time.Second,
		OutputBudget: 8192,
		Platforms:    []string{"linux"},
		Profiles:     []string{"default", "delegate", "eval"},
		Group:        "control",
		Tags:         []string{"subagents", "control"},
		Keywords:     []string{"subagent", "spawn", "delegate", "pause", "resume", "cancel", "redirect", "status"},
	}
}

type subagentArgs struct {
	Action      string          `json:"action"`
	Caller      string          `json:"caller"`
	Run         string          `json:"run"`
	To          string          `json:"to"`
	MessageType string          `json:"message_type"`
	Text        string          `json:"text"`
	Interrupt   bool            `json:"interrupt"`
	ToolOnly    bool            `json:"tool_only"`
	Card        json.RawMessage `json:"card"`
}

const subagentExamples = `examples: subagent {"action":"spawn","card":{"name":"worker","description":"scoped worker","profile":"implementer","goal":"...","deliverable":"...","scope":"general","done_criteria":["verified"],"report_contract":"summary"}}; subagent {"action":"status","run":"run_..."}`

// Invoke implements tools.Tool.
func (t *SubagentTool) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	if t.Orch == nil {
		return tools.Result{Status: tools.StatusToolError, Output: "subagent: no orchestrator wired"}, nil
	}
	if inv.IdempotencyKey != "" {
		t.mu.Lock()
		if cached, ok := t.keys[inv.IdempotencyKey]; ok {
			t.mu.Unlock()
			return cached, nil
		}
		t.mu.Unlock()
	}
	var a subagentArgs
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error() + "; " + subagentExamples}, nil
	}
	if a.Action == "" && tools.FrictionRepairEnabled() {
		if len(a.Card) > 0 && string(a.Card) != "null" {
			a.Action = "spawn"
		} else if a.Run != "" {
			a.Action = "status"
		}
	}
	if a.Action == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "action is required; " + subagentExamples}, nil
	}
	res, retryable := t.dispatch(a)
	if res.Status != tools.StatusOK {
		res.Output += "; " + subagentExamples
	}
	if inv.IdempotencyKey != "" && res.Status == tools.StatusOK && !retryable {
		t.mu.Lock()
		if t.keys == nil {
			t.keys = map[string]tools.Result{}
		}
		t.keys[inv.IdempotencyKey] = res
		t.mu.Unlock()
	}
	return res, nil
}

func (t *SubagentTool) dispatch(a subagentArgs) (tools.Result, bool) {
	ok := func(out any) (tools.Result, bool) {
		raw, _ := json.Marshal(out)
		return tools.Result{Status: tools.StatusOK, Output: string(raw)}, false
	}
	fail := func(s tools.ResultStatus, msg string) (tools.Result, bool) {
		return tools.Result{Status: s, Output: msg}, s == tools.StatusTransient
	}
	caller, err := t.caller(a.Caller)
	if err != nil {
		return fail(tools.StatusDenied, err.Error())
	}
	if caller.Level >= agents.MaxLevel {
		return fail(tools.StatusDenied,
			fmt.Sprintf("subagent: level-%d agents hold no subagent tool", caller.Level))
	}
	switch a.Action {
	case "spawn":
		card, err := decodeCard(a.Card)
		if err != nil {
			return fail(tools.StatusInputError, err.Error())
		}
		child, rec, err := t.Orch.Spawn(caller.ID, card)
		if err != nil {
			return fail(tools.StatusToolError, err.Error())
		}
		return ok(map[string]any{"agent_id": child.ID, "run_id": rec.Run.ID, "state": rec.Run.State})
	case "status":
		if a.Run == "" {
			return fail(tools.StatusInputError, "run is required for status")
		}
		st, err := t.Orch.Status(types.RunID(a.Run))
		if err != nil {
			return fail(tools.StatusToolError, err.Error())
		}
		if err := t.checkScope(caller, st.AgentID); err != nil {
			return fail(tools.StatusDenied, err.Error())
		}
		return ok(statusView(st))
	case "message":
		if a.To == "" || a.Text == "" {
			return fail(tools.StatusInputError, "to and text are required for message")
		}
		mtype := MessageType(a.MessageType)
		switch mtype {
		case MsgReport, MsgQuestion, MsgCompletion, MsgBlocked:
		default:
			return fail(tools.StatusInputError, fmt.Sprintf("bad message_type %q", a.MessageType))
		}
		to := types.AgentID(a.To)
		if err := t.checkScope(caller, to); err != nil {
			return fail(tools.StatusDenied, err.Error())
		}
		msg := Message{From: caller.ID, To: to, Type: mtype, Payload: a.Text}
		if err := t.Orch.SendMessage(msg); err != nil {
			return fail(tools.StatusToolError, err.Error())
		}
		return ok(map[string]any{"delivered_to": to})
	case "redirect":
		if a.Run == "" || a.Text == "" {
			return fail(tools.StatusInputError, "run and text are required for redirect")
		}
		rec, err := t.Orch.store.GetRun(types.RunID(a.Run))
		if err != nil {
			return fail(tools.StatusToolError, err.Error())
		}
		if err := t.checkScope(caller, rec.Run.AgentID); err != nil {
			return fail(tools.StatusDenied, err.Error())
		}
		if err := t.Orch.Redirect(rec.Run.ID, a.Text, ActionReplan, a.Interrupt); err != nil {
			return fail(tools.StatusToolError, err.Error())
		}
		return ok(map[string]any{"run_id": rec.Run.ID, "interrupt": a.Interrupt})
	case "pause":
		return t.runOrder(a, caller, "pause", func(id types.RunID) error {
			return t.Orch.Pause(id, types.PauseUser, "")
		})
	case "resume":
		return t.runOrder(a, caller, "resume", func(id types.RunID) error {
			return t.Orch.Resume(id, "")
		})
	case "interrupt":
		return t.runOrder(a, caller, "interrupt", func(id types.RunID) error {
			return t.Orch.Interrupt(id, a.ToolOnly)
		})
	case "cancel":
		return t.runOrder(a, caller, "cancel", func(id types.RunID) error {
			return t.Orch.Cancel(id)
		})
	default:
		return fail(tools.StatusInputError, fmt.Sprintf("unknown action %q", a.Action))
	}
}

func (t *SubagentTool) runOrder(a subagentArgs, caller types.Agent, action string, fn func(types.RunID) error) (tools.Result, bool) {
	ok := func(out any) (tools.Result, bool) {
		raw, _ := json.Marshal(out)
		return tools.Result{Status: tools.StatusOK, Output: string(raw)}, false
	}
	if a.Run == "" {
		return tools.Result{Status: tools.StatusInputError, Output: action + " needs run"}, false
	}
	rec, err := t.Orch.store.GetRun(types.RunID(a.Run))
	if err != nil {
		return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, false
	}
	if err := t.checkScope(caller, rec.Run.AgentID); err != nil {
		return tools.Result{Status: tools.StatusDenied, Output: err.Error()}, false
	}
	if err := fn(rec.Run.ID); err != nil {
		return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, false
	}
	after, _ := t.Orch.store.GetRun(rec.Run.ID)
	return ok(map[string]any{"run_id": after.Run.ID, "state": after.Run.State})
}

func (t *SubagentTool) caller(id string) (types.Agent, error) {
	if id == "" {
		return types.Agent{}, fmt.Errorf("subagent: caller is required (engine injects the calling agt_ id)")
	}
	agent, err := t.Orch.store.GetAgent(types.AgentID(id))
	if err != nil {
		return types.Agent{}, fmt.Errorf("subagent: %w", err)
	}
	if !agents.MaySpawn(mustLookup(agent.Profile), agent.Level) && agent.Level != 0 {
		return types.Agent{}, fmt.Errorf("subagent: agent %q holds no subagent tool", agent.Name)
	}
	return agent, nil
}

// checkScope confines a parent to its own subtree; the master (level
// 0) may act on any execution of the user (Section 13.2).
func (t *SubagentTool) checkScope(caller types.Agent, target types.AgentID) error {
	if caller.Level == 0 {
		return nil
	}
	cur := target
	for {
		if cur == caller.ID {
			return nil
		}
		agent, err := t.Orch.store.GetAgent(cur)
		if err != nil || agent.ParentID == nil {
			return fmt.Errorf("subagent: %q is outside the caller's subtree", target)
		}
		cur = *agent.ParentID
	}
}

// ResourceKeys implements tools.Tool: control orders serialize per
// target run (Section 7.4).
func (t *SubagentTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a subagentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Run != "" {
		return []string{"run:" + a.Run}, nil
	}
	if a.To != "" {
		return []string{"agent:" + a.To}, nil
	}
	return []string{"agent:" + a.Caller}, nil
}

// cardJSON is the wire form of a DelegationCard for spawn.
type cardJSON struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Profile     string       `json:"profile"`
	Goal        string       `json:"goal"`
	Deliverable string       `json:"deliverable"`
	Scope       string       `json:"scope"`
	Paths       []string     `json:"paths"`
	Inputs      []string     `json:"inputs"`
	Pinned      []string     `json:"pinned_instructions"`
	Skills      []string     `json:"suggested_skills"`
	Tools       []string     `json:"tools"`
	Budget      types.Budget `json:"budget"`
	Deadline    string       `json:"deadline"`
	Done        []string     `json:"done_criteria"`
	Report      string       `json:"report_contract"`
	ForkContext bool         `json:"fork_context"`
	Task        string       `json:"task_id"`
	Lane        string       `json:"lane"`
}

func decodeCard(raw json.RawMessage) (DelegationCard, error) {
	var c cardJSON
	if err := json.Unmarshal(raw, &c); err != nil {
		return DelegationCard{}, fmt.Errorf("subagent: bad card: %w", err)
	}
	scope, err := types.ParseScope(c.Scope)
	if err != nil {
		return DelegationCard{}, fmt.Errorf("subagent: %w", err)
	}
	card := DelegationCard{
		Name: cardName(c), Description: c.Description,
		Profile: agents.ProfileName(c.Profile),
		Goal:    c.Goal, Deliverable: c.Deliverable, Scope: scope,
		Paths: c.Paths, Inputs: c.Inputs, Pinned: c.Pinned, Skills: c.Skills,
		Tools: c.Tools, Budget: c.Budget, Done: c.Done, Report: c.Report,
		ForkContext: c.ForkContext,
	}
	if c.Task != "" {
		card.TaskID = types.TaskID(c.Task)
	}
	if c.Deadline != "" {
		d, err := time.Parse(time.RFC3339, c.Deadline)
		if err != nil {
			return DelegationCard{}, fmt.Errorf("subagent: bad deadline %q: want RFC3339", c.Deadline)
		}
		card.Deadline = d
	}
	switch c.Lane {
	case "", "P1":
		card.Lane = LaneP1
	case "P2":
		card.Lane = LaneP2
	case "P3":
		card.Lane = LaneP3
	default:
		return DelegationCard{}, fmt.Errorf("subagent: bad lane %q", c.Lane)
	}
	return card, nil
}

func cardName(c cardJSON) string {
	if c.Name != "" {
		return c.Name
	}
	return c.Profile + " child"
}

// statusView renders one snapshot node as tool output.
func statusView(st *RunStatus) map[string]any {
	kids := make([]any, 0, len(st.Children))
	names := make([]string, 0, len(st.Children))
	for _, c := range st.Children {
		kids = append(kids, statusView(c))
		names = append(names, string(c.AgentID))
	}
	out := map[string]any{
		"run_id": st.RunID, "agent": st.Name, "profile": st.Profile,
		"state": st.State, "reason": st.Reason, "plain": st.Plain(),
		"step": st.Step, "tool": st.Tool, "live": st.LiveTail,
		"last_progress": st.LastProgress, "plan": []int{st.PlanDone, st.PlanTotal},
		"budget_used_pct": st.BudgetUsedPct, "children": kids,
	}
	_ = names
	sort.Strings(names)
	return out
}
