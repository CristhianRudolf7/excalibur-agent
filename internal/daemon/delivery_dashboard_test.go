package daemon

import (
	"encoding/json"
	"strings"
	"testing"
)

func dashboardQualityFixture() deliveryDashboardQuality {
	var report deliveryDashboardQuality
	err := json.Unmarshal([]byte(`{
  "labels":[
    {"label":"Activity this month","selector":"#activity-kpi"},
    {"label":"Daily activity history","selector":"#history-heading"}
  ],
  "panels":[{"selector":"#attention-panel","count":0,"alert_visible":false,"empty_visible":true}],
  "charts":[{"selector":"#history-chart",
    "seeded":[{"date":"2026-01-01","value":12},{"date":"2026-01-02","value":18}],
    "rendered":[{"date":"2026-01-01","value":12},{"date":"2026-01-02","value":18}]
  }]
}`), &report)
	if err != nil {
		panic(err)
	}
	return report
}

func TestDeliveryDashboardCoherence(t *testing.T) {
	for _, tc := range []struct {
		name, diagnostic string
		mutate           func(*deliveryDashboardQuality)
	}{
		{"valid", "", func(*deliveryDashboardQuality) {}},
		{"duplicate KPI title", "unique, specific labels", func(r *deliveryDashboardQuality) { r.Labels[1].Label = " Activity THIS month " }},
		{"missing label", "unique, specific labels", func(r *deliveryDashboardQuality) { r.Labels[0].Label = "" }},
		{"no label inventory", "inventory KPI", func(r *deliveryDashboardQuality) { r.Labels = nil }},
		{"reused selector", "distinct DOM selectors", func(r *deliveryDashboardQuality) { r.Labels[1].Selector = r.Labels[0].Selector }},
		{"zero-count alert", "zero-count panels", func(r *deliveryDashboardQuality) { *r.Panels[0].AlertVisible = true }},
		{"zero-count no empty state", "zero-count panels", func(r *deliveryDashboardQuality) { *r.Panels[0].EmptyVisible = false }},
		{"unknown count", "needs a unique selector, count", func(r *deliveryDashboardQuality) { r.Panels[0].Count = nil }},
		{"unknown alert visibility", "alert_visible", func(r *deliveryDashboardQuality) { r.Panels[0].AlertVisible = nil }},
		{"negative count", "needs a unique selector, count", func(r *deliveryDashboardQuality) { *r.Panels[0].Count = -1 }},
		{"missing panel inventory", "inventory count-based panels", func(r *deliveryDashboardQuality) { r.Panels = nil }},
		{"positive-count alert", "", func(r *deliveryDashboardQuality) {
			*r.Panels[0].Count, *r.Panels[0].AlertVisible, *r.Panels[0].EmptyVisible = 2, true, false
		}},
		{"no count-based panels", "", func(r *deliveryDashboardQuality) { r.Panels = r.Panels[:0] }},
		{"missing charts", "needs charts", func(r *deliveryDashboardQuality) { r.Charts = nil }},
		{"single-day seed", "multiple populated days", func(r *deliveryDashboardQuality) {
			r.Charts[0].Seeded, r.Charts[0].Rendered = r.Charts[0].Seeded[:1], r.Charts[0].Rendered[:1]
		}},
		{"verification spike", "match seeded history", func(r *deliveryDashboardQuality) { r.Charts[0].Rendered[1].Value = 9000 }},
		{"unrendered dates", "match seeded history", func(r *deliveryDashboardQuality) { r.Charts[0].Rendered = r.Charts[0].Rendered[:1] }},
		{"zero history", "multiple populated days", func(r *deliveryDashboardQuality) {
			r.Charts[0].Seeded[1].Value, r.Charts[0].Rendered[1].Value = 0, 0
		}},
		{"same date twice", "match seeded history", func(r *deliveryDashboardQuality) { r.Charts[0].Seeded[1].Date = r.Charts[0].Seeded[0].Date }},
		{"invalid dates", "match seeded history", func(r *deliveryDashboardQuality) { r.Charts[0].Seeded[0].Date = "today" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := dashboardQualityFixture()
			tc.mutate(&report)
			pending := checkDashboardQuality(report)
			if tc.diagnostic == "" && len(pending) != 0 || tc.diagnostic != "" && !strings.Contains(strings.Join(pending, "\n"), tc.diagnostic) {
				t.Fatal(pending)
			}
		})
	}
}
