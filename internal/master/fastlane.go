package master

import (
	"context"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// StatusResolver serves deterministic snapshots for the fast lane
// (Section 15.6). The daemon wires the orchestrator; tests use a fake.
type StatusResolver interface {
	Snapshot(runID string) (api.RunStatusResult, error)
	ListRuns() []api.RunStatusResult
	FindRunsByAgentName(name string) []api.RunStatusResult
}

// ControlRunner executes fast-lane state and control orders without a
// model (Section 6.1 table). Orders serialize in arrival order in the
// orchestrator actor.
type ControlRunner interface {
	StatusResolver
	Pause(runID string) (api.RunStatusResult, error)
	Resume(runID string) (api.RunStatusResult, error)
	Cancel(runID string) (api.RunStatusResult, error)
	Interrupt(runID string, toolOnly bool) (api.RunStatusResult, error)
	Redirect(runID, text string, interrupt bool) (api.RunStatusResult, error)
	PauseAll() []api.RunStatusResult
	AnswerQuestion(questionID, answer string) error
	ApprovePlan(planID string) error
	DenyPlan(planID, reason string) error
	SetScope(conversationID, scope string) error
}

// FastLane is the deterministic Section 6.1 lane: slash commands and
// resolved state/control queries answer in milliseconds with no model
// call. Without a decider it serves slash commands only; with one it
// also serves natural-language queries whose target resolves
// unambiguously with high confidence.
type FastLane struct {
	Controls ControlRunner
	// HasDecider reports whether natural-language intake ran through
	// the decider. Without it only slash commands fast-path.
	HasDecider bool
}

// Outcome is one fast-lane answer.
type Outcome struct {
	// Handled true means the message is fully answered: do not open a
	// master turn.
	Handled bool
	// Reply is the user-visible deterministic answer.
	Reply string
	// Rest is the leftover for the master when a message mixes
	// control and content ("pause X and change R4").
	Rest string
	// Snapshot served, when any.
	Snapshot *api.RunStatusResult
}

// Handle applies the Section 6.1 table to one intake result.
func (f *FastLane) Handle(ctx context.Context, in IntakeResult) Outcome {
	_ = ctx
	text := strings.TrimSpace(in.Msg.Text)
	if in.Pre.IsCommand {
		return f.slash(in.Pre.Command, in.Pre.CommandArgs, in)
	}
	// Natural language: without a decider everything goes to the
	// master; with one, only high-confidence resolved targets.
	if !f.HasDecider || !in.Decided {
		return Outcome{}
	}
	if in.Triage.Confidence < 0.7 {
		return Outcome{}
	}
	switch in.Triage.Type {
	case TriageStatusQuery, TriageQuickAction:
		intent := in.Pre.ControlIntent
		if intent == "" {
			intent = "status"
		}
		return f.control(intent, in.Pre.ControlTarget, in, text)
	default:
		return Outcome{}
	}
}

func (f *FastLane) slash(cmd, args string, in IntakeResult) Outcome {
	lower := strings.ToLower(cmd)
	_ = lower
	switch strings.ToLower(cmd) {
	case "status":
		return f.control("status", args, in, "")
	case "pause":
		return f.control("pause", args, in, "")
	case "resume":
		return f.control("resume", args, in, "")
	case "cancel":
		return f.control("cancel", args, in, "")
	case "stop":
		if strings.TrimSpace(args) == "all" || strings.ToLower(strings.TrimSpace(args)) == "todo" {
			rows := f.Controls.PauseAll()
			return Outcome{Handled: true, Reply: fmt.Sprintf("Paused %d runs.", len(rows))}
		}
		return Outcome{Handled: true, Reply: "Interrupted the master turn in this conversation; background work continues."}
	case "approve":
		if args == "" {
			return Outcome{Handled: true, Reply: "Which plan? Say /approve <plan>."}
		}
		if err := f.Controls.ApprovePlan(firstWord(args)); err != nil {
			return Outcome{Handled: true, Reply: "Could not approve: " + err.Error()}
		}
		return Outcome{Handled: true, Reply: "Approved " + firstWord(args) + "."}
	case "deny":
		if args == "" {
			return Outcome{Handled: true, Reply: "Which plan? Say /deny <plan> <reason>."}
		}
		if err := f.Controls.DenyPlan(firstWord(args), strings.TrimSpace(strings.TrimPrefix(args, firstWord(args)))); err != nil {
			return Outcome{Handled: true, Reply: "Could not reject: " + err.Error()}
		}
		return Outcome{Handled: true, Reply: "Rejected " + firstWord(args) + "."}
	case "project":
		if args == "" {
			return Outcome{Handled: true, Reply: "Which project? Say /project <name>."}
		}
		if err := f.Controls.SetScope(string(in.MsgID), args); err != nil {
			return Outcome{Handled: true, Reply: "Could not switch scope: " + err.Error()}
		}
		return Outcome{Handled: true, Reply: "Active scope is now " + args + "."}
	default:
		return Outcome{}
	}
}

// control resolves the target and runs the order. Ambiguous targets
// produce a candidate question; mixed messages execute the control
// and return the rest to the master (Section 6.1).
func (f *FastLane) control(intent, target string, in IntakeResult, raw string) Outcome {
	if intent == "stop" {
		return Outcome{Handled: true, Reply: "Interrupted the master turn in this conversation; background work continues."}
	}
	if intent == "stop_all" {
		rows := f.Controls.PauseAll()
		return Outcome{Handled: true, Reply: fmt.Sprintf("Paused %d runs.", len(rows))}
	}
	if intent == "approve" || intent == "deny" {
		// Answering a pending question or plan approval.
		id := firstWord(target)
		if id == "" && len(in.Pre.TaskRefs) > 0 {
			id = in.Pre.TaskRefs[0]
		}
		if id == "" {
			return Outcome{Handled: true, Reply: "Which one? Tell me the id."}
		}
		if intent == "approve" {
			if err := f.Controls.ApprovePlan(id); err != nil {
				return Outcome{Handled: true, Reply: "Could not approve: " + err.Error()}
			}
			return Outcome{Handled: true, Reply: "Approved " + id + "."}
		}
		if err := f.Controls.DenyPlan(id, ""); err != nil {
			return Outcome{Handled: true, Reply: "Could not reject: " + err.Error()}
		}
		return Outcome{Handled: true, Reply: "Rejected " + id + "."}
	}
	runID, cands, rest := f.resolve(target, in)
	if runID == "" {
		if len(cands) > 1 {
			names := candidateNames(cands)
			return Outcome{Handled: true, Reply: "Which one? " + strings.Join(names, " or ") + "?"}
		}
		if len(cands) == 0 {
			// No target at all: list everything for status, else ask.
			if intent == "status" && looksLikeStatusQuery(raw) {
				rows := f.Controls.ListRuns()
				if len(rows) == 0 {
					return Outcome{Handled: true, Reply: "Nothing is running."}
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d active runs:", len(rows))
				for _, r := range rows {
					fmt.Fprintf(&b, "\n- %s is %s", r.RunID, r.State)
				}
				return Outcome{Handled: true, Reply: b.String()}
			}
			return Outcome{}
		}
	}
	var snap api.RunStatusResult
	var err error
	switch intent {
	case "status":
		snap, err = f.Controls.Snapshot(runID)
	case "pause":
		snap, err = f.Controls.Pause(runID)
	case "resume":
		snap, err = f.Controls.Resume(runID)
	case "cancel":
		snap, err = f.Controls.Cancel(runID)
	default:
		return Outcome{}
	}
	if err != nil {
		return Outcome{Handled: true, Reply: "Could not run that order: " + err.Error()}
	}
	reply := plainSnapshot(snap, intent)
	out := Outcome{Handled: true, Reply: reply, Snapshot: &snap}
	// Mixed control + content: the control ran here, the rest goes
	// to the master in the same turn.
	if rest = strings.TrimSpace(rest); rest != "" && intent != "status" {
		out.Rest = rest
		out.Reply += " Continuing with the rest of your message."
	}
	_ = raw
	return out
}

// resolve maps a raw target phrase to one run id. It checks explicit
// run refs first, then agent-name mentions (with ambiguity), then a
// bare id. The second return lists ambiguous candidates; rest is the
// message remainder after the control clause for mixed messages.
func (f *FastLane) resolve(target string, in IntakeResult) (runID string, cands []api.RunStatusResult, rest string) {
	target = strings.TrimSpace(target)
	if target == "" && len(in.Pre.RunRefs) == 0 && len(in.Pre.Mentions) == 0 {
		return "", nil, ""
	}
	// Split mixed messages at " and " / " y ": control first.
	if idx := strings.Index(strings.ToLower(target), " and "); idx > 0 {
		rest = strings.TrimSpace(target[idx+5:])
		target = strings.TrimSpace(target[:idx])
	}
	if idx := strings.Index(strings.ToLower(target), " y "); idx > 0 {
		rest = strings.TrimSpace(target[idx+3:])
		target = strings.TrimSpace(target[:idx])
	}
	// Explicit run id wins.
	fields := strings.Fields(target)
	for _, fld := range fields {
		if strings.HasPrefix(fld, "run_") {
			if _, err := f.Controls.Snapshot(fld); err == nil {
				return fld, nil, rest
			}
		}
	}
	// Agent-name mentions from the lexicon.
	for _, m := range in.Pre.Mentions {
		if m.Kind != MentionAgent {
			continue
		}
		var matched []api.RunStatusResult
		for _, c := range m.Candidates {
			matched = append(matched, f.Controls.FindRunsByAgentName(c)...)
		}
		if len(matched) == 1 {
			return matched[0].RunID, nil, rest
		}
		if len(matched) > 1 {
			return "", matched, rest
		}
	}
	// Bare agent-name target from the raw phrase.
	if target != "" {
		matched := f.Controls.FindRunsByAgentName(target)
		if len(matched) == 1 {
			return matched[0].RunID, nil, rest
		}
		if len(matched) > 1 {
			return "", matched, rest
		}
		// Maybe the target is a run id itself.
		if snap, err := f.Controls.Snapshot(target); err == nil {
			return snap.RunID, nil, rest
		}
	}
	return "", nil, rest
}

func candidateNames(cands []api.RunStatusResult) []string {
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		if c.ToolInFlight != "" {
			names = append(names, fmt.Sprintf("%s (%s)", c.RunID, c.ToolInFlight))
		} else {
			names = append(names, c.RunID)
		}
	}
	return names
}

// plainSnapshot renders the Section 15.6 template without a model.
func plainSnapshot(s api.RunStatusResult, intent string) string {
	who := s.RunID
	if s.ToolInFlight != "" {
		who += " (" + s.ToolInFlight + ")"
	}
	switch intent {
	case "pause":
		return fmt.Sprintf("Paused %s.", who)
	case "resume":
		return fmt.Sprintf("Resumed %s.", who)
	case "cancel":
		return fmt.Sprintf("Canceled %s.", who)
	default:
		line := fmt.Sprintf("%s is %s", who, s.State)
		if s.Reason != "" {
			line += " (" + s.Reason + ")"
		}
		if s.ToolInFlight != "" {
			line += "; runs " + s.ToolInFlight
			if s.LiveOutput != "" {
				line += "; last line: " + lastLineOf(s.LiveOutput)
			}
		}
		if s.PlanTotal > 0 {
			line += fmt.Sprintf("; %d of %d plan steps", s.PlanDone, s.PlanTotal)
		}
		return line + "."
	}
}

func lastLineOf(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// statusWords guard untargeted status replies: the triage model sometimes
// labels short confirmations ("Sí, créala") as status, which must reach the
// master instead of being answered with the run list.
var statusWords = []string{"estado", "status", "cómo va", "como va", "cómo vas", "como vas", "qué está", "que esta", "corriendo", "ejecutando", "running", "progress", "progreso", "avance", "qué hay", "pendiente", "what's going", "how is it going", "how's it going"}

func looksLikeStatusQuery(raw string) bool {
	low := strings.ToLower(raw)
	for _, w := range statusWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}
