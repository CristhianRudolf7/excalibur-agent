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

func TestFinishTurnPassesWhenVerified(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("created /app/filter.py and verified the import")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	run := testRun(Profile{})
	run.Request = "Create /app/filter.py."
	history := writeHistory("/app/filter.py")
	history = append(history,
		ctxcompiler.ActivityEvent{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"python3 -c 'import filter'","workdir":"/app"}`)}},
		ctxcompiler.ActivityEvent{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "/app/filter.py ok"}},
	)
	run.History = history
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if model.calls() != 1 {
		t.Fatalf("verified work finishes on the first final, got %d calls", model.calls())
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
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	cfg := tasks.DefaultSelfVerifyConfig()
	e.deps.SelfVerify = &cfg
	run := testRun(Profile{})
	run.Request = "Create /app/x.py."
	run.History = []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "write_file", `{"path":"/app/x.py"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "write_file", Output: "wrote /app/x.py"}},
		{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"test -s /app/x.py"}`)}},
		{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "/app/x.py ok"}},
	}
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	run.mu.Lock()
	defer run.mu.Unlock()
	if !run.refined || len(run.drafts) == 0 || run.drafts[0].Source != "refined" {
		t.Fatalf("refine pass should improve drafts: %+v", run.drafts)
	}
}
