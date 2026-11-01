package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func TestGeneralizationVerifierReturnsMissingEvidenceToExecutor(t *testing.T) {
	for _, tc := range []struct {
		name, reply   string
		enabled, pass bool
	}{
		{"visible reference insufficient", "FRESH_INPUTS: FAIL - only visible example\nPROPERTY: PASS - JSON parsed\nOUTPUT_CONTRACT: PASS\nVERDICT: PASS", true, false},
		{"output shape missing", "FRESH_INPUTS: PASS - new size\nPROPERTY: FAIL - shape untested\nOUTPUT_CONTRACT: FAIL\nVERDICT: PASS", true, false},
		{"missing report", "VERDICT: PASS", true, false},
		{"fresh input and format", "FRESH_INPUTS: PASS - generated seed 19 checked against independent oracle\nPROPERTY: PASS - assert integer type\nPROPERTY: PASS - assert keys in order\nOUTPUT_CONTRACT: PASS\nVERDICT: PASS", true, true},
		{"ablation", "VERDICT: PASS", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(tc.reply)}}
			e, _ := testEngine(model, scriptedRunner(), passingGate())
			run := testRun(Profile{})
			store := journal.NewMemStore()
			e.AttachJournal(run.ID, store)
			run.Request = "Write /app/convert.py: convert integer lists to JSON with fields count,sum in that order, integer values."
			cfg := tasks.DefaultSelfVerifyConfig()
			cfg.EnableGeneralizeChecks = tc.enabled
			ok, reasons := e.runStrongVerifier(context.Background(), run, cfg, tasks.ExtractDraftCriteria(run.Request, 6), nil, "done")
			if ok != tc.pass {
				t.Fatalf("pass=%v reasons=%v", ok, reasons)
			}
			if !ok && len(reasons) == 0 {
				t.Fatal("executor needs actionable reasons")
			}
			entries, err := store.List(context.Background(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.enabled && (len(entries) != 1 || entries[0].Type != journal.EntryType("generalization_check")) {
				t.Fatalf("coverage not recorded: %v", entries)
			}
			if !tc.enabled && len(entries) != 0 {
				t.Fatal("ablation recorded coverage")
			}
			if model.calls() != 1 {
				t.Fatal("must reuse single independent verifier pass")
			}
		})
	}
}

func TestGeneralizationReviewsMethodWithoutExtractedDrafts(t *testing.T) {
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("FRESH_INPUTS: FAIL - no fresh generated inputs\nOUTPUT_CONTRACT: NA\nVERDICT: FAIL")}}
	e, _ := testEngine(model, scriptedRunner(), passingGate())
	run := testRun(Profile{})
	run.Request = "Implement a function that sorts integers."
	history := []tasks.HistoryTool{{Name: "exec", Args: `{"command":"python3 -c 'assert sort([2,1]) == [1,2]'"}`, Output: "ok"}}
	ok, pending := e.runStrongVerifier(context.Background(), run, tasks.DefaultSelfVerifyConfig(), nil, history, "done")
	if ok || len(pending) == 0 || model.calls() != 1 {
		t.Fatalf("draft-free method bypassed review: ok=%v pending=%v calls=%d", ok, pending, model.calls())
	}
}

func TestGeneralizationVerifierErrorHonorsSwitch(t *testing.T) {
	t.Setenv("EXCALIBUR_VERIFIER_VERDICT", "off")
	for _, enabled := range []bool{false, true} {
		model := &fakeEndpoint{err: errors.New("simulated unavailable verifier")}
		e, _ := testEngine(model, scriptedRunner(), passingGate())
		run := testRun(Profile{})
		run.Request = "Create converter.py."
		cfg := tasks.DefaultSelfVerifyConfig()
		cfg.EnableGeneralizeChecks = enabled
		ok, pending := e.runStrongVerifier(context.Background(), run, cfg, tasks.ExtractDraftCriteria(run.Request, 6), nil, "done")
		if ok == enabled || (enabled && len(pending) == 0) {
			t.Fatalf("enabled=%v ok=%v pending=%v", enabled, ok, pending)
		}
	}
}
