// Strong verification through the finish path: the run must
// exercise the deliverable (not merely observe it) and survive the
// independent verifier pass before it may complete (Sections 7.3,
// 7.5, 8.7).
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

func verifierFailResp() providers.ChatResponse {
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant,
		Content: "CHECKS:\n- python3 /app/x.py hello\nD1: FAIL - program never run with representative inputs\nVERDICT: FAIL\nREASONS: D1: no exercising evidence after the last change"}}
}

func shallowHistory(path string) []ctxcompiler.ActivityEvent {
	history := writeHistory(path)
	return append(history,
		ctxcompiler.ActivityEvent{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"ls -l `+path+`"}`)}},
		ctxcompiler.ActivityEvent{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "-rw-r--r-- " + path}},
	)
}

func TestFinishTurnBlocksShallowObservation(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	// Fake /app paths exist only in scripted history, never on the
	// test filesystem: pin the artifact gate off here (its behavior
	// lives in artifact_verify_test.go).
	e.deps.Artifacts = artifactOff()
	run := testRun(Profile{})
	run.Request = "Create the file /app/x.py."
	run.History = shallowHistory("/app/x.py")
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	if model.calls() != 2 {
		t.Fatalf("bounded returns need 2 model calls, got %d", model.calls())
	}
	joined := strings.Join(run.VolatileNotes(), "; ")
	if !strings.Contains(strings.ToLower(joined), "exercis") {
		t.Fatalf("retry note must demand exercising the deliverable: %v", run.VolatileNotes())
	}
}

func TestStrongVerifierDisagreementReturnsToWorkThenBlocks(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		finalResp("created /app/x.py and verified the import"),
		verifierFailResp(),
		finalResp("created /app/x.py and verified the import"),
		verifierFailResp(),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
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
	// Verifier disagreement is a blocking honest state (Section 7.3):
	// after two rounds both positions stand.
	mustState(t, run, types.RunBlocked)
	if model.calls() != 4 {
		t.Fatalf("two finish attempts each carry final plus verifier, got %d calls", model.calls())
	}
	if got := tasks.CompactNote(run.LastGate); !strings.Contains(strings.ToLower(got), "verifier") {
		t.Fatalf("pendings should carry the verifier reasons: %q", got)
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(strings.ToLower(n), "verifier") {
			found = true
		}
	}
	if !found {
		t.Fatalf("first retry note must carry the verifier reasons: %v", run.VolatileNotes())
	}
}

func TestStrongVerifierFallbackPassesOnGibberishWithGeneralizationOff(t *testing.T) {
	t.Setenv("EXCALIBUR_VERIFIER_VERDICT", "off")
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		finalResp("created /app/filter.py and verified the import"),
		finalResp("looks good to me"),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	e.deps.SelfVerify.EnableGeneralizeChecks = false
	e.deps.Artifacts = artifactOff()
	run := testRun(Profile{})
	run.Request = "Create /app/filter.py."
	run.History = []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "write_file", `{"path":"/app/filter.py"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "write_file", Output: "wrote /app/filter.py"}},
		{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"python3 /app/filter.py hello","workdir":"/app"}`)}},
		{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "/app/filter.py ok"}},
	}
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	// An unparseable verifier reply never blocks: the deterministic
	// outcome stands.
	mustState(t, run, types.RunCompleted)
}

func TestStrongVerifierSkippedWhenDaemonOwnsVerdict(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		finalResp("created /app/filter.py and verified the import"),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	e.deps.Artifacts = artifactOff()
	run := testRun(Profile{})
	run.Request = "Create /app/filter.py."
	run.GateInput.VerifierRequired = true
	run.History = []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "write_file", `{"path":"/app/filter.py"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "write_file", Output: "wrote /app/filter.py"}},
		{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"python3 /app/filter.py hello","workdir":"/app"}`)}},
		{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "/app/filter.py ok"}},
	}
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	// The orchestrator's 8.7 spawn owns the verdict here: no embedded
	// verifier call, so the run completes on the single final.
	if model.calls() != 1 {
		t.Fatalf("daemon-owned verdict must not spend a verifier call, got %d", model.calls())
	}
}

func TestStrongSwitchOffRestoresShallowCompletion(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	cfg := verifyConfig()
	cfg.EnableStrong = false
	e.deps.SelfVerify = cfg
	e.deps.Artifacts = artifactOff()
	run := testRun(Profile{})
	run.Request = "Create the file /app/x.py."
	run.History = shallowHistory("/app/x.py")
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
}
