package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// newTimedRun returns a time-only run with a wall budget of total seconds.
func newTimedRun(total int64) *Run {
	return NewRun("run", "task", Profile{
		TimeBudgetOnly: true,
		Limit:          tasks.Limit{MaxSeconds: total},
	})
}

func TestDeadlinePacingAndClosing(t *testing.T) {
	t.Setenv(DeadlineSwitch, "on")
	t.Setenv(TimeFooterSwitch, "0") // legacy volatile-note channel
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
	if len(notes) != 3 || !strings.Contains(notes[1], "stop exploring; verify and finish") || !strings.Contains(notes[2], "finish now with what works") {
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

func TestTimeFooterLineAndThresholds(t *testing.T) {
	t.Setenv(TimeFooterSwitch, "")
	r := newTimedRun(900)
	start := r.StartedAt
	at := func(secs int) string {
		return r.timeFooter(start.Add(time.Duration(secs) * time.Second))
	}
	if got := at(250); got != "[time: 4m10s used, about 10m50s left]" {
		t.Fatalf("footer at 250s = %q", got)
	}
	// Before 50 percent: the line alone, no advice.
	if got := at(449); strings.Contains(got, "Pacing") {
		t.Fatalf("advice before 50%%: %q", got)
	}
	// Crossing 50 percent adds the advice once.
	got := at(450)
	if !strings.HasPrefix(got, "[time: 7m30s used, about 7m30s left] Pacing 50%: check that the deliverables exist.") {
		t.Fatalf("50%% footer = %q", got)
	}
	if got := at(460); strings.Contains(got, "Pacing") {
		t.Fatalf("50%% advice repeated: %q", got)
	}
	got = at(800)
	if !strings.HasSuffix(got, "Pacing 75%: stop exploring; verify and finish.") {
		t.Fatalf("75%% footer = %q", got)
	}
	got = at(850)
	if !strings.HasSuffix(got, "Pacing 90%: finish now with what works; do not start new work.") {
		t.Fatalf("90%% footer = %q", got)
	}
	if got := at(860); strings.Contains(got, "Pacing") {
		t.Fatalf("90%% advice repeated: %q", got)
	}
	if got := at(1000); got != "[time: 16m40s used, about 0s left]" {
		t.Fatalf("past the budget = %q", got)
	}
}

func TestTimeFooterHighestThresholdWhenSeveralCrossed(t *testing.T) {
	t.Setenv(TimeFooterSwitch, "")
	r := newTimedRun(900)
	got := r.timeFooter(r.StartedAt.Add(820 * time.Second))
	if !strings.HasSuffix(got, "Pacing 90%: finish now with what works; do not start new work.") {
		t.Fatalf("one result past all thresholds = %q", got)
	}
	if got := r.timeFooter(r.StartedAt.Add(830 * time.Second)); strings.Contains(got, "Pacing") {
		t.Fatalf("lower thresholds announced late: %q", got)
	}
}

func TestTimeFooterAppliesOnlyWhenActive(t *testing.T) {
	t.Setenv(TimeFooterSwitch, "")
	legacy := NewRun("run", "task", Profile{Limit: tasks.Limit{MaxSeconds: 100}})
	if got := legacy.timeFooter(legacy.StartedAt.Add(time.Second)); got != "" {
		t.Fatalf("legacy run footer = %q", got)
	}
	if legacy.footerActive() {
		t.Fatal("legacy run must not report an active footer")
	}
	noBudget := NewRun("run", "task", Profile{TimeBudgetOnly: true})
	if got := noBudget.timeFooter(noBudget.StartedAt.Add(time.Second)); got != "" {
		t.Fatalf("run without a budget footer = %q", got)
	}
	timed := newTimedRun(100)
	if !timed.footerActive() {
		t.Fatal("time-only run with a budget must report an active footer")
	}
	t.Setenv(TimeFooterSwitch, "0")
	if timed.footerActive() || timed.timeFooter(timed.StartedAt.Add(time.Second)) != "" {
		t.Fatal("EXCALIBUR_TIME_FOOTER=0 must switch the footer off")
	}
}

func TestPacingMovesToFooterWhenActive(t *testing.T) {
	t.Setenv(DeadlineSwitch, "on")
	t.Setenv(TimeFooterSwitch, "")
	r := newTimedRun(100)
	r.DeadlinePacing(r.StartedAt.Add(60 * time.Second))
	if notes := r.VolatileNotes(); len(notes) != 0 {
		t.Fatalf("footer active: pacing must not add volatile notes: %v", notes)
	}
	// The threshold is still unannounced, so the footer carries it.
	if got := r.timeFooter(r.StartedAt.Add(61 * time.Second)); !strings.Contains(got, "Pacing 50%") {
		t.Fatalf("footer after threshold = %q", got)
	}
}

func TestWithFooter(t *testing.T) {
	for _, tt := range []struct{ out, footer, want string }{
		{"", "[f]", "[f]"},
		{"a", "", "a"},
		{"a", "[f]", "a\n[f]"},
		{"a\n", "[f]", "a\n[f]"},
	} {
		if got := withFooter(tt.out, tt.footer); got != tt.want {
			t.Fatalf("withFooter(%q,%q) = %q, want %q", tt.out, tt.footer, got, tt.want)
		}
	}
}

func TestClockSpan(t *testing.T) {
	for _, tt := range []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"}, {45 * time.Second, "45s"}, {59999 * time.Millisecond, "59s"},
		{60 * time.Second, "1m00s"}, {250 * time.Second, "4m10s"},
		{time.Hour + 5*time.Minute, "1h05m"}, {-time.Second, "0s"},
	} {
		if got := clockSpan(tt.d); got != tt.want {
			t.Fatalf("clockSpan(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// Autonomous pacing must not shorten or refuse commands before the wall.
func TestTimeOnlyDeadlineCommandIsAdvisory(t *testing.T) {
	t.Setenv(DeadlineSwitch, "on")
	r := newTimedRun(100)
	for _, secs := range []int{50, 75, 90, 99} {
		bound, msg := r.deadlineCommand(r.StartedAt.Add(time.Duration(secs)*time.Second), "exec", []byte(`{"command":"sleep 20","timeout_s":300,"background":true}`))
		if bound != 0 || msg != "" {
			t.Fatalf("at %d: bound=%v refusal=%q", secs, bound, msg)
		}
	}
}
