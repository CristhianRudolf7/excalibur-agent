package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestEvidenceReceiptSurvivesDiagnosticWrite(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{ExplicitFinish: true})
	r.Workdir = t.TempDir()
	path := filepath.Join(r.Workdir, "main.py")
	dep := filepath.Join(r.Workdir, "helper.py")
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "import helper\nprint(helper.value)")
	write(dep, "value=1")
	r.Request = "Create and update " + path
	tc := toolCall("check", "exec", `{"command":"python3 main.py"}`)
	receipt := e.captureEvidenceDependencies(r, tc, tools.Result{Status: tools.StatusOK, Output: "1\nexit: 0"})
	if _, ok := receipt[dep]; !ok {
		t.Fatal("local import missing from receipt")
	}
	r.History = append(writeHistory(path), []ctxcompiler.ActivityEvent{
		{Seq: 2, Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{tc}},
		{Seq: 3, Role: providers.RoleTool, Dependencies: receipt, ToolResult: &providers.ToolResult{CallID: "check", Name: "exec", Output: "1\nexit: 0"}},
	}...)
	write(filepath.Join(r.Workdir, "diagnostics.log"), "a later diagnostic")
	history := historyTools(r.History)
	if p := tasks.CheckStrongExercise(history, []tasks.DraftCriterion{{ID: "D1", Method: "command", Key: path}}, 1); len(p) > 0 {
		t.Fatal(p)
	}
	if why := e.deliverableHardReasons(r, r.Request, history); len(why) > 0 {
		t.Fatal(why)
	}
	write(dep, "value=2")
	history = historyTools(r.History)
	if !history[len(history)-1].DependenciesChanged {
		t.Fatal("import edit not detected")
	}
	if why := e.deliverableHardReasons(r, r.Request, history); len(why) == 0 {
		t.Fatalf("stale imported dependency accepted: files=%+v history=%+v", e.deliverables(r, r.Request, history), history)
	}
}
