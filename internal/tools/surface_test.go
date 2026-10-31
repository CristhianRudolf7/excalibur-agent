package tools

import (
	"context"
	"encoding/json"
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
	dir := t.TempDir()
	path := dir + "/w.txt"
	qp, _ := json.Marshal(path)
	w := &WriteFileTool{Deps: &FileDeps{Fingerprints: NewFingerprintStore(), Leases: NewMemoryLeases(), History: NewMemoryHistory()}}
	if res, _ := w.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":` + string(qp) + `,"content":"one"}`)}); res.Status != StatusOK {
		t.Fatalf("write: %+v", res)
	}
	e := &EditFileTool{Deps: w.Deps}
	res, _ := e.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":` + string(qp) + `,"old_text":"one","new_text":"two","expected_fp":"*"}`)})
	if res.Status != StatusOK || !strings.Contains(res.Output, "skipped") {
		t.Fatalf("wildcard fp must skip with a note: %+v", res)
	}
}
