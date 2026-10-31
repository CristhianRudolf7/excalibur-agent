// Delivery report tests (Section 7.6; verification row 7.7.2):
// pausing and blocking a run emits a report holding every field,
// with plain-language states.
package tasks

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func fullInput() ReportInput {
	return ReportInput{
		State:        types.RunPaused,
		Reason:       string(types.PauseBudget),
		Summary:      "export ran out of budget mid-suite",
		Deliverables: []string{"docs/invoice.md"},
		Evidence:     []types.EvidenceID{"evd_01JTESTTESTTESTTEST1"},
		Files:        []string{"pdf/export.go"},
		Assumptions:  []string{"landscape A4"},
		Questions:    []string{"which paper size?"},
		Gate:         GateReport{OK: false, Pendings: []string{"C1.2 without fresh evidence (stale)"}},
		Metrics:      ReportMetrics{InputTokens: 1200, OutputTokens: 300, CostUSD: 0.02, ToolCalls: 9},
	}
}

func TestBuildReportHoldsEveryField(t *testing.T) {
	rep := BuildReport(fullInput())
	if rep.State != types.RunPaused || rep.Reason != string(types.PauseBudget) {
		t.Fatalf("state/reason lost: %+v", rep)
	}
	if rep.Summary == "" || len(rep.Deliverables) != 1 || len(rep.Evidence) != 1 ||
		len(rep.Files) != 1 || len(rep.Assumptions) != 1 || len(rep.Questions) != 1 {
		t.Fatalf("report fields missing: %+v", rep)
	}
	if len(rep.Pendings) != 1 || rep.Metrics.ToolCalls != 9 || rep.Metrics.CostUSD != 0.02 {
		t.Fatalf("pendings/metrics missing: %+v", rep)
	}
	if rep.PlainState != "incomplete" {
		t.Fatalf("paused-by-budget must read incomplete, got %q", rep.PlainState)
	}
}

func TestPlainLanguageStates(t *testing.T) {
	cases := map[types.RunState]string{
		types.RunCompleted:        "done",
		types.RunCompletedPending: "incomplete",
		types.RunPaused:           "incomplete",
		types.RunBlocked:          "blocked",
		types.RunFailed:           "failed",
		types.RunCanceled:         "canceled",
	}
	for state, want := range cases {
		if got := PlainLanguage(state, ""); got != want {
			t.Fatalf("state %q reads %q, want %q", state, got, want)
		}
	}
}

func TestBlockedReportKeepsPendings(t *testing.T) {
	in := fullInput()
	in.State = types.RunBlocked
	in.Reason = string(types.BlockLoop)
	rep := BuildReport(in)
	if rep.PlainState != "blocked" || len(rep.Pendings) != 1 {
		t.Fatalf("blocked report wrong: %+v", rep)
	}
}
