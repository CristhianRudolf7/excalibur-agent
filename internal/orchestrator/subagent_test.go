package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// The Section 13.2 subagent tool in embedded mode: every action,
// authority boundaries (master anywhere, parent in its subtree,
// level 2 denied), and idempotency-key dedupe.

func toolCall(t *testing.T, tool *orchestrator.SubagentTool, args map[string]any, key string) tools.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Invoke(context.Background(), tools.Invocation{Tool: "subagent", Arguments: raw, IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSubagentToolDefinition(t *testing.T) {
	tool := &orchestrator.SubagentTool{}
	def := tool.Definition()
	if err := def.Validate(); err != nil {
		t.Fatalf("subagent definition invalid: %v", err)
	}
	if def.Name != "subagent" {
		t.Fatalf("bad tool name %q", def.Name)
	}
	if keys, err := tool.ResourceKeys(json.RawMessage(`{"action":"status","caller":"x","run":"run_1"}`)); err != nil || len(keys) != 1 {
		t.Fatalf("resource keys must serialize per run: %v %v", keys, err)
	}
}

func TestSubagentSpawnStatusMessage(t *testing.T) {
	f := newFixture(t)
	tool := &orchestrator.SubagentTool{Orch: f.o}
	master, parent := string(f.master.ID), string(f.parent.ID)
	// Spawn through the tool.
	res := toolCall(t, tool, map[string]any{
		"action": "spawn", "caller": parent,
		"card": map[string]any{
			"name": "lena", "description": "implements X", "profile": "implementer",
			"goal": "g", "deliverable": "d", "scope": "project:p1",
			"done_criteria": []string{"done"}, "report_contract": "summary",
		},
	}, "")
	if res.Status != tools.StatusOK {
		t.Fatalf("spawn failed: %s", res.Output)
	}
	var spawned struct {
		AgentID string `json:"agent_id"`
		RunID   string `json:"run_id"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal([]byte(res.Output), &spawned); err != nil {
		t.Fatal(err)
	}
	// Status through the tool renders the snapshot.
	res = toolCall(t, tool, map[string]any{"action": "status", "caller": parent, "run": spawned.RunID}, "")
	if res.Status != tools.StatusOK {
		t.Fatalf("status failed: %s", res.Output)
	}
	// Message delivery parent -> child.
	res = toolCall(t, tool, map[string]any{
		"action": "message", "caller": parent, "to": spawned.AgentID,
		"message_type": "report", "text": "halfway",
	}, "")
	if res.Status != tools.StatusOK {
		t.Fatalf("message failed: %s", res.Output)
	}
	_ = master
}

func TestSubagentAuthority(t *testing.T) {
	f := newFixture(t)
	tool := &orchestrator.SubagentTool{Orch: f.o}
	// A second tree the parent must not touch.
	p2, p2rec := f.spawnOk(f.master.ID, childCard("p2", agents.ProfileCoordinator))
	_, kid := f.spawnOk(p2.ID, childCard("k", agents.ProfileImplementer))
	_ = p2rec
	// Parent acting outside its subtree is denied.
	res := toolCall(t, tool, map[string]any{"action": "pause", "caller": string(f.parent.ID), "run": string(kid.Run.ID)}, "")
	if res.Status != tools.StatusDenied {
		t.Fatalf("parent outside subtree must be denied: %s", res.Output)
	}
	// The master may act on any execution.
	res = toolCall(t, tool, map[string]any{"action": "pause", "caller": string(f.master.ID), "run": string(kid.Run.ID)}, "")
	if res.Status != tools.StatusOK {
		t.Fatalf("master may act anywhere: %s", res.Output)
	}
	// Level-2 callers hold no subagent tool at all.
	lenaAgent, _ := f.spawnOk(f.parent.ID, childCard("lena", agents.ProfileImplementer))
	res = toolCall(t, tool, map[string]any{"action": "status", "caller": string(lenaAgent.ID), "run": string(f.prun.Run.ID)}, "")
	if res.Status != tools.StatusDenied {
		t.Fatalf("level-2 callers must be denied: %s", res.Output)
	}
	// Unknown callers are denied, not executed.
	res = toolCall(t, tool, map[string]any{"action": "cancel", "caller": "agt_00000000000000000000000000", "run": string(f.prun.Run.ID)}, "")
	if res.Status != tools.StatusDenied {
		t.Fatalf("unknown caller must be denied: %s", res.Output)
	}
}

func TestSubagentControlActions(t *testing.T) {
	f := newFixture(t)
	tool := &orchestrator.SubagentTool{Orch: f.o}
	parent := string(f.parent.ID)
	_, rec := f.spawnOk(f.parent.ID, childCard("w", agents.ProfileImplementer))
	_ = f.attach(rec)
	run := string(rec.Run.ID)
	for _, action := range []string{"pause", "resume", "interrupt", "redirect"} {
		args := map[string]any{"action": action, "caller": parent, "run": run}
		if action == "redirect" {
			args["text"] = "adjust"
			args["interrupt"] = true
		}
		if res := toolCall(t, tool, args, ""); res.Status != tools.StatusOK {
			t.Fatalf("%s failed: %s", action, res.Output)
		}
	}
	res := toolCall(t, tool, map[string]any{"action": "cancel", "caller": parent, "run": run}, "")
	if res.Status != tools.StatusOK {
		t.Fatalf("cancel failed: %s", res.Output)
	}
	if got := runState(t, f.store, rec.Run.ID); got != types.RunCanceled {
		t.Fatalf("tool cancel must cancel, got %q", got)
	}
}

func TestSubagentIdempotency(t *testing.T) {
	f := newFixture(t)
	tool := &orchestrator.SubagentTool{Orch: f.o}
	parent := string(f.parent.ID)
	card := map[string]any{
		"name": "once", "description": "spawn once", "profile": "implementer",
		"goal": "g", "deliverable": "d", "scope": "project:p1",
		"done_criteria": []string{"done"}, "report_contract": "summary",
	}
	first := toolCall(t, tool, map[string]any{"action": "spawn", "caller": parent, "card": card}, "key-1")
	if first.Status != tools.StatusOK {
		t.Fatalf("spawn failed: %s", first.Output)
	}
	again := toolCall(t, tool, map[string]any{"action": "spawn", "caller": parent, "card": card}, "key-1")
	if again.Status != tools.StatusOK || again.Output != first.Output {
		t.Fatal("same idempotency key must replay the recorded result, not spawn again")
	}
	kids := f.o.Tree(f.parent.ID)
	if len(kids) != 2 {
		t.Fatalf("idempotent replay must not spawn twice, tree has %d", len(kids))
	}
}
