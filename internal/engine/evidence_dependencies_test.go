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

func TestImplicitProjectCheckTracksSourceManifest(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = t.TempDir()
	source := filepath.Join(r.Workdir, "main.go")
	if err := os.WriteFile(source, []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	tc := toolCall("check", "exec", `{"command":"go test ./..."}`)
	receipt := e.captureEvidenceDependencies(r, tc, tools.Result{Status: tools.StatusOK, Output: "ok"})
	if len(receipt) == 0 {
		t.Fatal("implicit inputs have no receipt")
	}
	ev := []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{tc}}, {Dependencies: receipt, ToolResult: &providers.ToolResult{CallID: "check", Name: "exec", Output: "ok"}}}
	stale := func() bool { return historyTools(ev)[0].DependenciesChanged }
	if err := os.WriteFile(filepath.Join(r.Workdir, "diagnostics.log"), []byte("output"), 0600); err != nil {
		t.Fatal(err)
	}
	if stale() {
		t.Fatal("unrelated diagnostic staled source check")
	}
	if err := os.WriteFile(filepath.Join(r.Workdir, "new.go"), []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	if !stale() {
		t.Fatal("new compiled source retained check")
	}
}

func TestCheckOutputLogIsNotAnInputDependency(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = t.TempDir()
	source := filepath.Join(r.Workdir, "main.py")
	log := filepath.Join(r.Workdir, "check.log")
	for _, p := range []string{source, log} {
		if err := os.WriteFile(p, []byte("contents"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tc := toolCall("check", "exec", `{"command":"python3 main.py > check.log"}`)
	receipt := e.captureEvidenceDependencies(r, tc, tools.Result{Status: tools.StatusOK, Output: "exit: 0"})
	if _, ok := receipt[log]; ok {
		t.Fatal("stdout destination became an input")
	}
	if _, ok := receipt[source]; !ok {
		t.Fatal("executable source missing")
	}
	if err := os.WriteFile(log, []byte("later diagnostic"), 0600); err != nil {
		t.Fatal(err)
	}
	ev := []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{tc}}, {Dependencies: receipt, ToolResult: &providers.ToolResult{CallID: "check", Name: "exec", Output: "exit: 0"}}}
	if historyTools(ev)[0].DependenciesChanged {
		t.Fatal("log rewrite invalidated check")
	}
}
