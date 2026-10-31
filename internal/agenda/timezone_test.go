package agenda

import (
	"testing"
	"time"
)

// 21.10 row 1: cron schedules in the user zone across DST changes.

func TestResolveGapAdvances(t *testing.T) {
	loc, err := LoadZone("America/New_York")
	if err != nil {
		t.Skip("zone data unavailable")
	}
	// 2026-03-08 02:30 does not exist (spring forward 02:00 -> 03:00).
	got := ResolveWallTime(2026, time.March, 8, 2, 30, 0, loc).In(loc)
	if got.Hour() != 3 || got.Minute() != 0 {
		t.Fatalf("gap 02:30 should advance to 03:00, have %v", got)
	}
}

func TestResolveAmbiguousFirstOccurrence(t *testing.T) {
	loc, err := LoadZone("America/New_York")
	if err != nil {
		t.Skip("zone data unavailable")
	}
	// 2026-11-01 01:30 happens twice (fall back 02:00 -> 01:00).
	got := ResolveWallTime(2026, time.November, 1, 1, 30, 0, loc)
	second := got.Add(30 * time.Minute).Add(30 * time.Minute)
	// The first occurrence plus one hour is still 01:30 local.
	if h := got.Add(time.Hour).In(loc); h.Hour() != 1 || h.Minute() != 30 {
		t.Fatalf("want first occurrence (EDT), have %v then %v", got, second)
	}
	_, offFirst := got.In(loc).Zone()
	_, offSecond := got.Add(time.Hour).In(loc).Zone()
	if offFirst == offSecond {
		t.Fatalf("occurrences should differ in offset: %v", got)
	}
}

func TestCronAcrossSpringForward(t *testing.T) {
	loc, err := LoadZone("America/New_York")
	if err != nil {
		t.Skip("zone data unavailable")
	}
	spec, err := ParseCron("30 2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	// 02:30 never fires on 2026-03-08; the next fire is 2026-03-09.
	base := time.Date(2026, time.March, 7, 12, 0, 0, 0, loc)
	next, err := spec.Next(base, loc)
	if err != nil {
		t.Fatal(err)
	}
	wall := next.In(loc)
	if wall.Day() != 9 || wall.Hour() != 2 || wall.Minute() != 30 {
		t.Fatalf("want 2026-03-09 02:30 local, have %v", wall)
	}
}

func TestZoneChangeRecalculates(t *testing.T) {
	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	s := &Schedule{ID: "s1", Kind: ScheduleCron, CronExpr: "0 9 * * *", Timezone: "America/Lima", Missed: MissedSkip}
	loc, _ := LoadZone("America/Lima")
	next, err := s.NextAfter(now, loc)
	if err != nil {
		t.Fatal(err)
	}
	s.NextFireUTC = next.UTC()
	rep, err := RecalculateForZoneChange([]*Schedule{s}, "America/Lima", "America/New_York", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Recalculated) != 1 || s.Timezone != "America/New_York" {
		t.Fatalf("zone change not applied: %+v", rep)
	}
	// 09:00 in New York is 13:00 UTC in June (EDT), not 14:00 (Lima).
	if h := s.NextFireUTC.Hour(); h != 13 {
		t.Fatalf("want 13:00 UTC after zone change, have %v", s.NextFireUTC)
	}
}

func TestValidateZone(t *testing.T) {
	if err := ValidateZone("America/Lima"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateZone("Nope/Nowhere"); err == nil {
		t.Fatal("want error for unknown zone")
	}
}
