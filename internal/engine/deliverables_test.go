// Tests for the deliverable hard checks (D3), soft warnings (D4, D9),
// the confirmation checklist (D5) and the verdict texts (D1, D2).
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// hist builds a tool-result history from entries.
func hist(entries ...tasks.HistoryTool) []tasks.HistoryTool { return entries }

// withPortProbe replaces the localhost probe for one test.
func withPortProbe(t *testing.T, open bool) {
	t.Helper()
	saved := portProbe
	portProbe = func(int) bool { return open }
	t.Cleanup(func() { portProbe = saved })
}

// tmpFile writes a file under t.TempDir and returns its path.
func tmpFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// D3b: the most recent verification after the last edit decides. A probe
// does not count, a failure marker or non-zero exit refuses with excerpt.
func TestVerificationFailureRefusesWithExcerpt(t *testing.T) {
	files := []deliverable{{Path: "/app/solve.py", Base: "solve.py", Program: true}}
	failing := hist(
		tasks.HistoryTool{Name: "write_file", Args: `{"path":"/app/solve.py","content":"x"}`, Output: "ok"},
		tasks.HistoryTool{Name: "exec", Args: `{"command":"python3 /app/test_solve.py"}`,
			Output: "exit: 1  duration: 0.2s  session: sess-1  alive: false\nAssertionError: 2 != 3"},
	)
	if reason := verificationFailureReason(failing, files); !strings.Contains(reason, "AssertionError") || !strings.Contains(reason, "exit 1") {
		t.Fatalf("failing verification must refuse with its excerpt, got %q", reason)
	}
	probe := hist(
		tasks.HistoryTool{Name: "write_file", Args: `{"path":"/app/solve.py","content":"x"}`, Output: "ok"},
		tasks.HistoryTool{Name: "exec", Args: `{"command":"test -s /app/solve.py"}`,
			Output: "exit: 1  duration: 0.0s  session: sess-2  alive: false"},
	)
	if reason := verificationFailureReason(probe, files); reason != "" {
		t.Fatalf("a probe must not count as verification: %q", reason)
	}
	summary := hist(
		tasks.HistoryTool{Name: "write_file", Args: `{"path":"/app/solve.py","content":"x"}`, Output: "ok"},
		tasks.HistoryTool{Name: "exec", Args: `{"command":"python3 /app/test_solve.py"}`,
			Output: "exit: 0  duration: 0.2s  session: sess-3  alive: false\nPASS 3\nFAIL: 0"},
	)
	if reason := verificationFailureReason(summary, files); reason != "" {
		t.Fatalf("a zero failure count must pass: %q", reason)
	}
	stale := hist(
		tasks.HistoryTool{Name: "exec", Args: `{"command":"python3 /app/test_solve.py"}`,
			Output: "exit: 1  duration: 0.2s  session: sess-4  alive: false\nFAILED"},
		tasks.HistoryTool{Name: "write_file", Args: `{"path":"/app/solve.py","content":"y"}`, Output: "ok"},
	)
	if reason := verificationFailureReason(stale, files); reason != "" {
		t.Fatalf("a failure before the last edit is stale: %q", reason)
	}
}

// D3c: a program must run with exit 0 after its last edit, unless the
// instruction says not to run it.
func TestUnexecutedProgramRefusesUntilRunAfterEdit(t *testing.T) {
	files := []deliverable{{Path: "/app/run.py", Base: "run.py", Program: true}}
	write := tasks.HistoryTool{Name: "write_file", Args: `{"path":"/app/run.py","content":"print(3)"}`, Output: "ok"}
	ran := tasks.HistoryTool{Name: "exec", Args: `{"command":"python3 /app/run.py"}`, Output: "exit: 0  duration: 0.1s  session: sess-1  alive: false\n3"}
	if got := unexecutedProgramReasons("Create /app/run.py that prints 3.", hist(write), files); len(got) != 1 {
		t.Fatalf("written and never run must refuse, got %v", got)
	}
	if got := unexecutedProgramReasons("Create /app/run.py that prints 3.", hist(write, ran), files); len(got) != 0 {
		t.Fatalf("run with exit 0 after the edit must pass, got %v", got)
	}
	if got := unexecutedProgramReasons("Create /app/run.py that prints 3.", hist(ran, write), files); len(got) != 1 {
		t.Fatalf("a run before the last edit does not count, got %v", got)
	}
	if got := unexecutedProgramReasons("Create /app/run.py. Do not run it.", hist(write), files); len(got) != 0 {
		t.Fatalf("an instruction forbidding the run must exempt it, got %v", got)
	}
}

// D3d: a live package manager or build refuses; a non-service process that
// wrote a deliverable in the last 10 seconds refuses; a service does not.
func TestRunningWorkRefusesPackageManagerAndRecentWriter(t *testing.T) {
	apt := hist(tasks.HistoryTool{Name: "exec", Args: `{"command":"apt-get install -y jq"}`,
		Output: "exit: running  duration: 1.0s  session: sess-1  alive: true  next_offset: 0"})
	if got := runningWorkReasons(apt, nil, time.Now()); len(got) != 1 || !strings.Contains(got[0], "package manager") {
		t.Fatalf("live apt must refuse, got %v", got)
	}
	gen := tmpFile(t, "out.bin", "data")
	writer := hist(tasks.HistoryTool{Name: "exec", Args: `{"command":"python3 gen.py"}`,
		Output: "exit: running  duration: 1.0s  session: sess-2  alive: true  next_offset: 0"})
	files := []deliverable{{Path: gen, Base: "out.bin"}}
	if got := runningWorkReasons(writer, files, time.Now()); len(got) != 1 || !strings.Contains(got[0], "in the last 10 s") {
		t.Fatalf("recent writer must refuse, got %v", got)
	}
	server := hist(tasks.HistoryTool{Name: "exec", Args: `{"command":"python3 -m http.server 8000 --bind localhost"}`,
		Output: "exit: running  duration: 1.0s  session: sess-3  alive: true  next_offset: 0"})
	if got := runningWorkReasons(server, files, time.Now()); len(got) != 0 {
		t.Fatalf("a service may keep running, got %v", got)
	}
}

// D3e: a proof file with an escape token refuses, with its line.
func TestProofEscapeRefuses(t *testing.T) {
	bad := tmpFile(t, "proof.v", "Lemma t : True.\nProof.\n  admit.\nQed.\n")
	if got := proofEscapeReasons([]deliverable{{Path: bad}}); len(got) != 1 || !strings.Contains(got[0], "line 3") {
		t.Fatalf("admit must refuse at line 3, got %v", got)
	}
	clean := tmpFile(t, "clean.v", "Lemma t : True.\nProof. exact I. Qed.\n")
	if got := proofEscapeReasons([]deliverable{{Path: clean}}); len(got) != 0 {
		t.Fatalf("a complete proof must pass, got %v", got)
	}
}

// D3f: a port the instruction says must serve is probed; nothing listening
// refuses.
func TestDeclaredServiceProbe(t *testing.T) {
	req := "Serve the API on port 18081 and keep it listening."
	withPortProbe(t, false)
	if got := declaredServiceReasons(req, "", 8); len(got) != 1 || !strings.Contains(got[0], "18081") {
		t.Fatalf("closed port must refuse, got %v", got)
	}
	withPortProbe(t, true)
	if got := declaredServiceReasons(req, "", 8); len(got) != 0 {
		t.Fatalf("answering port must pass, got %v", got)
	}
}

// D3a/item 100: a file provided before the run is not a deliverable unless
// the instruction asks to modify it; creating it is not the same as
// modifying it.
func TestPreexistingInputIsNotADeliverable(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.csv")
	if err := os.WriteFile(input, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.Workdir = dir
	run.StartedAt = time.Now().Add(time.Hour) // every existing file predates the run
	run.Request = "Create " + input + " with the summary table."
	if got := e.deliverables(run, run.Request, nil); len(got) != 0 {
		t.Fatalf("provided input named as output must not be a deliverable: %+v", got)
	}
	run.Request = "Write an update to " + input + " with the summary table."
	if got := e.deliverables(run, run.Request, nil); len(got) != 1 {
		t.Fatalf("an instruction to modify the input makes it a deliverable: %+v", got)
	}
}

// D4a: a provided file that changed without a request to change it gets a
// soft warning; the snapshot is taken before the first model action.
func TestSoftWarningInputModified(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(data, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXCALIBUR_INPUT_SNAPSHOT", "on")
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.Workdir = dir
	run.Request = "Summarise " + data + "."
	e.startInputSnapshot(run)
	if err := os.WriteFile(data, []byte("CHANGED!"), 0o644); err != nil {
		t.Fatal(err)
	}
	warnings := e.softFinishWarnings(run, run.Request, nil)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "was modified") {
		t.Fatalf("modified provided input must warn once, got %v", warnings)
	}
}

// D4b: reference-named files read by the run, timing reassignments, weakened
// checks and widened tolerances in the deliverable warn.
func TestSoftWarningsDeliverableContent(t *testing.T) {
	refs := []string{"expected_output.txt"}
	if got := deliverableContentWarnings(deliverable{Path: "/app/solve.py", Base: "solve.py"},
		"import time\ntime.perf_counter = lambda: 0.0\n", nil); len(got) != 1 || !strings.Contains(got[0], "timing") {
		t.Fatalf("timing reassignment must warn, got %v", got)
	}
	if got := deliverableContentWarnings(deliverable{Path: "/app/test_solve.py", Base: "test_solve.py"},
		"@pytest.mark.skip\ndef test_x():\n    pass\n", nil); len(got) != 1 || !strings.Contains(got[0], "weakens") {
		t.Fatalf("skipped test must warn, got %v", got)
	}
	if got := deliverableContentWarnings(deliverable{Path: "/app/test_solve.py", Base: "test_solve.py"},
		"assert_close(a, b, tol = 0.5)\n", nil); len(got) != 1 || !strings.Contains(got[0], "tolerance") {
		t.Fatalf("widened tolerance in a test must warn, got %v", got)
	}
	if got := deliverableContentWarnings(deliverable{Path: "/app/solve.py", Base: "solve.py"},
		"open(\"expected_output.txt\")\n", refs); len(got) != 1 || !strings.Contains(got[0], "reference") {
		t.Fatalf("reading a shipped reference must warn, got %v", got)
	}
	if got := deliverableContentWarnings(deliverable{Path: "/app/solve.py", Base: "solve.py"},
		"print(total)\n", refs); len(got) != 0 {
		t.Fatalf("clean code must not warn, got %v", got)
	}
}

// D4c (item 107): an example value from the instruction embedded verbatim
// in a program warns; a computed value does not.
func TestSoftWarningExampleValueEmbedded(t *testing.T) {
	req := "For example, the output for the sample input is '4815162342'. Print the total for the real input."
	f := deliverable{Path: "/app/total.py", Base: "total.py", Program: true}
	if got := exampleValueWarnings(f, "print('4815162342')\n", req); len(got) != 1 {
		t.Fatalf("embedded example must warn, got %v", got)
	}
	if got := exampleValueWarnings(f, "print(sum(read_input()))\n", req); len(got) != 0 {
		t.Fatalf("computed value must not warn, got %v", got)
	}
}

// D9: a shipped script that runs after a deliverable's last edit and names
// it warns that it may overwrite the deliverable.
func TestSoftWarningBaselineOverwrite(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "baseline_writer.py")
	if err := os.WriteFile(script, []byte("open('out.bin', 'w').write('x')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := tasks.TakeInputSnapshot(dir, tasks.InputSnapshotHashBudget)
	if err != nil || snap == nil {
		t.Fatalf("snapshot: %v %v", snap, err)
	}
	files := []deliverable{{Path: filepath.Join(dir, "out.bin"), Base: "out.bin"}}
	history := hist(
		tasks.HistoryTool{Name: "write_file", Args: `{"path":"` + files[0].Path + `","content":"x"}`, Output: "ok"},
		tasks.HistoryTool{Name: "exec", Args: `{"command":"python3 baseline_writer.py"}`, Output: "exit: 0"},
	)
	if got := baselineOverwriteWarnings(snap, files, history); len(got) != 1 || !strings.Contains(got[0], "baseline_writer.py") {
		t.Fatalf("baseline run after the last edit must warn, got %v", got)
	}
}

// D5: the checklist names the requirement sentences, numbered, and the
// verdict of the first passing finish carries it.
func TestChecklistNamesRequirements(t *testing.T) {
	req := "The reader must finish in at most 200 ms. Output exactly three rows. Write the rows to /app/rows.csv. Use the weather mention for flavour."
	items := checklistItems(req, 12)
	if len(items) != 3 {
		t.Fatalf("checklist items = %v, want the three requirement sentences", items)
	}
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.Request = req
	text := e.confirmationText(run)
	if !strings.Contains(text, "finish checks passed.") || !strings.Contains(text, "Before the run ends, confirm: 1)") ||
		!strings.Contains(text, "measured value") || !strings.Contains(text, "Call finish again") {
		t.Fatalf("confirmation text = %q", text)
	}
}

// D1: a refused finish's tool result is the refusal text itself, numbered
// and with the concrete evidence; the next model request carries it.
func TestRefusalIsTheFinishToolResult(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.mu.Lock()
	run.History = append(run.History, historyEventForTool(run.nextHistorySeqLocked(),
		providers.ToolCall{ID: "f1", Name: FinishToolName}, tools.Result{Status: tools.StatusOK, Output: "finish received"}))
	run.mu.Unlock()
	e.refuseCall(context.Background(), run, providers.ToolCall{ID: "f1", Name: FinishToolName},
		[]string{"required artifact missing: /tmp/report — create it as stated"})
	run.mu.Lock()
	last := run.History[len(run.History)-1].ToolResult
	run.mu.Unlock()
	if last == nil || !strings.HasPrefix(last.Output, "finish refused (attempt 1): 1) ") || !last.IsError {
		t.Fatalf("finish tool result must be the refusal text, got %+v", last)
	}
	if !strings.HasSuffix(last.Output, "Fix these, then call finish again.") {
		t.Fatalf("refusal must end with the next action: %q", last.Output)
	}
}

// D2: an unchanged finish after a refusal is answered from the same reasons;
// any tool work lifts the fingerprint.
func TestNoChangeRefusalFingerprint(t *testing.T) {
	run := explicitRun()
	if _, ok := run.unchangedRefusal(); ok {
		t.Fatal("no refusal yet: nothing to repeat")
	}
	run.rememberRefusal([]string{"required artifact missing: /tmp/x"})
	reasons, ok := run.unchangedRefusal()
	if !ok || len(reasons) != 1 {
		t.Fatalf("unchanged refusal must repeat its reasons, got %v %v", reasons, ok)
	}
	run.mu.Lock()
	run.History = append(run.History, historyEventForTool(run.nextHistorySeqLocked(),
		providers.ToolCall{ID: "c1", Name: "exec"}, tools.Result{Status: tools.StatusOK, Output: "finish received"}))
	run.mu.Unlock()
	if _, ok := run.unchangedRefusal(); ok {
		t.Fatal("tool work after the refusal must lift the no-change rule")
	}
}

// D2 (item 99): a loop guard never ends an explicit-finish run while wall
// time remains; the run escalates and keeps working.
func TestLoopGuardNeverTerminalWhileTimeRemains(t *testing.T) {
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.Tracker.Limit = tasks.Limit{MaxSeconds: 3600}
	run.StartedAt = time.Now().UTC()
	run.HeartbeatAt = run.StartedAt
	startRun(t, e, run, js)
	stop, err := e.onDetection(context.Background(), run, tasks.Detection{Action: tasks.ActionBlock, Note: "repeated edit and run"})
	if err != nil || stop {
		t.Fatalf("loop guard must not stop the run: stop=%v err=%v", stop, err)
	}
	mustState(t, run, types.RunRunning)
}

// D2 (items 47, 48): after an accepted finish the state is terminal: a
// later failure is refused by the transition table and changes nothing.
func TestTerminalStateSurvivesLaterFailure(t *testing.T) {
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	startRun(t, e, run, js)
	run.mu.Lock()
	run.state = types.RunCompleted
	run.mu.Unlock()
	if err := e.fail(context.Background(), run, "model call: 429 quota exhausted"); err == nil {
		t.Fatal("failing a completed run must be refused")
	}
	mustState(t, run, types.RunCompleted)
}

// D2: completed_with_pending is reachable from verifying when only soft
// items remain near the wall edge.
func TestSoftItemsCompleteWithPending(t *testing.T) {
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	startRun(t, e, run, js)
	run.mu.Lock()
	run.state = types.RunVerifying
	run.mu.Unlock()
	if err := e.completeWithPending(context.Background(), run, []string{"independent verifier unavailable"}); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	if !allSoftReasons([]string{"independent verifier unavailable", "constraint holds: the table is sorted"}) ||
		allSoftReasons([]string{"required artifact missing: /app/out.txt"}) {
		t.Fatal("soft classification drifted")
	}
}

func TestSoftVerifierBudgetAndIncompleteVerdict(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	if !run.takeVerifierRefusal() || run.takeVerifierRefusal() {
		t.Fatal("verifier may refuse only once")
	}
	ok, pending := e.verifierUnverified(run, "empty verifier verdict")
	if !ok || len(pending) != 0 {
		t.Fatalf("incomplete verdict must be advisory: %v %v", ok, pending)
	}
	if !strings.Contains(e.confirmationText(run), "empty verifier verdict") {
		t.Fatal("missing advisory checklist note")
	}
	run.Profile.ExplicitFinish = false
	if ok, _ := e.verifierUnverified(run, "empty verifier verdict"); ok {
		t.Fatal("interactive verification must retain its contract")
	}
}

func TestBlockedReasonNeedsCommandAndOutput(t *testing.T) {
	history := []tasks.HistoryTool{{Name: "exec", Args: `{"command":"make build"}`, Output: "exit: 1\ncompiler: permission denied", IsError: true}}
	for _, reason := range []string{"permission denied", "make build failed"} {
		if reasonQuotesFailure(reason, history) {
			t.Fatalf("incomplete evidence accepted: %q", reason)
		}
	}
	if !reasonQuotesFailure("make build: compiler: permission denied", history) {
		t.Fatal("complete evidence rejected")
	}
}

func TestDeclaredLocalURLProbe(t *testing.T) {
	withPortProbe(t, false)
	got := declaredServiceReasons("Keep the service available at http://localhost:18991/api.", "", 8)
	if len(got) == 0 || !strings.Contains(strings.Join(got, ";"), "18991") {
		t.Fatalf("local URL not probed: %v", got)
	}
	if got := declaredServiceReasons("Keep the service available at https://example.com/api.", "", 8); len(got) != 0 {
		t.Fatalf("external URL must not cause localhost probe: %v", got)
	}
}

func TestProgramDetectionExecutableAndNamedOutput(t *testing.T) {
	p := tmpFile(t, "worker", "binary data")
	if err := os.Chmod(p, 0755); err != nil {
		t.Fatal(err)
	}
	if !isProgramPath(p) {
		t.Fatal("executable output must be treated as a program")
	}
	if !instructionRunsPath("Create /app/worker then run /app/worker.", "/app/worker") {
		t.Fatal("named program must run")
	}
	if instructionRunsPath("Create /app/worker without running it.", "/app/worker") {
		t.Fatal("no-run instruction must be respected")
	}
}

func TestDeliverablesExcludeAgentScratchPaths(t *testing.T) {
	e := &Engine{}
	run := &Run{Request: "Create /app/out.txt with the result.", Workdir: "/app"}
	history := hist(
		tasks.HistoryTool{Name: "write_file", Args: `{"path":"/tmp/check.py","content":"print(1)"}`},
		tasks.HistoryTool{Name: "write_file", Args: `{"path":"/app/helper.py","content":"print(1)"}`},
		tasks.HistoryTool{Name: "write_file", Args: `{"path":"/app/out.txt","content":"result"}`},
	)
	files := e.deliverables(run, run.Request, history)
	if len(files) != 1 || files[0].Path != "/app/out.txt" {
		t.Fatalf("only instruction paths are deliverables: %+v", files)
	}
	if got := unexecutedProgramReasons(run.Request, history, files); len(got) != 0 {
		t.Fatalf("scratch scripts cannot require execution: %v", got)
	}
	t.Setenv("EXCALIBUR_INSTRUCTION_DELIVERABLES", "0")
	if got := e.deliverables(run, run.Request, history); len(got) != 3 {
		t.Fatalf("compatibility switch must restore write inference: %+v", got)
	}
}

func TestInputCopiesInFinishChecklist(t *testing.T) {
	for _, kind := range []string{"modified", "vanished", "requested modification", "switch off"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("EXCALIBUR_INPUT_SNAPSHOT", "on")
			t.Setenv("EXCALIBUR_INPUT_COPIES", "on")
			if kind == "switch off" {
				t.Setenv("EXCALIBUR_INPUT_COPIES", "off")
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "provided.txt")
			if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
				t.Fatal(err)
			}
			e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
			run := explicitRun()
			run.Workdir = dir
			run.Request = "Summarise " + path + "."
			run.SetInputCopyDir(filepath.Join(t.TempDir(), "originals"))
			e.startInputSnapshot(run)
			if kind == "vanished" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
				t.Fatal(err)
			}
			if kind == "requested modification" {
				run.Request = "Modify " + path + "."
			}
			checklist := e.confirmationText(run)
			copy := run.fin.snapshot.Entries[path].OriginalCopy
			if kind == "requested modification" || kind == "switch off" {
				if strings.Contains(checklist, "original copy:") {
					t.Fatalf("unexpected restore warning: %s", checklist)
				}
				return
			}
			want := "restore with restore_input path=" + path
			if copy == "" || !strings.Contains(checklist, want) {
				t.Fatalf("missing restore hint %q: %s", want, checklist)
			}
			raw, err := os.ReadFile(copy)
			if err != nil || string(raw) != "original" {
				t.Fatalf("original bytes: %q %v", raw, err)
			}
		})
	}
}

func TestRestoreHintQuotesShellPaths(t *testing.T) {
	if got := shellRestorePath("/tmp/input file's.txt"); got != `'/tmp/input file'"'"'s.txt'` {
		t.Fatalf("unsafe restore path: %s", got)
	}
}
