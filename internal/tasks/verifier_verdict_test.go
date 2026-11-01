package tasks

import (
	"encoding/json"
	"testing"
)

func TestMetadataProbesDoNotExerciseBehaviour(t *testing.T) {
	for _, command := range []string{"test -s /tmp/check.py && ls -l /tmp/check.py", "stat /tmp/assert.json", "wc -c /tmp/verify.py", "ls -l ./program.sh | wc -l", "cd /tmp && test -s program.py && echo verified", "[ -s /tmp/program.py ]", "bash -lc 'test -s check.py && wc -c check.py'", "/usr/bin/stat check.py", "command ls ./check.py"} {
		args, _ := json.Marshal(map[string]string{"command": command})
		h := HistoryTool{Name: "exec", Args: string(args), Output: "ok"}
		if !ExistenceOnlyEvidence(h) || IsExercisingCall(h) {
			t.Fatalf("metadata accepted: %s", command)
		}
	}
	for _, command := range []string{"test -s result.json && python3 -c 'assert 2+2==4'", "./program | wc -l", "curl -fsS localhost:8080 | wc -c", "python3 converter.py && diff expected actual"} {
		args, _ := json.Marshal(map[string]string{"command": command})
		if !IsExercisingCall(HistoryTool{Name: "exec", Args: string(args), Output: "ok"}) {
			t.Fatalf("behaviour rejected: %s", command)
		}
	}
	// Size checks are observations: they do not exercise a deliverable
	// even with the verdict ablation on (the legacy marker rule counted
	// `wc ` as a check; EXCALIBUR_EXERCISE_STRICT removes that).
	t.Setenv("EXCALIBUR_VERIFIER_VERDICT", "off")
	if IsExercisingCall(HistoryTool{Name: "exec", Args: `{"command":"wc -c check.py"}`, Output: "17"}) {
		t.Fatal("size check must not exercise under the strict rule")
	}
	t.Setenv("EXCALIBUR_EXERCISE_STRICT", "off")
	if !IsExercisingCall(HistoryTool{Name: "exec", Args: `{"command":"wc -c check.py"}`, Output: "17"}) {
		t.Fatal("ablation ignored")
	}
}

func TestOverallVerdictRequired(t *testing.T) {
	if ParseStrongVerdict("D1: PASS - ran program", nil).Explicit {
		t.Fatal("draft alone is not verdict")
	}
	if !ParseStrongVerdict("D1: PASS - ran program\nVERDICT: PASS", nil).Pass {
		t.Fatal("explicit pass rejected")
	}
	if ParseStrongVerdict("D1: FAIL - wrong output\nVERDICT: PASS", nil).Pass {
		t.Fatal("contradictory pass accepted")
	}
}

func TestBehaviouralRequirementsRejectMetadataOnly(t *testing.T) {
	history := []HistoryTool{{Name: "exec", Args: `{"command":"test -s result && wc -c result"}`, Output: "17"}}
	for _, request := range []string{"Program prints 17", "Service responds with OK", "Conversion correctness must be checked", "Accuracy at least 0.9"} {
		if len(BehaviouralEvidencePending(request, history)) == 0 {
			t.Fatal(request)
		}
	}
	if len(BehaviouralEvidencePending("Create a file named result", history)) != 0 {
		t.Fatal("existence requirement rejected")
	}
	history = append(history, HistoryTool{Name: "exec", Args: `{"command":"python3 -c 'assert actual == expected'"}`, Output: "ok"})
	if len(BehaviouralEvidencePending("Program prints 17", history)) != 0 {
		t.Fatal("behavioural check rejected")
	}
}

// The retry of an empty or truncated verdict doubles the output
// budget, bounded by the cap; the switch off keeps the first budget.
func TestVerdictRetryBudget(t *testing.T) {
	t.Setenv("EXCALIBUR_VERDICT_RETRY", "")
	if got := VerdictRetryBudget(8192); got != 16384 {
		t.Fatalf("retry budget = %d, want 16384", got)
	}
	if got := VerdictRetryBudget(20000); got != verdictRetryCap {
		t.Fatalf("retry budget must be capped at %d, got %d", verdictRetryCap, got)
	}
	if got := VerdictRetryBudget(0); got != 0 {
		t.Fatalf("non-positive budgets pass through, got %d", got)
	}
	t.Setenv("EXCALIBUR_VERDICT_RETRY", "off")
	if got := VerdictRetryBudget(8192); got != 8192 {
		t.Fatalf("switch off must keep the original budget, got %d", got)
	}
}
