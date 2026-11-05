package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestAutonomousApprovedSpecHandoff(t *testing.T) {
	history := []master.Normalized{{Role: "user", Text: "Mi cliente aprobó estos TDR.", Attachments: []string{"doc_client"}}}
	raw, ok := autonomousDeliveryCard(history, master.Normalized{Text: "Sí, prográmalo todo y pásame un enlace de Cloudflare para verlo."})
	if !ok {
		t.Fatal("approved spec was not delegated")
	}
	for _, criterion := range []string{"doc_client", "Every numbered source requirement", "Dashboard totals", "app-ui-quality", "delivery-evidence.json", "Ask zero questions", "descriptive identifiers", "formatters", "MAX(existing_number) + 1", "Never use count+1", "data_delivery", "isolated storage", "clean seed", "unique, specific KPI", "zero", "seeded history"} {
		if !strings.Contains(string(raw), criterion) {
			t.Fatal("handoff missing", criterion)
		}
	}
	for _, text := range []string{"Mi cliente aprobó estos TDR.", "Do not build it yet", "What do you think of it?"} {
		if _, ok := autonomousDeliveryCard(history, master.Normalized{Text: text}); ok {
			t.Fatal("unsolicited build", text)
		}
	}
	if _, ok := autonomousDeliveryCard(nil, master.Normalized{Text: "Build it all"}); ok {
		t.Fatal("missing spec accepted")
	}
}

func TestAutonomousTurnStartsWorkerWithoutConversationalQuestion(t *testing.T) {
	requests := make(chan providers.ChatRequest, 1)
	fake := timingEndpoint{chat: func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		select {
		case requests <- req:
		default:
		}
		<-ctx.Done()
		return providers.ChatResponse{}, ctx.Err()
	}}
	b, s := assistantTestWiring(t, false, nil)
	s.delegations.owner.endpoint = fake
	s.engineOptions.Models = fake
	s.workspaceDataDir = t.TempDir()
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	text, tree, err := a.CompleteTurn(context.Background(), "cnv_spec", master.Normalized{Text: "Sí, prográmalo todo y pásame un enlace de Cloudflare para verlo.", Attachments: []string{"doc_client"}})
	if err != nil || strings.ContainsAny(text, "?¿") || len(tree) < 2 || len(s.Projects.List()) != 1 {
		t.Fatal(text, tree, err)
	}
	select {
	case req := <-requests:
		var content strings.Builder
		for _, msg := range req.Messages {
			content.WriteString(msg.Content)
		}
		for _, required := range []string{"Implement the complete attached client specification", "doc_client", "delivery-evidence.json", "The user explicitly requested a public preview", "Never use count+1", "data_delivery", "isolated storage", "clean seed", "unique, specific KPI", "zero", "seeded history"} {
			if !strings.Contains(content.String(), required) {
				t.Errorf("child missing %s", required)
			}
		}
		found := false
		previewFound := false
		inspectFound := false
		for _, def := range req.Tools {
			if def.Name == "ask_user" {
				t.Error("autonomous child can ask user")
			}
			if def.Name == "inspect_image" {
				inspectFound = true
			}
			if def.Name == "public_preview" {
				previewFound = true
			}
			if def.Name == "delivery_check" {
				found = true
			}
		}
		if !found {
			t.Error("delivery gate tool unavailable")
		}
		if !previewFound {
			t.Error("authorized worker cannot publish preview")
		}
		if !inspectFound {
			t.Error("worker cannot inspect screenshot evidence")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker never started")
	}
	parentCompleted, childRunning := false, false
	for _, event := range b.Bus.Replay(types.Cursor{}) {
		if event.Type != types.EventRunProgress {
			continue
		}
		var progress api.RunProgressEvent
		if err := json.Unmarshal(event.Payload, &progress); err != nil {
			t.Fatal(err)
		}
		if progress.ConversationID != "cnv_spec" || progress.TurnID != tree[0] {
			t.Fatalf("missing autonomous coding association: %+v", progress)
		}
		if string(progress.RunID) == tree[0] {
			parentCompleted = parentCompleted || progress.State == types.RunCompleted
		} else {
			if progress.ParentRunID != tree[0] {
				t.Fatalf("missing autonomous parent: %+v", progress)
			}
			childRunning = childRunning || progress.State == types.RunRunning
		}
	}
	if !parentCompleted || !childRunning {
		t.Fatalf("autonomous handoff activity: parent completed=%v child running=%v", parentCompleted, childRunning)
	}
}

func evidenceFixture(t *testing.T) (*deliveryEvidence, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	var source strings.Builder
	requirements := []any{}
	for id := 1; id <= 18; id++ {
		fmt.Fprintf(&source, "%d. Required behavior\n", id)
		requirements = append(requirements, map[string]any{"id": id, "feature": fmt.Sprintf("Feature %d", id), "files": []string{"app.js"}, "check": "node verify-api.js"})
	}
	for _, name := range []string{"app.js", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, size := range []struct {
		name          string
		width, height int
	}{{"desktop.png", 1440, 900}, {"mobile.png", 390, 844}} {
		f, err := os.Create(filepath.Join(dir, size.name))
		if err != nil {
			t.Fatal(err)
		}
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
		err = encoder.Encode(f, image.NewRGBA(image.Rect(0, 0, size.width, size.height)))
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	screens := []any{}
	for _, name := range []string{"login", "dashboard"} {
		screens = append(screens, map[string]any{"name": name, "desktop": "desktop.png", "mobile": "mobile.png", "reviewed": true})
	}
	e := newDeliveryEvidence(dir, source.String())
	for _, path := range []string{"desktop.png", "mobile.png"} {
		args, _ := json.Marshal(map[string]string{"path": path})
		e.observe("inspect_image", args, tools.Result{Status: tools.StatusOK})
	}
	formatterExit := 0
	e.observe("exec", json.RawMessage(`{"command":"prettier --check ."}`), tools.Result{Status: tools.StatusOK, ExitCode: &formatterExit})
	manifest := map[string]any{"formatters": []deliveryFormatter{{Check: "prettier --check .", Files: []string{"app.js"}}}, "requirements": requirements, "screens": screens, "database_totals_check": "node verify-api.js", "demo_data_check": "node verify-api.js", "assumptions": []string{"Local demo access"}}
	writeBrowserFixture(t, e, manifest, browserReportFixture())
	manifest["data_delivery"] = deliveryDataEvidence{Check: "node verify-delivery-data.mjs", Report: "data-delivery-report.json"}
	writeDataFixture(t, e, dataReportFixture())
	return e, manifest
}
func writeEvidence(t *testing.T, e *deliveryEvidence, manifest map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(e.dir, "delivery-evidence.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestDeliveryEvidenceRequiresAll18AndExecutedChecks(t *testing.T) {
	e, manifest := evidenceFixture(t)
	if e.check().OK {
		t.Fatal("empty delivery completed")
	}
	writeEvidence(t, e, manifest)
	if e.check().OK {
		t.Fatal("unexecuted claims accepted")
	}
	success := 0
	e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusOK, ExitCode: &success})
	writeDataFixture(t, e, dataReportFixture())
	if report := e.check(); !report.OK {
		t.Fatal(report)
	}
	requirements := manifest["requirements"].([]any)
	manifest["requirements"] = requirements[:17]
	writeEvidence(t, e, manifest)
	if report := e.check(); report.OK || !strings.Contains(strings.Join(report.Pendings, ";"), "requirement 18") {
		t.Fatal(report)
	}
	manifest["requirements"] = requirements
	writeEvidence(t, e, manifest)
	failure := 1
	e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusToolError, ExitCode: &failure})
	if e.check().OK {
		t.Fatal("latest failing check accepted")
	}
}

func TestDeliveryEvidenceScreensAndBackgroundChecks(t *testing.T) {
	e, manifest := evidenceFixture(t)
	writeEvidence(t, e, manifest)
	success := 0
	e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusOK, Sessions: []tools.SessionReceipt{{ID: "session-check", Alive: true}}})
	if e.check().OK {
		t.Fatal("running check accepted")
	}
	e.observe("exec_io", json.RawMessage(`{"session":"unrelated"}`), tools.Result{Status: tools.StatusOK, ExitCode: &success})
	if e.check().OK {
		t.Fatal("unrelated exit accepted")
	}
	e.observe("exec_io", json.RawMessage(`{"session":"session-check"}`), tools.Result{Status: tools.StatusOK, ExitCode: &success})
	writeDataFixture(t, e, dataReportFixture())
	if report := e.check(); !report.OK {
		t.Fatal(report)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "mobile.png"), []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	if report := e.check(); report.OK || !strings.Contains(strings.Join(report.Pendings, ";"), "390x844") {
		t.Fatal(report)
	}
	e = newDeliveryEvidence(t.TempDir(), "1. Inventory, sales, purchases, suppliers, customers and stock movements")
	if len(e.requiredScreens) != 14 {
		t.Fatal(e.requiredScreens)
	}
}

func TestDeliveryEndpointReturnsIncompleteFinalToWork(t *testing.T) {
	ready := false
	endpoint := &deliveryEndpoint{ModelEndpoint: timingEndpoint{chat: func(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
		return textReply("Should I finish with just login?"), nil
	}}, check: func() tasks.GateReport {
		return tasks.GateReport{OK: ready, Pendings: []string{"17 modules still unverified"}}
	}}
	for i := 0; i < 4; i++ {
		response, _, err := endpoint.Chat(context.Background(), types.RoleInterlocutor, providers.ChatRequest{})
		if err != nil || response.Message.Content != "" || len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "delivery_check" {
			t.Fatal(response, err)
		}
	}
	ready = true
	response, _, err := endpoint.Chat(context.Background(), types.RoleInterlocutor, providers.ChatRequest{})
	if err != nil || len(response.Message.ToolCalls) != 0 {
		t.Fatal(response, err)
	}
}

func TestApprovedSpecAcknowledgementHasNoQuestion(t *testing.T) {
	b, s := assistantTestWiring(t, false, func(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
		t.Error("approval needlessly called conversational model")
		return textReply("Please register a project?"), nil
	})
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	text, _, err := a.CompleteTurn(context.Background(), "cnv_approval", master.Normalized{Text: "Mi cliente aprobó estos TDR.", Attachments: []string{"doc_spec"}})
	if err != nil || strings.ContainsAny(text, "?¿") || text != "Recibí los TDR aprobados." || len(s.Projects.List()) != 0 {
		t.Fatal(text, err)
	}
	e, manifest := evidenceFixture(t)
	writeEvidence(t, e, manifest)
	for _, spanish := range []bool{false, true} {
		summary := e.summary(spanish)
		if strings.ContainsAny(summary, "?¿") || !strings.Contains(summary, "18") || !strings.Contains(summary, "README.md") {
			t.Fatal(summary)
		}
	}
}

func TestDeliveryEvidenceRefreshesSourceAndRejectsStaleChecks(t *testing.T) {
	e, manifest := evidenceFixture(t)
	writeEvidence(t, e, manifest)
	success := 0
	e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusOK, ExitCode: &success})
	writeDataFixture(t, e, dataReportFixture())
	// A deterministic file time avoids filesystem timestamp granularity races.
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(e.dir, "app.js"), future, future); err != nil {
		t.Fatal(err)
	}
	if report := e.check(); report.OK || !strings.Contains(strings.Join(report.Pendings, ";"), "changed after its check") {
		t.Fatal(report)
	}
	late := newDeliveryEvidence(t.TempDir(), "Document is still being processed")
	late.refresh = func() string { return "RF-01: Login\nREQ-18: README" }
	_ = late.check()
	if len(late.required) != 18 {
		t.Fatal("late source did not supply all IDs", late.required)
	}
}

func TestDeliveryEndpointPreservesWorkBatchedWithPrematureFinish(t *testing.T) {
	endpoint := &deliveryEndpoint{ModelEndpoint: timingEndpoint{chat: func(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "check-api", Name: "exec", Arguments: json.RawMessage(`{"command":"node verify-api.js"}`)}, {ID: "finish", Name: "finish", Arguments: json.RawMessage(`{"summary":"done","evidence":"pending"}`)}}}}, nil
	}}, check: func() tasks.GateReport { return tasks.GateReport{Pendings: []string{"run API checks"}} }}
	response, _, err := endpoint.Chat(context.Background(), types.RoleInterlocutor, providers.ChatRequest{})
	if err != nil || len(response.Message.ToolCalls) != 2 || response.Message.ToolCalls[0].Name != "exec" || response.Message.ToolCalls[1].Name != "delivery_check" {
		t.Fatal(response, err)
	}
}

func TestDeliveryEvidenceRequiresScreenshotInspectionReceipts(t *testing.T) {
	e, manifest := evidenceFixture(t)
	writeEvidence(t, e, manifest)
	success := 0
	e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusOK, ExitCode: &success})
	writeDataFixture(t, e, dataReportFixture())
	e.inspected = map[string]time.Time{}
	report := e.check()
	if report.OK || !strings.Contains(strings.Join(report.Pendings, ";"), "inspect screenshot") {
		t.Fatal("review claim accepted without inspection", report)
	}
}
