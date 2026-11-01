package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func TestDeadlineSources(t *testing.T) {
	t.Setenv(DeadlineSwitch, "on")
	for _, key := range HarborTimeoutEnvs {
		t.Setenv(key, "")
	}
	for _, tt := range []struct {
		text string
		want int64
	}{
		{"You have 900 seconds to complete this task", 900},
		{"YOU HAVE 2.5 minutes", 150}, {"You have 1 hour", 3600},
		{"You have 2 hours. You have 30 seconds", 30},
		{"The command takes 3 seconds", 42}, {"You have 0 seconds", 42},
		{"You have 9999999999999999999999 hours", 42},
	} {
		got, _ := ResolveDeadline(0, tt.text, 42, "configured")
		if got != tt.want {
			t.Fatalf("%q: %d want %d", tt.text, got, tt.want)
		}
	}
	t.Setenv(HarborTimeoutEnvs[0], "900")
	if got, src := ResolveDeadline(0, "You have 2 hours", 42, "configured"); got != 900 || src != HarborTimeoutEnvs[0] {
		t.Fatalf("%d %s", got, src)
	}
	if got, src := ResolveDeadline(60, "You have 2 hours", 42, "configured"); got != 60 || src != "explicit" {
		t.Fatalf("%d %s", got, src)
	}
	t.Setenv(DeadlineSwitch, "off")
	if got, src := ResolveDeadline(60, "You have 2 hours", 42, "configured"); got != 42 || src != "configured" {
		t.Fatalf("%d %s", got, src)
	}
}

func TestDeadlinePacingAndClosing(t *testing.T) {
	t.Setenv(DeadlineSwitch, "on")
	r := NewRun("run", "task", Profile{Limit: tasks.Limit{MaxSeconds: 100}})
	start := r.StartedAt
	for i, secs := range []int{49, 50, 75, 90} {
		r.DeadlinePacing(start.Add(time.Duration(secs) * time.Second))
		notes := r.VolatileNotes()
		if len(notes) != i {
			t.Fatalf("at %d: %v", secs, notes)
		}
	}
	r.DeadlinePacing(start.Add(95 * time.Second))
	notes := r.VolatileNotes()
	if len(notes) != 3 || !strings.Contains(notes[1], "Stop exploring") || !strings.Contains(notes[2], "call finish") {
		t.Fatal(notes)
	}
	for _, tt := range []struct {
		secs       int
		name, args string
		reject     bool
		bound      time.Duration
	}{
		{89, "exec", `{"timeout_s":300}`, false, 0},
		{90, "exec", `{"timeout_s":300}`, true, 5 * time.Second},
		{90, "exec", `{"background":true}`, true, 5 * time.Second},
		{90, "exec", `{"command":"sleep 100"}`, false, 5 * time.Second},
		{98, "exec", `{"timeout_s":3}`, true, 2 * time.Second},
		{100, "exec", `{}`, true, time.Nanosecond},
		{99, "finish", `{}`, false, 0},
	} {
		bound, msg := r.deadlineCommand(start.Add(time.Duration(tt.secs)*time.Second), tt.name, []byte(tt.args))
		if (msg != "") != tt.reject || bound != tt.bound {
			t.Fatalf("%+v: %s %v", tt, msg, bound)
		}
	}
	t.Setenv(DeadlineSwitch, "off")
	if bound, msg := r.deadlineCommand(start.Add(95*time.Second), "exec", []byte(`{"timeout_s":300}`)); bound != 0 || msg != "" {
		t.Fatal(bound, msg)
	}
}
