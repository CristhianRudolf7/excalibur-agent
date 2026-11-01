package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

func TestBenchReportSeparatesInfrastructure(t *testing.T) {
	root := t.TempDir()
	for _, trial := range []string{"billing", "completed"} {
		files := map[string]string{}
		if trial == "billing" {
			files["result.json"] = `{"task_name":"sample-billing","trial_name":"billing","verifier_result":{"rewards":{"reward":0}}}`
			files["agent/excalibur/model_calls.jsonl"] = `{"provider_error_class":"auth","provider_error_reason":"billing"}` + "\n"
		} else {
			files["result.json"] = `{"task_name":"sample-completed","trial_name":"completed","verifier_result":{"rewards":{"reward":1}}}`
			files["agent/excalibur/final.json"] = `{"state":"completed"}`
			files["verifier/ctrf.json"] = `{"results":{"summary":{"tests":1,"passed":1}}}`
		}
		for name, body := range files {
			path := filepath.Join(root, trial, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	dest := t.TempDir()
	if err := runBenchReport(eval.DatasetTB21, []string{root, "--out", dest, "--k", "1"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dest, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report eval.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.InfraTrials != 1 || report.ScoredTrials != 1 || report.Success.Mean != 1 || report.SuccessWithInfra.Mean != 0.5 || report.InfraReasons[eval.InfraProviderBilling] != 1 {
		t.Fatalf("report: %+v", report)
	}
}
