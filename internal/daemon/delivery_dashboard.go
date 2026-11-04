package daemon

import (
	"fmt"
	"strings"
	"time"
)

type deliveryDashboardQuality struct {
	Labels []struct {
		Label    string `json:"label"`
		Selector string `json:"selector"`
	} `json:"labels"`
	Panels []struct {
		Selector     string `json:"selector"`
		Count        *int   `json:"count"`
		AlertVisible *bool  `json:"alert_visible"`
		EmptyVisible *bool  `json:"empty_visible"`
	} `json:"panels"`
	Charts []struct {
		Selector string                 `json:"selector"`
		Seeded   []deliveryHistoryPoint `json:"seeded"`
		Rendered []deliveryHistoryPoint `json:"rendered"`
	} `json:"charts"`
}

type deliveryHistoryPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// These observations come from read-only browsing of the clean delivered app,
// in the same executed check that proves its persisted data equals the seed.
func checkDashboardQuality(report deliveryDashboardQuality) []string {
	var pending []string
	add := func(format string, args ...any) { pending = append(pending, fmt.Sprintf(format, args...)) }
	if len(report.Labels) < 2 {
		add("dashboard evidence must inventory KPI card and section labels with DOM selectors")
	}
	labels, selectors := map[string]bool{}, map[string]bool{}
	for _, label := range report.Labels {
		normalized := strings.ToLower(strings.Join(strings.Fields(label.Label), " "))
		selector := strings.TrimSpace(label.Selector)
		if normalized == "" || labels[normalized] || selector == "" || selectors[selector] {
			add("dashboard KPI cards and sections need unique, specific labels and distinct DOM selectors")
		}
		labels[normalized], selectors[selector] = true, true
	}
	if report.Panels == nil {
		add("dashboard evidence must inventory count-based panels, using [] when none exist")
	}
	panels := map[string]bool{}
	for _, panel := range report.Panels {
		selector := strings.TrimSpace(panel.Selector)
		if selector == "" || panels[selector] || panel.Count == nil || panel.AlertVisible == nil || panel.EmptyVisible == nil || *panel.Count < 0 {
			add("dashboard panel evidence needs a unique selector, count, alert_visible and empty_visible")
		} else if *panel.Count == 0 && (*panel.AlertVisible || !*panel.EmptyVisible) {
			add("dashboard zero-count panels must show a calm empty state with no alert")
		}
		panels[selector] = true
	}
	if len(report.Charts) == 0 {
		add("dashboard evidence needs charts compared with seeded history")
	}
	charts := map[string]bool{}
	for _, chart := range report.Charts {
		selector := strings.TrimSpace(chart.Selector)
		if selector == "" || charts[selector] {
			add("dashboard charts need distinct DOM selectors")
		}
		charts[selector] = true
		valid, populated := len(chart.Seeded) == len(chart.Rendered), 0
		previous := ""
		for index, point := range chart.Seeded {
			_, err := time.Parse("2006-01-02", point.Date)
			if err != nil || point.Date <= previous || index >= len(chart.Rendered) || chart.Rendered[index] != point {
				valid = false
			}
			previous = point.Date
			if point.Value != 0 {
				populated++
			}
		}
		if !valid || populated < 2 {
			add("dashboard chart %s must match seeded history on multiple populated days; single-day verification spikes fail", selector)
		}
	}
	return pending
}
