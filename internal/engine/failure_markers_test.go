package engine

import (
	"encoding/json"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func TestFailureMarkersDistinguishMeasurements(t *testing.T) {
	for _, tc := range []struct {
		command, output string
		fail            bool
	}{
		{"python3 measure.py", "exit: 0\nError: 0.0123", false},
		{"python3 measure.py", "exit: 0\nerror rate 3%", false},
		{"go test ./...", "exit: 0\nError: 0.0123", false},
		{"python3 measure.py", "exit: 0\nerror: estimated uncertainty", false},
		{"go test ./...", "exit: 0\nerror: could not load package", true},
		{"pytest", "exit: 0\n2 failed, 1 passed", true},
		{"pytest", "exit: 0\n0 failed, 3 passed", false},
		{"python3 check.py", "exit: 0\nFAIL: wrong value", true},
		{"python3 check.py", "exit: 0\nAssertionError: value", true},
		{"python3 check.py", "exit: 0\nTraceback (most recent call last):", true},
		{"python3 check.py", "exit: 2\nError: 0.0123", true},
	} {
		t.Run(tc.output, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tc.command})
			failed, why := execFailed(tasks.HistoryTool{Name: "exec", Args: string(args), Output: tc.output})
			if failed != tc.fail {
				t.Fatalf("failed=%v reason=%s", failed, why)
			}
		})
	}
	t.Setenv("EXCALIBUR_PRECISE_FAILURE_MARKERS", "0")
	if failureMarker("Error: 0.0123") == "" {
		t.Fatal("legacy switch ignored")
	}
}
