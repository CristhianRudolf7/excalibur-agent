package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func TestExampleFormatFullAndRecoveryReview(t *testing.T) {
	for _, generalize := range []bool{true, false} {
		var sizes [2][2]int
		for mode, value := range []string{"off", ""} {
			t.Setenv("EXCALIBUR_EXAMPLE_FORMAT", value)
			model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(""), verifierPassResp()}}
			e, _ := testEngine(model, scriptedRunner(), passingGate())
			r := explicitRun()
			r.Request = "Create /tmp/transform.py accepting different input files."
			cfg := *verifyConfig()
			cfg.EnableGeneralizeChecks = generalize
			e.runStrongVerifier(context.Background(), r, cfg, tasks.ExtractDraftCriteria(r.Request, 6), nil, "done")
			if len(model.requests) != 2 {
				t.Fatal("expected recovery", len(model.requests))
			}
			for i, req := range model.requests {
				prompt := req.Messages[0].Content
				sizes[mode][i] = len(prompt)
				for _, want := range []string{"example values cannot be reproduced", "format-only", "offsets, scaling or renaming", "second, different input created by the executor"} {
					if strings.Contains(prompt, want) != (mode == 1) {
						t.Fatalf("review %d generalize=%v mode=%d guidance %q missing or unexpected", i, generalize, mode, want)
					}
				}
			}
		}
		t.Logf("O2 generalize=%v full bytes: off=%d on=%d delta=%+d; recovery off=%d on=%d delta=%+d", generalize, sizes[0][0], sizes[1][0], sizes[1][0]-sizes[0][0], sizes[0][1], sizes[1][1], sizes[1][1]-sizes[0][1])
	}
}
