package tasks

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"testing"
)

func TestEvidenceFreshnessIgnoresUnrelatedWrites(t *testing.T) {
	history := []HistoryTool{
		{Name: "write_file", Args: `{"path":"/work/main.py"}`, Output: "written"},
		{Name: "exec", Args: `{"command":"python3 /work/main.py"}`, Output: "passed\nexit: 0"},
		{Name: "write_file", Args: `{"path":"/work/diagnostics.log"}`, Output: "written"},
		{Name: "exec", Args: `{"command":"printf done > /work/notes.txt"}`, Output: "exit: 0"},
	}
	drafts := []DraftCriterion{{ID: "D1", Method: types.CriterionCommand, Key: "/work/main.py"}}
	if p := CheckFreshVerification(history, 1); len(p) > 0 {
		t.Fatal(p)
	}
	if p := CheckStrongExercise(history, drafts, 1); len(p) > 0 {
		t.Fatal(p)
	}
	history = append(history, HistoryTool{Name: "edit_file", Args: `{"path":"/work/main.py"}`, Output: "edited"})
	if p := CheckStrongExercise(history, drafts, 1); len(p) == 0 {
		t.Fatal("dependency edit retained stale check")
	}
	t.Setenv("EXCALIBUR_EVIDENCE_DEPENDENCIES", "0")
	if p := CheckFreshVerification(history[:3], 1); len(p) == 0 {
		t.Fatal("legacy switch ignored")
	}
}

func TestEvidenceReceiptInvalidation(t *testing.T) {
	h := []HistoryTool{{Name: "exec", Args: `{"command":"python3 main.py"}`, Output: "passed", Dependencies: map[string]string{"main.py": "v1"}}}
	if !EvidenceFresh(h, 0, nil) {
		t.Fatal("unchanged receipt stale")
	}
	h[0].DependenciesChanged = true
	if EvidenceFresh(h, 0, nil) {
		t.Fatal("changed dependency reused")
	}
}
