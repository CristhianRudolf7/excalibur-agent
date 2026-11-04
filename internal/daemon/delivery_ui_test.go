package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func browserReportFixture() deliveryBrowserReport {
	report := deliveryBrowserReport{Runner: "playwright"}
	for _, name := range []string{"login", "dashboard"} {
		report.Screens = append(report.Screens, deliveryBrowserScreen{
			Name: name, URL: "http://127.0.0.1:5308/" + name, Discovered: 1,
			Controls: []deliveryControl{{ID: "open", Label: "Open", Selector: "button", Kind: "button"}},
		})
		report.Checks = append(report.Checks, deliveryControlCheck{
			Screen: name, Control: "open", Action: "click", Effect: "dialog", Target: "[role=dialog]",
			Before: json.RawMessage(`false`), After: json.RawMessage(`true`), Passed: true,
		})
	}
	return report
}

func writeBrowserFixture(t *testing.T, e *deliveryEvidence, manifest map[string]any, report deliveryBrowserReport) {
	t.Helper()
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "browser-report.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	success := 0
	e.observe("exec", json.RawMessage(`{"command":"node check-browser.mjs"}`), tools.Result{Status: tools.StatusOK, ExitCode: &success})
	manifest["ui"] = deliveryUIEvidence{Check: "node check-browser.mjs", Report: "browser-report.json"}
}

func TestDeliveryControlEffectRequiresLabelledStateChange(t *testing.T) {
	for _, tc := range []struct {
		name, kind, effect, before, after string
		valid                             bool
	}{
		{"search filters", "search", "rows", `["alpha","beta"]`, `["alpha"]`, true},
		{"search only notice", "search", "notice", `""`, `"Filter: alpha"`, false},
		{"search only input", "search", "form", `{"values":{"search":""}}`, `{"values":{"search":"alpha"}}`, false},
		{"filter unchanged", "filter", "rows", `["alpha"]`, `["alpha"]`, false},
		{"filter narrows", "filter", "rows", `[1,2,3]`, `[2]`, true},
		{"filter only reorders", "filter", "rows", `[1,2]`, `[2,1]`, false},
		{"detail renders", "detail", "detail", `null`, `{"visible":true,"record":{"id":7,"items":[{"id":3}]}}`, true},
		{"detail flat response", "detail", "detail", `null`, `[{"id":7}]`, false},
		{"detail empty", "detail", "detail", `null`, `{"visible":true,"record":{}}`, false},
		{"detail hidden", "detail", "detail", `null`, `{"visible":false,"record":{"id":7}}`, false},
		{"add fourth item", "add-item", "items", `[1,2,3]`, `[1,2,3,4]`, true},
		{"remove item", "remove-item", "items", `[1,2]`, `[1]`, true},
		{"fixed item rows", "add-item", "items", `[1,2,3]`, `[1,2,3]`, false},
		{"wrong direction", "remove-item", "items", `[1]`, `[1,2]`, false},
		{"field edits", "field", "form", `{"values":{"name":""}}`, `{"values":{"name":"Example"}}`, true},
		{"validation", "form", "form", `{"errors":{}}`, `{"errors":{"name":"Required"}}`, true},
		{"form saves", "form", "records", `[]`, `[{"id":7}]`, true},
		{"form notice", "form", "records", `{"message":"before"}`, `{"message":"saved"}`, false},
		{"link navigates", "link", "route", `"/home"`, `"/settings"`, true},
		{"link toast", "link", "route", `"home"`, `"opened"`, false},
		{"button dialog", "button", "dialog", `false`, `true`, true},
		{"button notice", "button", "notice", `false`, `true`, false},
		{"invalid json", "button", "records", `oops`, `{}`, false},
		{"semantic equality", "button", "records", `{"a":1,"b":2}`, `{"b":2,"a":1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := deliveryControlCheck{Effect: tc.effect, Before: json.RawMessage(tc.before), After: json.RawMessage(tc.after)}
			if got := controlEffect(tc.kind, check); got != tc.valid {
				t.Fatalf("got %v, expected %v", got, tc.valid)
			}
		})
	}
}

func TestDeliveryBrowserReportRejectsDeadAndUncoveredControls(t *testing.T) {
	for _, tc := range []struct {
		name, diagnostic string
		mutate           func(*deliveryBrowserReport)
	}{
		{"missing screen", "screen dashboard", func(r *deliveryBrowserReport) { r.Screens = r.Screens[:1] }},
		{"missing check", "dashboard/open", func(r *deliveryBrowserReport) { r.Checks = r.Checks[:1] }},
		{"failed check", "failed to prove", func(r *deliveryBrowserReport) { r.Checks[0].Passed = false }},
		{"notice only", "notice-only", func(r *deliveryBrowserReport) { r.Checks[0].Effect = "notice" }},
		{"unwired control", "failed to prove", func(r *deliveryBrowserReport) { r.Checks[0].After = r.Checks[0].Before }},
		{"incomplete inventory", "enumerate every discovered", func(r *deliveryBrowserReport) { r.Screens[0].Discovered++ }},
		{"unknown control", "no discovered control", func(r *deliveryBrowserReport) { r.Checks[0].Control = "absent" }},
		{"duplicate control", "unique ID", func(r *deliveryBrowserReport) {
			r.Screens[0].Controls = append(r.Screens[0].Controls, r.Screens[0].Controls[0])
			r.Screens[0].Discovered++
		}},
		{"duplicate screen", "unique nonempty screen", func(r *deliveryBrowserReport) { r.Screens = append(r.Screens, r.Screens[0]) }},
		{"missing selector", "DOM selector", func(r *deliveryBrowserReport) { r.Screens[0].Controls[0].Selector = "" }},
		{"no browser runner", "executed playwright", func(r *deliveryBrowserReport) { r.Runner = "api" }},
		{"no visited url", "visited application URL", func(r *deliveryBrowserReport) { r.Screens[0].URL = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, manifest := evidenceFixture(t)
			report := browserReportFixture()
			tc.mutate(&report)
			writeBrowserFixture(t, e, manifest, report)
			writeEvidence(t, e, manifest)
			pending := strings.Join(e.check().Pendings, "\n")
			if !strings.Contains(pending, tc.diagnostic) {
				t.Fatalf("expected %q in %s", tc.diagnostic, pending)
			}
		})
	}
}

func TestDeliveryRepeatableFormsNeedWorkingAddAndRemove(t *testing.T) {
	e, manifest := evidenceFixture(t)
	report := browserReportFixture()
	report.Screens[1].Controls = []deliveryControl{
		{ID: "editor", Label: "Edit", Selector: "form", Kind: "form", Repeatable: true},
		{ID: "add", Label: "Add item", Selector: "#add", Kind: "add-item", Form: "editor"},
		{ID: "remove", Label: "Remove item", Selector: "#remove", Kind: "remove-item", Form: "editor"},
	}
	report.Screens[1].Discovered = 3
	report.Checks = report.Checks[:1]
	report.Checks = append(report.Checks,
		deliveryControlCheck{Screen: "dashboard", Control: "editor", Action: "submit", Effect: "records", Target: "table", Before: json.RawMessage(`[]`), After: json.RawMessage(`[{"id":1}]`), Passed: true},
		deliveryControlCheck{Screen: "dashboard", Control: "add", Action: "click", Effect: "items", Target: "form", Before: json.RawMessage(`[1,2,3]`), After: json.RawMessage(`[1,2,3,4]`), Passed: true},
	)
	writeBrowserFixture(t, e, manifest, report)
	writeEvidence(t, e, manifest)
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "executed remove-item") {
		t.Fatal(pending)
	}
	report.Checks = append(report.Checks, deliveryControlCheck{Screen: "dashboard", Control: "remove", Action: "click", Effect: "items", Target: "form", Before: json.RawMessage(`[1,2,3,4]`), After: json.RawMessage(`[1,2,3]`), Passed: true})
	writeBrowserFixture(t, e, manifest, report)
	success := 0
	e.observe("exec", json.RawMessage(`{"command":"node verify-api.js"}`), tools.Result{Status: tools.StatusOK, ExitCode: &success})
	writeDataFixture(t, e, dataReportFixture())
	if result := e.check(); !result.OK {
		t.Fatal(result)
	}
	report.Checks[1].Effect = "form"
	report.Checks[1].Before = json.RawMessage(`{"errors":{}}`)
	report.Checks[1].After = json.RawMessage(`{"errors":{"name":"Required"}}`)
	writeBrowserFixture(t, e, manifest, report)
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "submit/read-back") {
		t.Fatal("validation alone accepted as repeatable form persistence", pending)
	}
}

func TestDeliveryBrowserReportRequiresFreshSuccessfulExecution(t *testing.T) {
	e, manifest := evidenceFixture(t)
	writeEvidence(t, e, manifest)
	failure := 1
	e.observe("exec", json.RawMessage(`{"command":"node check-browser.mjs"}`), tools.Result{Status: tools.StatusToolError, ExitCode: &failure})
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "successful executed browser suite") {
		t.Fatal(pending)
	}
	writeBrowserFixture(t, e, manifest, browserReportFixture())
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(e.dir, "browser-report.json"), future, future); err != nil {
		t.Fatal(err)
	}
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "report changed after its check") {
		t.Fatal(pending)
	}
	writeBrowserFixture(t, e, manifest, browserReportFixture())
	if err := os.Chtimes(filepath.Join(e.dir, "app.js"), future, future); err != nil {
		t.Fatal(err)
	}
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "source changed after browser evidence") {
		t.Fatal(pending)
	}
}

func TestDeliveryBrowserReportParsingAndContainment(t *testing.T) {
	for _, content := range []string{
		`not json`, `{"runner":"playwright","unexpected":true}`, `{"runner":"playwright"} {}`,
		`{"runner":"playwright","screens":"claim"}`,
	} {
		e, manifest := evidenceFixture(t)
		if err := os.WriteFile(filepath.Join(e.dir, "browser-report.json"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		writeEvidence(t, e, manifest)
		if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "invalid browser control report") {
			t.Fatal(content, pending)
		}
	}
	e, manifest := evidenceFixture(t)
	manifest["ui"] = deliveryUIEvidence{Check: "node check-browser.mjs", Report: "../browser-report.json"}
	writeEvidence(t, e, manifest)
	if pending := strings.Join(e.check().Pendings, "\n"); !strings.Contains(pending, "inside the workspace") {
		t.Fatal(pending)
	}
}
