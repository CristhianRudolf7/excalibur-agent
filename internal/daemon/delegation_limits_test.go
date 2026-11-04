package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestDelegationUserLimitsAndBudgets(t *testing.T) {
	for _, tc := range []struct {
		text    string
		seconds int64
		cost    float64
	}{
		{"Build the whole spec", 0, 0}, {"hazlo en 10 minutos", 600, 0}, {"Build within 2 hours", 7200, 0}, {"no gastes más de $3.50", 0, 3.5}, {"Budget $8", 0, 8},
	} {
		seconds, cost := textualDeliveryLimits(tc.text)
		if seconds != tc.seconds || cost != tc.cost {
			t.Fatalf("%s: %d, %f", tc.text, seconds, cost)
		}
		policy := deliveryPolicy{MaxSeconds: seconds, MaxCostUSD: cost}
		raw, err := policy.prepareDelegation(json.RawMessage(`{"action":"spawn","card":{"goal":"build","budget":{"max_steps":4,"max_seconds":120,"max_cost_usd":0.01},"deadline":"2026-11-04T00:00:00Z"}}`))
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Card struct {
				Budget   types.Budget `json:"budget"`
				Deadline string       `json:"deadline"`
			} `json:"card"`
		}
		_ = json.Unmarshal(raw, &got)
		if got.Card.Budget != policy.budget() || got.Card.Deadline != "" || got.Card.Budget.MaxSteps < 10000 {
			t.Fatal(string(raw))
		}
	}
}

func TestDelegationRejectsHurryWithoutUserLimit(t *testing.T) {
	for _, text := range []string{"Finaliza y reporta ahora, sin más cambios", "Stop expanding work and report now", "Finish now", "Wrap up", "The remaining budget is low; rush", "Date prisa", "deja de programar"} {
		for _, action := range []string{"message", "redirect"} {
			args, _ := json.Marshal(map[string]string{"action": action, "text": text})
			if _, err := (deliveryPolicy{}).prepareDelegation(args); err == nil {
				t.Error("hurry accepted", text)
			}
			if _, err := (deliveryPolicy{MaxSeconds: 600}).prepareDelegation(args); err != nil {
				t.Error(err)
			}
		}
	}
	args := json.RawMessage(`{"action":"message","text":"Please verify dashboard totals from the database."}`)
	if _, err := (deliveryPolicy{}).prepareDelegation(args); err != nil {
		t.Fatal(err)
	}
}

func TestChatChildWaitBlocksUntilProgressCompletionOrCancellation(t *testing.T) {
	for _, next := range []string{`{"state":"completed"}`, `{"state":"running","last_progress":"API tests passed"}`, `{"state":"blocked"}`} {
		var advanced atomic.Bool
		var reads atomic.Int32
		done := make(chan tools.Result, 1)
		safego.Go("test-child-wait", nil, func() {
			result, _ := waitChatStatus(context.Background(), time.Second, func() (tools.Result, error) {
				reads.Add(1)
				output := `{"state":"running"}`
				if advanced.Load() {
					output = next
				}
				return tools.Result{Status: tools.StatusOK, Output: output}, nil
			})
			done <- result
		})
		select {
		case <-done:
			t.Fatal("running child was polled immediately")
		case <-time.After(40 * time.Millisecond):
		}
		advanced.Store(true)
		select {
		case result := <-done:
			if result.Output != next {
				t.Fatal(result)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("progress did not wake wait")
		}
		if reads.Load() > 3 {
			t.Fatal("busy polling", reads.Load())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, _ := waitChatStatus(ctx, time.Minute, func() (tools.Result, error) {
		return tools.Result{Status: tools.StatusOK, Output: `{"state":"running"}`}, nil
	})
	if result.Status != tools.StatusCanceled {
		t.Fatal(result)
	}
	result, _ = waitChatStatus(context.Background(), 20*time.Millisecond, func() (tools.Result, error) {
		return tools.Result{Status: tools.StatusOK, Output: `{"state":"running"}`}, nil
	})
	if !strings.Contains(result.Output, "running") {
		t.Fatal(result)
	}
}

func TestChatRunnerEnforcesLimitsAndAwait(t *testing.T) {
	s, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runner := &assistantToolRunner{daemonToolRunner: daemonToolRunner{ex: s.Exec, reg: s.Tools}, allowed: map[string]bool{"subagent": true}, root: s.masterAgentID, policy: &deliveryPolicy{}, waitChildren: true}
	args := json.RawMessage(`{"action":"spawn","card":{"name":"builder","description":"implements the spec","profile":"implementer","goal":"build","deliverable":"full app","scope":"general","done_criteria":["tests pass"],"report_contract":"summary","budget":{"max_steps":3,"max_seconds":120}}}`)
	result, err := runner.Execute(context.Background(), "subagent", args, tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatal(result, err)
	}
	var spawned struct {
		Run   types.RunID   `json:"run_id"`
		Agent types.AgentID `json:"agent_id"`
	}
	_ = json.Unmarshal([]byte(result.Output), &spawned)
	rec, err := s.Store.GetRun(spawned.Run)
	if err != nil || rec.Run.Budget.MaxSteps != defaultDeliverySteps || rec.Run.Budget.MaxSeconds != 0 {
		t.Fatal(rec, err)
	}
	args, _ = json.Marshal(map[string]string{"action": "message", "to": string(spawned.Agent), "text": "Finaliza y reporta ahora"})
	result, err = runner.Execute(context.Background(), "subagent", args, tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusDenied || len(s.Store.PendingMessages(spawned.Agent)) != 0 {
		t.Fatal(result, err)
	}
	args, _ = json.Marshal(map[string]string{"action": "await", "run_id": string(spawned.Run)})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	result, err = runner.Execute(ctx, "subagent", args, tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusCanceled || time.Since(start) < 30*time.Millisecond {
		t.Fatal(result, err)
	}
}

func TestTaskServiceDefaultBudgetAndEvaluationAdmission(t *testing.T) {
	s, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTaskService(s.Orch, s.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"default", "eval"} {
		started, err := service.Start(api.TaskStartParams{Request: "Build the complete spec", Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		id, err := service.RunOf(started.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		rec, err := s.Store.GetRun(id)
		if err != nil {
			t.Fatal(err)
		}
		if profile == "default" && (rec.Run.Budget.MaxSteps != defaultDeliverySteps || rec.Run.Budget.MaxSeconds != 0) {
			t.Fatal(rec)
		}
		if profile == "eval" && rec.Run.Budget.MaxSteps == defaultDeliverySteps {
			t.Fatal("evaluation admission adopted delivery budget")
		}
	}
}
