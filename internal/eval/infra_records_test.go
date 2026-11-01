package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTrialFiles creates one synthetic Harbor trial directory. files maps
// slash-separated relative paths to contents.
func writeTrialFiles(t *testing.T, root, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// harborResult builds a Harbor result.json with an optional exception.
func harborResult(t *testing.T, name string, reward float64, excType, excMsg string) string {
	t.Helper()
	doc := map[string]any{
		"task_name":       "terminal-bench/" + name,
		"trial_name":      name,
		"verifier_result": map[string]any{"rewards": map[string]float64{"reward": reward}},
	}
	if excType != "" {
		doc["exception_info"] = map[string]string{"exception_type": excType, "exception_message": excMsg}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// passingVerifier is a verifier output with a non-empty pytest report.
const passingVerifier = `{"results":{"summary":{"tests":3,"passed":1,"failed":2}}}`

const completedFinal = `{"state":"completed","reason":"","summary":"done"}`

func TestTrialInfraReasonRules(t *testing.T) {
	root := t.TempDir()
	modelRows := func(rows ...string) string { return strings.Join(rows, "\n") + "\n" }
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "agent install failed",
			files: map[string]string{
				"result.json": harborResult(t, "install", 0, "RuntimeError", "Agent install failed: excalibur_agent: sha256 mismatch: got abc"),
			},
			want: InfraAgentInstall,
		},
		{
			name: "environment did not start",
			files: map[string]string{
				"result.json": harborResult(t, "env", 0, "RuntimeError", "Docker compose command failed for environment formal-crypto"),
			},
			want: InfraEnvironmentStart,
		},
		{
			name: "billing response with no successful model call",
			files: map[string]string{
				"result.json":                       harborResult(t, "billing", 0, "", ""),
				"verifier/ctrf.json":                passingVerifier,
				"agent/excalibur/model_calls.jsonl": modelRows(`{"provider_error_class":"auth","provider_error_reason":"billing"}`),
			},
			want: InfraProviderBilling,
		},
		{
			name: "rate limit with no successful model call",
			files: map[string]string{
				"result.json":                       harborResult(t, "limited", 0, "", ""),
				"verifier/ctrf.json":                passingVerifier,
				"agent/excalibur/model_calls.jsonl": modelRows(`{"provider_error_class":"rate_limited","provider_error_reason":"rate_limited"}`, `{"provider_error_class":"rate_limited"}`),
			},
			want: InfraProviderError,
		},
		{
			name: "one successful call keeps the trial scored",
			files: map[string]string{
				"result.json":                       harborResult(t, "partial", 0, "", ""),
				"verifier/ctrf.json":                passingVerifier,
				"agent/excalibur/final.json":        completedFinal,
				"agent/excalibur/model_calls.jsonl": modelRows(`{"provider_error_class":"rate_limited"}`, `{"input_tokens":12}`),
			},
			want: "",
		},
		{
			name: "content refusal is model behaviour, not infra",
			files: map[string]string{
				"result.json":                       harborResult(t, "refusal", 0, "", ""),
				"verifier/ctrf.json":                passingVerifier,
				"agent/excalibur/final.json":        completedFinal,
				"agent/excalibur/model_calls.jsonl": modelRows(`{"provider_error_class":"content_rejected"}`),
			},
			want: "",
		},
		{
			name: "verifier without any pytest report",
			files: map[string]string{
				"result.json":                "{\"task_name\":\"x\",\"verifier_result\":{\"rewards\":{\"reward\":0}}}",
				"verifier/reward.txt":        "0",
				"verifier/test-stdout.txt":   "/tests/test.sh: line 8: curl: command not found\n",
				"agent/excalibur/final.json": completedFinal,
			},
			want: InfraVerifierNoResults,
		},
		{
			name: "verifier report with zero tests",
			files: map[string]string{
				"result.json":                harborResult(t, "zero", 0, "", ""),
				"verifier/ctrf.json":         `{"results":{"summary":{"tests":0}}}`,
				"agent/excalibur/final.json": completedFinal,
			},
			want: InfraVerifierNoResults,
		},
		{
			name: "verifier timed out",
			files: map[string]string{
				"result.json":                harborResult(t, "vtimeout", 0, "VerifierTimeoutError", "Verifier execution timed out after 900.0 seconds"),
				"agent/excalibur/final.json": completedFinal,
			},
			want: InfraVerifierNoResults,
		},
		{
			name: "cancelled before run finished",
			files: map[string]string{
				"result.json": harborResult(t, "cancel", 0, "CancelledError", ""),
			},
			want: InfraCancelled,
		},
		{
			name: "no terminal record at all",
			files: map[string]string{
				"result.json":                       harborResult(t, "killed", 0, "", ""),
				"verifier/ctrf.json":                passingVerifier,
				"agent/excalibur/transitions.jsonl": `{"from":"","to":"created"}` + "\n",
			},
			want: InfraCancelled,
		},
		{
			name: "agent timeout with a terminal record is agent work",
			files: map[string]string{
				"result.json":                       harborResult(t, "timeout", 0, "AgentTimeoutError", "Agent execution timed out after 900.0 seconds"),
				"verifier/ctrf.json":                passingVerifier,
				"agent/excalibur/final.json":        `{"state":"stopped_by_timeout","reason":"timeout"}`,
				"agent/excalibur/model_calls.jsonl": modelRows(`{"input_tokens":5}`),
			},
			want: "",
		},
		{
			name: "reward does not hide missing lifecycle or verifier",
			files: map[string]string{
				"result.json":         harborResult(t, "pass", 1, "", ""),
				"verifier/reward.txt": "1",
			},
			want: InfraCancelled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTrialFiles(t, root, strings.ReplaceAll(tc.name, " ", "_"), tc.files)
			reward := 0.0
			if strings.Contains(tc.files["result.json"], `"reward":1`) {
				reward = 1
			}
			if got := TrialInfraReason(dir, reward); got != tc.want {
				t.Fatalf("TrialInfraReason = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestInfraTrialsExcludedFromPassRate covers the report: infra trials leave
// the headline rate and are counted with their reasons; the with-infra rate
// keeps them as failures.
func TestInfraTrialsExcludedFromPassRate(t *testing.T) {
	trials := []TrialOutcome{
		{TaskID: "a", TrialID: "a1", Reward: 1},
		{TaskID: "a", TrialID: "a2", Reward: 0, FailCategory: FailVerifier},
		{TaskID: "b", TrialID: "b1", Reward: 0, InfraReason: InfraProviderBilling, FailCategory: FailHarness},
		{TaskID: "b", TrialID: "b2", Reward: 1},
		{TaskID: "c", TrialID: "c1", Reward: 0, InfraReason: InfraVerifierNoResults},
	}
	rep, err := Aggregate(trials, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Trials != 5 || rep.ScoredTrials != 3 || rep.InfraTrials != 2 {
		t.Fatalf("counts trials=%d scored=%d infra=%d", rep.Trials, rep.ScoredTrials, rep.InfraTrials)
	}
	if rep.InfraReasons[InfraProviderBilling] != 1 || rep.InfraReasons[InfraVerifierNoResults] != 1 {
		t.Fatalf("infra reasons %v", rep.InfraReasons)
	}
	// Scored tasks: a (1 of 2) and b (1 of 1); task c has no scored trial.
	if rep.Success.Mean < 0.7 || rep.Success.Mean > 0.8 {
		t.Fatalf("headline success %.3f, want 0.75 (infra excluded)", rep.Success.Mean)
	}
	// With infra: a 0.5, b 0.5, c 0 => 1/3.
	if rep.SuccessWithInfra.Mean < 0.3 || rep.SuccessWithInfra.Mean > 0.37 {
		t.Fatalf("with-infra success %.3f, want 0.333", rep.SuccessWithInfra.Mean)
	}
	if rep.Failures[FailVerifier] != 1 || rep.Failures[FailHarness] != 0 {
		t.Fatalf("failures must not count infra: %v", rep.Failures)
	}
	if got := rep.ByTask["b"]; got.Trials != 1 || got.Infra != 1 || got.Rate != 1 {
		t.Fatalf("task b summary %+v", got)
	}
	if got := rep.ByTask["c"]; got.Trials != 0 || got.Infra != 1 {
		t.Fatalf("task c summary %+v", got)
	}
}

func TestAggregateAllInfraReportsZeroRate(t *testing.T) {
	rep, err := Aggregate([]TrialOutcome{{TaskID: "a", InfraReason: InfraCancelled}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ScoredTrials != 0 || rep.Success.Mean != 0 || rep.InfraTrials != 1 {
		t.Fatalf("report %+v", rep)
	}
}

// TestIndexedInfraReachesReport runs the index on synthetic trial directories
// and reads them back through the run index, the path the report uses.
func TestIndexedInfraReachesReport(t *testing.T) {
	root := t.TempDir()
	install := writeTrialFiles(t, root, "install__1", map[string]string{
		"result.json": harborResult(t, "install", 0, "RuntimeError", "Agent install failed: excalibur_agent: sha256 mismatch"),
	})
	scored := writeTrialFiles(t, root, "scored__1", map[string]string{
		"result.json":                harborResult(t, "scored", 0, "", ""),
		"verifier/ctrf.json":         passingVerifier,
		"verifier/reward.txt":        "0",
		"agent/excalibur/final.json": completedFinal,
	})
	run := filepath.Join(root, "run")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{install, scored} {
		row, err := IndexTrial(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := AppendTrialIndex(run, row); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := LoadRunTrials(run)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Aggregate(loaded, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.InfraTrials != 1 || rep.InfraReasons[InfraAgentInstall] != 1 || rep.ScoredTrials != 1 {
		t.Fatalf("report %+v", rep)
	}
}

func TestTriageClassifiesInfraSeparately(t *testing.T) {
	root := t.TempDir()
	writeTrialFiles(t, root, "infra__1", map[string]string{
		"result.json":                       harborResult(t, "infra", 0, "", ""),
		"verifier/ctrf.json":                passingVerifier,
		"verifier/reward.txt":               "0",
		"agent/excalibur/model_calls.jsonl": `{"provider_error_class":"auth","provider_error_reason":"billing"}` + "\n",
	})
	writeTrialFiles(t, root, "agent__1", map[string]string{
		"result.json":                harborResult(t, "agent", 0, "", ""),
		"verifier/ctrf.json":         passingVerifier,
		"verifier/reward.txt":        "0",
		"agent/excalibur/final.json": completedFinal,
	})
	results, stats, err := TriageRun(t.Context(), root, TriageOptions{NoModel: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Infra != 1 || stats.Failed != 1 {
		t.Fatalf("stats %+v", stats)
	}
	var sawInfra bool
	for _, r := range results {
		if r.Trial == "infra__1" {
			sawInfra = r.Cause == TriageInfra && r.Domain == InfraProviderBilling && r.DecidedBy == "infra-rule"
		}
	}
	if !sawInfra {
		t.Fatalf("results %+v", results)
	}
}

// TestRecordedExecExitIsTheRealExit: the exec header carries the exit status;
// the record must use it (not the transport status) and mark errors from it.
func TestRecordedExecExitIsTheRealExit(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewTrialRecorder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordToolCall(ToolCall{Tool: "exec", ExitCode: 0}, "exit: 2  duration: 0.1s  session: s1  alive: false\nboom\n"); err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordToolCall(ToolCall{Tool: "exec", ExitCode: 0}, "exit: 0  duration: 0.1s  session: s2  alive: false\nok\n"); err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordToolCall(ToolCall{Tool: "exec", ExitCode: 1, IsError: true}, "exit: running  duration: 1.0s  session: s3  alive: true  next_offset: 0\n"); err != nil {
		t.Fatal(err)
	}
	calls := readToolCalls(t, dir)
	if calls[0].ExitCode != 2 || !calls[0].IsError || calls[0].HeaderExit == nil || *calls[0].HeaderExit != 2 {
		t.Fatalf("failing exec record %+v", calls[0])
	}
	if calls[1].ExitCode != 0 || calls[1].IsError {
		t.Fatalf("passing exec record %+v", calls[1])
	}
	if calls[2].HeaderExit != nil {
		t.Fatalf("running session must not record a header exit: %+v", calls[2])
	}
}

func TestRecordedNonExecToolKeepsCallerExit(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewTrialRecorder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordToolCall(ToolCall{Tool: "read_file", ExitCode: 1, IsError: true}, "exit: 7 looks like text\n"); err != nil {
		t.Fatal(err)
	}
	call := readToolCalls(t, dir)[0]
	if call.ExitCode != 1 || !call.IsError || call.HeaderExit != nil {
		t.Fatalf("non-exec record %+v", call)
	}
}

// TestRecordedOutputPreviewAndElision: stdout_inline is the 4 KiB head preview,
// stdout_tail keeps the last 2 KiB, and model_elided is set only when the
// model saw an elision marker.
func TestRecordedOutputPreviewAndElision(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewTrialRecorder(dir)
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Repeat("h", 5000)
	tail := strings.Repeat("t", 3000) + "END"
	elided := head + "\n…[middle 9000 bytes elided; full output above the pipeline budget pages via get_tool_output]…\n" + tail
	if err := rec.RecordToolCall(ToolCall{Tool: "exec", ExitCode: 0}, "exit: 0  duration: 0.1s  session: a  alive: false\n"+elided); err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordToolCall(ToolCall{Tool: "read_file"}, strings.Repeat("x", 5000)); err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordToolCall(ToolCall{Tool: "exec"}, "exit: 0  duration: 0.1s  session: b  alive: false\nshort"); err != nil {
		t.Fatal(err)
	}
	calls := readToolCalls(t, dir)
	first := calls[0]
	if !first.ModelElided || !first.Truncated {
		t.Fatalf("elided flags %+v", first)
	}
	if len(first.StdoutTail) != inlineTailBytes || !strings.HasSuffix(first.StdoutTail, "END") {
		t.Fatalf("tail len=%d suffix=%q", len(first.StdoutTail), first.StdoutTail[len(first.StdoutTail)-5:])
	}
	if !strings.HasSuffix(first.StdoutInline, "...[truncated]") || len(first.StdoutInline) > inlinePreviewBytes+len("...[truncated]") {
		t.Fatalf("inline preview is not the bounded head: len=%d", len(first.StdoutInline))
	}
	if calls[1].ModelElided || calls[1].StdoutTail == "" {
		t.Fatalf("long output without a marker: %+v", calls[1])
	}
	if calls[2].ModelElided || calls[2].StdoutTail != "" || calls[2].Truncated {
		t.Fatalf("short output: %+v", calls[2])
	}
}

// TestFinalRecordCarriesExitAndFinishDetail: final.json reports the last exec
// exit and the accepted finish detail.
func TestFinalRecordCarriesExitAndFinishDetail(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewTrialRecorder(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = rec.RecordToolCall(ToolCall{Tool: "exec"}, "exit: 3  duration: 0.1s  session: a  alive: false\nfail")
	_ = rec.RecordToolCall(ToolCall{Tool: "exec"}, "exit: 0  duration: 0.1s  session: b  alive: false\nok")
	rec.SetFinishDetail(FinishDetail{Summary: "Wrote out.txt", FinalText: "All done.", Evidence: "python3 check.py exited 0"})
	if err := rec.WriteFinal("completed", "", 0); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "final.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fin FinalState
	if err := json.Unmarshal(raw, &fin); err != nil {
		t.Fatal(err)
	}
	if fin.FinalExit == nil || *fin.FinalExit != 0 {
		t.Fatalf("final_exit = %v, want 0 (the last exec)", fin.FinalExit)
	}
	if fin.Summary != "Wrote out.txt" || fin.FinalText != "All done." || fin.FinishEvidence != "python3 check.py exited 0" {
		t.Fatalf("finish detail %+v", fin)
	}
	if bytes.Contains(raw, []byte("completed: ")) {
		t.Fatal("final record must not echo a prompt-style summary")
	}
}

func TestProviderReasonRecordedOnModelCall(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewTrialRecorder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordModelCall(ModelCall{Seq: 1, ProviderError: "auth", ProviderReason: "billing", CredentialSwitches: 1, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "model_calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var m ModelCall
	if err := json.Unmarshal(bytes.TrimSpace(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.ProviderReason != "billing" || m.CredentialSwitches != 1 {
		t.Fatalf("model call %+v", m)
	}
}

func readToolCalls(t *testing.T, dir string) []ToolCall {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "tool_calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []ToolCall
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var c ToolCall
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func TestVerifierTextResultsAndLateCancellation(t *testing.T) {
	for _, tc := range []struct{ name, summary, exception, want string }{
		{"pytest failed", "=== 2 failed, 1 passed in 0.12s ===", "", ""},
		{"quiet pytest", "1 passed in 0.12s", "", ""},
		{"pytest collection error", "=== 1 error in 0.12s ===", "", InfraVerifierNoResults},
		{"pytest empty", "=== no tests ran in 0.12s ===", "", InfraVerifierNoResults},
		{"late cancellation", "=== 1 failed in 0.12s ===", "CancelledError", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTrialFiles(t, t.TempDir(), "trial", map[string]string{
				"result.json":                harborResult(t, "trial", 0, tc.exception, ""),
				"agent/excalibur/final.json": completedFinal,
				"verifier/test-stdout.txt":   tc.summary,
			})
			if got := TrialInfraReason(dir, 0); got != tc.want {
				t.Fatalf("infra = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInfraClassificationWithoutRewardAndWithPositiveReward(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"missing-reward", "positive-reward"} {
		reward := `null`
		if name == "positive-reward" {
			reward = `{"rewards":{"reward":1}}`
		}
		writeTrialFiles(t, root, name, map[string]string{
			"result.json":                       `{"task_name":"sample","verifier_result":` + reward + `}`,
			"agent/excalibur/model_calls.jsonl": `{"provider_error_class":"auth","provider_error_reason":"billing"}` + "\n",
		})
	}
	results, stats, err := TriageRun(context.Background(), root, TriageOptions{NoModel: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Infra != 2 || stats.Skipped != 0 || stats.Passed != 0 || len(results) != 2 {
		t.Fatalf("stats=%+v results=%+v", stats, results)
	}
	for _, result := range results {
		if result.Domain != InfraProviderBilling {
			t.Fatalf("result=%+v", result)
		}
	}
	t.Setenv("EXCALIBUR_EVAL_INFRA_CLASSIFICATION", "0")
	if got := TrialInfraReason(filepath.Join(root, "missing-reward"), 0); got != "" {
		t.Fatalf("disabled classification = %q", got)
	}
}

func TestCompletedFinalCannotBeReplacedByLaterProviderFailure(t *testing.T) {
	r, err := NewTrialRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.SetFinishDetail(FinishDetail{Summary: "delivered", FinalText: "final assistant text", Evidence: "validated"})
	if err := r.WriteFinal("completed", "", 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.Dir(), "final.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r.SetFinishDetail(FinishDetail{Summary: "provider failed"})
	if err := r.WriteFinal("failed", "provider_billing", 1); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("accepted final replaced: %s", after)
	}
}

func TestRunRecordsV2Off(t *testing.T) {
	t.Setenv("EXCALIBUR_RUN_RECORDS_V2", "0")
	r, err := NewTrialRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RecordToolCall(ToolCall{Tool: "exec"}, "exit: 2  duration: 0.1s\n"+strings.Repeat("a", 5000)); err != nil {
		t.Fatal(err)
	}
	call := readToolCalls(t, r.Dir())[0]
	if call.ExitCode != 0 || call.HeaderExit != nil || call.StdoutTail != "" || call.ModelElided {
		t.Fatalf("legacy record: %+v", call)
	}
}

func TestInfraReportMarkdownShowsBothRatesAndReason(t *testing.T) {
	rep, err := Aggregate([]TrialOutcome{
		{TaskID: "a", Reward: 1},
		{TaskID: "b", InfraReason: InfraProviderBilling},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	text := RenderReportMarkdown(rep, RunManifest{})
	for _, want := range []string{"Success without infra: 1.0000", "Success with infra included: 0.5000", "provider_billing: 1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}

func TestReportRefreshesLegacyIndexInfrastructure(t *testing.T) {
	root := t.TempDir()
	writeTrialFiles(t, root, "billing-trial", map[string]string{
		"result.json":                       harborResult(t, "billing-trial", 0, "", ""),
		"agent/excalibur/model_calls.jsonl": `{"provider_error_class":"auth","provider_error_reason":"billing"}` + "\n",
	})
	for _, indexed := range []bool{false, true} {
		if indexed {
			if err := os.WriteFile(filepath.Join(root, "trials.jsonl"), []byte(`{"trial":"billing-trial","task":"sample","reward":0}`+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		trials, err := LoadReportTrials(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(trials) != 1 || trials[0].InfraReason != InfraProviderBilling {
			t.Fatalf("indexed=%v trials=%+v", indexed, trials)
		}
	}
}

func TestRunFinishedEventPreventsCancellationClassification(t *testing.T) {
	dir := writeTrialFiles(t, t.TempDir(), "trial", map[string]string{
		"result.json":                  harborResult(t, "trial", 0, "CancelledError", ""),
		"verifier/ctrf.json":           passingVerifier,
		"agent/excalibur/events.jsonl": `{"event":"run_finished","state":"completed"}` + "\n",
	})
	if got := TrialInfraReason(dir, 0); got != "" {
		t.Fatalf("late cancellation = %q", got)
	}
}
