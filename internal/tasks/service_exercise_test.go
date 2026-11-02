package tasks

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestLiveServiceClientCountsAsExercise(t *testing.T) {
	history := []HistoryTool{
		{Name: "write_file", Args: `{"path":"/app/service.py"}`, Output: "written"},
		{Name: "exec", Args: `{"command":"python3 /app/service.py --port 8765"}`, Output: "exit: running\nsession: sess-1 alive: true"},
		{Name: "exec", Args: `{"command":"python3 -c 'import socket; socket.create_connection((\"localhost\",8765))'"}`, Output: "exit: 0\nconnected"},
	}
	drafts := []DraftCriterion{{Key: "service.py", Method: types.CriterionCommand, Check: "python3 service.py"}}
	if p := CheckStrongExercise(history, drafts, 1); len(p) != 0 {
		t.Fatalf("live service rejected: %v", p)
	}
	t.Setenv("EXCALIBUR_SERVICE_EXERCISE", "0")
	if p := CheckStrongExercise(history, drafts, 1); len(p) == 0 {
		t.Fatal("disabled policy accepted live server")
	}
	t.Setenv("EXCALIBUR_SERVICE_EXERCISE", "1")
	history[2].Output = "exit: 1\nconnection failed"
	if ServiceExercised(history, "service.py") {
		t.Fatal("failed client accepted")
	}
	history[2].Output = "exit: 0\nconnected"
	history = append(history, HistoryTool{Name: "exec_io", Args: `{"session":"sess-1"}`, Output: "session: sess-1 exited: true"})
	if ServiceExercised(history, "service.py") {
		t.Fatal("stopped server accepted")
	}
}

func TestVerificationAfterEditDoesNotBecomeMutation(t *testing.T) {
	history := []HistoryTool{
		{Name: "write_file", Args: "/app/service.py", Output: "written"},
		{Name: "exec", Args: `{"command":"python3 -c 'assert check(\"make tee --output\")'"}`, Output: "exit: 0\nchecks passed"},
	}
	drafts := []DraftCriterion{{Key: "service.py", Method: types.CriterionCommand, Check: "python3 service.py"}}
	for _, pending := range CheckStrongExercise(history, drafts, 1) {
		if strings.Contains(pending, "changes since") {
			t.Fatal(pending)
		}
	}
	if p := CheckFreshVerification(history, 1); len(p) != 0 {
		t.Fatalf("fresh verification rejected: %v", p)
	}
	t.Setenv("EXCALIBUR_VERIFICATION_FRESHNESS", "0")
	if lastChangeIndex(history) != 1 {
		t.Fatal("disabled policy no longer demonstrates old substring mutation")
	}
}
