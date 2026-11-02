package daemon

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type activityState struct {
	mu             sync.Mutex
	runs           map[string]string // successful spawn run -> conversation
	automations    map[string]string // created automation -> conversation
	automationRuns map[string]string // manually launched run -> conversation
	sent           map[string]activitySent
	once           sync.Once
}
type activitySent struct {
	item api.ActivityItem
	at   time.Time
}

// trackActivity records server-observed identities, never names in model text.
func (b *DaemonBackend) trackActivity(conv, kind, id string) {
	if conv == "" || id == "" {
		return
	}
	a := &b.activity
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runs == nil {
		a.runs = map[string]string{}
		a.automations = map[string]string{}
		a.automationRuns = map[string]string{}
	}
	switch kind {
	case "run":
		a.runs[id] = conv
	case "automation":
		a.automations[id] = conv
	case "automation_run":
		a.automationRuns[id] = conv
	}
}

func activityRunState(s types.RunState) string {
	switch s {
	case types.RunCreated, types.RunQueued:
		return "queued"
	case types.RunWaiting, types.RunBlocked:
		return "waiting"
	case types.RunPaused:
		return "paused"
	case types.RunCompleted, types.RunCompletedPending:
		return "completed"
	case types.RunFailed:
		return "failed"
	case types.RunCanceled:
		return "cancelled"
	default:
		return "running"
	}
}
func activityTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// activityItems expands only the descendants of trusted successful spawns.
// The shared master agent is deliberately never used as a tree root.
func (b *DaemonBackend) collectActivity(ctx context.Context, conv string, now time.Time) ([]api.ActivityItem, error) {
	spanish := b.activitySpanish(conv)
	a := &b.activity
	a.mu.Lock()
	seeds, automationIDs, automationRuns := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for id, c := range a.runs {
		if c == conv {
			seeds[id] = true
		}
	}
	for id, c := range a.automations {
		if c == conv {
			automationIDs[id] = true
		}
	}
	for id, c := range a.automationRuns {
		if c == conv {
			automationRuns[id] = true
		}
	}
	a.mu.Unlock()
	out := []api.ActivityItem{}
	if b.Ctrls != nil && b.Ctrls.Store != nil {
		store := b.Ctrls.Store
		agents := map[types.AgentID]types.Agent{}
		owners := map[types.AgentID]string{}
		runs := store.ListRuns()
		for _, agent := range store.ListAgents() {
			agents[agent.ID] = agent
		}
		for _, rec := range runs {
			if seeds[string(rec.Run.ID)] {
				owners[rec.Run.AgentID] = string(rec.Run.ID)
			}
		}
		for changed := true; changed; {
			changed = false
			for _, agent := range agents {
				if agent.ParentID != nil && owners[*agent.ParentID] != "" && owners[agent.ID] == "" {
					owners[agent.ID] = owners[*agent.ParentID]
					changed = true
				}
			}
		}
		taskRows := map[string]map[string]any{}
		if b.Services != nil && b.Services.Tasks != nil {
			rows, err := b.Services.Tasks.List()
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				if id, ok := row["run_id"].(string); ok {
					taskRows[id] = row
				}
			}
		}
		agentRuns := map[types.AgentID]string{}
		for _, rec := range runs {
			if owners[rec.Run.AgentID] != "" {
				id := string(rec.Run.ID)
				if row := taskRows[id]; row != nil {
					if taskID, ok := row["task_id"].(string); ok {
						id = taskID
					}
				}
				agentRuns[rec.Run.AgentID] = id
			}
		}
		for _, rec := range runs {
			r := rec.Run
			if owners[r.AgentID] == "" {
				continue
			}
			agent := agents[r.AgentID]
			item := api.ActivityItem{ID: string(r.ID), Kind: "subagent", Name: agent.Name, Goal: activityGoal(agent.Description), Profile: agent.Profile, State: activityRunState(r.State), StartedAt: activityTime(r.CreatedAt), UpdatedAt: activityTime(r.UpdatedAt)}
			if b.Ctrls.Orch != nil {
				if st, err := b.Ctrls.Orch.Status(r.ID); err == nil {
					status := st.ToAPI()
					if status.PlanTotal > 0 {
						item.Progress = &api.ActivityProgress{Done: status.PlanDone, Total: status.PlanTotal}
					}
				}
			}
			var live orchestrator.RunnerSnapshot
			if b.Ctrls.Orch != nil {
				if runner, ok := b.Ctrls.Orch.RunnerFor(r.ID); ok {
					live = runner.Snapshot()
				}
			}
			item.Step = activityStep(item.State, r.LastProgress, live.ToolName, live.ToolArgs, spanish)
			if row := taskRows[string(r.ID)]; row != nil {
				item.Kind = "task"
				item.ID, _ = row["task_id"].(string)
				item.Name, _ = row["requested"].(string)
				item.Goal = activityGoal(item.Name)
			}
			if agent.ParentID != nil {
				item.ParentID = agentRuns[*agent.ParentID]
			}
			if r.State.IsTerminal() {
				item.FinishedAt = item.UpdatedAt
				if r.State == types.RunFailed {
					item.Error = r.LastProgress
				}
			}
			out = append(out, item)
		}
	}
	if b.Services != nil && b.Services.Automations != nil && (len(automationIDs) > 0 || len(automationRuns) > 0) {
		list, err := b.Services.Automations.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, automation := range list {
			runs, err := b.Services.Automations.Runs(ctx, automation.ID, 100)
			if err != nil {
				return nil, err
			}
			for _, run := range runs {
				if !automationIDs[automation.ID] && !automationRuns[run.ID] {
					continue
				}
				item := automationActivity(automation, run)
				if item.FinishedAt != "" {
					item.Step = activityStep(item.State, item.Error, "", "", spanish)
				}
				out = append(out, item)
			}
		}
	}
	filtered := out[:0]
	for _, item := range out {
		if item.FinishedAt != "" {
			finished, err := time.Parse(time.RFC3339Nano, item.FinishedAt)
			if err != nil || finished.Before(now.Add(-30*time.Minute)) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool {
		x, y := filtered[i], filtered[j]
		if (x.FinishedAt == "") != (y.FinishedAt == "") {
			return x.FinishedAt == ""
		}
		if x.UpdatedAt != y.UpdatedAt {
			tx, _ := time.Parse(time.RFC3339Nano, x.UpdatedAt)
			ty, _ := time.Parse(time.RFC3339Nano, y.UpdatedAt)
			return tx.After(ty)
		}
		return x.ID < y.ID
	})
	return filtered, nil
}
func (b *DaemonBackend) activityItems(ctx context.Context, conv string, now time.Time) ([]api.ActivityItem, error) {
	items, err := b.collectActivity(ctx, conv, now)
	if len(items) > 50 {
		items = items[:50]
	}
	return items, err
}
func activityGoal(s string) string {
	r := []rune(s)
	if len(r) > 300 {
		r = r[:300]
	}
	return string(r)
}
func automationActivity(a automations.Automation, r automations.Run) api.ActivityItem {
	item := api.ActivityItem{ID: r.ID, Kind: "automation", Name: a.Name, Goal: activityGoal(a.Description), State: string(r.Status), StartedAt: activityTime(r.StartedAt), UpdatedAt: activityTime(r.StartedAt), Error: r.Error, Progress: &api.ActivityProgress{Total: len(r.Steps)}}
	if r.Status == automations.RunSucceeded {
		item.State = "completed"
	}
	for _, step := range r.Steps {
		if step.Status == automations.StepSucceeded || step.Status == automations.StepSkipped {
			item.Progress.Done++
		}
		if step.Status == automations.StepRunning {
			item.Step = step.StepID
			for _, def := range a.Steps {
				if def.ID == step.StepID {
					item.Step = def.Title
				}
			}
		}
		if step.StartedAt != nil && step.StartedAt.After(r.StartedAt) {
			item.UpdatedAt = activityTime(*step.StartedAt)
		}
		if step.FinishedAt != nil {
			item.UpdatedAt = activityTime(*step.FinishedAt)
		}
	}
	if r.FinishedAt != nil {
		item.FinishedAt = activityTime(*r.FinishedAt)
		item.UpdatedAt = item.FinishedAt
	}
	return item
}
func (b *DaemonBackend) activityList(ctx context.Context, raw json.RawMessage) (any, error) {
	var p api.ActivityListParams
	if err := strictLibraryParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ConversationID) == "" {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "conversation_id is required"}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := b.activityItems(ctx, p.ConversationID, time.Now())
	return api.ActivityListResult{Items: items}, err
}

// publishActivity coalesces rapid changes, retaining the latest snapshot until
// its per-item 500 ms window expires. A terminal update is never dropped.
func (b *DaemonBackend) publishActivity(item api.ActivityItem, now time.Time) {
	if b.Bus == nil {
		return
	}
	a := &b.activity
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sent == nil {
		a.sent = map[string]activitySent{}
	}
	prev := a.sent[item.ID]
	if reflect.DeepEqual(prev.item, item) || (!prev.at.IsZero() && now.Sub(prev.at) < 500*time.Millisecond) {
		return
	}
	_, err := b.Bus.Publish(api.EventActivityUpdated, nil, nil, api.ActivityUpdatedEvent{Item: item})
	if err == nil {
		a.sent[item.ID] = activitySent{item, now}
	}
}

// StartActivity observes live service snapshots, including engine step/plan
// changes that have no orchestrator state event. It stops with the daemon.
func (b *DaemonBackend) StartActivity(ctx context.Context) {
	b.activity.once.Do(func() {
		safego.Go("daemon-activity", nil, func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case now := <-ticker.C:
					conversations := map[string]bool{}
					a := &b.activity
					a.mu.Lock()
					for _, c := range a.runs {
						conversations[c] = true
					}
					for _, c := range a.automations {
						conversations[c] = true
					}
					for _, c := range a.automationRuns {
						conversations[c] = true
					}
					a.mu.Unlock()
					for conv := range conversations {
						items, err := b.collectActivity(ctx, conv, now)
						if err != nil {
							continue
						}
						for _, item := range items {
							b.publishActivity(item, now)
						}
					}
				}
			}
		})
	})
}
