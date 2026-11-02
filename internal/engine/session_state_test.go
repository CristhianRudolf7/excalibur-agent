package engine

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestCurrentSessionStateOverridesHistoricalLivenessEveryGate(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	zero := 0
	e.deps.ActiveSessions = func() []tools.SessionInfo {
		return []tools.SessionInfo{{ID: "sess-1", Alive: false, ExitCode: &zero}, {ID: "sess-2", Alive: true}}
	}
	raw := []tasks.HistoryTool{{Name: "exec", Args: `{"command":"make check"}`, Output: "session: sess-1 alive: true\nexit: running"}}
	history := e.currentSessionHistory(raw)
	if ids := tasks.LiveSessions(history); len(ids) != 0 {
		t.Fatal(ids)
	}
	if p := tasks.CheckFreshVerification(history, 1); len(p) > 0 {
		t.Fatal(p)
	}
	if p := tasks.CheckBackgroundSessions(history, "compile sources", ""); len(p) > 0 {
		t.Fatal(p)
	}
	if p := runningWorkReasons(history, nil, e.deps.Clock(), e.deps.ActiveSessions()); len(p) > 0 {
		t.Fatal(p)
	}
	if raw[0].SessionStates != nil {
		t.Fatal("history changed")
	}
	// Mixed listings must use state per ID, never the first textual marker.
	mixed := e.currentSessionHistory([]tasks.HistoryTool{{Name: "exec_io", Output: "sess-1 alive=true\nsess-2 alive=false"}})
	if ids := tasks.LiveSessions(mixed); len(ids) != 1 || ids[0] != "sess-2" {
		t.Fatal(ids)
	}
	// A vanished/evicted session cannot stay live from old text.
	e.deps.ActiveSessions = func() []tools.SessionInfo { return nil }
	if ids := tasks.LiveSessions(e.currentSessionHistory(raw)); len(ids) > 0 {
		t.Fatal(ids)
	}
	t.Setenv("EXCALIBUR_SESSION_AUTHORITY", "0")
	if ids := tasks.LiveSessions(e.currentSessionHistory(raw)); len(ids) != 1 {
		t.Fatal("legacy switch ignored", ids)
	}
}

func TestFinishNarrativeDoesNotInheritHealthyService(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.ActiveSessions = func() []tools.SessionInfo { return []tools.SessionInfo{{ID: "sess-3", Alive: true}} }
	h := e.currentSessionHistory([]tasks.HistoryTool{{Name: "finish", Args: `{"summary":"service sess-3 remains healthy"}`, Output: "finish received"}})
	if tasks.HistoryRunning(h[0]) {
		t.Fatal("finish acquired healthy service liveness")
	}
	for _, pending := range tasks.CheckFreshVerification(h, 1) {
		if strings.Contains(pending, "finish is still running") {
			t.Fatal(pending)
		}
	}
}

func TestTypedBuildReceiptStillBlocksFinish(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	r.Profile.WallSource = "default"
	e.deps.ActiveSessions = func() []tools.SessionInfo {
		return []tools.SessionInfo{{ID: "sess-4", Alive: true, Command: "make check", Build: true}}
	}
	r.History = []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{{ID: "build", Name: "exec", Arguments: []byte(`{"command":"make check"}`)}}}, {Sessions: []tools.SessionReceipt{{ID: "sess-4", Alive: true}}, ToolResult: &providers.ToolResult{CallID: "build", Name: "exec", Output: "excerpt omitted"}}}
	if reason := e.pendingCommandReason(r, e.deps.Clock()); !strings.Contains(reason, "still running a build or test") {
		t.Fatal(reason)
	}
}
