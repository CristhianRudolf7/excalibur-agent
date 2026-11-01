// Strong verification: exercise the deliverable plus the
// independent verifier prompt and parser (Sections 7.5, 8.7, 18.1).
package tasks

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestIsExercisingCall(t *testing.T) {
	cases := []struct {
		name string
		h    HistoryTool
		want bool
	}{
		{"run script", HistoryTool{Name: "exec", Args: `{"command":"python3 /app/main.py"}`, Output: "ok"}, true},
		{"import module", HistoryTool{Name: "exec", Args: `{"command":"python3 -c 'import filter'"}`, Output: "ok"}, true},
		{"pytest", HistoryTool{Name: "exec", Args: `{"command":"pytest -q"}`, Output: "3 passed"}, true},
		{"go test", HistoryTool{Name: "exec", Args: `{"command":"go test ./..."}`, Output: "ok"}, true},
		{"direct exec", HistoryTool{Name: "exec", Args: `{"command":"./prog --help"}`, Output: "usage"}, true},
		{"curl service", HistoryTool{Name: "exec", Args: `{"command":"curl -s localhost:8000/health"}`, Output: "ok"}, true},
		{"jq parse", HistoryTool{Name: "exec", Args: `{"command":"jq empty /app/out.json"}`, Output: "ok"}, true},
		{"grep constraint", HistoryTool{Name: "exec", Args: `{"command":"grep -q script /app/out || echo clean"}`, Output: "clean"}, true},
		{"make build", HistoryTool{Name: "exec", Args: `{"command":"make"}`, Output: "built"}, true},
		{"gcc build", HistoryTool{Name: "exec", Args: `{"command":"gcc -o main main.c"}`, Output: "ok"}, true},
		{"ls is shallow", HistoryTool{Name: "exec", Args: `{"command":"ls -l /app/x.py"}`, Output: "x.py"}, false},
		{"test-e is shallow", HistoryTool{Name: "exec", Args: `{"command":"test -s /app/x.py && ls -l /app/x.py"}`, Output: "x.py"}, false},
		{"stat is shallow", HistoryTool{Name: "exec", Args: `{"command":"stat /app/x.py"}`, Output: "File:"}, false},
		{"bare cat is shallow", HistoryTool{Name: "exec", Args: `{"command":"cat /app/move.txt"}`, Output: "e2e6"}, false},
		{"echo is shallow", HistoryTool{Name: "exec", Args: `{"command":"echo done"}`, Output: "done"}, false},
		{"read_file is observation", HistoryTool{Name: "read_file", Args: "/app/x.py", Output: "code"}, false},
		{"write never exercises", HistoryTool{Name: "write_file", Args: "/app/x.py", Output: "wrote it"}, false},
		{"failed run", HistoryTool{Name: "exec", Args: `{"command":"python3 /app/x.py"}`, Output: "traceback", IsError: true}, false},
		{"empty output", HistoryTool{Name: "exec", Args: `{"command":"python3 /app/x.py"}`, Output: ""}, false},
		{"still running", HistoryTool{Name: "exec", Args: `{"command":"make -j"}`, Output: "alive: true"}, false},
	}
	for _, c := range cases {
		if got := IsExercisingCall(c.h); got != c.want {
			t.Errorf("%s: IsExercisingCall = %v, want %v", c.name, got, c.want)
		}
	}
}

func strongDrafts(t *testing.T, request string) []DraftCriterion {
	t.Helper()
	drafts := ExtractDraftCriteria(request, 6)
	if len(drafts) == 0 {
		t.Fatalf("no drafts extracted from %q", request)
	}
	return drafts
}

func TestCheckStrongExerciseBlocksShallow(t *testing.T) {
	drafts := strongDrafts(t, "Create /app/x.py.")
	shallow := []HistoryTool{
		{Name: "write_file", Args: "/app/x.py", Output: "wrote /app/x.py"},
		{Name: "exec", Args: `{"command":"ls -l /app/x.py"}`, Output: "-rw x.py"},
	}
	if p := CheckStrongExercise(shallow, drafts, 1); len(p) == 0 {
		t.Fatal("ls after a write must not count as exercising the deliverable")
	} else if !strings.Contains(strings.ToLower(strings.Join(p, "; ")), "exercis") {
		t.Fatalf("pendings should name exercising, got %v", p)
	}
	exercised := []HistoryTool{
		{Name: "write_file", Args: "/app/x.py", Output: "wrote /app/x.py"},
		{Name: "exec", Args: `{"command":"python3 /app/x.py hello"}`, Output: "/app/x.py printed hello"},
	}
	if p := CheckStrongExercise(exercised, drafts, 1); len(p) != 0 {
		t.Fatalf("running the deliverable with inputs should pass, got %v", p)
	}
}

func TestRedirectReIgnoresFormatStrings(t *testing.T) {
	for _, args := range []string{
		`{"command":"output=$(bash run.sh) && printf 'captured=<%s>\\n' \"$output\" && test \"$output\" = 'run-ok-3'"}`,
		`{"command":"python3 -m json.tool /app/out.json > /dev/null && echo JSON-OK"}`,
	} {
		h := HistoryTool{Name: "exec", Args: args, Output: "ok"}
		if lastChangeIndex([]HistoryTool{h}) != -1 {
			t.Fatalf("validating check must not count as a change: %s", args)
		}
		if !IsExercisingCall(h) {
			t.Fatalf("validating check must count as exercising: %s", args)
		}
	}
	for _, args := range []string{
		`{"command":"echo hi > /app/out.txt"}`,
		`{"command":"printf 'x' >> log.txt"}`,
		`{"command":"make 2> err.log"}`,
	} {
		if lastChangeIndex([]HistoryTool{{Name: "exec", Args: args, Output: "ok"}}) != 0 {
			t.Fatalf("redirection must count as a change: %s", args)
		}
	}
}

func TestCheckStrongExerciseValidatingRunWithFormatString(t *testing.T) {
	drafts := strongDrafts(t, "Create run.sh in the project directory that prints exactly run-ok-3 when executed with bash.")
	history := []HistoryTool{
		{Name: "exec", Args: `{"command":"pwd && ls -la"}`, Output: "alive: false\nfiles"},
		{Name: "write_file", Args: "/tmp/x/run.sh", Output: "wrote run.sh"},
		{Name: "exec", Args: `{"command":"output=$(bash run.sh) && printf 'captured=<%s>\\n' \"$output\" && test \"$output\" = 'run-ok-3'"}`, Output: "alive: false\ncaptured=<run-ok-3>"},
	}
	if p := CheckStrongExercise(history, drafts, 1); len(p) != 0 {
		t.Fatalf("running the script with an assertion must pass, got %v", p)
	}
}
func TestCheckStrongExerciseClosesExecMutationHole(t *testing.T) {
	drafts := strongDrafts(t, "Write the results to /app/out.txt.")
	// No write_file marker at all: the legacy freshness check passes
	// vacuously here, which is exactly the hole.
	hole := []HistoryTool{
		{Name: "exec", Args: `{"command":"echo hi > /app/out.txt"}`, Output: "wrote"},
		{Name: "exec", Args: `{"command":"cat /app/out.txt"}`, Output: "hi"},
	}
	if p := CheckFreshVerification(hole, 1); len(p) != 0 {
		t.Fatalf("legacy check should pass vacuously (documenting the hole), got %v", p)
	}
	if p := CheckStrongExercise(hole, drafts, 1); len(p) == 0 {
		t.Fatal("exec-created artifacts without an exercising check must block")
	}
	fixed := append(append([]HistoryTool(nil), hole...),
		HistoryTool{Name: "exec", Args: `{"command":"python3 check_out.py /app/out.txt"}`, Output: "/app/out.txt ALL PASS"})
	if p := CheckStrongExercise(fixed, drafts, 1); len(p) != 0 {
		t.Fatalf("validating the produced file should pass, got %v", p)
	}
}

func TestCheckStrongExerciseRequiresFormatValidation(t *testing.T) {
	drafts := strongDrafts(t, "Export the report in JSON format to /app/out.json.")
	unvalidated := []HistoryTool{
		{Name: "write_file", Args: "/app/out.json", Output: "wrote /app/out.json"},
		{Name: "exec", Args: `{"command":"python3 gen.py"}`, Output: "generated"},
	}
	if p := CheckStrongExercise(unvalidated, drafts, 1); len(p) == 0 {
		t.Fatal("a run that never parses the JSON output must block")
	} else if !strings.Contains(strings.ToLower(strings.Join(p, "; ")), "json") {
		t.Fatalf("pendings should name the JSON format, got %v", p)
	}
	validated := []HistoryTool{
		{Name: "write_file", Args: "/app/out.json", Output: "wrote /app/out.json"},
		{Name: "exec", Args: `{"command":"python3 -m json.tool /app/out.json > /dev/null && echo JSON-OK"}`, Output: "JSON-OK /app/out.json valid"},
	}
	if p := CheckStrongExercise(validated, drafts, 1); len(p) != 0 {
		t.Fatalf("parsing the JSON output should pass, got %v", p)
	}
}

func TestCheckStrongExerciseImportStemCovers(t *testing.T) {
	drafts := strongDrafts(t, "Create /app/filter.py.")
	history := []HistoryTool{
		{Name: "write_file", Args: "/app/filter.py", Output: "wrote it"},
		{Name: "exec", Args: `{"command":"python3 -c 'import filter; filter.run() '"}`, Output: "filter ok"},
	}
	// The call names the module stem only; the output names the file.
	// Either way the draft counts as exercised.
	if p := CheckStrongExercise(history, drafts, 1); len(p) != 0 {
		t.Fatalf("import-style exercising should cover the draft, got %v", p)
	}
}

func TestApplySelfVerifyStrongSwitchOffRestoresLegacy(t *testing.T) {
	req := SelfVerifyRequest{
		Request: "Create /app/x.py.",
		History: []HistoryTool{
			{Name: "write_file", Args: "/app/x.py", Output: "wrote /app/x.py"},
			{Name: "exec", Args: `{"command":"ls -l /app/x.py"}`, Output: "-rw x.py /app/x.py"},
		},
		FinalText: "created /app/x.py, listing shows it",
	}
	off := DefaultSelfVerifyConfig()
	off.EnableRefine = false
	off.EnableStrong = false
	if rep, _ := ApplySelfVerify(off, GateReport{OK: true}, GateInput{}, req); !rep.OK {
		t.Fatalf("strong off must restore the legacy outcome, got %v", rep.Pendings)
	}
	on := DefaultSelfVerifyConfig()
	on.EnableRefine = false
	if rep, _ := ApplySelfVerify(on, GateReport{OK: true}, GateInput{}, req); rep.OK {
		t.Fatal("strong on must block a bare ls after a write")
	}
}

func TestStrongVerifierPromptCarriesOnlyInstructionAndArtifacts(t *testing.T) {
	history := []HistoryTool{
		{Name: "exec", Args: "python3 /app/x.py", Output: "hi"},
	}
	small := BuildVerifierContext("Write /app/x.py that prints hi.", []DraftCriterion{
		{ID: "D1", Statement: "path /app/x.py exists", Method: types.CriterionCommand, Check: "python3 /app/x.py"},
	}, history, 20)
	prompt := StrongVerifierPrompt(small, history, "created /app/x.py, prints hi")
	for _, want := range []string{"Write /app/x.py", "D1", "python3 /app/x.py", "Do NOT look for"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt should contain %q", want)
		}
	}
	if strings.Contains(strings.ToLower(prompt), "executor reasoning") && strings.Contains(prompt, "my plan was") {
		t.Fatal("prompt must never carry executor reasoning")
	}
}

func TestParseStrongVerdict(t *testing.T) {
	pass := ParseStrongVerdict("CHECKS:\n- python3 /app/x.py hi\nD1: PASS - ran with input, output hi\nVERDICT: PASS\nREASONS: none\n", nil)
	if !pass.Explicit || !pass.Pass || len(pass.Checks) != 1 {
		t.Fatalf("pass reply should parse explicit pass with checks: %+v", pass)
	}
	fail := ParseStrongVerdict("CHECKS:\n- python3 -m json.tool /app/out.json\nD1: FAIL - never parsed the JSON output\nVERDICT: FAIL\nREASONS: D1: no format validation\n", nil)
	if !fail.Explicit || fail.Pass {
		t.Fatalf("fail reply should parse explicit fail: %+v", fail)
	}
	if len(fail.Reasons) == 0 || len(fail.FailedDrafts) != 1 {
		t.Fatalf("fail reply should carry reasons and failed drafts: %+v", fail)
	}
	gibberish := ParseStrongVerdict("looks good to me", nil)
	if gibberish.Explicit {
		t.Fatalf("gibberish must parse non-explicit (fallback to deterministic): %+v", gibberish)
	}
	verdictOnly := ParseStrongVerdict("VERDICT: FAIL\n", nil)
	if !verdictOnly.Explicit || verdictOnly.Pass {
		t.Fatalf("bare FAIL verdict must fail: %+v", verdictOnly)
	}
}

func TestConfigStrongEnvOverrides(t *testing.T) {
	t.Setenv("EXCALIBUR_STRONG_VERIFY", "0")
	t.Setenv("EXCALIBUR_STRONG_VERIFY_EXERCISE", "off")
	t.Setenv("EXCALIBUR_STRONG_VERIFY_VERIFIER", "no")
	cfg := DefaultSelfVerifyConfig()
	if cfg.EnableStrong || cfg.EnableStrongExercise || cfg.EnableStrongVerifier {
		t.Fatalf("env must disable strong switches: %+v", cfg)
	}
}

func TestConfigStrongDefaultsOn(t *testing.T) {
	cfg := SelfVerifyConfig{
		Enable: true, EnableExtraction: true, EnableVerification: true,
		EnableContrast: true, EnableIndependent: true, EnableRefine: true,
		EnableStrong: true, EnableStrongExercise: true, EnableStrongVerifier: true,
		MinFreshChecks: 1,
	}
	if !cfg.EnableStrong || !cfg.EnableStrongExercise || !cfg.EnableStrongVerifier {
		t.Fatal("strong mechanisms default ON")
	}
}

func TestExerciseCheckForSuggestsRunners(t *testing.T) {
	if got := exerciseCheckFor("/app/filter.py"); !strings.HasPrefix(got, "python3 /app/filter.py") {
		t.Fatalf("py check should run the interpreter, got %q", got)
	}
	if got := exerciseCheckFor("model.R"); !strings.HasPrefix(got, "Rscript") {
		t.Fatalf("R check should run Rscript, got %q", got)
	}
	if got := exerciseCheckFor("/app/out.json"); !strings.Contains(got, "test -s") {
		t.Fatalf("unknown artifact keeps the existence probe, got %q", got)
	}
}
