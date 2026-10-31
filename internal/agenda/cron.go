package agenda

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSpec is a parsed cron expression of 5 fields
// (minute hour dom month dow) or 6 fields (second minute hour dom
// month dow), always evaluated with an explicit IANA zone (21.1).
type CronSpec struct {
	Raw     string
	HasSecs bool
	Second  map[int]bool
	Minute  map[int]bool
	Hour    map[int]bool
	Dom     map[int]bool
	Month   map[int]bool
	Dow     map[int]bool
	DomStar bool
	DowStar bool
}

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var dowNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// ParseCron parses a 5- or 6-field cron expression. Supported atoms:
// numbers, month/dow names, "*", "*/step", "a-b", "a-b/step" and
// comma lists. Out-of-range values are rejected.
func ParseCron(expr string) (*CronSpec, error) {
	fields := strings.Fields(expr)
	var withSecs bool
	switch len(fields) {
	case 5:
		fields = append([]string{"0"}, fields...)
		withSecs = false
	case 6:
		withSecs = true
	default:
		return nil, fmt.Errorf("agenda: cron %q: want 5 or 6 fields, have %d", expr, len(fields))
	}
	bounds := [][2]int{{0, 59}, {0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	names := []map[string]int{nil, nil, nil, nil, monthNames, dowNames}
	sets := make([]map[int]bool, 6)
	stars := make([]bool, 6)
	for i := range fields {
		set, star, err := parseCronField(fields[i], bounds[i][0], bounds[i][1], names[i], i == 5)
		if err != nil {
			return nil, fmt.Errorf("agenda: cron %q field %d: %w", expr, i, err)
		}
		sets[i] = set
		stars[i] = star
	}
	// Sunday 7 is an alias of 0.
	if sets[5][7] {
		sets[5][0] = true
		delete(sets[5], 7)
	}
	return &CronSpec{
		Raw: expr, HasSecs: withSecs,
		Second: sets[0], Minute: sets[1], Hour: sets[2],
		Dom: sets[3], Month: sets[4], Dow: sets[5],
		DomStar: stars[3], DowStar: stars[5],
	}, nil
}

func parseCronField(field string, lo, hi int, names map[string]int, isDow bool) (map[int]bool, bool, error) {
	out := map[int]bool{}
	if field == "*" {
		for v := lo; v <= hi; v++ {
			out[v] = true
		}
		return out, true, nil
	}
	star := false
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return nil, false, fmt.Errorf("empty list element")
		}
		step := 1
		base := part
		if i := strings.Index(part, "/"); i >= 0 {
			base = part[:i]
			var err error
			step, err = strconv.Atoi(part[i+1:])
			if err != nil || step < 1 {
				return nil, false, fmt.Errorf("bad step in %q", part)
			}
		}
		var from, to int
		switch {
		case base == "*" || base == "":
			from, to = lo, hi
			star = true
		case strings.Contains(base, "-"):
			ab := strings.SplitN(base, "-", 2)
			var err error
			from, err = cronValue(ab[0], names)
			if err != nil {
				return nil, false, err
			}
			to, err = cronValue(ab[1], names)
			if err != nil {
				return nil, false, err
			}
			if from > to {
				return nil, false, fmt.Errorf("reversed range %q", base)
			}
		default:
			v, err := cronValue(base, names)
			if err != nil {
				return nil, false, err
			}
			from, to = v, v
		}
		if from < lo || to > hi {
			// For dow, 7 is allowed as Sunday alias; validated by caller.
			if !(isDow && from >= 0 && to <= 7) {
				return nil, false, fmt.Errorf("value out of range %d-%d in %q", lo, hi, part)
			}
		}
		for v := from; v <= to; v += step {
			if v >= lo && v <= hi {
				out[v] = true
			}
		}
	}
	if len(out) == 0 {
		return nil, false, fmt.Errorf("field %q matches nothing", field)
	}
	return out, star, nil
}

func cronValue(s string, names map[string]int) (int, error) {
	if names != nil {
		if v, ok := names[strings.ToLower(s)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bad value %q", s)
	}
	return v, nil
}

// Matches reports whether the wall time t (already in the schedule
// zone) matches the spec.
func (c *CronSpec) Matches(t time.Time) bool {
	if c.HasSecs && !c.Second[t.Second()] {
		return false
	}
	if !c.Minute[t.Minute()] || !c.Hour[t.Hour()] || !c.Month[int(t.Month())] {
		return false
	}
	domMatch := c.Dom[t.Day()]
	dowMatch := c.Dow[int(t.Weekday())]
	// Standard cron day semantics: when both dom and dow are
	// restricted, either may match; when one is "*", the other decides.
	switch {
	case c.DomStar && c.DowStar:
		// every day
	case c.DomStar:
		if !dowMatch {
			return false
		}
	case c.DowStar:
		if !domMatch {
			return false
		}
	default:
		if !domMatch && !dowMatch {
			return false
		}
	}
	return true
}

// nextFieldValue returns the smallest value in set >= cur, or -1.
func nextFieldValue(set map[int]bool, cur int) int {
	best := -1
	for v := range set {
		if v >= cur && (best < 0 || v < best) {
			best = v
		}
	}
	return best
}

// Next returns the first fire strictly after base, evaluated in loc.
// DST gaps advance to the next valid time; ambiguous times use their
// first occurrence (21.1). An error is returned when no fire exists
// within ~5 years (e.g. February 30th).
func (c *CronSpec) Next(base time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	step := time.Minute
	if c.HasSecs {
		step = time.Second
	}
	cand := base.Add(step)
	// Truncate below the finest precision so arithmetic stays on grid.
	if c.HasSecs {
		cand = cand.Truncate(time.Second)
	} else {
		cand = cand.Truncate(time.Minute)
	}
	for i := 0; i < 600000; i++ {
		wall := cand.In(loc)
		if c.Matches(wall) {
			y, mo, d := wall.Date()
			h, mi, s := wall.Clock()
			resolved := ResolveWallTime(y, mo, d, h, mi, s, loc)
			if !resolved.After(base) {
				cand = base.Add(step)
				base = resolved
				continue
			}
			// A gap resolution may have moved the wall time off the
			// spec (e.g. 02:30 -> 03:00); only accept exact matches.
			rw := resolved.In(loc)
			if c.Matches(rw) {
				return resolved, nil
			}
			cand = resolved.Add(step)
			continue
		}
		next := nextCronCandidate(c, cand, loc)
		if !next.After(cand) {
			// The runtime normalizes nonexistent wall jumps back
			// onto cand; force progress on the minute/second grid.
			next = cand.Add(step)
		}
		cand = next
	}
	return time.Time{}, fmt.Errorf("agenda: cron %q has no fire within search bound", c.Raw)
}

// nextCronCandidate advances cand to the next wall time that can
// plausibly match, by jumping the smallest mismatching field.
func nextCronCandidate(c *CronSpec, cand time.Time, loc *time.Location) time.Time {
	wall := cand.In(loc)
	sec := 0
	if c.HasSecs {
		sec = wall.Second()
	}
	step := time.Minute
	if c.HasSecs {
		step = time.Second
	}
	if !c.Month[int(wall.Month())] {
		// Jump to the first day of the next matching month.
		y, mo, _ := wall.Date()
		for k := 1; k <= 24; k++ {
			nm := mo + time.Month(k)
			ny := y
			for nm > 12 {
				nm -= 12
				ny++
			}
			if c.Month[int(nm)] {
				return time.Date(ny, nm, 1, 0, 0, 0, 0, loc)
			}
		}
		return cand.Add(366 * 24 * time.Hour)
	}
	if !dayMatches(c, wall) {
		// Next day at midnight.
		y, mo, d := wall.Date()
		mid := time.Date(y, mo, d, 0, 0, 0, 0, loc)
		return mid.Add(24 * time.Hour)
	}
	if !c.Hour[wall.Hour()] {
		if nv := nextFieldValue(c.Hour, wall.Hour()+1); nv >= 0 {
			y, mo, d := wall.Date()
			return time.Date(y, mo, d, nv, 0, 0, 0, loc)
		}
		y, mo, d := wall.Date()
		return time.Date(y, mo, d, 0, 0, 0, 0, loc).Add(24 * time.Hour)
	}
	if !c.Minute[wall.Minute()] {
		if !c.HasSecs {
			if nv := nextFieldValue(c.Minute, wall.Minute()+1); nv >= 0 {
				y, mo, d := wall.Date()
				return time.Date(y, mo, d, wall.Hour(), nv, 0, 0, loc)
			}
			y, mo, d := wall.Date()
			return time.Date(y, mo, d, wall.Hour()+1, 0, 0, 0, loc)
		}
		y, mo, d := wall.Date()
		h := wall.Hour()
		mi := wall.Minute()
		if nv := nextFieldValue(c.Minute, mi+1); nv >= 0 {
			return time.Date(y, mo, d, h, nv, 0, 0, loc)
		}
		if nv := nextFieldValue(c.Hour, h+1); nv >= 0 {
			return time.Date(y, mo, d, nv, 0, 0, 0, loc)
		}
		return time.Date(y, mo, d, 0, 0, 0, 0, loc).Add(24 * time.Hour)
	}
	if c.HasSecs {
		if nv := nextFieldValue(c.Second, sec+1); nv >= 0 {
			y, mo, d := wall.Date()
			return time.Date(y, mo, d, wall.Hour(), wall.Minute(), nv, 0, loc)
		}
		return cand.Add(step)
	}
	return cand.Add(step)
}

func dayMatches(c *CronSpec, wall time.Time) bool {
	domMatch := c.Dom[wall.Day()]
	dowMatch := c.Dow[int(wall.Weekday())]
	switch {
	case c.DomStar && c.DowStar:
		return true
	case c.DomStar:
		return dowMatch
	case c.DowStar:
		return domMatch
	default:
		return domMatch || dowMatch
	}
}
