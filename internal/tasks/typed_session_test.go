package tasks

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestTypedSessionNarrativeDoesNotOwnService(t *testing.T) {
	states := map[string]SessionState{"sess-7": {Alive: true}}
	for _, name := range []string{"finish", "status", "read_file", "exec"} {
		h := HistoryTool{Name: name, Args: `{"summary":"Healthy service sess-7 remains available"}`, Output: "finish received: sess-7 alive: true", SessionStates: states}
		if HistoryRunning(h) || len(HistorySessionIDs(h)) > 0 {
			t.Fatalf("%s narrative acquired a process", name)
		}
		for _, p := range CheckFreshVerification([]HistoryTool{h}, 1) {
			if strings.Contains(p, "still running") {
				t.Fatal(p)
			}
		}
	}
	t.Setenv("EXCALIBUR_TYPED_SESSION_EVIDENCE", "0")
	if !HistoryRunning(HistoryTool{Name: "finish", Output: "sess-7", SessionStates: states}) {
		t.Fatal("legacy switch ignored")
	}
}

func TestTypedSessionReceiptUsesCurrentAuthority(t *testing.T) {
	zero, one := 0, 1
	h := HistoryTool{Name: "exec", Output: "excerpt omitted", Sessions: []tools.SessionReceipt{{ID: "sess-8", Alive: true}}}
	if !HistoryRunning(h) || HistorySucceeded(h) {
		t.Fatal("live finite work lost")
	}
	h.SessionStates = map[string]SessionState{"sess-8": {ExitCode: &zero}}
	if HistoryRunning(h) || !HistorySucceeded(h) {
		t.Fatal("successful tombstone ignored")
	}
	h.SessionStates["sess-8"] = SessionState{ExitCode: &one}
	if HistorySucceeded(h) {
		t.Fatal("failed process passed")
	}
	h.SessionStates = map[string]SessionState{}
	if HistoryRunning(h) || HistorySucceeded(h) {
		t.Fatal("unknown tombstone must not pass or stay live")
	}
}

func TestMaskedServiceReceiptRetainsLivenessAndExercise(t *testing.T) {
	zero := 0
	h := []HistoryTool{
		{Name: "exec", Args: `{"command":"python3 service.py --port 8123"}`, Output: "[observation omitted]", Sessions: []tools.SessionReceipt{{ID: "sess-9", Alive: true}}},
		{Name: "exec", Args: `{"command":"curl --fail http://localhost:8123/health"}`, Output: "ready", Sessions: []tools.SessionReceipt{{ID: "sess-10", ExitCode: &zero}}},
	}
	if !ServiceExercised(h, "service.py") {
		t.Fatal("masking lost service receipt liveness")
	}
	h[0].SessionStates = map[string]SessionState{"sess-9": {Alive: false}, "sess-10": {ExitCode: &zero}}
	if ServiceExercised(h, "service.py") {
		t.Fatal("stopped service survived authority refresh")
	}
}

func TestLegacySessionListKeepsPerProcessState(t *testing.T) {
	h := HistoryTool{Name: "exec_io", Args: `{"action":"list"}`, Output: "2 session(s)\n- sess-1 alive=true owner=run cmd=\"echo sess-99\"\n- sess-2 alive=false owner=run\n  | cited sess-1 alive=true"}
	ids := LiveSessions([]HistoryTool{h})
	if len(ids) != 1 || ids[0] != "sess-1" {
		t.Fatalf("mixed replay listing lost per-process state: %v", ids)
	}
}
