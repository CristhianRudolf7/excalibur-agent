package engine

import (
	"encoding/json"
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
	write(filepath.Join(r.Workdir, "unrelated.py"), "a source file the script does not import")
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

func TestDiagnosticContentsDoNotEditNamedProgram(t *testing.T) {
	f := deliverable{Path: "/work/main.py", Base: "main.py", Program: true}
	history := []tasks.HistoryTool{
		{Name: "write_file", Args: `{"path":"/work/main.py","content":"print(1)"}`, Output: "written"},
		{Name: "exec", Args: `{"command":"python3 /work/main.py"}`, Output: "exit: 0\n1"},
		{Name: "write_file", Args: `{"path":"/work/diagnostics.log","content":"main.py passed"}`, Output: "written"},
		{Name: "write_file", Args: `{"path":"/work/other/main.py","content":"unrelated"}`, Output: "written"},
	}
	if last := lastEditOf(history, f); last != 0 {
		t.Fatalf("unrelated write became deliverable edit at %d", last)
	}
	if reasons := unexecutedProgramReasons("Create /work/main.py", history, []deliverable{f}); len(reasons) > 0 {
		t.Fatal(reasons)
	}
	t.Setenv("EXCALIBUR_EVIDENCE_DEPENDENCIES", "0")
	if last := lastEditOf(history, f); last != 3 {
		t.Fatal("legacy inference switch ignored", last)
	}
}

func TestReceiptOverridesAmbiguousRelativeWriteInference(t *testing.T) {
	f := deliverable{Path: "/work/pkg/main.py", Base: "main.py", Program: true}
	history := []tasks.HistoryTool{
		{Name: "write_file", Args: `{"path":"/work/pkg/main.py"}`, Output: "written"},
		{Name: "exec", Args: `{"command":"python3 pkg/main.py"}`, Output: "exit: 0\n1", Dependencies: map[string]string{f.Path: "same version"}},
		{Name: "write_file", Args: `{"path":"main.py","content":"different workspace file"}`, Output: "written"},
	}
	// The engine's receipt says the inspected /work/pkg/main.py is unchanged;
	// a basename-only inference cannot supersede that filesystem fact.
	if reasons := unexecutedProgramReasons("Create /work/pkg/main.py", history, []deliverable{f}); len(reasons) > 0 {
		t.Fatal(reasons)
	}
}

func TestCompiledExecutionUsesSuccessfulBuildReceipt(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{ExplicitFinish: true})
	r.Workdir = t.TempDir()
	source := filepath.Join(r.Workdir, "main.c")
	binary := filepath.Join(r.Workdir, "program")
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write(source, "int main(){return 0;}")
	write(binary, "\x7fELFretained")
	build := toolCall("build", "exec", `{"command":"cc main.c -o program"}`)
	receipt := e.captureEvidenceDependencies(r, build, tools.Result{Status: tools.StatusOK, Output: "exit: 0"})
	if len(receipt) == 0 {
		t.Fatal("missing compiler receipt")
	}
	r.History = []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{build}}, {Dependencies: receipt, ToolResult: &providers.ToolResult{CallID: "build", Name: "exec", Output: "exit: 0"}}}
	run := toolCall("run", "exec", `{"command":"./program"}`)
	check := func() bool {
		deps := e.captureEvidenceDependencies(r, run, tools.Result{Status: tools.StatusOK, Output: "exit: 0"})
		events := append(append([]ctxcompiler.ActivityEvent(nil), r.History...), ctxcompiler.ActivityEvent{ToolCalls: []providers.ToolCall{run}}, ctxcompiler.ActivityEvent{Dependencies: deps, ToolResult: &providers.ToolResult{CallID: "run", Name: "exec", Output: "exit: 0"}})
		h := historyTools(events)
		return h[len(h)-1].DependenciesChanged
	}
	if check() {
		t.Fatal("fresh successful build rejected")
	}
	write(source, "int main(){broken}")
	if !check() {
		t.Fatal("retained binary certified changed source")
	}
	failed := toolCall("failed", "exec", `{"command":"cc main.c -o program"}`)
	r.History = append(r.History, ctxcompiler.ActivityEvent{ToolCalls: []providers.ToolCall{failed}}, ctxcompiler.ActivityEvent{ToolResult: &providers.ToolResult{CallID: "failed", Name: "exec", Output: "exit: 1", IsError: true}})
	if !check() {
		t.Fatal("failed compiler certified retained binary")
	}
	write(source, "int main(){return 0;}")
	if !check() {
		t.Fatal("failed compiler ignored after source restored")
	}
	rebuilt := toolCall("rebuilt", "exec", `{"command":"cc main.c -o program && ./program"}`)
	fresh := e.captureEvidenceDependencies(r, rebuilt, tools.Result{Status: tools.StatusOK, Output: "exit: 0"})
	event := []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{rebuilt}}, {Dependencies: fresh, ToolResult: &providers.ToolResult{CallID: "rebuilt", Name: "exec", Output: "exit: 0"}}}
	if historyTools(event)[0].DependenciesChanged {
		t.Fatal("successful rebuild-and-run inherited an older failure")
	}
	t.Setenv("EXCALIBUR_BUILD_EVIDENCE", "off")
	if check() {
		t.Fatal("disabled build binding still applied")
	}
}

func TestMaskedCompilerFailureCannotCertify(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = t.TempDir()
	if err := os.WriteFile(filepath.Join(r.Workdir, "main.c"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	call := toolCall("masked", "exec", `{"command":"cc main.c -o program; echo done"}`)
	deps := e.captureEvidenceDependencies(r, call, tools.Result{Status: tools.StatusOK, Output: "exit: 0"})
	ev := []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{call}}, {Dependencies: deps, ToolResult: &providers.ToolResult{CallID: "masked", Name: "exec", Output: "exit: 0"}}}
	if !historyTools(ev)[0].DependenciesChanged {
		t.Fatal("masked compiler exit accepted")
	}
}

func TestYieldedCompilerReceiptFollowsSessionExit(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = t.TempDir()
	source := filepath.Join(r.Workdir, "main.c")
	binary := filepath.Join(r.Workdir, "program")
	if err := os.WriteFile(source, []byte("source v1"), 0600); err != nil {
		t.Fatal(err)
	}
	build := toolCall("build", "exec", `{"command":"cc main.c -o program"}`)
	pending := "exit: running  session: sess-1  alive: true"
	deps := e.captureEvidenceDependencies(r, build, tools.Result{Status: tools.StatusOK, Output: pending})
	if len(deps) == 0 {
		t.Fatal("yield lost source receipt")
	}
	if err := os.WriteFile(binary, []byte("\x7fELFcomplete"), 0700); err != nil {
		t.Fatal(err)
	}
	r.History = []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{build}}, {Dependencies: deps, ToolResult: &providers.ToolResult{CallID: "build", Name: "exec", Output: pending}}}
	io := toolCall("wait", "exec_io", `{"action":"read","session":"sess-1"}`)
	r.History = append(r.History, ctxcompiler.ActivityEvent{ToolCalls: []providers.ToolCall{io}}, ctxcompiler.ActivityEvent{ToolResult: &providers.ToolResult{CallID: "wait", Name: "exec_io", Output: "exit: 0  session: sess-1\nexited: true"}})
	run := toolCall("run", "exec", `{"command":"./program"}`)
	check := func() bool {
		deps := e.captureEvidenceDependencies(r, run, tools.Result{Status: tools.StatusOK, Output: "exit: 0"})
		ev := []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{run}}, {Dependencies: deps, ToolResult: &providers.ToolResult{CallID: "run", Name: "exec", Output: "exit: 0"}}}
		return historyTools(ev)[0].DependenciesChanged
	}
	if check() {
		t.Fatal("completed yielded compiler rejected")
	}
	if err := os.WriteFile(source, []byte("source v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if !check() {
		t.Fatal("yielded compiler recertified later source")
	}
}

func TestCompilerExitChains(t *testing.T) {
	for _, command := range []string{"cc main.c -o app", "cd src && cc main.c -o app && ./app"} {
		if !compilerExitUnmasked(command) {
			t.Fatal(command)
		}
	}
	for _, command := range []string{"cc main.c; echo ok", "cc main.c || true", "cc main.c | cat", "cc main.c &", "! cc main.c"} {
		if compilerExitUnmasked(command) {
			t.Fatal(command)
		}
	}
}

func TestNewCompiledProgramNeedsBuildProvenance(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = t.TempDir()
	binary := filepath.Join(r.Workdir, "program")
	if err := os.WriteFile(filepath.Join(r.Workdir, "main.c"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := tasks.TakeInputSnapshot(r.Workdir, tasks.InputSnapshotHashBudget)
	if err != nil {
		t.Fatal(err)
	}
	r.fin.snapshot = snapshot
	if err := os.WriteFile(binary, []byte("\x7fELFunknown"), 0700); err != nil {
		t.Fatal(err)
	}
	call := toolCall("run", "exec", `{"command":"./program"}`)
	stale := func() bool {
		deps := e.captureEvidenceDependencies(r, call, tools.Result{Status: tools.StatusOK, Output: "exit: 0"})
		events := []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{call}}, {Dependencies: deps, ToolResult: &providers.ToolResult{CallID: "run", Name: "exec", Output: "exit: 0"}}}
		return historyTools(events)[0].DependenciesChanged
	}
	if !stale() {
		t.Fatal("new compiled program passed without a compiler receipt")
	}
	// A supplied binary is an input and keeps its ordinary behavior evidence.
	r.fin.snapshot, err = tasks.TakeInputSnapshot(r.Workdir, tasks.InputSnapshotHashBudget)
	if err != nil {
		t.Fatal(err)
	}
	if stale() {
		t.Fatal("supplied binary acquired a local compilation obligation")
	}
}

func TestLoadedLibraryInheritsCompilerReceipt(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = t.TempDir()
	source := filepath.Join(r.Workdir, "core.c")
	library := filepath.Join(r.Workdir, "libcore.so")
	if err := os.WriteFile(source, []byte("source v1"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(library, []byte("\x7fELFlibrary"), 0700); err != nil {
		t.Fatal(err)
	}
	build := toolCall("build", "exec", `{"command":"cc -shared core.c -o libcore.so"}`)
	deps := e.captureEvidenceDependencies(r, build, tools.Result{Status: tools.StatusOK, Output: "exit: 0"})
	r.History = []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{build}}, {Dependencies: deps, ToolResult: &providers.ToolResult{CallID: "build", Name: "exec", Output: "exit: 0"}}}
	if err := os.WriteFile(source, []byte("broken source v2"), 0600); err != nil {
		t.Fatal(err)
	}
	call := toolCall("load", "exec", `{"command":"python3 -c 'import ctypes; ctypes.CDLL(\"./libcore.so\")'"}`)
	receipt := e.captureEvidenceDependencies(r, call, tools.Result{Status: tools.StatusOK, Output: "exit: 0"}, library)
	events := []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{call}}, {Dependencies: receipt, ToolResult: &providers.ToolResult{CallID: "load", Name: "exec", Output: "exit: 0"}}}
	if !historyTools(events)[0].DependenciesChanged {
		t.Fatal("retained shared library certified changed source")
	}
}

func TestCompilerOutputAssociation(t *testing.T) {
	for _, tc := range []struct{ command, want string }{
		{"cc main.c", "/work/a.out"},
		{"cc -c main.c", ""},
		{"cc -E main.c", ""},
		{"rustc main.rs", "/work/main"},
		{"cc main.c -o result", "/work/result"},
	} {
		args, _ := json.Marshal(map[string]string{"command": tc.command})
		info := compilerFor(tasks.HistoryTool{Name: "exec", Args: string(args)}, "/work")
		got := ""
		if len(info.outputs) > 0 {
			got = info.outputs[0]
		}
		if got != tc.want {
			t.Fatalf("%s: output %q want %q", tc.command, got, tc.want)
		}
	}
}
