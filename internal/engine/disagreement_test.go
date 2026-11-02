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
)

const changedDisagreementNote = "deliverable changed after a disagreement; confirm with concrete samples which version follows the request"

func TestDisagreementRewriteReview(t *testing.T) {
	for _, tc := range []struct {
		name, change  string
		enabled, want bool
	}{
		{"rewritten checked output", "deliverable", true, true},
		{"unchanged output", "none", true, false},
		{"unrelated file", "other", true, false},
		{"new request", "request", true, false},
		{"updated instruction", "directive", true, false},
		{"disabled", "deliverable", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EXCALIBUR_DISAGREEMENT_PROTOCOL", map[bool]string{true: "", false: "off"}[tc.enabled])
			t.Setenv("EXCALIBUR_VERDICT_REUSE", "off")
			model := &fakeEndpoint{responses: []providers.ChatResponse{verifierFailResp(), finalResp(""), verifierPassResp()}}
			e, _ := testEngine(model, scriptedRunner(), passingGate())
			cfg := verifyConfig()
			cfg.EnableGeneralizeChecks = false
			e.deps.SelfVerify = cfg
			e.deps.Artifacts = artifactOff()
			r := explicitRun()
			r.Workdir = t.TempDir()
			r.StartedAt = time.Now().Add(-time.Hour)
			path := filepath.Join(r.Workdir, "output.py")
			r.Request = "Create the file " + path + "."
			if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
				t.Fatal(err)
			}
			addFinishCheck(r, "before", "python3 output.py", false)
			first := e.selfVerify(context.Background(), r, tasks.GateReport{OK: true}, "done")
			if first.OK {
				t.Fatal("expected first verifier refusal", first)
			}
			if tc.enabled && !strings.Contains(strings.Join(first.Pendings, " "), "before changing") {
				t.Fatal("refusal lacks reconciliation guidance", first.Pendings)
			}
			switch tc.change {
			case "deliverable":
				if err := os.WriteFile(path, []byte("modified\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "other":
				if err := os.WriteFile(filepath.Join(r.Workdir, "notes.txt"), []byte("updated"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directive":
				r.literalDirectives = append(r.literalDirectives, "Use the revised interface.")
			case "request":
				r.Request = "Create the file " + path + " with a different interface."
			}
			addFinishCheck(r, "after", "python3 output.py", false)
			e.selfVerify(context.Background(), r, tasks.GateReport{OK: true}, "done")
			if len(model.requests) != 3 {
				t.Fatalf("expected full and recovery review: %d", len(model.requests))
			}
			for i, req := range model.requests {
				prompt := req.Messages[0].Content
				want := tc.want && i > 0
				if strings.Contains(prompt, changedDisagreementNote) != want {
					t.Fatalf("review %d rewrite note want=%v: %s", i, want, prompt)
				}
				if tc.enabled && !strings.Contains(prompt, "concrete samples") {
					t.Fatalf("review %d lost reconciliation guidance", i)
				}
			}
		})
	}
}

func TestDisagreementRecoveryKeepsDirectRuleException(t *testing.T) {
	var sizes [2][2]int
	for mode, value := range []string{"off", ""} {
		t.Setenv("EXCALIBUR_DISAGREEMENT_PROTOCOL", value)
		model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(""), finalResp("FRESH_INPUTS: NA - inspected direct-rule samples\nOUTPUT_CONTRACT: NA\nVERDICT: PASS")}}
		e, _ := testEngine(model, scriptedRunner(), passingGate())
		r := explicitRun()
		r.Request = "Implement a function following the supplied rule."
		history := []tasks.HistoryTool{{Name: "exec", Args: `{"command":"python3 check.py"}`, Output: "assertions passed"}}
		if ok, reasons := e.runStrongVerifier(context.Background(), r, *verifyConfig(), nil, history, "done"); !ok {
			t.Fatal(reasons)
		}
		if len(model.requests) != 2 {
			t.Fatal("expected recovery", len(model.requests))
		}
		for i, req := range model.requests {
			prompt := req.Messages[0].Content
			sizes[mode][i] = len(prompt)
			if strings.Contains(prompt, "FRESH_INPUTS may be NA") != (mode == 1) {
				t.Fatalf("review %d lost switch boundary: %s", i, prompt)
			}
			if mode == 1 && strings.Contains(prompt, "FRESH_INPUTS must cite generated inputs and an independent oracle") {
				t.Fatalf("review %d lost direct-rule exception: %s", i, prompt)
			}
		}
	}
	t.Logf("O1 full review bytes: off=%d on=%d delta=%+d; recovery: off=%d on=%d delta=%+d; rewrite note with newlines=%d", sizes[0][0], sizes[1][0], sizes[1][0]-sizes[0][0], sizes[0][1], sizes[1][1], sizes[1][1]-sizes[0][1], len(changedDisagreementNote)+2)
}
