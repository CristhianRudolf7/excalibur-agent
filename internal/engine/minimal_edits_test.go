package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestMinimalValueEditsVerifierRecovery(t *testing.T) {
	var sizes [2][2]int
	for mode, setting := range []string{"off", ""} {
		t.Setenv("EXCALIBUR_MINIMAL_VALUE_EDITS", setting)
		for _, autonomous := range []bool{true, false} {
			for _, generalize := range []bool{true, false} {
				model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(""), finalResp("FRESH_INPUTS: NA - direct replacement\nPROPERTY: PASS\nOUTPUT_CONTRACT: PASS\nVERDICT: PASS")}}
				e, _ := testEngine(model, scriptedRunner(), passingGate())
				r := testRun(Profile{ExplicitFinish: autonomous})
				r.Request = "Replace selected values in settings.txt."
				cfg := *verifyConfig()
				cfg.EnableGeneralizeChecks = generalize
				drafts := []tasks.DraftCriterion{{ID: "D1", Statement: "Replace selected values", Method: types.CriterionInspection}}
				if ok, why := e.runStrongVerifier(context.Background(), r, cfg, drafts, nil, "done"); !ok {
					t.Fatal(why)
				}
				if len(model.requests) != 2 {
					t.Fatal("expected full and recovery review", len(model.requests))
				}
				for i, req := range model.requests {
					prompt := req.Messages[0].Content
					if strings.Contains(prompt, "For replacements or renames, check that only the requested values changed.") != (mode == 1) {
						t.Fatalf("review %d lost minimal-edits rule switch, autonomous=%v generalize=%v", i, autonomous, generalize)
					}
					if autonomous && generalize {
						sizes[mode][i] = len(prompt)
					}
				}
			}
		}
	}
	t.Logf("minimal edits full review: off=%d on=%d delta=%+d; recovery: off=%d on=%d delta=%+d bytes", sizes[0][0], sizes[1][0], sizes[1][0]-sizes[0][0], sizes[0][1], sizes[1][1], sizes[1][1]-sizes[0][1])
}
