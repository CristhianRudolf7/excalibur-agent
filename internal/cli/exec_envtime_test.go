package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func doneImmediately() []providers.ChatResponse {
	// Explicit finish (agent quality): trivial runs still end ONLY
	// through `finish` with evidence, never through a text-only turn.
	return finishScript("done", "no tool work needed; request verified by inspection")
}

func systemText(req providers.ChatRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == providers.RoleSystem {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// block extracts the injected snapshot block (up to the L2
// instructions trailer) for byte comparison.
func block(sys string) string {
	start := strings.Index(sys, "Environment snapshot")
	if start < 0 {
		return ""
	}
	end := strings.Index(sys[start:], "\ninstructions:")
	if end < 0 {
		return sys[start:]
	}
	return sys[start : start+end]
}

// The proactive snapshot rides L2 once: every model call of the run
// carries it, byte-identical across steps (Section 13.5, 11.1).
func TestExecEngineEnvSnapshotInjectedOnce(t *testing.T) {
	fake := providerfake.New("envsnap")
	finishTail := finishScript("done", "clock verified the time")
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "clock", Arguments: []byte(`{}`)}}}},
	}
	fake.Responses = append(fake.Responses, finishTail...)
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "what time is it", LogsDir: t.TempDir(),
		Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if len(fake.ChatRequests) != 3 {
		t.Fatalf("model calls = %d, want 3 (tool turn plus the two-turn finish confirmation)", len(fake.ChatRequests))
	}
	first, second := systemText(fake.ChatRequests[0]), systemText(fake.ChatRequests[1])
	for _, want := range []string{"Environment snapshot", "os: ", "shell:", "workdir"} {
		if !strings.Contains(first, want) {
			t.Fatalf("L2 missing %q:\n%s", want, first)
		}
	}
	// The snapshot block itself is byte-identical across steps (the
	// L5 budget line legitimately advances: steps 0/25 -> 1/25).
	if block(first) != block(second) {
		t.Fatalf("snapshot bytes must be identical across steps (stable L2 prefix):\n%s\n---\n%s", block(first), block(second))
	}
}

// EXCALIBUR_ENV_SNAPSHOT=0 restores the prompt without the snapshot.
func TestExecEngineEnvSnapshotSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_ENV_SNAPSHOT", "0")
	fake := providerfake.New("envsnap-off")
	fake.Responses = doneImmediately()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "hi", LogsDir: t.TempDir(), Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if got := systemText(fake.ChatRequests[0]); strings.Contains(got, "Environment snapshot") {
		t.Fatalf("disabled snapshot must not inject:\n%s", got)
	}
}

// Every model call carries the compact remaining budget in L5, and
// the discipline lines ride the fixed instructions (Sections 7.3,
// 18.4). Legacy path with time-only off keeps the 25-step cap.
func TestExecEngineBudgetLineAndDiscipline(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "0")
	fake := providerfake.New("budgetline")
	fake.Responses = doneImmediately()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "hi", LogsDir: t.TempDir(), Profile: ProfileEval,
		Provider: fake, MaxSteps: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	sys := systemText(fake.ChatRequests[0])
	if !strings.Contains(sys, "steps 0/25") {
		t.Fatalf("L5 must carry remaining steps, got:\n%s", sys)
	}
	if !strings.Contains(sys, "Budget your time") || !strings.Contains(sys, "wait_for") {
		t.Fatalf("discipline lines missing:\n%s", sys)
	}
}

// With the time switch off there is no budget line and no discipline
// prompt: the ablation baseline. The long-horizon pacing line has its
// own switch, so the silent baseline disables both.
func TestExecEngineTimeBudgetSwitchOff(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "0")
	t.Setenv("EXCALIBUR_LONG_HORIZON", "off")
	fake := providerfake.New("budgetline-off")
	fake.Responses = doneImmediately()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "hi", LogsDir: t.TempDir(), Profile: ProfileEval,
		Provider: fake, MaxSteps: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	sys := systemText(fake.ChatRequests[0])
	if strings.Contains(sys, "steps 0/25") || strings.Contains(sys, "Budget your time") {
		t.Fatalf("disabled time budget must stay silent, got:\n%s", sys)
	}
}

// The Harbor override flows into the engine wall budget without an
// explicit flag (Section 23.2: 90% closing budget).
func TestExecEngineHarborTimeoutBecomesWallBudget(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "1000")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "")
	if got := engine.ResolveMaxSeconds(0); got != 900 {
		t.Fatalf("override must resolve to 900, got %d", got)
	}
}

func TestProbeHost(t *testing.T) {
	if got := probeHost("https://api.openai.com/v1"); got != "api.openai.com" {
		t.Fatalf("got %q", got)
	}
	if got := probeHost("http://localhost:11434/v1"); got != "localhost:11434" {
		t.Fatalf("got %q", got)
	}
	for _, bad := range []string{"", "  ", "://missing", "not a url %%"} {
		if got := probeHost(bad); got != "" {
			t.Fatalf("probeHost(%q) = %q, want empty", bad, got)
		}
	}
}
