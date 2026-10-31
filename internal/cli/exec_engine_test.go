package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func scriptedWriteThenDone(target, content string) []providers.ChatResponse {
	args, _ := json.Marshal(map[string]string{"path": target, "content": content})
	readArgs, _ := json.Marshal(map[string]string{"path": target})
	return []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "write_file", Arguments: args}}}},
		// The closing self-verification requires a fresh executable
		// check after the last change before completed (Section
		// 7.5): the agent reads the file back.
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c2", Name: "read_file", Arguments: readArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "wrote " + target}},
	}
}

// TestExecEnginePathEndToEnd covers the embedded eval profile using
// all of the wiring through excalibur exec with a scripted fake
// provider: the real engine loop, the real tool pipeline with leases
// and shared history, deferred tools through search_tools/call_tool,
// vault redaction in outputs, and the Appendix F artifacts.
func TestExecEnginePathEndToEnd(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")
	fake := providerfake.New("e2e-embedded")
	fake.Responses = scriptedWriteThenDone(target, "hello embedded")
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write hello", LogsDir: logs, Profile: ProfileEval,
		Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted || out.ExitCode != ExitOK {
		t.Fatalf("state = %q/%d", out.State, out.ExitCode)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "hello embedded" {
		t.Fatalf("file = %q, err = %v", data, err)
	}
	if len(fake.ChatRequests) != 3 {
		t.Fatalf("model calls = %d, want 3 (tool turn, verification turn, final)", len(fake.ChatRequests))
	}
	// The tool definitions ride the prefix in fixed order so the
	// input cache is stable across steps.
	first, second := fake.ChatRequests[0], fake.ChatRequests[1]
	if len(first.Tools) == 0 || len(second.Tools) == 0 {
		t.Fatal("both model calls must carry the tool prefix")
	}
	if first.Tools[0].Name != second.Tools[0].Name {
		t.Fatal("tool prefix order must be stable across calls")
	}
	if raw, err := os.ReadFile(out.ATIFPath); err != nil {
		t.Fatalf("harbor ATIF missing: %v", err)
	} else {
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil || doc["schema_version"] != "ATIF-v1.8" {
			t.Fatalf("harbor ATIF invalid: %v %v", doc["schema_version"], err)
		}
	}
	for _, name := range []string{"profile.json", "result.json", "events.jsonl", "metrics.json"} {
		if _, err := os.Stat(filepath.Join(out.Dir, name)); err != nil {
			t.Fatalf("missing artifact %s: %v", name, err)
		}
	}
}

// TestExecEngineEvalSurfaceClamp covers the Section 23.2 eval table
// end to end: the model only sees the profile's core tools, and a
// call outside the surface — an assistant tool or a disabled
// task_db operation — is rejected by the gate before execution.
func TestExecEngineEvalSurfaceClamp(t *testing.T) {
	fake := providerfake.New("e2e-eval-clamp")
	badTaskDB, _ := json.Marshal(map[string]any{
		"ops": []map[string]any{{"op": "register_project", "name": "evil"}},
	})
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "send_message", Arguments: json.RawMessage(`{}`)}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c2", Name: "task_db", Arguments: badTaskDB}}}},
		// The closing self-verification blocks completed while the
		// last result is a rejection: the scripted model recovers
		// with an in-surface check before finishing.
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c3", Name: "exec", Arguments: json.RawMessage(`{"command":"true"}`)}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "done within surface"}},
	}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "stay inside the eval surface", LogsDir: logs, Profile: ProfileEval,
		Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Assistant tools must not ride the eval tool prefix.
	for _, req := range fake.ChatRequests {
		for _, tool := range req.Tools {
			if tool.Name == "send_message" || tool.Name == "ask_user" {
				t.Fatalf("assistant tool %q exposed to the eval model", tool.Name)
			}
		}
	}
	if len(fake.ChatRequests) == 0 || len(fake.ChatRequests[0].Tools) == 0 {
		t.Fatal("eval model calls must carry the tool prefix")
	}
	raw, err := os.ReadFile(out.ATIFPath)
	if err != nil {
		t.Fatal(err)
	}
	trajectory := string(raw)
	if !strings.Contains(trajectory, "send_message") {
		t.Fatal("ATIF must record the rejected assistant-tool call")
	}
	if !strings.Contains(trajectory, "register_project") {
		t.Fatal("ATIF must record the rejected task_db operation")
	}
	// Rejections surface as tool errors, so the scripted model
	// recovers and finishes: the gate contains without failing
	// the run.
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed after contained rejections", out.State)
	}
}

// TestProfileGatedRunnerUnits pins the gate matrix without an engine:
// eval rejects outside the surface and disabled task_db ops, while
// other profiles pass everything through.
func TestProfileGatedRunnerUnits(t *testing.T) {
	base := engine.NewRegistryRunner(nil, nil)
	gate := newProfileGatedRunner(ProfileEval, base)
	if err := gate.check("send_message", nil); err == nil {
		t.Fatal("eval must reject send_message")
	}
	good, _ := json.Marshal(map[string]any{
		"ops": []map[string]any{{"op": "query", "entity": "criteria"}},
	})
	if err := gate.check("task_db", good); err != nil {
		t.Fatalf("eval must allow task-internal task_db ops: %v", err)
	}
	bad, _ := json.Marshal(map[string]any{
		"ops": []map[string]any{{"op": "register_project"}},
	})
	if err := gate.check("task_db", bad); err == nil {
		t.Fatal("eval must reject task_db register_project")
	}
	if err := gate.check("exec", nil); err != nil {
		t.Fatalf("eval must allow exec: %v", err)
	}
	open := newProfileGatedRunner(ProfileDefault, base)
	if err := open.check("send_message", nil); err != nil {
		t.Fatalf("default must not gate: %v", err)
	}
}

// TestPruneDeferredToProfile pins the eval search_tools/call_tool
// surface: only the profile's deferred tools stay marked.
func TestPruneDeferredToProfile(t *testing.T) {
	reg := tools.NewMapRegistry()
	cat := tools.NewDeferredCatalog(reg)
	for _, name := range []string{"lsp", "tool_package", "email"} {
		cat.MarkDeferred(name)
	}
	core, deferred := EffectiveTools(ProfileEval)
	allowed := map[string]bool{}
	for _, n := range core {
		allowed[n] = true
	}
	for _, n := range deferred {
		allowed[n] = true
	}
	pruneDeferredToProfile(cat, allowed)
	if !cat.IsDeferred("lsp") {
		t.Fatal("lsp must stay deferred in eval")
	}
	if cat.IsDeferred("tool_package") || cat.IsDeferred("email") {
		t.Fatal("non-table deferred tools must be pruned in eval")
	}
}

// TestExecEnginePathDeferredCallTool covers deferred reachability in
// the embedded run: the scripted model searches for a deferred tool
// and invokes it through call_tool without breaking the cache prefix.
func TestExecEnginePathDeferredCallTool(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "note.txt")
	writeArgs, _ := json.Marshal(map[string]string{"path": target, "content": "v1"})
	searchArgs, _ := json.Marshal(map[string]string{"query": "file versions history"})
	callArgs, _ := json.Marshal(map[string]any{
		"name": "file_history", "arguments": map[string]any{"path": target},
	})
	fake := providerfake.New("e2e-deferred")
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "write_file", Arguments: writeArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{
				{ID: "c2", Name: "search_tools", Arguments: searchArgs},
			}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{
				{ID: "c3", Name: "call_tool", Arguments: callArgs},
			}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "done with history"}},
	}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write and list history", LogsDir: logs, Profile: ProfileEval,
		Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if len(fake.ChatRequests) != 4 {
		t.Fatalf("model calls = %d, want 4", len(fake.ChatRequests))
	}
	raw, err := os.ReadFile(out.ATIFPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "file_history") {
		t.Fatalf("ATIF must show the deferred tool result, got %s", raw)
	}
}

// TestExecEngineSkillsWiring covers the skills-library seam end to
// end with a scripted provider: the L1 catalog and task-start
// suggestions ride the first model call, load_skill resolves through
// the wired index, and the skills.json ablation record lands in the
// run directory.
func TestExecEngineSkillsWiring(t *testing.T) {
	t.Setenv("EXCALIBUR_SKILLS", "auto")
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")
	fake := providerfake.New("e2e-skills")
	args, _ := json.Marshal(map[string]string{"path": target, "content": "hello skills"})
	loadArgs, _ := json.Marshal(map[string]string{"name": "shell-power-use", "section": "pitfalls"})
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "load_skill", Arguments: loadArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c2", Name: "write_file", Arguments: args}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "done"}},
	}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "debug the failing python test and write hello", LogsDir: logs,
		Profile: ProfileEval, ProjectDir: dir, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed", out.State)
	}
	// The L1 catalog rides the first model call as a system message.
	catalogSeen := false
	for _, msg := range fake.ChatRequests[0].Messages {
		if strings.Contains(string(msg.Role)+" "+msg.Content, "shell-power-use") {
			catalogSeen = true
		}
	}
	if !catalogSeen {
		t.Fatal("first model call must carry the L1 skill catalog")
	}
	// load_skill resolves through the wired index, not the stub.
	raw, err := os.ReadFile(out.ATIFPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Unquoted variables") {
		t.Fatal("ATIF must show the loaded skill section")
	}
	// The ablation record pins mode, catalog hash and suggestions.
	skillsRaw, err := os.ReadFile(filepath.Join(out.Dir, "skills.json"))
	if err != nil {
		t.Fatalf("skills.json missing: %v", err)
	}
	var rec struct {
		Mode      string `json:"mode"`
		Catalog   string `json:"catalog_hash"`
		Suggested []struct {
			Name string `json:"name"`
		} `json:"suggested"`
	}
	if err := json.Unmarshal(skillsRaw, &rec); err != nil {
		t.Fatalf("skills.json invalid: %v", err)
	}
	if rec.Mode != "auto" || rec.Catalog == "" || len(rec.Suggested) == 0 {
		t.Fatalf("skills.json must record mode/catalog/suggestions, got %+v", rec)
	}
}
