package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestNativeWindowsDeliverableIdentity(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = t.TempDir()
	path := filepath.Join(r.Workdir, "Main.py")
	if err := os.WriteFile(path, []byte("print(1)"), 0600); err != nil {
		t.Fatal(err)
	}
	r.Request = "Create and update " + strings.ToUpper(path)
	check := trialCall("exec", map[string]string{"command": "python3 main.py"})
	receipt := e.captureEvidenceDependencies(r, check, tools.Result{Status: tools.StatusOK, Output: "exit: 0\n1"})
	r.History = append(writeHistory(strings.ToLower(path)), []ctxcompiler.ActivityEvent{
		{ToolCalls: []providers.ToolCall{check}},
		{Dependencies: receipt, ToolResult: &providers.ToolResult{CallID: check.ID, Name: "exec", Output: "exit: 0\n1"}},
	}...)
	files := e.deliverables(r, r.Request, historyTools(r.History))
	if len(files) != 1 || !files[0].Program {
		t.Fatalf("native program missing: %+v", files)
	}
	if last := lastEditOf(historyTools(r.History), files[0]); last != 0 {
		t.Fatalf("case alias lost edit: %d", last)
	}
	if reasons := e.deliverableHardReasons(r, r.Request, historyTools(r.History)); len(reasons) != 0 {
		t.Fatalf("verified native program refused: %v", reasons)
	}
}

func TestNativeWindowsProbeCopyCaseAliases(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, strings.ToUpper(original), strings.ToUpper(copy))
	if len(r.probes.copies) != 1 {
		t.Fatalf("case alias copy missing: %+v", r.probes)
	}
	editTrialCopy(t, e, r, copy, "alpha\nfixed\n")
	check := trialCall("exec", map[string]string{"command": "python3 validate.py " + strings.ToUpper(copy)})
	observeTrial(t, e, r, check, "exit: 0\n0 warnings\n", nil)
	if !strings.Contains(probeNotes(r), "the deliverable at "+original+" is unchanged") {
		t.Fatalf("case alias reminder missing: %s", probeNotes(r))
	}
}
