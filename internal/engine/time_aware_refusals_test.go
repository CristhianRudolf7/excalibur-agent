package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const propertyFailReply = "D1: PASS - report exists\nPROPERTY: FAIL - display the requested chart | captured image shows an empty panel\nUNMET_PROPERTY: display the requested chart | captured image shows an empty panel\nVERDICT: FAIL\nREASONS: requested chart has not been demonstrated"

func propertyRefusalRun(t *testing.T, percent int, source string) (*Engine, *Run, []string) {
	t.Helper()
	model := &fakeEndpoint{responses: []providers.ChatResponse{{Message: providers.Message{Content: propertyFailReply}}}}
	e, _ := testEngine(model, scriptedRunner(), passingGate())
	r := explicitRun()
	r.Workdir = t.TempDir()
	r.Request = "Create report.txt and display the requested chart"
	r.Profile.WallSource = source
	now := time.Now()
	e.deps.Clock = func() time.Time { return now }
	r.StartedAt = now.Add(-time.Duration(percent) * time.Second)
	r.Tracker.Limit.MaxSeconds = 100
	cfg := *verifyConfig()
	cfg.EnableGeneralizeChecks = false
	drafts := []tasks.DraftCriterion{{ID: "D1", Key: "report.txt", Statement: "report.txt exists"}}
	ok, reasons := e.runStrongVerifier(context.Background(), r, cfg, drafts, nil, "done")
	if ok || len(reasons) == 0 {
		t.Fatalf("missing failed verdict: %v", reasons)
	}
	return e, r, []string{"independent verifier disagreement: " + strings.Join(reasons, "; ")}
}

func TestTimeAwareRefusalBudgetBands(t *testing.T) {
	for _, tc := range []struct {
		percent  int
		source   string
		wantHard bool
	}{
		{0, "explicit", true}, {49, "explicit", true}, {50, "explicit", false}, {69, "explicit", false}, {70, "explicit", false}, {85, "explicit", false},
		{10, "default", false}, {10, "", false},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.source, tc.percent), func(t *testing.T) {
			e, r, reasons := propertyRefusalRun(t, tc.percent, tc.source)
			for i := 0; i < 2; i++ {
				e.countBlockingReasons(r, reasons)
				addFinishCheck(r, fmt.Sprint(i), "check report.txt", false)
			}
			hard := e.hardFinishReasons(r, reasons)
			if (len(hard) > 0) != tc.wantHard {
				t.Fatalf("hard=%v, want blocking=%t", hard, tc.wantHard)
			}
			note := e.timeAwareRefusalNote(r, reasons)
			if tc.wantHard && (!strings.Contains(note, fmt.Sprintf("%d s remain", 100-tc.percent)) || !strings.Contains(note, "display the requested chart")) {
				t.Fatalf("missing time/property: %q", note)
			}
		})
	}
}

func TestTimeAwareRefusalNoProgressStopsAfterTwo(t *testing.T) {
	e, r, reasons := propertyRefusalRun(t, 10, "explicit")
	for i := 0; i < 2; i++ {
		if len(e.hardFinishReasons(r, reasons)) == 0 {
			t.Fatalf("refusal %d prematurely advisory", i+1)
		}
		e.countBlockingReasons(r, reasons)
	}
	if hard := e.hardFinishReasons(r, reasons); len(hard) != 0 {
		t.Fatalf("unchanged refusals looped: %v", hard)
	}
	addFinishCheck(r, "progress", "check report.txt", false)
	if hard := e.hardFinishReasons(r, reasons); len(hard) == 0 {
		t.Fatal("new tool activity did not restore early property check")
	}
	t.Setenv(timeAwareRefusalCapSwitch, "0")
	if hard := e.hardFinishReasons(r, reasons); len(hard) != 0 {
		t.Fatal("disabled time-aware policy bypassed legacy cap")
	}
}

func TestTimeAwareRefusalScopeAndSubstance(t *testing.T) {
	e, r, reasons := propertyRefusalRun(t, 10, "explicit")
	if !e.protectVerifierRefusal(r, reasons) {
		t.Fatal("specific verifier failure was not protected")
	}
	r.Profile.ExplicitFinish = false
	if e.protectVerifierRefusal(r, reasons) {
		t.Fatal("assistant profile changed")
	}
	r.Profile.ExplicitFinish = true
	r.Tracker.Limit.MaxSeconds = 0
	if e.protectVerifierRefusal(r, reasons) {
		t.Fatal("missing wall limit treated as explicit budget")
	}
	r.Tracker.Limit.MaxSeconds = 100
	r.finishVerifierReport.properties = nil
	if e.protectVerifierRefusal(r, reasons) {
		t.Fatal("generic failure escaped cap")
	}
	r.finishVerifierReport.properties = []string{"specific property"}
	if e.protectVerifierRefusal(r, []string{"independent verifier unavailable"}) {
		t.Fatal("unavailable verifier escaped cap")
	}
}

func TestSubstantiveVerifierReasonsBindLiteralRequest(t *testing.T) {
	request := "Create report.txt and display the requested chart"
	for _, tc := range []struct {
		reply  string
		drafts []tasks.DraftCriterion
		want   int
	}{
		{propertyFailReply, nil, 1},
		{"UNMET_PROPERTY: invented feature | absent\nVERDICT: FAIL", nil, 0},
		{"UNMET_PROPERTY: display the requested chart | missing\nVERDICT: PASS", nil, 0},
		{"VERDICT: FAIL\nREASONS: uncertain", nil, 0},
		{"D1: FAIL - output file empty\nVERDICT: FAIL", []tasks.DraftCriterion{{ID: "D1", Key: "report.txt"}}, 1},
		{"D1: FAIL - output file empty\nVERDICT: FAIL", []tasks.DraftCriterion{{ID: "D1", Key: "invented.txt"}}, 0},
	} {
		got := substantiveVerifierReasons(tc.reply, request, tc.drafts)
		if len(got) != tc.want {
			t.Fatalf("reply=%q: properties=%v want %d", tc.reply, got, tc.want)
		}
	}
}

func TestTimeAwareFinishRefusesWithRemainingTimeThenAcceptsNoProgress(t *testing.T) {
	e, r, reasons := propertyRefusalRun(t, 10, "explicit")
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	e.deps.Gate = GateFunc(func(*Run) tasks.GateReport { return tasks.GateReport{Pendings: reasons} })
	startRun(t, e, r, journal.NewMemStore())
	for i := 0; i < 6; i++ {
		if i < 3 {
			addFinishCheck(r, fmt.Sprint(i), "check report.txt", false)
		}
		done, err := e.execFinishBatch(finishTestContext(t), r, toolCall(fmt.Sprintf("finish-%d", i), "finish", finishArgs("done", "checked")), "")
		if err != nil {
			t.Fatal(err)
		}
		if done != (i == 5) {
			t.Fatalf("finish %d: done=%t", i+1, done)
		}
		if i < 4 {
			output := r.History[len(r.History)-1].ToolResult.Output
			if !strings.Contains(output, "90 s remain") || !strings.Contains(output, "display the requested chart") {
				t.Fatalf("refusal lacks recovery guidance: %s", output)
			}
		}
	}
	if r.FinishRefusals != 4 {
		t.Fatalf("refusals=%d", r.FinishRefusals)
	}
	mustState(t, r, types.RunCompleted)
}

func TestTimeAwareSubstantiveFailBypassesSoftVerifierAllowance(t *testing.T) {
	e, r, _ := propertyRefusalRun(t, 10, "explicit")
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		{Message: providers.Message{Content: propertyFailReply}},
		{Message: providers.Message{Content: propertyFailReply}},
		{Message: providers.Message{Content: propertyFailReply}},
	}}
	e.deps.Models = model
	r.Request = "Create " + r.Workdir + "/report.txt and display the requested chart"
	e.deps.Artifacts = artifactOffCfg()
	cfg := *verifyConfig()
	cfg.EnableGeneralizeChecks = false
	cfg.EnableExtraction = true
	cfg.EnableVerification = false
	cfg.EnableContrast = false
	cfg.EnableStrongExercise = false
	e.deps.SelfVerify = &cfg
	for i := 0; i < 3; i++ {
		addFinishCheck(r, fmt.Sprint(i), fmt.Sprintf("check report.txt %d", i), false)
		rep := e.selfVerify(context.Background(), r, tasks.GateReport{OK: true}, "done")
		if rep.OK {
			t.Fatalf("substantive failure %d downgraded by soft verifier: %+v", i+1, rep)
		}
	}
}
