package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func norm(t *testing.T, name, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(NormalizeArgs(name, json.RawMessage(raw), DefaultSurfaceTuning()), &out); err != nil {
		t.Fatalf("%s normalize: %v", name, err)
	}
	return out
}

func TestNormalizeArgsAliases(t *testing.T) {
	if got := norm(t, "exec", `{"cmd":"echo hi","cwd":"/tmp","timeout":5}`); got["command"] != "echo hi" || got["workdir"] != "/tmp" {
		t.Fatalf("exec aliases: %v", got)
	} else if _, ok := got["target"]; ok {
		t.Fatalf("exec must drop target: %v", got)
	}
	if got := norm(t, "exec", `"echo bare"`); got["command"] != "echo bare" {
		t.Fatalf("exec bare string: %v", got)
	}
	got := norm(t, "search_tools", `{"q":"pdf merge","limit":3}`)
	if got["query"] != "pdf merge" {
		t.Fatalf("search_tools aliases: %v", got)
	}
	if k, ok := got["k"].(float64); !ok || k != 3 {
		t.Fatalf("search_tools limit->k: %v", got)
	}
	if got := norm(t, "call_tool", `{"tool":"file_history","input":{"path":"x"}}`); got["name"] != "file_history" {
		t.Fatalf("call_tool aliases: %v", got)
	} else if args, ok := got["arguments"].(map[string]any); !ok || args["path"] != "x" {
		t.Fatalf("call_tool input alias: %v", got)
	}
	got = norm(t, "search_tools", `{}`)
	if q, ok := got["query"]; !ok || q != "" {
		t.Fatalf("search_tools empty must default query: %v", got)
	}
	off := DefaultSurfaceTuning()
	off.TolerantArgs = false
	off.SimpleExec = false
	raw := NormalizeArgs("exec", json.RawMessage(`{"cmd":"x"}`), off)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["cmd"]; !ok {
		t.Fatalf("switches off must leave args alone: %s", string(raw))
	}
}

func TestExecutorRepairHint(t *testing.T) {
	reg := NewMapRegistry()
	_ = RegisterBuiltins(reg, nil)
	cat := NewDeferredCatalog(reg)
	wireExec := NewExecutor(reg)
	WireSearchCall(reg, cat, wireExec)
	exec := NewExecutor(reg)
	res, err := exec.Execute(context.Background(), "exec", json.RawMessage(`{"workdir":"/tmp"}`), ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusInputError || !strings.Contains(res.Output, "exec needs") {
		t.Fatalf("exec missing command must carry a repair hint: %+v", res)
	}
	res2, err := exec.Execute(context.Background(), "search_tools", json.RawMessage(`{}`), ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != StatusOK {
		t.Fatalf("search_tools empty must list instead of erroring: %+v", res2)
	}
}

func TestExecResultPrefixAndHints(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	tool := &ExecTool{Sessions: m}
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"command":"echo hi"}`)})
	if !strings.Contains(res.Output, "exit: 0") || !strings.Contains(res.Output, "duration:") {
		t.Fatalf("exec must lead with exit and duration: %q", res.Output)
	}
	if out := actionableExecHint("python3 -c 1", "bash: line 1: python3: command not found"); !strings.Contains(out, "package managers") {
		t.Fatalf("missing binary must name package managers: %q", out)
	}
	big := strings.Repeat("x\n", 6000)
	if got := headTailInline(big, 8000); !strings.Contains(got, "elided") {
		t.Fatal("head+tail must elide the middle of large outputs")
	}
}

func TestWildcardFingerprint(t *testing.T) {
	// "*" is a placeholder, not a wildcard: the harness check still
	// applies, so a write over the harness's own last write goes through
	// and an external change is still refused.
	dir := t.TempDir()
	path := dir + "/w.txt"
	qp, _ := json.Marshal(path)
	w := &WriteFileTool{Deps: &FileDeps{Fingerprints: NewFingerprintStore(), Leases: NewMemoryLeases(), History: NewMemoryHistory()}}
	if res, _ := w.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":` + string(qp) + `,"content":"one"}`)}); res.Status != StatusOK {
		t.Fatalf("write: %+v", res)
	}
	e := &EditFileTool{Deps: w.Deps}
	res, _ := e.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":` + string(qp) + `,"old_text":"one","new_text":"two","expected_fp":"*"}`)})
	if res.Status != StatusOK || strings.Contains(res.Output, "skipped") {
		t.Fatalf("placeholder fp over the harness's own write must apply without a skip note: %+v", res)
	}
	if err := os.WriteFile(path, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ = e.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":` + string(qp) + `,"old_text":"external","new_text":"x","expected_fp":"*"}`)})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "changed since read") {
		t.Fatalf("wildcard must not bypass an external change: %+v", res)
	}
}

func TestCallToolNamePromotion(t *testing.T) {
	// Nested inside arguments.
	got := norm(t, "call_tool", `{"arguments":{"name":"file_history","path":"x"}}`)
	if got["name"] != "file_history" {
		t.Fatalf("nested name must promote: %v", got)
	} else if args, ok := got["arguments"].(map[string]any); !ok || args["path"] != "x" {
		t.Fatalf("promoted arguments must keep the rest: %v", got)
	} else if _, consumed := args["name"]; consumed {
		t.Fatalf("promoted name key must be consumed: %v", got)
	}
	// Tool alias, case-insensitive, nested.
	got = norm(t, "call_tool", `{"arguments":{"Tool":"LSP","path":"y"}}`)
	if got["name"] != "lsp" {
		t.Fatalf("nested tool alias must promote lowercased: %v", got)
	}
	// Empty top-level name falls back to the nested one.
	got = norm(t, "call_tool", `{"name":"","arguments":{"tool_name":"code_graph"}}`)
	if got["name"] != "code_graph" {
		t.Fatalf("empty name must fall back to nested: %v", got)
	}
	// Single-key wrap: leading key names the tool.
	got = norm(t, "call_tool", `{"file_history":{"path":"z"}}`)
	if got["name"] != "file_history" {
		t.Fatalf("wrapped name must promote: %v", got)
	} else if args, ok := got["arguments"].(map[string]any); !ok || args["path"] != "z" {
		t.Fatalf("wrapped value must become arguments: %v", got)
	}
	// Known leading keys never trigger the wrap rule.
	got = norm(t, "call_tool", `{"arguments":{"path":"z"}}`)
	if _, has := got["name"]; has {
		t.Fatalf("arguments-first must not invent a name: %v", got)
	}
	// Switch off restores the strict legacy shape.
	flexOff := DefaultSurfaceTuning()
	flexOff.CallNameFlex = false
	var m map[string]any
	_ = json.Unmarshal(NormalizeArgs("call_tool", json.RawMessage(`{"arguments":{"name":"x"}}`), flexOff), &m)
	if _, has := m["name"]; has {
		t.Fatalf("flex off must not promote: %v", m)
	}
}

func TestFirstTopKey(t *testing.T) {
	k, v, ok := firstTopKey(`{"b":1,"a":2}`)
	if !ok || k != "b" {
		t.Fatalf("wire order must win, got %q %v", k, ok)
	}
	var n float64
	_ = json.Unmarshal(v, &n)
	if n != 1 {
		t.Fatalf("raw value must match the key, got %s", string(v))
	}
	for _, raw := range []string{``, `[]`, `"s"`, `{}`, `{"a"}`} {
		if _, _, ok := firstTopKey(raw); ok {
			t.Fatalf("firstTopKey(%q) must fail", raw)
		}
	}
}

func TestCallToolDescriptionExamples(t *testing.T) {
	reg := NewMapRegistry()
	_ = RegisterBuiltins(reg, nil)
	tool, err := reg.Get("call_tool")
	if err != nil {
		t.Fatal(err)
	}
	desc := tool.Definition().Description
	for _, want := range []string{`"name":"lsp"`, `"arguments"`, "inside arguments"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("call_tool description must carry one-line examples, got %q", desc)
		}
	}
}

func TestCallNameFlexEnvSwitch(t *testing.T) {
	t.Setenv("EXCALIBUR_TOOL_CALL_NAME", "0")
	tune := TuningFromEnv(DefaultSurfaceTuning())
	if tune.CallNameFlex {
		t.Fatal("EXCALIBUR_TOOL_CALL_NAME=0 must disable name promotion")
	}
	if got := DefaultSurfaceTuning(); !got.CallNameFlex {
		t.Fatal("name promotion must default ON")
	}
}
