package agenda

import (
	"testing"
	"time"
)

// 17.4/17.8: missed firings honor per-schedule policy and fire_id
// prevents duplicates.

func TestScheduleValidation(t *testing.T) {
	good := &Schedule{ID: "a", Kind: ScheduleCron, CronExpr: "0 9 * * *", Missed: MissedSkip}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*Schedule{
		{ID: "b", Kind: "nope", Missed: MissedSkip},
		{ID: "c", Kind: ScheduleCron, CronExpr: "nope", Missed: MissedSkip},
		{ID: "d", Kind: ScheduleEvery, Missed: MissedSkip},
		{ID: "e", Kind: ScheduleAt, Missed: MissedSkip},
		{ID: "f", Kind: ScheduleAfter, Missed: MissedSkip},
		{ID: "g", Kind: ScheduleCron, CronExpr: "0 9 * * *", Missed: "nope"},
		{ID: "h", Kind: ScheduleCron, CronExpr: "0 9 * * *", Timezone: "Nope/X", Missed: MissedSkip},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("want error for %+v", bad)
		}
	}
}

func TestEveryAnchorStable(t *testing.T) {
	s := &Schedule{ID: "e", Kind: ScheduleEvery, Interval: time.Hour, Missed: MissedSkip}
	base := time.Date(2026, time.January, 1, 0, 30, 0, 0, time.UTC)
	s.NextFireUTC = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	next, err := s.NextAfter(base, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	// Anchor 00:00 + interval grid: 01:00, not base+1h.
	if !next.Equal(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("anchor grid broken: %v", next)
	}
}

func TestMissedRunOnceNow(t *testing.T) {
	s := &Schedule{ID: "h", Kind: ScheduleEvery, Interval: time.Hour, Missed: MissedRunOnceNow, Timezone: "UTC"}
	s.NextFireUTC = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	down := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := down.Add(3*time.Hour + 10*time.Minute)
	fires, err := s.ReconcileMissed(down, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 {
		t.Fatalf("run_once_now delivers exactly one late fire, have %d", len(fires))
	}
	if !fires[0].Late || fires[0].FireID == "" {
		t.Fatalf("late flag and fire_id required: %+v", fires[0])
	}
	if !s.NextFireUTC.After(now) {
		t.Fatalf("schedule must advance past now: %v", s.NextFireUTC)
	}
}

func TestMissedSkip(t *testing.T) {
	s := &Schedule{ID: "s", Kind: ScheduleCron, CronExpr: "0 * * * *", Missed: MissedSkip, Timezone: "UTC"}
	down := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := down.Add(3 * time.Hour)
	fires, err := s.ReconcileMissed(down, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 0 {
		t.Fatalf("skip must deliver nothing, have %d", len(fires))
	}
	if !s.NextFireUTC.After(now) {
		t.Fatalf("skip still advances to the next fire: %v", s.NextFireUTC)
	}
}

func TestMissedRunAll(t *testing.T) {
	s := &Schedule{ID: "r", Kind: ScheduleEvery, Interval: time.Hour, Missed: MissedRunAll, Timezone: "UTC"}
	s.NextFireUTC = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	down := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := down.Add(3 * time.Hour)
	fires, err := s.ReconcileMissed(down, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 3 {
		t.Fatalf("run_all replays every missed fire, have %d", len(fires))
	}
	seen := map[string]bool{}
	for i, f := range fires {
		if seen[f.FireID] || f.FireID == "" {
			t.Fatalf("fire_ids must be unique: %+v", fires)
		}
		seen[f.FireID] = true
		if i > 0 && !f.Scheduled.After(fires[i-1].Scheduled) {
			t.Fatal("run_all fires must be ordered")
		}
	}
}

func TestMissedAtOneShot(t *testing.T) {
	at := time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)
	s := &Schedule{ID: "o", Kind: ScheduleAt, At: at, Missed: MissedRunOnceNow, Timezone: "UTC"}
	down := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	fires, err := s.ReconcileMissed(down, down.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 || !fires[0].Scheduled.Equal(at) {
		t.Fatalf("one-shot missed fire wrong: %+v", fires)
	}
	if !s.Done {
		t.Fatal("one-shot schedule must be done after its fire")
	}
}

func TestWindowFire(t *testing.T) {
	w := &WindowSpec{Weekdays: []time.Weekday{time.Monday}, StartHour: 2, EndHour: 4}
	monday := time.Date(2026, time.January, 5, 1, 0, 0, 0, time.UTC) // Monday
	next, err := NextWindowFire(w, monday, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if next.Hour() != 2 {
		t.Fatalf("want window start 02:00, have %v", next)
	}
	inside, err := NextWindowFire(w, time.Date(2026, time.January, 5, 3, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !inside.Equal(time.Date(2026, time.January, 5, 3, 0, 0, 0, time.UTC)) {
		t.Fatalf("inside the window fires now, have %v", inside)
	}
}
