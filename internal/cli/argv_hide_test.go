package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func TestPrepareInstructionArgvHandsOffRequest(t *testing.T) {
	const request = "fix the parser without touching the lexer"
	argv := []string{"/usr/bin/excalibur", "exec", "--profile", "eval", "--json", "--logs-dir", "/logs/agent", request}
	env := []string{"PATH=/usr/bin", "EXCALIBUR_PROVIDER=openai"}
	newArgv, newEnv, path, err := PrepareInstructionArgv(argv, env, request)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if path == "" {
		t.Fatal("no instruction file written")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("instruction file mode %v, want 0600", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != request {
		t.Fatalf("file content %q", raw)
	}
	for _, a := range newArgv {
		if strings.Contains(a, "fix the parser") {
			t.Fatalf("request still in argv: %q", newArgv)
		}
	}
	joined := strings.Join(newArgv, "\x00")
	if !strings.Contains(joined, InstructionFileFlag+"\x00"+path) {
		t.Fatalf("argv lacks %s %s: %q", InstructionFileFlag, path, newArgv)
	}
	if got := newArgv[len(newArgv)-2:]; got[0] != InstructionFileFlag || got[1] != path {
		t.Fatalf("flag not at the request position: %q", newArgv)
	}
	hasVar := false
	for _, kv := range newEnv {
		if kv == InstructionFileEnv+"="+path {
			hasVar = true
		}
	}
	if !hasVar || !envHasKey(newEnv, "PATH") || !envHasKey(newEnv, "EXCALIBUR_PROVIDER") {
		t.Fatalf("env not carried over: %q", newEnv)
	}
	if argv[len(argv)-1] != request || len(env) != 2 {
		t.Fatal("inputs were modified")
	}
}

func TestPrepareInstructionArgvCreatesMissingTempDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses a different temporary directory environment variable")
	}
	tempDir := filepath.Join(t.TempDir(), "missing", "tmp")
	t.Setenv("TMPDIR", tempDir)
	_, _, path, err := PrepareInstructionArgv([]string{"excalibur", "exec", "request"}, nil, "request")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != tempDir {
		t.Fatalf("instruction path %q is outside %q", path, tempDir)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("instruction file mode = %o, want 600", info.Mode().Perm())
	}
	text, err := ConsumeInstructionFile(path)
	if err != nil || text != "request" {
		t.Fatalf("consume = %q, %v", text, err)
	}
}

func TestPrepareInstructionArgvNoOpCases(t *testing.T) {
	argv := []string{"/usr/bin/excalibur", "exec", "--json", "task text"}
	env := []string{"PATH=/usr/bin"}
	cases := map[string]struct {
		argv []string
		env  []string
		req  string
	}{
		"empty request":           {argv, env, ""},
		"request not in argv":     {argv, env, "other text"},
		"loop guard (env set)":    {argv, append(env, InstructionFileEnv+"=/tmp/x"), "task text"},
		"request is program name": {[]string{"task text"}, env, "task text"},
	}
	for name, c := range cases {
		newArgv, newEnv, path, err := PrepareInstructionArgv(c.argv, c.env, c.req)
		if err != nil || path != "" {
			t.Errorf("%s: path=%q err=%v", name, path, err)
		}
		if strings.Join(newArgv, " ") != strings.Join(c.argv, " ") || len(newEnv) != len(c.env) {
			t.Errorf("%s: argv or env changed", name)
		}
	}
}

func TestConsumeInstructionFileReadsOnceAndClearsEnv(t *testing.T) {
	const request = "consume me"
	path := filepath.Join(t.TempDir(), "instruction")
	if err := os.WriteFile(path, []byte(request), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(InstructionFileEnv, path)
	text, err := ConsumeInstructionFile("")
	if err != nil || text != request {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("instruction file left behind")
	}
	if _, ok := os.LookupEnv(InstructionFileEnv); ok {
		t.Fatal("environment variable not cleared")
	}
	if _, err := ConsumeInstructionFile(""); err == nil {
		t.Fatal("second consume returned a request")
	}
}

func TestConsumeInstructionFileRejectsNonRegular(t *testing.T) {
	dir := t.TempDir()
	if _, err := ConsumeInstructionFile(dir); err == nil {
		t.Fatal("directory accepted as instruction file")
	}
}

func TestHideInstructionArgvSwitch(t *testing.T) {
	t.Setenv(HideInstructionArgvSwitch, "")
	if !HideInstructionArgvEnabled() {
		t.Fatal("default must be on")
	}
	t.Setenv(HideInstructionArgvSwitch, "off")
	if HideInstructionArgvEnabled() {
		t.Fatal("switch off ignored")
	}
}

// handoffMarker is unique to this test binary run, so no ancestor's command
// line can carry it.
// The hand-off option now precedes --; the test binary must accept it too.
func init() {
	_ = flag.String("instruction-file", "", "private instruction hand-off for the re-exec test")
}

var handoffMarker = fmt.Sprintf("handoff-task-text-%d", os.Getpid())

// TestInstructionArgvHandoffReexec drives the production hand-off through a
// real exec of the test binary. The first run plans the hand-off and execs
// itself with the request file in place of the request; the second run
// reads the request back and must not hand off again.
func TestInstructionArgvHandoffReexec(t *testing.T) {
	if os.Getenv("W22_HANDOFF_HELPER") == "1" {
		// The request is the last argument the parent passed.
		handoffHelper(os.Args[len(os.Args)-1])
	}
	if runtime.GOOS != "linux" {
		t.Skip("re-exec requires Linux")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstructionArgvHandoffReexec$", "--", handoffMarker)
	cmd.Env = append(os.Environ(), "W22_HANDOFF_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "TEXT="+handoffMarker+"\n") {
		t.Fatalf("request not recovered in re-executed process:\n%s", text)
	}
	if !strings.Contains(text, "FILE_GONE=true") {
		t.Fatalf("instruction file survived the re-exec:\n%s", text)
	}
	var argv []string
	for _, line := range strings.Split(text, "\n") {
		if raw, ok := strings.CutPrefix(line, "ARGV="); ok {
			if err := json.Unmarshal([]byte(raw), &argv); err != nil {
				t.Fatalf("argv line: %v", err)
			}
		}
	}
	if len(argv) == 0 {
		t.Fatalf("no argv reported:\n%s", text)
	}
	for _, a := range argv {
		if strings.Contains(a, handoffMarker) {
			t.Fatalf("task text in re-executed argv: %q", argv)
		}
	}
	if !strings.Contains(strings.Join(argv, " "), InstructionFileFlag) {
		t.Fatalf("re-executed argv lacks %s: %q", InstructionFileFlag, argv)
	}
}

// handoffHelper is the helper-process side of TestInstructionArgvHandoffReexec.
// It never returns: it either re-execs or reports and exits.
func handoffHelper(request string) {
	if os.Getenv(InstructionFileEnv) != "" {
		path := os.Getenv(InstructionFileEnv)
		text, err := ConsumeInstructionFile("")
		if err != nil {
			fmt.Printf("CONSUME_ERROR=%v\n", err)
			os.Exit(6)
		}
		_, statErr := os.Stat(path)
		argvJSON, _ := json.Marshal(os.Args)
		fmt.Printf("TEXT=%s\n", text)
		fmt.Printf("FILE_GONE=%v\n", os.IsNotExist(statErr))
		fmt.Printf("ARGV=%s\n", argvJSON)
		os.Exit(0)
	}
	argv, env, path, err := PrepareInstructionArgv(os.Args, os.Environ(), request)
	if err != nil || path == "" {
		fmt.Printf("PREPARE_ERROR=%v path=%q\n", err, path)
		os.Exit(7)
	}
	if err := platform.Reexec(argv, env); err != nil {
		_ = os.Remove(path)
		fmt.Printf("REEXEC_ERROR=%v\n", err)
		os.Exit(8)
	}
	os.Exit(9)
}

func TestExecScopePathsCoversHarnessLocations(t *testing.T) {
	logs := t.TempDir()
	t.Setenv("EXCALIBUR_LOGS", "")
	paths := ExecScopePaths(logs)
	has := func(p string) bool {
		for _, x := range paths {
			if x == p {
				return true
			}
		}
		return false
	}
	if !has(logs) || !has("/logs/agent") {
		t.Fatalf("paths %v lack the trial logs or /logs/agent", paths)
	}
	for _, p := range paths {
		if p == "/" || p == os.TempDir() {
			t.Fatalf("overly broad scope path %q", p)
		}
	}
	rel := ExecScopePaths("relative-logs")
	if len(rel) == 0 || !filepath.IsAbs(rel[0]) {
		t.Fatalf("relative logs dir not absolute: %v", rel)
	}
}
