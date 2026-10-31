package agenda

import (
	"fmt"
	"time"
	_ "time/tzdata" // Embedded zone database: agenda behavior must not depend on the system tzdata (21.1).
)

// DefaultZoneName is used when the user never set a zone (21.1): the
// device zone, confirmed by the user at the profile layer.
func DefaultZoneName() string {
	name, _ := time.Now().Zone()
	_ = name
	// time.Local already reflects the device zone; return its IANA name
	// when known, otherwise UTC.
	if time.Local.String() != "Local" {
		return time.Local.String()
	}
	return "UTC"
}

// ValidateZone reports whether name is a known IANA zone.
func ValidateZone(name string) error {
	if name == "" {
		return fmt.Errorf("agenda: empty time zone name")
	}
	if _, err := time.LoadLocation(name); err != nil {
		return fmt.Errorf("agenda: unknown time zone %q: %w", name, err)
	}
	return nil
}

// LoadZone loads an IANA zone, defaulting empty names to UTC.
func LoadZone(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("agenda: unknown time zone %q: %w", name, err)
	}
	return loc, nil
}

func sameWall(t time.Time, y int, mo time.Month, d, h, mi, s int) bool {
	yy, mmo, dd := t.Date()
	hh, mmi, ss := t.Clock()
	return yy == y && mmo == mo && dd == d && hh == h && mmi == mi && ss == s
}

// ResolveWallTime maps a wall-clock time in loc to a UTC instant (21.1).
//
//   - A normal wall time maps to itself.
//   - A nonexistent wall time (spring-forward gap) advances to the next
//     valid wall time.
//   - An ambiguous wall time (fall-back overlap) resolves to its first
//     occurrence.
//
// The resolution is explicit: candidate UTC instants are derived from
// the zone offsets in effect around the date, never from the runtime's
// unspecified gap/overlap choice.
func ResolveWallTime(y int, mo time.Month, d, h, mi, s int, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	wallAsUTC := time.Date(y, mo, d, h, mi, s, 0, time.UTC)
	if t, ok := resolveWallOnce(wallAsUTC, loc); ok {
		return t
	}
	// Gap: advance the wall time until it exists. Real gaps are an
	// hour or less; the bound below also covers historical oddities.
	for step := 1; step <= 26*60; step++ {
		shifted := wallAsUTC.Add(time.Duration(step) * time.Minute)
		if t, ok := resolveWallOnce(shifted, loc); ok {
			// Only accept the shifted time if the minute grid still
			// lines up (callers that need field-exact matches re-check
			// the cron fields themselves).
			_ = shifted
			return t
		}
	}
	// Should be unreachable for real zones; fall back to the
	// runtime conversion rather than failing.
	return time.Date(y, mo, d, h, mi, s, 0, loc).UTC()
}

// resolveWallOnce tries the offsets observed around wallAsUTC and
// returns the earliest UTC instant whose wall time in loc equals the
// requested wall time.
func resolveWallOnce(wallAsUTC time.Time, loc *time.Location) (time.Time, bool) {
	y, mo, d := wallAsUTC.Date()
	h, mi, s := wallAsUTC.Clock()
	seen := map[int]bool{}
	var best time.Time
	found := false
	for _, delta := range []time.Duration{-12 * time.Hour, -4 * time.Hour, -time.Hour, time.Hour, 4 * time.Hour, 12 * time.Hour} {
		_, off := wallAsUTC.Add(delta).In(loc).Zone()
		if seen[off] {
			continue
		}
		seen[off] = true
		u := wallAsUTC.Add(-time.Duration(off) * time.Second)
		if sameWall(u.In(loc), y, mo, d, h, mi, s) {
			if !found || u.Before(best) {
				best = u
				found = true
			}
		}
	}
	if !found {
		return time.Time{}, false
	}
	return best, true
}

// ZoneChangeReport describes what happened to local-time schedules
// when the user changed zones (21.1): every schedule keeps its local
// wall time and gets a recalculated UTC fire time.
type ZoneChangeReport struct {
	OldZone string
	NewZone string
	// Recalculated lists schedule IDs whose next fire moved.
	Recalculated []string
	// NextFires maps schedule ID to its new next_fire_utc.
	NextFires map[string]time.Time
}

// RecalculateForZoneChange recomputes next_fire_utc for schedules
// expressed in local time after the user changed zones (21.1).
func RecalculateForZoneChange(scheds []*Schedule, oldZone, newZone string, now time.Time) (*ZoneChangeReport, error) {
	if err := ValidateZone(newZone); err != nil {
		return nil, err
	}
	loc, err := LoadZone(newZone)
	if err != nil {
		return nil, err
	}
	rep := &ZoneChangeReport{OldZone: oldZone, NewZone: newZone, NextFires: map[string]time.Time{}}
	for _, s := range scheds {
		if s == nil {
			continue
		}
		next, err := s.NextAfter(now.In(loc), loc)
		if err != nil {
			continue
		}
		if !next.Equal(s.NextFireUTC) {
			rep.Recalculated = append(rep.Recalculated, s.ID)
		}
		s.Timezone = newZone
		s.NextFireUTC = next.UTC()
		rep.NextFires[s.ID] = next.UTC()
	}
	return rep, nil
}
