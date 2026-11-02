package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestVerifierReusesDeliverableAndEvidenceVersion(t *testing.T) {
	model := &fakeEndpoint{responses: []providers.ChatResponse{verifierPassResp(), verifierPassResp(), verifierPassResp(), verifierPassResp(), verifierPassResp()}}
	e, _ := testEngine(model, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = t.TempDir()
	r.Request = "Create " + filepath.Join(r.Workdir, "result.py")
	path := filepath.Join(r.Workdir, "result.py")
	if err := os.WriteFile(path, []byte("print(1)"), 0600); err != nil {
		t.Fatal(err)
	}
	history := []tasks.HistoryTool{{Name: "exec", Args: `{"command":"python3 result.py"}`, Output: "1"}}
	drafts := []tasks.DraftCriterion{{ID: "D1", Key: path, Method: types.CriterionCommand}}
	cfg := *verifyConfig()
	check := func() {
		t.Helper()
		ok, why := e.runStrongVerifier(context.Background(), r, cfg, drafts, history, "delivery prose can change")
		if !ok {
			t.Fatal(why)
		}
	}
	check()
	check()
	check()
	if model.calls() != 1 {
		t.Fatalf("validation/confirmation/refresh called verifier %d times", model.calls())
	}
	if err := os.WriteFile(path, []byte("print(2)"), 0600); err != nil {
		t.Fatal(err)
	}
	check()
	if model.calls() != 2 {
		t.Fatal("edited deliverable reused verdict")
	}
	history[0].Output = "2"
	check()
	if model.calls() != 3 {
		t.Fatal("new evidence reused verdict")
	}
	r.Request += " with a newline"
	check()
	if model.calls() != 4 {
		t.Fatal("new instruction reused verdict")
	}
	t.Setenv("EXCALIBUR_VERDICT_REUSE", "0")
	check()
	if model.calls() != 5 {
		t.Fatal("switch ignored")
	}
}
