package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// execTestTool returns an ExecTool on a fresh session table with
// provisioning off, so failing commands never install packages.
func execTestTool(t *testing.T) *ExecTool {
	t.Helper()
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv(AutoProvisionEnv, "off")
	return &ExecTool{Sessions: NewSessionManager(TerminalDeps{})}
}

// callTool invokes a tool with JSON-encoded arguments.
func callTool(t *testing.T, tool Tool, args map[string]any) Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// closeSessions stops every session so no process outlives the test.
func closeSessions(m *SessionManager) {
	for _, s := range m.List("") {
		_ = m.Close(s.ID)
	}
}

// elisionMarkerRE matches the marker printed for elided output.
var elisionMarkerRE = regexp.MustCompile(`…\[middle (\d+) bytes elided; full output: get_tool_output ref=(\S+) offset=(\d+)\]…`)

// elisionParts returns the elided byte count, ref and offset from a marker.
func elisionParts(t *testing.T, out string) (elided int, ref string, offset int) {
	t.Helper()
	m := elisionMarkerRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no elision marker in output: %.300s", out)
	}
	elided, _ = strconv.Atoi(m[1])
	offset, _ = strconv.Atoi(m[3])
	return elided, m[2], offset
}

// seqText is the output of `seq 1 n`.
func seqText(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%d\n", i)
	}
	return b.String()
}

func TestExecExitStatusIsToolError(t *testing.T) {
	tool := execTestTool(t)
	dir := t.TempDir()
	cases := []struct {
		name   string
		cmd    string
		status ResultStatus
		code   int
		header string
	}{
		{"success", "true", StatusOK, 0, "exit: 0  duration:"},
		{"failure", "false", StatusToolError, 1, "exit: 1  duration:"},
		{"custom code", "exit 3", StatusToolError, 3, "exit: 3  duration:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := callTool(t, tool, map[string]any{"command": c.cmd, "workdir": dir})
			// The engine maps Status to provider is_error and the trial record.
			if res.Status != c.status || !strings.Contains(res.Output, c.header) {
				t.Fatalf("status %q, want %q; output %q", res.Status, c.status, res.Output)
			}
			if res.ExitCode == nil || *res.ExitCode != c.code {
				t.Fatalf("exit code %v, want %d", res.ExitCode, c.code)
			}
		})
	}
}

func TestExecExitStatusSwitchOffRestoresOkStatus(t *testing.T) {
	t.Setenv(ExecErrorStatusSwitch, "0")
	tool := execTestTool(t)
	res := callTool(t, tool, map[string]any{"command": "false", "workdir": t.TempDir()})
	if res.Status != StatusOK || res.ExitCode != nil || !strings.Contains(res.Output, "exit: 1  duration:") {
		t.Fatalf("switch off must keep the previous reporting: %+v", res)
	}
}

func TestExecSignalExitIsNamed(t *testing.T) {
	requirePOSIXFeature(t, "POSIX signal exit codes")
	// A child killed by SIGTERM: os/exec reports -1, the model must see
	// 128+15 and the signal name. The child is started by the test itself,
	// so the self-kill guard does not apply.
	child := exec.Command("/bin/sh", "-c", "kill -TERM $$")
	err := child.Run()
	if err == nil {
		t.Fatal("child must die by signal")
	}
	s := &Session{ID: "sess-test", done: make(chan struct{})}
	s.markDone(err)
	code, sig, exited := sessionExit(s)
	if !exited || code != 143 || sig != "SIGTERM" {
		t.Fatalf("signal exit: code %d sig %q exited %v", code, sig, exited)
	}

	// A shell that reports 128+n itself is named the same way.
	tool := execTestTool(t)
	res := callTool(t, tool, map[string]any{"command": "exit 139", "workdir": t.TempDir()})
	if !strings.Contains(res.Output, "exit: 139 (SIGSEGV)  duration:") || res.Status != StatusToolError || res.ExitCode == nil || *res.ExitCode != 139 {
		t.Fatalf("shell 128+n must be named: %+v", res)
	}
}

func TestExecSignalNamesOffKeepsPlainCode(t *testing.T) {
	t.Setenv(ExecErrorStatusSwitch, "0")
	tool := execTestTool(t)
	res := callTool(t, tool, map[string]any{"command": "exit 139", "workdir": t.TempDir()})
	if !strings.Contains(res.Output, "exit: 139  duration:") || strings.Contains(res.Output, "SIGSEGV") {
		t.Fatalf("switch off must not name signals: %q", res.Output)
	}
}

func TestExecProbeFailureCarriesNeutralNote(t *testing.T) {
	tool := execTestTool(t)
	dir := t.TempDir()
	res := callTool(t, tool, map[string]any{"command": "test -f /nope/missing-file", "workdir": dir})
	if res.Status != StatusToolError || res.ExitCode == nil || *res.ExitCode != 1 {
		t.Fatalf("probe failure is still an error in the record: %+v", res)
	}
	want := `note: exit 1 here usually means "not found / no match", not a failure`
	if !strings.Contains(res.Output, want) {
		t.Fatalf("probe note missing: %q", res.Output)
	}
	if !IsProbeFailureOutput(res.Output) {
		t.Fatal("finish gate must recognise the probe failure")
	}
	// GNU ls exits 2 for a missing path; BSD ls exits 1. Both are probes.
	ls := callTool(t, tool, map[string]any{"command": "ls /nope/missing-dir", "workdir": dir})
	lsCode := 2
	if runtime.GOOS == "darwin" {
		lsCode = 1
	}
	if ls.Status != StatusToolError || ls.ExitCode == nil || *ls.ExitCode != lsCode || !IsProbeFailureOutput(ls.Output) || !strings.Contains(ls.Output, fmt.Sprintf("note: exit %d here usually means", lsCode)) {
		t.Fatalf("ls of a missing path must carry the note: %q", ls.Output)
	}
	// A plain failure gets no probe note.
	plain := callTool(t, tool, map[string]any{"command": "false", "workdir": dir})
	if strings.Contains(plain.Output, "note: exit") || IsProbeFailureOutput(plain.Output) {
		t.Fatalf("non-probe failure must not be neutralised: %q", plain.Output)
	}
}

func TestProbeMissingPathDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		code         int
	}{
		{"GNU ls", "ls: cannot access '/nope/missing-dir': No such file or directory\n", 2},
		{"BSD ls", "ls: /nope/missing-dir: No such file or directory\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := missingPathIn(tc.output); got != "/nope/missing-dir" {
				t.Fatalf("missing path = %q", got)
			}
			if note := probeExitNote("ls /nope/missing-dir", tc.code); !IsProbeFailureOutput(note) {
				t.Fatalf("probe note missing: %q", note)
			}
		})
	}
}

func TestIsProbeCommandClassification(t *testing.T) {
	probes := []string{
		"test -f /x", "[ -d /x ]", "[[ -f x ]]", "grep -q foo f", "rg needle src",
		"grep -q foo f && echo found", "command -v python3 || echo none", "which make",
		"type -t go", "ls /missing", "diff a b", "pgrep -f server",
		"grep -E 'a|b' file", "grep foo f 2>&1", "FOO=1 test -f x",
	}
	for _, cmd := range probes {
		if !IsProbeCommand(cmd) {
			t.Errorf("IsProbeCommand(%q) = false, want true", cmd)
		}
	}
	notProbes := []string{
		"", "make test", "pkill -f x || true", "cat a | grep x", "echo hi",
		"python3 check.py", "grep foo f; make", "command make",
	}
	for _, cmd := range notProbes {
		if IsProbeCommand(cmd) {
			t.Errorf("IsProbeCommand(%q) = true, want false", cmd)
		}
	}
}

func TestExecZeroExitWarnsOnFailureText(t *testing.T) {
	tool := execTestTool(t)
	dir := t.TempDir()
	cases := []struct {
		name, cmd, phrase string
	}{
		{"guarded missing tool", "printf 'pkill: command not found\\n' || true", "command not found"},
		{"traceback", "printf 'Traceback (most recent call last):\\n'", "Traceback (most recent call last)"},
		{"unterminated heredoc", "cat <<EOF\nhello\n", "here-document at line 1 delimited by end-of-file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := callTool(t, tool, map[string]any{"command": c.cmd, "workdir": dir})
			if c.name == "unterminated heredoc" && runtime.GOOS == "darwin" {
				// Apple's /bin/bash 3.2 accepts EOF as the delimiter without
				// emitting a diagnostic, so there is no failure text to detect.
				if res.Status != StatusOK || !strings.HasSuffix(res.Output, "hello\n") || strings.Contains(res.Output, "warning:") {
					t.Fatalf("bash 3.2 silently accepts the heredoc: %q", res.Output)
				}
				return
			}
			want := fmt.Sprintf("warning: exit 0 but the output contains %q", c.phrase)
			if res.Status != StatusOK || !strings.Contains(res.Output, want) {
				t.Fatalf("missing warning %q: %q", want, res.Output)
			}
		})
	}
	clean := callTool(t, tool, map[string]any{"command": "echo ok", "workdir": dir})
	if strings.Contains(clean.Output, "warning:") {
		t.Fatalf("clean output must not warn: %q", clean.Output)
	}
}

func TestZeroExitWarningHeredocDiagnostic(t *testing.T) {
	// Keep detector coverage even on shells that emit no heredoc warning.
	phrase := "here-document at line 1 delimited by end-of-file"
	out := "/bin/bash: line 2: warning: " + phrase + " (wanted `EOF')\nhello\n"
	if got, want := zeroExitWarning(out), fmt.Sprintf("warning: exit 0 but the output contains %q\n", phrase); got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
}

func TestExecZeroExitWarningSwitchOff(t *testing.T) {
	t.Setenv(ExecWarningsSwitch, "0")
	tool := execTestTool(t)
	res := callTool(t, tool, map[string]any{"command": "printf 'pkill: command not found\\n' || true", "workdir": t.TempDir()})
	if strings.Contains(res.Output, "warning:") {
		t.Fatalf("switch off must hide the warning: %q", res.Output)
	}
}

func TestExecHintsNeedAFailedExit(t *testing.T) {
	tool := execTestTool(t)
	dir := t.TempDir()
	guarded := callTool(t, tool, map[string]any{"command": "printf 'pkill: command not found\\n' || true", "workdir": dir})
	if strings.Contains(guarded.Output, "hint:") {
		t.Fatalf("exit 0 must not carry a failure hint: %q", guarded.Output)
	}
	failed := callTool(t, tool, map[string]any{"command": "printf 'x: command not found\\n'; exit 1", "workdir": dir})
	if !strings.Contains(failed.Output, "hint: command not found") || strings.Contains(failed.Output, "mkdir -p") {
		t.Fatalf("failed exit must carry the reworded hint: %q", failed.Output)
	}
}

func TestExecElisionStoresFullOutputAndPagesWithRef(t *testing.T) {
	store := NewMemoryOutputStore()
	tool := execTestTool(t)
	tool.Outputs = store
	res := callTool(t, tool, map[string]any{"command": "seq 1 4000", "workdir": t.TempDir()})
	if !strings.HasPrefix(res.Output, "exit: 0  duration:") {
		t.Fatalf("header must lead: %.120q", res.Output)
	}
	elided, ref, offset := elisionParts(t, res.Output)
	full, ok := store.Get(ref)
	if !ok {
		t.Fatalf("elided output must be stored under %s", ref)
	}
	if full != seqText(4000) {
		t.Fatal("stored blob must be the full raw output")
	}
	if res.BlobRef != ref {
		t.Fatalf("result BlobRef %q, want %q", res.BlobRef, ref)
	}
	if offset+elided+execTailKeep != len(full) {
		t.Fatalf("marker offsets disagree: offset %d elided %d total %d", offset, elided, len(full))
	}
	if !strings.Contains(res.Output, full[:offset]) || !strings.HasSuffix(res.Output, full[len(full)-execTailKeep:]+execOutputDigest(full)) {
		t.Fatal("inline output must keep the head and the last 2000 bytes")
	}
	if len(res.Output) > execInlineBudget+400 {
		t.Fatalf("inline output too large: %d bytes", len(res.Output))
	}
	// The elided middle is reachable through the tool the marker names.
	page := &GetToolOutputTool{Outputs: store}
	args := fmt.Sprintf(`{"ref":%q,"offset":%d,"limit":%d}`, ref, offset, elided)
	pres, err := page.Invoke(context.Background(), Invocation{Arguments: []byte(args)})
	if err != nil || pres.Status != StatusOK || !strings.Contains(pres.Output, full[offset:offset+elided]) {
		t.Fatalf("get_tool_output must page the middle: %v %+v", err, pres)
	}
}

func TestExecElisionWithoutStoreNamesSession(t *testing.T) {
	tool := execTestTool(t)
	res := callTool(t, tool, map[string]any{"command": "seq 1 4000", "workdir": t.TempDir()})
	_, ref, _ := elisionParts(t, res.Output)
	if !strings.HasPrefix(ref, "sess-") || res.BlobRef != "" {
		t.Fatalf("without a store the marker names the session: ref %q blob %q", ref, res.BlobRef)
	}
}

func TestExecElisionSwitchOffRestoresLegacyMarker(t *testing.T) {
	t.Setenv(ExecElideRefSwitch, "0")
	store := NewMemoryOutputStore()
	tool := execTestTool(t)
	tool.Outputs = store
	res := callTool(t, tool, map[string]any{"command": "seq 1 4000", "workdir": t.TempDir()})
	if !strings.Contains(res.Output, "full output above the pipeline budget pages via get_tool_output") {
		t.Fatalf("switch off must keep the legacy marker: %.200q", res.Output)
	}
	if len(store.data) != 0 {
		t.Fatal("switch off must not store elided output")
	}
}

func TestExecStripsPipNoticesBeforeClipping(t *testing.T) {
	store := NewMemoryOutputStore()
	tool := execTestTool(t)
	tool.Outputs = store
	cmd := "printf '[notice] A new release of pip is available\\n'; seq 1 4000; printf '[notice] To update, run: pip install --upgrade pip\\n'"
	res := callTool(t, tool, map[string]any{"command": cmd, "workdir": t.TempDir()})
	if strings.Contains(res.Output, "[notice]") {
		t.Fatal("pip notices must not reach the model")
	}
	_, ref, _ := elisionParts(t, res.Output)
	full, _ := store.Get(ref)
	if strings.Contains(full, "[notice]") {
		t.Fatal("pip notices must not be stored either")
	}
}

func TestClipExecOutputKeepsUTF8Valid(t *testing.T) {
	// "aé" repeats every three bytes, so the cut lands inside a rune.
	full := strings.Repeat("aé", 10000)
	inline, ref := clipExecOutput(full, nil, "sess-1")
	if ref != "sess-1" {
		t.Fatalf("fallback ref %q", ref)
	}
	if !utf8.ValidString(inline) {
		t.Fatal("elision must not split a multi-byte rune")
	}
	if len(inline) > execInlineBudget+400 {
		t.Fatalf("inline size %d", len(inline))
	}
}

func TestExecIdenticalRerunIsLabelled(t *testing.T) {
	tool := execTestTool(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Setup writes must predate the runs by more than the clock slack.
	time.Sleep(60 * time.Millisecond)
	first := callTool(t, tool, map[string]any{"command": "cat data.txt", "workdir": dir})
	if strings.Contains(first.Output, "identical to call") {
		t.Fatalf("first run has nothing to repeat: %q", first.Output)
	}
	time.Sleep(60 * time.Millisecond)
	second := callTool(t, tool, map[string]any{"command": "cat data.txt", "workdir": dir})
	if !strings.Contains(second.Output, "note: identical to call #1 (no files changed since)") {
		t.Fatalf("identical re-run must be labelled: %q", second.Output)
	}
	if second.Status != StatusOK {
		t.Fatal("identical re-run must not be refused")
	}
	// Any write after the last run breaks the identity.
	time.Sleep(60 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	third := callTool(t, tool, map[string]any{"command": "cat data.txt", "workdir": dir})
	if strings.Contains(third.Output, "identical to call") {
		t.Fatalf("a file change must clear the note: %q", third.Output)
	}
}

func TestExecIdenticalRerunSwitchOff(t *testing.T) {
	t.Setenv(ExecWarningsSwitch, "0")
	tool := execTestTool(t)
	dir := t.TempDir()
	time.Sleep(60 * time.Millisecond)
	_ = callTool(t, tool, map[string]any{"command": "echo same", "workdir": dir})
	time.Sleep(60 * time.Millisecond)
	second := callTool(t, tool, map[string]any{"command": "echo same", "workdir": dir})
	if strings.Contains(second.Output, "identical to call") {
		t.Fatalf("switch off must hide the note: %q", second.Output)
	}
}

func TestExecMissingDirectoryListsNearestParent(t *testing.T) {
	tool := execTestTool(t)
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%02d.txt", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := callTool(t, tool, map[string]any{"command": "cat nope/deeper/x.txt", "workdir": dir})
	if strings.Contains(res.Output, "mkdir -p") {
		t.Fatalf("the mkdir directive must be gone: %q", res.Output)
	}
	if !strings.Contains(res.Output, "Check the path; nearest existing parent: "+dir) {
		t.Fatalf("nearest parent and the path sentence are missing: %q", res.Output)
	}
	if !strings.Contains(res.Output, "file19.txt") || strings.Contains(res.Output, "file20.txt") || !strings.Contains(res.Output, "... 5 more entries") {
		t.Fatalf("listing must be capped at 20 entries: %q", res.Output)
	}
}

func TestExecMissingDirectorySwitchOffRestoresDirective(t *testing.T) {
	t.Setenv(ExecWarningsSwitch, "0")
	tool := execTestTool(t)
	res := callTool(t, tool, map[string]any{"command": "cat nope/x.txt", "workdir": t.TempDir()})
	if !strings.Contains(res.Output, "create it with mkdir -p") {
		t.Fatalf("switch off must restore the previous hint: %q", res.Output)
	}
}

func TestExecPythonTestOnlyScriptIsNoted(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	tool := execTestTool(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "checks_test.py"), []byte("def test_addition():\n    assert 1 + 1 == 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := callTool(t, tool, map[string]any{"command": "python3 checks_test.py", "workdir": dir})
	if !strings.Contains(res.Output, "note: 0 tests ran: checks_test.py defines test_ functions") {
		t.Fatalf("silent test-only run must be noted: %q", res.Output)
	}
	// A script with a __main__ block runs its tests; no note.
	main := "def test_a():\n    assert True\n\nif __name__ == '__main__':\n    test_a()\n"
	if err := os.WriteFile(filepath.Join(dir, "main_test.py"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	ok := callTool(t, tool, map[string]any{"command": "python3 main_test.py", "workdir": dir})
	if strings.Contains(ok.Output, "0 tests ran") {
		t.Fatalf("a __main__ block must not be noted: %q", ok.Output)
	}
}

func TestExecTargetIsIgnoredWithNote(t *testing.T) {
	tool := execTestTool(t)
	res := callTool(t, tool, map[string]any{"command": "echo hi", "target": "node:abc", "workdir": t.TempDir()})
	if res.Status != StatusOK || !strings.Contains(res.Output, "target ignored: commands run locally") {
		t.Fatalf("unknown target must not fail the call: %+v", res)
	}
	local := callTool(t, tool, map[string]any{"command": "echo hi", "target": "local", "workdir": t.TempDir()})
	if strings.Contains(local.Output, "target ignored") {
		t.Fatalf("local target needs no note: %q", local.Output)
	}
}

func TestExecIOUnknownOwnerListsAllSessions(t *testing.T) {
	t.Setenv(AutoProvisionEnv, "off")
	m := NewSessionManager(TerminalDeps{})
	defer closeSessions(m)
	exTool := &ExecTool{Sessions: m}
	ioTool := &ExecIOTool{Sessions: m}
	_ = callTool(t, exTool, map[string]any{"command": "sleep 5", "owner": "e9", "yield_ms": 250, "workdir": t.TempDir()})
	unknown := callTool(t, ioTool, map[string]any{"action": "list", "owner": "child"})
	if !strings.Contains(unknown.Output, `owner ignored: no session has owner "child"`) || !strings.Contains(unknown.Output, "owner=e9") {
		t.Fatalf("unknown owner must list every session with a note: %q", unknown.Output)
	}
	known := callTool(t, ioTool, map[string]any{"action": "list", "owner": "e9"})
	if strings.Contains(known.Output, "owner ignored") || !strings.Contains(known.Output, "owner=e9") {
		t.Fatalf("known owner must filter without a note: %q", known.Output)
	}
}

func TestExecRunningSessionNamesThePollHandle(t *testing.T) {
	cases := []struct {
		name     string
		friction string
		want     string
		notWant  string
	}{
		{"exec_io visible", "1", `to poll: exec_io {"action":"read","session":"sess-`, "still running"},
		{"exec_io hidden", "0", "still running in session sess-", "to poll"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("EXCALIBUR_TOOL_FRICTION_REPAIR", c.friction)
			m := NewSessionManager(TerminalDeps{})
			defer closeSessions(m)
			tool := &ExecTool{Sessions: m}
			t.Setenv(AutoProvisionEnv, "off")
			res := callTool(t, tool, map[string]any{"command": "sleep 5", "yield_ms": 250, "workdir": t.TempDir()})
			if res.Status != StatusOK || !strings.Contains(res.Output, "exit: running") || !strings.Contains(res.Output, c.want) {
				t.Fatalf("running session must return a handle: %q", res.Output)
			}
			if strings.Contains(res.Output, c.notWant) {
				t.Fatalf("unexpected %q in %q", c.notWant, res.Output)
			}
		})
	}
}

func TestExecProvisioningNeverRunsFromReadPaths(t *testing.T) {
	// The W9 read-path test pins the same rule with a real session; this
	// one checks that a list over a session with a missing tool stays silent.
	t.Setenv(AutoProvisionEnv, "off")
	m := NewSessionManager(TerminalDeps{})
	defer closeSessions(m)
	_ = callTool(t, &ExecTool{Sessions: m}, map[string]any{"command": "printf 'frobnicate: command not found\\n'; sleep 2", "yield_ms": 250, "workdir": t.TempDir()})
	res := callTool(t, &ExecIOTool{Sessions: m}, map[string]any{"action": "list"})
	if strings.Contains(res.Output, "provision:") {
		t.Fatalf("read paths must not provision: %q", res.Output)
	}
}
