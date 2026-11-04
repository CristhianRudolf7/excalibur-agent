package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestDeliveryDataProofBlocksPublicationAndCompletion(t *testing.T) {
	e, manifest := evidenceFixture(t)
	exit := 0
	e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusOK, ExitCode: &exit})
	delete(manifest, "data_delivery")
	writeEvidence(t, e, manifest)
	reg := tools.NewMapRegistry()
	if err := reg.Register(&publicPreviewTool{manager: previewTestManager(t), services: &Services{}}); err != nil {
		t.Fatal(err)
	}
	runner := &assistantToolRunner{
		daemonToolRunner: daemonToolRunner{reg: reg},
		allowed:          map[string]bool{"public_preview": true}, delivery: e,
	}
	result, err := runner.Execute(context.Background(), "public_preview", json.RawMessage(`{"action":"publish","port":5308}`), tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusDenied || !strings.Contains(result.Output, "data_delivery.check") {
		t.Fatal(result, err)
	}
	endpoint := &deliveryEndpoint{ModelEndpoint: timingEndpoint{chat: func(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
		return textReply("done"), nil
	}}, check: e.check}
	response, _, err := endpoint.Chat(context.Background(), types.RoleInterlocutor, providers.ChatRequest{})
	if err != nil || len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "delivery_check" {
		t.Fatal(response, err)
	}
}

func dataReportFixture() deliveryDataReport {
	report := deliveryDataReport{DeliveryStorage: "demo-store", SeedFingerprint: strings.Repeat("a", 64), DeliveredFingerprint: strings.Repeat("a", 64)}
	for _, command := range []string{"node verify-api.js", "node check-browser.mjs"} {
		report.Verification = append(report.Verification, deliveryVerificationStorage{Check: command, Storage: "temporary-store"})
	}
	report.Dashboard = dashboardQualityFixture()
	return report
}

func writeDataFixture(t *testing.T, e *deliveryEvidence, report deliveryDataReport) {
	t.Helper()
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "data-delivery-report.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	exit := 0
	e.observe("exec", json.RawMessage(`{"command":"node verify-delivery-data.mjs"}`), tools.Result{Status: tools.StatusOK, ExitCode: &exit})
}

func TestDeliveryDataIsolationAndCleanSeed(t *testing.T) {
	for _, tc := range []struct {
		name, diagnostic string
		mutate           func(*deliveryDataReport)
	}{
		{"valid", "", func(*deliveryDataReport) {}},
		{"shared storage", "isolated storage", func(r *deliveryDataReport) { r.Verification[0].Storage = r.DeliveryStorage }},
		{"whitespace alias", "isolated storage", func(r *deliveryDataReport) { r.Verification[0].Storage = " demo-store " }},
		{"omitted check", "record isolated", func(r *deliveryDataReport) { r.Verification = r.Verification[:1] }},
		{"omitted storage", "isolated storage", func(r *deliveryDataReport) { r.Verification[0].Storage = "" }},
		{"omitted delivery", "canonical delivery_storage", func(r *deliveryDataReport) { r.DeliveryStorage = "" }},
		{"polluted data", "must equal the clean seed", func(r *deliveryDataReport) { r.DeliveredFingerprint = strings.Repeat("b", 64) }},
		{"no seed proof", "SHA-256", func(r *deliveryDataReport) { r.SeedFingerprint, r.DeliveredFingerprint = "", "" }},
		{"prose proof", "SHA-256", func(r *deliveryDataReport) { r.SeedFingerprint, r.DeliveredFingerprint = "passed", "passed" }},
		{"duplicate check", "unique checks", func(r *deliveryDataReport) { r.Verification = append(r.Verification, r.Verification[0]) }},
		{"unexecuted check", "successful verification", func(r *deliveryDataReport) { r.Verification[0].Check = "unexecuted" }},
		{"missing dashboard proof", "inventory KPI", func(r *deliveryDataReport) { r.Dashboard = deliveryDashboardQuality{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, manifest := evidenceFixture(t)
			exit := 0
			e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusOK, ExitCode: &exit})
			report := dataReportFixture()
			tc.mutate(&report)
			writeDataFixture(t, e, report)
			writeEvidence(t, e, manifest)
			gate := e.check()
			if tc.diagnostic == "" {
				if !gate.OK {
					t.Fatal(gate)
				}
			} else if gate.OK || !strings.Contains(strings.Join(gate.Pendings, "\n"), tc.diagnostic) {
				t.Fatal(gate)
			}
		})
	}
}

func TestDeliveryDataProofRequiresFreshSuccessfulExecution(t *testing.T) {
	e, manifest := evidenceFixture(t)
	writeEvidence(t, e, manifest)
	exit := 0
	e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusOK, ExitCode: &exit})
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "after successful verification") {
		t.Fatal(pending)
	}
	writeDataFixture(t, e, dataReportFixture())
	if gate := e.check(); !gate.OK {
		t.Fatal(gate)
	}
	delete(e.passed, "node verify-delivery-data.mjs")
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "data_delivery.check") {
		t.Fatal(pending)
	}
	writeDataFixture(t, e, dataReportFixture())
	exit = 1
	e.observe("exec", json.RawMessage(`{"command":"node verify-delivery-data.mjs"}`), tools.Result{Status: tools.StatusToolError, ExitCode: &exit})
	if e.check().OK {
		t.Fatal("failed seed proof accepted")
	}
	for _, file := range []string{"data-delivery-report.json", "app.js"} {
		writeDataFixture(t, e, dataReportFixture())
		future := time.Now().Add(time.Minute)
		if err := os.Chtimes(filepath.Join(e.dir, file), future, future); err != nil {
			t.Fatal(err)
		}
		if gate := e.check(); gate.OK || !strings.Contains(strings.Join(gate.Pendings, "\n"), "changed after") {
			t.Fatal(gate)
		}
	}
}

func TestDeliveryDataReportParsingAndContainment(t *testing.T) {
	for _, content := range []string{`not json`, `{} {}`, `{"unexpected":true}`, `{"verification":"claim"}`} {
		e, manifest := evidenceFixture(t)
		if err := os.WriteFile(filepath.Join(e.dir, "data-delivery-report.json"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		writeEvidence(t, e, manifest)
		if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "invalid data delivery report") {
			t.Fatal(pending)
		}
	}
	e, manifest := evidenceFixture(t)
	manifest["data_delivery"] = deliveryDataEvidence{Check: "node verify-delivery-data.mjs", Report: "../outside.json"}
	writeEvidence(t, e, manifest)
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "inside the workspace") {
		t.Fatal(pending)
	}
}
