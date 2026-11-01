package wireengine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// TestEmbeddedSurfaceWired covers the task-scoped embedded surface:
// every eval-profile tool owned elsewhere answers for real instead of
// "not wired", and legacy exec target/owner forms run locally.
func TestEmbeddedSurfaceWired(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{RunID: "run_surface_1"})
	if err != nil {
		t.Fatal(err)
	}
	if emb.Surface == nil {
		t.Fatal("surface stores must be built by default")
	}
	ctx := context.Background()
	call := func(name string, args any) tools.Result {
		t.Helper()
		raw, _ := json.Marshal(args)
		res, err := emb.Exec.Execute(ctx, name, raw, tools.ExecuteOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Status != tools.StatusOK {
			t.Fatalf("%s must succeed, got %+v", name, res)
		}
		return res
	}
	// search_skills and load_skill resolve from the builtin index.
	sres := call("search_skills", map[string]string{"query": ""})
	if !strings.Contains(sres.Output, "skill") && !strings.Contains(sres.Output, "no skills") {
		t.Fatalf("search_skills: %q", sres.Output)
	}
	lres := call("load_skill", map[string]string{"name": "task-breakdown"})
	if !strings.Contains(lres.Output, "task-breakdown") {
		t.Fatalf("load_skill: %q", lres.Output)
	}
	// task_db plans against the embedded store: query starts empty,
	// a requirement round-trips, and the batch is transactional.
	qres := call("task_db", map[string]any{"ops": []any{map[string]any{"op": "query", "entity": "criteria"}}})
	if !strings.Contains(qres.Output, "results") {
		t.Fatalf("task_db query: %q", qres.Output)
	}
	mkres := call("task_db", map[string]any{"ops": []any{map[string]any{"op": "add_requirement", "text": "surface must work"}}})
	if !strings.Contains(mkres.Output, "add_requirement") {
		t.Fatalf("task_db add_requirement: %q", mkres.Output)
	}
	// Common model guesses alias to real ops.
	alias := call("task_db", map[string]any{"ops": []any{map[string]any{"op": "create_requirement", "text": "aliased"}}})
	if !strings.Contains(alias.Output, "add_requirement") {
		t.Fatalf("task_db create_requirement alias: %q", alias.Output)
	}
	bad, err := emb.Exec.Execute(ctx, "task_db", json.RawMessage(`{"ops":[{"op":"nope_missing"}]}`), tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if bad.Status == tools.StatusOK {
		t.Fatalf("task_db must reject unknown ops: %+v", bad)
	}
	// subagent spawn records a child with a scope note; the caller
	// defaults to the embedded root.
	spawn := call("subagent", map[string]any{
		"action": "spawn",
		"card": map[string]any{
			"name": "worker", "description": "does work", "goal": "do it",
			"deliverable": "done", "scope": "general",
			"done_criteria": []string{"done"}, "report_contract": "report",
		},
	})
	if !strings.Contains(spawn.Output, "scope_note") {
		t.Fatalf("subagent spawn must carry the single-run scope note: %q", spawn.Output)
	}
	// ack_directive with nothing pending succeeds with a scope note.
	ack := call("ack_directive", map[string]any{})
	if !strings.Contains(ack.Output, "single-run scope") {
		t.Fatalf("ack_directive: %q", ack.Output)
	}
	// history_search observes earlier calls through the run log.
	emb.Surface.History.Append("exec", `{"command":"echo hi"}`, "hi")
	hres := call("history_search", map[string]any{"text": "echo"})
	if !strings.Contains(hres.Output, "exec") {
		t.Fatalf("history_search: %q", hres.Output)
	}
	// pins and notes round-trip.
	pres := call("pin_observation", map[string]any{"observation": "keep this"})
	if !strings.Contains(pres.Output, "pinned") {
		t.Fatalf("pin_observation: %q", pres.Output)
	}
	if got := emb.Surface.Pins.List(); len(got) != 1 || got[0] != "keep this" {
		t.Fatalf("pins: %v", got)
	}
	nres := call("leave_continuation_note", map[string]any{"note": "next: verify"})
	if !strings.Contains(nres.Output, "noted") {
		t.Fatalf("leave_continuation_note: %q", nres.Output)
	}
	if got := emb.Surface.Notes.Latest(); got != "next: verify" {
		t.Fatalf("notes: %q", got)
	}
	// Legacy exec forms run locally instead of failing on target.
	execRes := call("exec", map[string]any{
		"command": "echo surface-ok", "target": "container", "owner": "main", "workdir": "/tmp",
	})
	if !strings.Contains(execRes.Output, "surface-ok") || !strings.Contains(execRes.Output, "exit: 0") {
		t.Fatalf("exec legacy form: %q", execRes.Output)
	}
	// search_tools with an empty query lists instead of erroring.
	stres := call("search_tools", map[string]any{})
	if !strings.Contains(stres.Output, "tools") {
		t.Fatalf("search_tools empty: %q", stres.Output)
	}
	// call_tool tolerates the input alias and reaches the deferred tool.
	ctres := call("call_tool", map[string]any{
		"name":  "file_history",
		"input": map[string]any{"path": "any.txt"},
	})
	if !strings.Contains(ctres.Output, "version(s)") {
		t.Fatalf("call_tool input alias: %q", ctres.Output)
	}
	// get_tool_output points sess-N refs at exec_io instead of a bare
	// unknown-blob error.
	gres, err := emb.Exec.Execute(ctx, "get_tool_output",
		json.RawMessage(`{"ref":"sess-6"}`), tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if gres.Status == tools.StatusOK || !strings.Contains(gres.Output, "exec_io") {
		t.Fatalf("get_tool_output sess ref must redirect to exec_io: %+v", gres)
	}
}

// TestEmbeddedSurfaceDisabled keeps the ablation switch honest: with
// WireEmbedded off the stubs report their owner again.
func TestEmbeddedSurfaceDisabled(t *testing.T) {
	off := tools.SurfaceTuning{}
	emb, err := BuildEmbedded(EmbeddedOptions{RunID: "run_surface_off", Tune: &off})
	if err != nil {
		t.Fatal(err)
	}
	if emb.Surface != nil {
		t.Fatal("surface must stay nil when WireEmbedded is off")
	}
	ctx := context.Background()
	res, err := emb.Exec.Execute(ctx, "task_db",
		json.RawMessage(`{"ops":[{"op":"query","entity":"criteria"}]}`), tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "no backend") || !strings.Contains(res.Output, "nothing was done") {
		t.Fatalf("disabled surface must say nothing was done: %+v", res)
	}
}
