package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"strings"
	"testing"
	"time"
)

func TestAutomationToolLocalTimes(t *testing.T) {
	at := time.Date(2026, 11, 4, 0, 30, 0, 0, time.UTC)
	now := at.Add(-time.Hour)
	for _, tc := range []struct{ zone, want, now string }{
		{"America/Lima", "Tue 2026-11-03 19:30 America/Lima (UTC-05:00)", "Tue 2026-11-03 18:30 America/Lima (UTC-05:00)"},
		{"Europe/Madrid", "Wed 2026-11-04 01:30 Europe/Madrid (UTC+01:00)", "Wed 2026-11-04 00:30 Europe/Madrid (UTC+01:00)"},
		{"Asia/Kathmandu", "Wed 2026-11-04 06:15 Asia/Kathmandu (UTC+05:45)", "Wed 2026-11-04 05:15 Asia/Kathmandu (UTC+05:45)"},
	} {
		a := Automation{Schedule: Schedule{Kind: ScheduleOnce, Timezone: tc.zone, At: &at}, NextRunAt: &at}
		out := savedResultAt("create", a, now)
		if out["at_local"] != tc.want || out["next_run_at_local"] != tc.want || out["now_local"] != tc.now || out["at"] != "2026-11-04T00:30:00Z" {
			t.Fatalf("%s: %+v", tc.zone, out)
		}
		raw, err := json.Marshal(localAutomation(a, now))
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			NextRunAt      string `json:"next_run_at"`
			NextRunAtLocal string `json:"next_run_at_local"`
			NowLocal       string `json:"now_local"`
			Schedule       struct {
				At       string `json:"at"`
				AtLocal  string `json:"at_local"`
				NowLocal string `json:"now_local"`
				Timezone string `json:"timezone"`
			} `json:"schedule"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		if wire.Schedule.AtLocal != tc.want || wire.NextRunAtLocal != tc.want || wire.Schedule.NowLocal != tc.now || wire.NowLocal != tc.now || wire.Schedule.Timezone != tc.zone || wire.Schedule.At != "2026-11-04T00:30:00Z" || wire.NextRunAt != wire.Schedule.At {
			t.Fatalf("projection: %s", raw)
		}
	}
	summer := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if got := renderLocal(summer, "Europe/Madrid"); got != "Wed 2026-07-01 14:00 Europe/Madrid (UTC+02:00)" {
		t.Fatal(got)
	}
	reg := tools.NewMapRegistry()
	if err := WireTool(reg, &toolFake{}); err != nil {
		t.Fatal(err)
	}
	wired, _ := reg.Get("automations")
	if !strings.Contains(wired.Definition().Description, "using at_local") {
		t.Fatal("missing local-time presentation guidance")
	}
}

func TestToolScheduleResultsIncludeLocalTimes(t *testing.T) {
	now := time.Date(2026, 11, 3, 23, 30, 0, 0, time.UTC)
	at := now.Add(time.Hour)
	manager, err := NewManager(Config{Now: func() time.Time { return now }, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	spec := Spec{Name: "Lima", Schedule: Schedule{Kind: ScheduleOnce, Timezone: "America/Lima", At: &at}, Steps: []Step{{ID: "n", Type: StepNotify, Title: "Notify", Message: "Reminder"}}}
	a, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"create", "update", "list", "get", "pause", "resume", "restore_version"} {
		raw, _ := json.Marshal(map[string]any{"action": action, "id": a.ID, "approved": true, "version": 1, "spec": spec})
		result, err := toolAction(context.Background(), manager, raw)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		out, _ := json.Marshal(result)
		for _, field := range []string{`"at_local":"Tue 2026-11-03 19:30 America/Lima (UTC-05:00)"`, `"now_local":`} {
			if !strings.Contains(string(out), field) {
				t.Fatalf("%s missing %s: %s", action, field, out)
			}
		}
		if action != "pause" && !strings.Contains(string(out), `"next_run_at_local":`) {
			t.Fatal(fmt.Sprint(action, ": ", string(out)))
		}
	}
}
