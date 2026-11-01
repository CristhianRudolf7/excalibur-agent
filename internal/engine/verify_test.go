// Closing-hook verification: the finish path blocks completed
// without fresh executable evidence and returns the agent to work
// with a concise list (Sections 7.3, 7.5).
package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func verifyConfig() *tasks.SelfVerifyConfig {
	cfg := tasks.DefaultSelfVerifyConfig()
	cfg.EnableRefine = false
	return &cfg
}

// artifactOff pins the required-artifact gate off for the legacy
// self-verification tests below: they use fake /app paths that exist
// only in scripted history, never on the test filesystem. Gate-on
// behavior with real files lives in artifact_verify_test.go.
func artifactOff() *tasks.ArtifactConfig {
	cfg := tasks.DefaultArtifactConfig()
	cfg.Enable = false
	return &cfg
}

func writeHistory(path string) []ctxcompiler.ActivityEvent {
	return []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "write_file", `{"path":"`+path+`"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "write_file", Output: "wrote " + path}},
	}
}

func TestFinishTurnBlocksUnverifiedWrite(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	run := testRun(Profile{})
	run.Request = "Create the file /app/filter.py that strips scripts."
	run.History = writeHistory("/app/filter.py")
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	if model.calls() != 2 {
		t.Fatalf("bounded returns need 2 model calls, got %d", model.calls())
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "self-verify") {
			found = true
		}
	}
	if !found {
		t.Fatalf("retry note must name the verification gap: %v", run.VolatileNotes())
	}
	if rep := e.Report(run); len(rep.Pendings) == 0 {
		t.Fatal("honest state needs pendings")
	}
}

func verifierPassResp() providers.ChatResponse {
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant,
		Content: "FRESH_INPUTS: PASS - generated hello input asserted independently\nPROPERTY: PASS - file is importable Python\nOUTPUT_CONTRACT: PASS - Python module import verified\nCHECKS:\n- python3 /app/filter.py hello\nD1: PASS - ran with input, output confirms behavior\nVERDICT: PASS\nREASONS: none"}}
}

func TestFinishTurnPassesWhenVerified(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("created /app/filter.py and verified the import"), verifierPassResp()}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	e.deps.Artifacts = artifactOff()
	run := testRun(Profile{})
	run.Request = "Create /app/filter.py."
	history := writeHistory("/app/filter.py")
	history = append(history,
		ctxcompiler.ActivityEvent{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"python3 /app/filter.py hello","workdir":"/app"}`)}},
		ctxcompiler.ActivityEvent{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "/app/filter.py ok"}},
	)
	run.History = history
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	// One executor final plus the independent verifier pass.
	if model.calls() != 2 {
		t.Fatalf("verified work finishes with final plus verifier, got %d calls", model.calls())
	}
}

func TestFinishTurnBlocksStillRunning(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	run := testRun(Profile{})
	run.Request = "Build the project with make."
	run.History = []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "exec", `{"command":"make -j","workdir":"/app"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "exec", Output: "session: sess-1  alive: true\nbuilding..."}},
	}
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	if got := tasks.CompactNote(run.LastGate); !strings.Contains(got, "still running") {
		t.Fatalf("pendings should name the running session: %q", got)
	}
}

func TestFinishTurnStashesRequestFromLayer(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	e.deps.Context = &fakeBuilder{
		compiled: ctxcompiler.Compiled{Layers: [6]string{
			"", "", "", "request:\nDo X\nfixed instructions:\n- verify\n", "", "",
		}},
	}
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.Request != "Do X" {
		t.Fatalf("request = %q, want stashed layer text", run.Request)
	}
}

func TestFinishTurnRefineImprovesDrafts(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		finalResp("created /app/x.py and verified"),
		{Message: providers.Message{Role: providers.RoleAssistant,
			Content: "command | file /app/x.py exists | test -s /app/x.py"}},
		verifierPassResp(),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	cfg := tasks.DefaultSelfVerifyConfig()
	e.deps.SelfVerify = &cfg
	e.deps.Artifacts = artifactOff()
	run := testRun(Profile{})
	run.Request = "Create /app/x.py."
	run.History = []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "write_file", `{"path":"/app/x.py"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "write_file", Output: "wrote /app/x.py"}},
		{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"python3 /app/x.py"}`)}},
		{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "/app/x.py ok"}},
	}
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	// Final, refine pass, and the independent verifier pass.
	if model.calls() != 3 {
		t.Fatalf("refine plus verifier need 3 model calls, got %d", model.calls())
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if !run.refined || len(run.drafts) == 0 || run.drafts[0].Source != "refined" {
		t.Fatalf("refine pass should improve drafts: %+v", run.drafts)
	}
}
