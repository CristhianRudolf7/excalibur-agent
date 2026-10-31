// Self-verification closing hooks: extraction, fresh-evidence,
// contrast and big-task inference (Sections 7.5, 8.7, 18.1).
package tasks

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestExtractPathsAndFiles(t *testing.T) {
	req := "Create the python file /app/filter.py that removes scripts. Also update ./tools/run.sh and docs/notes.md."
	drafts := ExtractDraftCriteria(req, 6)
	keys := map[string]bool{}
	for _, d := range drafts {
		keys[d.Key] = true
		if d.Method != types.CriterionCommand {
			t.Fatalf("path draft %q should be command, got %s", d.Statement, d.Method)
		}
		if d.Check == "" {
			t.Fatalf("path draft %q needs a check command", d.Statement)
		}
	}
	for _, want := range []string{"/app/filter.py", "./tools/run.sh", "notes.md"} {
		if !keys[want] {
			t.Fatalf("missing draft for %q in %v", want, drafts)
		}
	}
}

func TestExtractFormatsCountsConstraints(t *testing.T) {
	req := "Export the output in JSON format with 100 samples. The file must never include scripts; only rows count."
	drafts := ExtractDraftCriteria(req, 6)
	joined := ""
	for _, d := range drafts {
		joined += d.Statement + " | "
	}
	lower := strings.ToLower(joined)
	if !strings.Contains(lower, "json") {
		t.Fatalf("missing format draft in %v", drafts)
	}
	if !strings.Contains(lower, "100") {
		t.Fatalf("missing count draft in %v", drafts)
	}
	if !strings.Contains(lower, "constraint holds") {
		t.Fatalf("missing constraint draft in %v", drafts)
	}
}

func TestExtractDedupesAndCaps(t *testing.T) {
	req := "/app/a.py /app/a.py /app/a.py /app/b.py /app/c.py /app/d.py /app/e.py /app/f.py /app/g.py /app/h.py"
	drafts := ExtractDraftCriteria(req, 6)
	if len(drafts) != 6 {
		t.Fatalf("drafts = %d, want cap 6", len(drafts))
	}
	seen := map[string]bool{}
	for _, d := range drafts {
		if seen[d.Statement] {
			t.Fatalf("duplicate draft %q", d.Statement)
		}
		seen[d.Statement] = true
	}
	if got := ExtractDraftCriteria("", 6); len(got) != 0 {
		t.Fatalf("empty request should yield no drafts, got %v", got)
	}
}

func TestRefineRoundtripAndFallback(t *testing.T) {
	drafts := []DraftCriterion{
		{ID: "D1", Statement: "file /app/filter.py exists", Method: types.CriterionCommand, Check: "test -s /app/filter.py"},
	}
	prompt := RefinePrompt("Create /app/filter.py.", drafts)
	if !strings.Contains(prompt, "/app/filter.py") {
		t.Fatalf("refine prompt lost the draft: %q", prompt)
	}
	parsed := ParseRefinedCriteria("command | file /app/filter.py exists and strips scripts | test -s /app/filter.py && python3 check.py\njunk line without separator\nbogus | |", 6)
	if len(parsed) != 1 || parsed[0].Statement == "" {
		t.Fatalf("parse = %+v, want one draft", parsed)
	}
	if got := ParseRefinedCriteria("nothing parseable here", 6); len(got) != 0 {
		t.Fatalf("unparseable refine should yield no drafts, got %v", got)
	}
}

func TestFreshVerificationRules(t *testing.T) {
	ok := []HistoryTool{
		{Name: "write_file", Args: "filter.py", Output: "wrote filter.py"},
		{Name: "exec", Args: "python3 check.py", Output: "ALL PASS"},
	}
	if p := CheckFreshVerification(ok, 1); len(p) != 0 {
		t.Fatalf("verified history should pass, got %v", p)
	}
	failed := []HistoryTool{
		{Name: "exec", Args: "pytest -q", Output: "2 failed", IsError: true},
	}
	if p := CheckFreshVerification(failed, 1); len(p) == 0 {
		t.Fatal("failed last tool must block")
	}
	running := []HistoryTool{
		{Name: "exec", Args: "make -j", Output: "session: sess-1  alive: true\nbuilding..."},
	}
	if p := CheckFreshVerification(running, 1); len(p) == 0 {
		t.Fatal("still-running session must block")
	}
	unverified := []HistoryTool{
		{Name: "exec", Args: "ls", Output: "ok"},
		{Name: "write_file", Args: "filter.py", Output: "wrote filter.py"},
	}
	if p := CheckFreshVerification(unverified, 1); len(p) == 0 {
		t.Fatal("write without a later check must block")
	}
	if p := CheckFreshVerification(nil, 1); len(p) != 0 {
		t.Fatalf("empty history has nothing to verify, got %v", p)
	}
}

func TestCoverageBasenameAndOmission(t *testing.T) {
	drafts := []DraftCriterion{
		{ID: "D1", Statement: "file /app/filter.py exists", Key: "/app/filter.py"},
		{ID: "D2", Statement: "file /app/other.py exists", Key: "/app/other.py"},
	}
	history := []HistoryTool{{Name: "write_file", Args: "/app/filter.py", Output: "wrote filter.py"}}
	covered, omissions := DraftCoverage(drafts, "wrote filter.py, all good", history)
	if covered || len(omissions) != 1 || !strings.Contains(omissions[0], "D2") {
		t.Fatalf("covered=%v omissions=%v", covered, omissions)
	}
	covered, _ = DraftCoverage(drafts, "both filter.py and other.py done", history)
	if !covered {
		t.Fatal("basename mentions should cover")
	}
	if covered, _ := DraftCoverage(nil, "", nil); !covered {
		t.Fatal("no drafts is vacuously covered")
	}
}

func TestApplySelfVerifyLegacyUntouched(t *testing.T) {
	cfg := DefaultSelfVerifyConfig()
	gate := GateReport{OK: true}
	rep, drafts := ApplySelfVerify(cfg, gate, GateInput{}, SelfVerifyRequest{})
	if !rep.OK || len(rep.Pendings) != 0 || len(drafts) != 0 {
		t.Fatalf("legacy empty input must pass untouched: %+v %v", rep, drafts)
	}
	off := cfg
	off.Enable = false
	rep, _ = ApplySelfVerify(off, GateReport{Pendings: []string{"x"}}, GateInput{},
		SelfVerifyRequest{Request: "write /app/x.py", History: []HistoryTool{{Name: "exec", Output: "ok"}}})
	if rep.OK {
		t.Fatal("master switch off must leave the gate report alone")
	}
}

func TestApplySelfVerifyMergesBlocking(t *testing.T) {
	cfg := DefaultSelfVerifyConfig()
	cfg.EnableRefine = false
	req := SelfVerifyRequest{
		Request:   "Create /app/filter.py and verify it strips scripts.",
		History:   []HistoryTool{{Name: "write_file", Args: "/app/filter.py", Output: "wrote it"}},
		FinalText: "done",
	}
	rep, drafts := ApplySelfVerify(cfg, GateReport{OK: true}, GateInput{}, req)
	if rep.OK {
		t.Fatal("unverified write must block")
	}
	if len(drafts) == 0 {
		t.Fatal("extraction must produce drafts")
	}
	found := false
	for _, d := range drafts {
		if strings.Contains(d.Key, "filter.py") {
			found = true
		}
	}
	if !found {
		t.Fatalf("drafts should name filter.py: %+v", drafts)
	}
	note := VerificationNote(rep.Pendings)
	if !strings.Contains(note, "self-verify") || !strings.Contains(note, "unverified") {
		t.Fatalf("note should be concise and name the gap: %q", note)
	}
	// A draft whose artifact appears nowhere must surface as an
	// omission: contrast re-reads the request against the artifacts.
	req2 := SelfVerifyRequest{
		Request:   "Create /app/filter.py and /app/other.py.",
		History:   []HistoryTool{{Name: "write_file", Args: "/app/filter.py", Output: "wrote it"}},
		FinalText: "done",
	}
	rep2, _ := ApplySelfVerify(cfg, GateReport{OK: true}, GateInput{}, req2)
	joined := strings.Join(rep2.Pendings, "; ")
	if !strings.Contains(joined, "other.py") {
		t.Fatalf("contrast should name the missing artifact: %v", rep2.Pendings)
	}
}

func TestApplySelfVerifyPassesWhenVerified(t *testing.T) {
	cfg := DefaultSelfVerifyConfig()
	cfg.EnableRefine = false
	req := SelfVerifyRequest{
		Request: "Create /app/filter.py.",
		History: []HistoryTool{
			{Name: "write_file", Args: "/app/filter.py", Output: "wrote /app/filter.py"},
			{Name: "exec", Args: "python3 -c 'import filter'", Output: "/app/filter.py ok"},
		},
		FinalText: "created /app/filter.py and verified the import",
	}
	rep, _ := ApplySelfVerify(cfg, GateReport{OK: true}, GateInput{}, req)
	if !rep.OK {
		t.Fatalf("verified work should pass: %v", rep.Pendings)
	}
}

func TestApplySelfVerifyKeepsGateDefects(t *testing.T) {
	cfg := DefaultSelfVerifyConfig()
	in := GateInput{Criteria: []GateCriterion{{
		Criterion: types.Criterion{
			Code: mustDraftRef("C1.1"), Method: types.CriterionTest, Statement: "x",
		},
		State: types.CriterionUnverified,
	}}}
	rep, _ := ApplySelfVerify(cfg, GateReport{OK: false, Pendings: []string{"C1.1 without evidence"}}, in, SelfVerifyRequest{})
	if rep.OK {
		t.Fatal("gate defects must still block")
	}
}

func mustDraftRef(s string) types.CriterionRef {
	r, err := types.ParseCriterionRef(s)
	if err != nil {
		panic(err)
	}
	return r
}

func TestExtractRequestFromTaskLayer(t *testing.T) {
	layer := "request:\nCreate /app/filter.py.\nSecond line.\nfixed instructions:\n- verify\n"
	if got := ExtractRequestFromTaskLayer(layer); got != "Create /app/filter.py.\nSecond line." {
		t.Fatalf("request = %q", got)
	}
	if got := ExtractRequestFromTaskLayer("no prefix here"); got != "" {
		t.Fatalf("missing prefix should yield empty, got %q", got)
	}
}

func TestInferBigTaskAndContext(t *testing.T) {
	if !InferBigTask(types.TaskN2, 5, 0, 0) {
		t.Fatal("N2 with 5 requirements is big")
	}
	if !InferBigTask(types.TaskN3, 0, 0, 0) {
		t.Fatal("N3 is big")
	}
	if InferBigTask(types.TaskN1, 0, 3, 1) {
		t.Fatal("small N1 work is not big")
	}
	drafts := []DraftCriterion{{ID: "D1", Statement: "file /app/x.py exists", Method: types.CriterionCommand}}
	ctx := BuildVerifierContext("Write /app/x.py.", drafts, []HistoryTool{
		{Name: "exec", Args: "pytest", Output: "ok"},
	}, 20)
	if ctx.Request == "" || len(ctx.Drafts) != 1 || len(ctx.Evidence) != 1 {
		t.Fatalf("small context incomplete: %+v", ctx)
	}
}

func TestConfigEnvOverrides(t *testing.T) {
	t.Setenv("EXCALIBUR_SELFVERIFY", "0")
	t.Setenv("EXCALIBUR_SELFVERIFY_MIN_CHECKS", "2")
	t.Setenv("EXCALIBUR_SELFVERIFY_MAX_DRAFTS", "3")
	cfg := DefaultSelfVerifyConfig()
	if cfg.Enable {
		t.Fatal("env must disable the master switch")
	}
	if cfg.MinFreshChecks != 2 || cfg.MaxDrafts != 3 {
		t.Fatalf("env counts not applied: %+v", cfg)
	}
}
