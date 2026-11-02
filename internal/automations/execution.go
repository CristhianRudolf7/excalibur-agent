package automations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/google/uuid"
)

func (m *Manager) RunNow(ctx context.Context, id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Run{}, err
	}
	return m.runLocked(id, TriggerManual)
}
func (m *Manager) runLocked(id string, trigger RunTrigger) (Run, error) {
	r := m.records[id]
	if r == nil {
		return Run{}, ErrNotFound
	}
	if _, ok := m.running[id]; ok {
		return Run{}, ErrAlreadyRunning
	}
	if m.stopped || m.ctx.Err() != nil {
		return Run{}, fmt.Errorf("automation manager stopped")
	}
	r = copyRecord(r)
	a := clone(r.Automation)
	now := m.now()
	run := Run{ID: uuid.NewString(), AutomationID: id, Version: a.Version, Trigger: trigger, Status: RunRunning, StartedAt: now, Steps: []StepRun{}}
	for _, s := range a.Steps {
		run.Steps = append(run.Steps, StepRun{StepID: s.ID, Status: StepPending})
	}
	r.Runs = append([]Run{clone(run)}, r.Runs...)
	if len(r.Runs) > 200 {
		r.Runs = r.Runs[:200]
	}
	r.Automation.LastRun = &run
	// Consume the due slot before starting work so a restart cannot replay it.
	// A manual run also consumes a due once schedule.
	if a.Schedule.Kind == ScheduleOnce {
		r.Automation.NextRunAt = nil
	} else if trigger != TriggerManual {
		next, err := NextRun(a.Schedule, now)
		if err != nil {
			return Run{}, err
		}
		r.Automation.NextRunAt = next
	}
	if err := m.persistLocked(m.ctx, r); err != nil {
		return Run{}, err
	}
	ctx, cancel := context.WithTimeout(m.ctx, m.runDuration)
	m.running[id] = activeRun{id: run.ID, cancel: cancel}
	m.emitLocked(Event{Kind: EventRun, Run: &run})
	m.wg.Add(1)
	safego.Go("automation-run-"+id, nil, func() { defer m.wg.Done(); defer cancel(); m.execute(ctx, a, clone(run)) })
	return clone(run), nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

var placeholder = regexp.MustCompile(`\{\{\s*steps\.([^{}.\s]+)\.output((?:\.[A-Za-z0-9_-]+)*)\s*\}\}`)

// render replaces {{steps.<id>.output}} and, for JSON outputs,
// {{steps.<id>.output.<field>...}} with the referenced value.
func render(s string, prior map[string]string) string {
	return placeholder.ReplaceAllStringFunc(s, func(match string) string {
		m := placeholder.FindStringSubmatch(match)
		out, ok := prior[m[1]]
		if !ok || m[2] == "" {
			return out
		}
		var v any
		if json.Unmarshal([]byte(out), &v) != nil {
			return match
		}
		for _, key := range strings.Split(strings.TrimPrefix(m[2], "."), ".") {
			switch x := v.(type) {
			case map[string]any:
				var found bool
				if v, found = x[key]; !found {
					return match
				}
			case []any:
				i, err := strconv.Atoi(key)
				if err != nil || i < 0 || i >= len(x) {
					return match
				}
				v = x[i]
			default:
				return match
			}
		}
		return scalarText(v)
	})
}

func scalarText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
func renderValue(v any, prior map[string]string) any {
	switch x := v.(type) {
	case string:
		return render(x, prior)
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			out[k] = renderValue(v, prior)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = renderValue(v, prior)
		}
		return out
	default:
		return v
	}
}
func conditionData(prior map[string]string) map[string]any {
	steps := map[string]any{}
	for id, output := range prior {
		// JSON outputs are exposed parsed as output (so output.sum works) and
		// raw as text; plain outputs are strings in both fields.
		v := map[string]any{"output": output, "text": output, "ok": true}
		var parsed any
		if json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed) == nil {
			v["json"] = parsed
			if _, isString := parsed.(string); !isString {
				v["output"] = parsed
			}
		}
		steps[id] = v
	}
	return map[string]any{"steps": steps}
}

var conditionRef = regexp.MustCompile(`steps(?:\.[A-Za-z0-9_-]+)+`)

// checkConditionRefs rejects references that do not resolve, because the
// evaluator treats a missing value as truthy in comparisons.
func checkConditionRefs(expr string, data map[string]any) error {
	for _, ref := range conditionRef.FindAllString(expr, -1) {
		var v any = data
		for _, key := range strings.Split(ref, ".") {
			switch x := v.(type) {
			case map[string]any:
				next, ok := x[key]
				if !ok {
					return fmt.Errorf("condition references %s, which does not exist", ref)
				}
				v = next
			case []any:
				i, err := strconv.Atoi(key)
				if err != nil || i < 0 || i >= len(x) {
					return fmt.Errorf("condition references %s, which does not exist", ref)
				}
				v = x[i]
			default:
				return fmt.Errorf("condition references %s, which does not exist", ref)
			}
		}
	}
	return nil
}

func (m *Manager) saveRun(run Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.records[run.AutomationID]; r != nil {
		r = copyRecord(r)
		for i := range r.Runs {
			if r.Runs[i].ID == run.ID {
				r.Runs[i] = clone(run)
			}
		}
		latest := clone(run)
		r.Automation.LastRun = &latest
		if err := m.persistLocked(context.Background(), r); err != nil {
			return err
		}
	}
	m.emitLocked(Event{Kind: EventRun, Run: &run})
	return nil
}
func (m *Manager) execute(ctx context.Context, a Automation, run Run) {
	prior := map[string]string{}
	summary := ""
	lastNotify := ""
	lastType := StepType("")
	var failure error
	defer func() {
		if p := recover(); p != nil {
			failure = fmt.Errorf("step panicked: %v", p)
		}
		if err := ctx.Err(); err != nil {
			failure = err
		}
		status := RunFailed
		if failure == nil {
			status = RunSucceeded
		} else if errors.Is(failure, context.Canceled) {
			status = RunCancelled
		} else {
			status = RunFailed
		}
		if failure != nil {
			run.Error = failure.Error()
		}
		now := m.now()
		count := 0
		for i := range run.Steps {
			s := &run.Steps[i]
			if s.Status == StepSucceeded {
				count++
			}
			if s.Status == StepRunning {
				s.Status = StepFailed
				s.Error = run.Error
				s.FinishedAt = &now
				if err := m.saveRun(run); err != nil {
					slog.Error("persist automation step", "error", err)
				}
			}
			if s.Status == StepPending {
				s.Status = StepSkipped
				s.FinishedAt = &now
				if err := m.saveRun(run); err != nil {
					slog.Error("persist automation step", "error", err)
				}
			}
		}
		if summary == "" {
			summary = fmt.Sprintf("%d/%d steps succeeded", count, len(run.Steps))
		}
		run.Status = status
		run.FinishedAt = &now
		run.Summary = truncate(summary, 300)
		m.finish(a, run, lastType, lastNotify)
	}()
	index := map[string]int{}
	outgoing := map[string][]Edge{}
	for i, s := range a.Steps {
		index[s.ID] = i
	}
	for _, e := range a.Edges {
		outgoing[e.From] = append(outgoing[e.From], e)
	}
	id := a.Steps[0].ID
	for id != "" {
		if err := ctx.Err(); err != nil {
			failure = err
			return
		}
		i := index[id]
		step := clone(a.Steps[i])
		started := m.now()
		run.Steps[i].Status = StepRunning
		run.Steps[i].StartedAt = &started
		if err := m.saveRun(run); err != nil {
			failure = err
			return
		}
		step.Instruction = render(step.Instruction, prior)
		step.Code = render(step.Code, prior)
		step.Message = render(step.Message, prior)
		if step.Args != nil {
			step.Args = renderValue(step.Args, prior).(map[string]any)
		}
		var output string
		var err error
		branch := ""
		switch step.Type {
		case StepAgent:
			if m.cfg.AgentRunner == nil {
				err = fmt.Errorf("agent runner is not configured")
			} else {
				output, err = m.cfg.AgentRunner.RunAgentStep(ctx, clone(a), step, clone(prior))
			}
		case StepTool:
			if m.cfg.ToolInvoker == nil {
				err = fmt.Errorf("tool invoker is not configured")
			} else {
				output, err = m.cfg.ToolInvoker.InvokeTool(ctx, step.Tool, step.Args)
			}
		case StepScript:
			output, err = m.runScript(ctx, a.ID, step, prior)
		case StepCondition:
			var yes bool
			data := conditionData(prior)
			if err = checkConditionRefs(step.Expression, data); err == nil {
				yes, err = agenda.EvalCondition(step.Expression, data)
			}
			branch = "no"
			if yes {
				branch = "yes"
			}
			output = fmt.Sprint(yes)
		case StepNotify:
			output = strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", `\t`, "\t").Replace(step.Message)
			if m.cfg.Notifier == nil {
				err = fmt.Errorf("notifier is not configured")
			} else {
				err = m.cfg.Notifier.Notify(ctx, output)
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		finished := m.now()
		run.Steps[i].FinishedAt = &finished
		run.Steps[i].OutputPreview = truncate(output, 2000)
		if err != nil {
			run.Steps[i].Status = StepFailed
			run.Steps[i].Error = err.Error()
			failure = err
		} else {
			run.Steps[i].Status = StepSucceeded
			prior[id] = output
			lastType = step.Type
			if step.Type == StepAgent || step.Type == StepNotify {
				summary = output
			}
			if step.Type == StepNotify {
				lastNotify = output
			}
		}
		if err := m.saveRun(run); err != nil {
			failure = err
			return
		}
		if failure != nil {
			return
		}
		next := ""
		for _, e := range outgoing[id] {
			if step.Type != StepCondition || e.Label == branch {
				next = e.To
			}
		}
		if step.Type == StepCondition {
			reachable := map[string]bool{}
			var visit func(string)
			visit = func(id string) {
				if id == "" || reachable[id] {
					return
				}
				reachable[id] = true
				for _, e := range outgoing[id] {
					visit(e.To)
				}
			}
			visit(next)
			for j := range run.Steps {
				if run.Steps[j].Status == StepPending && !reachable[run.Steps[j].StepID] {
					run.Steps[j].Status = StepSkipped
					t := m.now()
					run.Steps[j].FinishedAt = &t
					if err := m.saveRun(run); err != nil {
						failure = err
						return
					}
				}
			}
		}
		id = next
	}
}
func (m *Manager) finish(a Automation, run Run, lastType StepType, lastNotify string) {
	m.mu.Lock()
	if r := m.records[a.ID]; r != nil {
		r = copyRecord(r)
		for i := range r.Runs {
			if r.Runs[i].ID == run.ID {
				r.Runs[i] = clone(run)
			}
		}
		latest := clone(run)
		r.Automation.LastRun = &latest
		r.Automation.UpdatedAt = m.now()
		switch run.Status {
		case RunSucceeded:
			r.Automation.ConsecutiveFailures = 0
		case RunFailed:
			r.Automation.ConsecutiveFailures++
			if r.Automation.ConsecutiveFailures >= MaxConsecutiveFailures {
				r.Automation.Status = StatusPaused
				r.Automation.PausedReason = "3 consecutive failures"
				r.Automation.NextRunAt = nil
			}
		}
		if err := m.persistLocked(context.Background(), r); err != nil {
			slog.Error("persist finished automation", "error", err)
		}
		m.emitLocked(Event{Kind: EventRun, Run: &run})
		m.updatedLocked(r.Automation)
	} else {
		m.emitLocked(Event{Kind: EventRun, Run: &run})
	}
	delete(m.running, a.ID)
	// Completion notifications are independent of run cancellation.
	m.mu.Unlock()
	text := fmt.Sprintf("✅ Automatización «%s»: %s", a.Name, run.Summary)
	if run.Status != RunSucceeded {
		text = fmt.Sprintf("⚠️ Automatización «%s» falló: %s", a.Name, run.Error)
	}
	if m.cfg.Language == "en" {
		text = fmt.Sprintf("✅ Automation «%s»: %s", a.Name, run.Summary)
		if run.Status != RunSucceeded {
			text = fmt.Sprintf("⚠️ Automation «%s» failed: %s", a.Name, run.Error)
		}
	}
	// Automations with their own notify steps decide what reaches the chat;
	// the generic message is then only posted when the run fails.
	hasNotify := false
	for _, st := range a.Steps {
		hasNotify = hasNotify || st.Type == StepNotify
	}
	if m.cfg.Notifier != nil && !(run.Status == RunSucceeded && (lastType == StepNotify || hasNotify)) {
		// Cancellation still posts a completion message; the notification has its
		// own bounded context because the run context may already be cancelled.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := m.cfg.Notifier.Notify(ctx, text); err != nil {
			slog.Error("notify automation completion", "error", err)
		}
		cancel()
	}
	signal(m.wake)
}
