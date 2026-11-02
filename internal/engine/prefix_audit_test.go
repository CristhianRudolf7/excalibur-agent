package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Audit full provider messages, including nested tool arguments and results.
// L5 is explicitly volatile; legitimate masking batches are the only permitted
// prefix breaks in the second mode. Compaction stays disabled in both modes.
func TestExecutorSerializedPrefixAudit(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("mask_batches_%v", batch), func(t *testing.T) {
			cfg := ctxcompiler.DefaultConfig()
			cfg.MaskTriggerTokens, cfg.MaskTargetTokens, cfg.LiteralObservations = 1000000, 500000, 2
			if batch {
				cfg.MaskTriggerTokens, cfg.MaskTargetTokens = 1800, 900
			}
			no := false
			run := testRun(Profile{})
			b := &DefaultBuilder{Config: cfg, EnableCompact: &no, Snapshot: func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
				return ctxcompiler.Snapshot{Role: "child", Task: ctxcompiler.TaskView{OriginalRequest: "Compile the project and inspect diagnostics"}, Volatile: ctxcompiler.Volatile{Now: time.Now(), BudgetState: fmt.Sprintf("step %d", run.Steps)}}, nil
			}}
			model := &fakeEndpoint{}
			var previous []providers.Message
			changed := map[string]int{}
			transitions := 0
			for turn := 0; turn < 65; turn++ {
				id := fmt.Sprintf("call-%d", turn)
				body := strings.Repeat("building compilation unit\n", 50)
				call := providers.ToolCall{ID: id, Name: "exec", Arguments: json.RawMessage(`{"command":"make check"}`)}
				run.History = append(run.History, ctxcompiler.ActivityEvent{Seq: run.nextHistorySeq(), Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{call}})
				run.History = append(run.History, historyEventOf(run.nextHistorySeq(), call, tools.Result{Status: tools.StatusOK, Output: body}))
				oldBatches := run.MaskBatches
				req, _, err := b.BuildRequest(context.Background(), run, false)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = model.Chat(context.Background(), types.RoleExecutor, req); err != nil {
					t.Fatal(err)
				}
				stable := req.Messages[:len(req.Messages)-1]
				if previous != nil {
					before, _ := json.Marshal(previous)
					after, _ := json.Marshal(stable[:len(previous)])
					if !bytes.Equal(before, after) {
						if !batch || run.MaskBatches == oldBatches {
							t.Fatalf("turn %d rewrote serialized prefix outside a masking batch", turn)
						}
						for i, old := range previous {
							a, _ := json.Marshal(old)
							z, _ := json.Marshal(stable[i])
							if bytes.Equal(a, z) {
								continue
							}
							result := stable[i].ToolResult
							if result == nil || old.ToolResult == nil || !strings.HasPrefix(result.Output, "[observation ") {
								t.Fatalf("turn %d changed non-observation message %d", turn, i)
							}
							changed[result.CallID]++
							if changed[result.CallID] > 1 {
								t.Fatalf("%s changed repeatedly", result.CallID)
							}
							transitions++
						}
					}
				}
				raw, _ := json.Marshal(stable)
				if err := json.Unmarshal(raw, &previous); err != nil {
					t.Fatal(err)
				}
				run.Steps++
			}
			if model.calls() != 65 {
				t.Fatal("missing executor request")
			}
			if batch && transitions == 0 {
				t.Fatal("audit did not exercise masking transitions")
			}
		})
	}
}

// Drive the actual executor step path too: startup hooks must not append a
// transient message after L5 or drop it from subsequent provider requests.
func TestExecutorStepStartupGuidanceKeepsSerializedPrefix(t *testing.T) {
	model := &fakeEndpoint{}
	runner := scriptedRunner()
	for turn := 0; turn < 5; turn++ {
		id := fmt.Sprintf("inspect-%d", turn)
		runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("inspection %d\n", turn)})
		model.responses = append(model.responses, toolResp(providers.ToolCall{ID: id, Name: "exec", Arguments: json.RawMessage(fmt.Sprintf(`{"command":"printf '%d'"}`, turn))}))
	}
	e, store := testEngine(model, runner, passingGate())
	run := testRun(Profile{Limit: tasks.Limit{MaxSteps: 12}})
	run.Workdir = t.TempDir()
	run.Request = "Inspect the project and check its outputs"
	cfg := ctxcompiler.DefaultConfig()
	cfg.MaskTriggerTokens = 1000000
	no := false
	e.deps.Context = &DefaultBuilder{Config: cfg, EnableCompact: &no, Snapshot: func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{Role: "child", Task: ctxcompiler.TaskView{OriginalRequest: run.Request}, Volatile: ctxcompiler.Volatile{Now: time.Now()}}, nil
	}}
	startRun(t, e, run, store)
	for turn := 0; turn < 5; turn++ {
		stop, err := e.step(context.Background(), run)
		if err != nil || stop {
			t.Fatalf("turn %d stop=%v err=%v", turn, stop, err)
		}
	}
	if !run.scopeGuardStarted {
		t.Fatal("startup hook was not exercised")
	}
	for turn := 1; turn < len(model.requests); turn++ {
		previous := model.requests[turn-1].Messages
		next := model.requests[turn].Messages
		if previous[len(previous)-1].Role != providers.RoleSystem {
			t.Fatal("volatile message must remain last")
		}
		old, _ := json.Marshal(previous[:len(previous)-1])
		current, _ := json.Marshal(next[:len(previous)-1])
		if !bytes.Equal(old, current) {
			t.Fatalf("executor startup changed prefix at turn %d", turn)
		}
	}
}
