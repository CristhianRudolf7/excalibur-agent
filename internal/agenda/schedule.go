package agenda

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// ScheduleKind identifies the schedule type of Section 21.1.
type ScheduleKind string

// Schedule kinds (21.1).
const (
	ScheduleAt     ScheduleKind = "at"
	ScheduleEvery  ScheduleKind = "every"
	ScheduleCron   ScheduleKind = "cron"
	ScheduleAfter  ScheduleKind = "after"
	ScheduleWindow ScheduleKind = "window"
)

// MissedPolicy decides what happens to fires that became due while
// the daemon was down (17.4).
type MissedPolicy string

// Missed-fire policies (17.4).
const (
	MissedRunOnceNow MissedPolicy = "run_once_now"
	MissedSkip       MissedPolicy = "skip"
	MissedRunAll     MissedPolicy = "run_all"
)

// WindowSpec bounds a low-activity window on given weekdays in a zone.
type WindowSpec struct {
	Weekdays  []time.Weekday `json:"weekdays,omitempty"`
	StartHour int            `json:"start_hour"`
	EndHour   int            `json:"end_hour"`
}

// Schedule is one programmed schedule (Appendix A: schedules). All
// fire times are stored in UTC together with the zone they were
// expressed in (21.1).
type Schedule struct {
	ID string `json:"id"`
	// Kind is one of at, every, cron, after, window.
	Kind ScheduleKind `json:"kind"`
	// Timezone is the IANA zone the schedule was expressed in.
	Timezone string `json:"timezone"`
	// At holds the wall time for ScheduleAt (in Timezone).
	At time.Time `json:"at,omitempty"`
	// Interval holds the period for ScheduleEvery.
	Interval time.Duration `json:"interval,omitempty"`
	// CronExpr holds the 5- or 6-field expression for ScheduleCron.
	CronExpr string `json:"cron_expr,omitempty"`
	// AfterID names the task or schedule this ScheduleAfter waits for.
	AfterID string `json:"after_id,omitempty"`
	// Window bounds ScheduleWindow.
	Window *WindowSpec `json:"window,omitempty"`
	// NextFireUTC is the next known fire in UTC.
	NextFireUTC time.Time `json:"next_fire_utc"`
	// Missed is the 17.4 policy for fires lost during downtime.
	Missed MissedPolicy `json:"missed"`
	// Payload is the opaque work description fired with fire_id.
	Payload string `json:"payload,omitempty"`
	// ScopeID limits the schedule to a scope when set.
	ScopeID string `json:"scope_id,omitempty"`
	// Done marks one-shot schedules that already fired.
	Done bool `json:"done"`
}

// Validate checks a schedule definition.
func (s *Schedule) Validate() error {
	if s == nil {
		return fmt.Errorf("agenda: nil schedule")
	}
	if err := ValidateZone(s.Timezone); err != nil && s.Timezone != "" {
		return err
	}
	switch s.Kind {
	case ScheduleAt:
		if s.At.IsZero() {
			return fmt.Errorf("agenda: schedule %q: at needs a time", s.ID)
		}
	case ScheduleEvery:
		if s.Interval <= 0 {
			return fmt.Errorf("agenda: schedule %q: every needs a positive interval", s.ID)
		}
	case ScheduleCron:
		if _, err := ParseCron(s.CronExpr); err != nil {
			return err
		}
	case ScheduleAfter:
		if s.AfterID == "" {
			return fmt.Errorf("agenda: schedule %q: after needs a dependency id", s.ID)
		}
	case ScheduleWindow:
		if s.Window == nil {
			return fmt.Errorf("agenda: schedule %q: window needs a window spec", s.ID)
		}
		if s.Window.StartHour < 0 || s.Window.StartHour > 23 || s.Window.EndHour < 0 || s.Window.EndHour > 24 {
			return fmt.Errorf("agenda: schedule %q: bad window hours", s.ID)
		}
	default:
		return fmt.Errorf("agenda: schedule %q: unknown kind %q", s.ID, s.Kind)
	}
	switch s.Missed {
	case MissedRunOnceNow, MissedSkip, MissedRunAll:
	default:
		return fmt.Errorf("agenda: schedule %q: unknown missed policy %q", s.ID, s.Missed)
	}
	return nil
}

// NextAfter computes the next fire strictly after base for the
// schedule kinds with a time rule (21.1). After-schedules have no
// next fire until their dependency completes; window schedules
// resolve against now via NextWindowFire.
func (s *Schedule) NextAfter(base time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		var err error
		loc, err = LoadZone(s.Timezone)
		if err != nil {
			return time.Time{}, err
		}
	}
	switch s.Kind {
	case ScheduleAt:
		if s.At.After(base) {
			return s.At.UTC(), nil
		}
		return time.Time{}, fmt.Errorf("agenda: schedule %q: one-shot time already passed", s.ID)
	case ScheduleEvery:
		// Anchor-based so fires stay on grid across restarts: the
		// stored NextFireUTC is the anchor, never the query time.
		return s.nextFrom(base, loc)
	case ScheduleCron:
		spec, err := ParseCron(s.CronExpr)
		if err != nil {
			return time.Time{}, err
		}
		return spec.Next(base, loc)
	case ScheduleWindow:
		return NextWindowFire(s.Window, base, loc)
	case ScheduleAfter:
		return time.Time{}, fmt.Errorf("agenda: schedule %q: after fires on dependency completion", s.ID)
	}
	return time.Time{}, fmt.Errorf("agenda: schedule %q: unknown kind %q", s.ID, s.Kind)
}

// NextWindowFire returns the next instant inside the window spec.
func NextWindowFire(w *WindowSpec, base time.Time, loc *time.Location) (time.Time, error) {
	if w == nil {
		return time.Time{}, fmt.Errorf("agenda: nil window spec")
	}
	inDays := func(wd time.Weekday) bool {
		if len(w.Weekdays) == 0 {
			return true
		}
		for _, d := range w.Weekdays {
			if d == wd {
				return true
			}
		}
		return false
	}
	wall := base.In(loc)
	for d := 0; d < 9; d++ {
		day := wall.AddDate(0, 0, d)
		if !inDays(day.Weekday()) {
			continue
		}
		y, mo, dd := day.Date()
		if d == 0 {
			h := wall.Hour()
			switch {
			case h < w.StartHour:
				return ResolveWallTime(y, mo, dd, w.StartHour, 0, 0, loc), nil
			case h < w.EndHour || w.EndHour <= w.StartHour:
				// Already inside the window: fire now.
				return base.UTC(), nil
			default:
				continue
			}
		}
		return ResolveWallTime(y, mo, dd, w.StartHour, 0, 0, loc), nil
	}
	return time.Time{}, fmt.Errorf("agenda: no window fire within search bound")
}

// newFireID mints a fire_id: the idempotency key of one firing (17.4).
// The format is local to the agenda package.
func newFireID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("fire_%d", time.Now().UnixNano())
	}
	return "fire_" + hex.EncodeToString(b[:])
}

// Fire is one due firing of a schedule, keyed by FireID (17.4).
type Fire struct {
	FireID     string    `json:"fire_id"`
	ScheduleID string    `json:"schedule_id"`
	Scheduled  time.Time `json:"scheduled_utc"`
	FiredAt    time.Time `json:"fired_at_utc"`
	// Late is true when the fire became due during downtime (17.4).
	Late    bool   `json:"late"`
	Payload string `json:"payload,omitempty"`
}

// ReconcileMissed recomputes what must fire after a downtime window
// [downSince, now] for one schedule (17.4). It also advances the
// schedule's NextFireUTC past now. Returned fires carry fresh fire_ids.
func (s *Schedule) ReconcileMissed(downSince, now time.Time) ([]Fire, error) {
	loc, err := LoadZone(s.Timezone)
	if err != nil {
		return nil, err
	}
	if s.Kind == ScheduleAfter || s.Done {
		return nil, nil
	}
	var due []time.Time
	switch s.Kind {
	case ScheduleAt:
		at := s.At.UTC()
		if !at.Before(downSince.UTC()) && !at.After(now.UTC()) {
			due = append(due, at)
		}
	case ScheduleEvery, ScheduleCron, ScheduleWindow:
		due = missedSeries(s, downSince, now, loc)
	}
	// Advance past now.
	if next, err := s.NextAfter(now, loc); err == nil {
		s.NextFireUTC = next.UTC()
	} else if s.Kind == ScheduleAt {
		s.Done = true
	}
	switch s.Missed {
	case MissedSkip:
		return nil, nil
	case MissedRunOnceNow:
		if len(due) == 0 {
			return nil, nil
		}
		last := due[len(due)-1]
		return []Fire{{
			FireID: newFireID(), ScheduleID: s.ID,
			Scheduled: last, FiredAt: now.UTC(), Late: true, Payload: s.Payload,
		}}, nil
	case MissedRunAll:
		fires := make([]Fire, 0, len(due))
		for _, t := range due {
			fires = append(fires, Fire{
				FireID: newFireID(), ScheduleID: s.ID,
				Scheduled: t, FiredAt: now.UTC(), Late: true, Payload: s.Payload,
			})
		}
		return fires, nil
	}
	return nil, nil
}

// maxMissedSeries bounds missed-fire enumeration (17.4): a schedule
// firing faster than this across a downtime window is re-anchored at
// now instead of replaying unbounded history.
const maxMissedSeries = 10000

// missedSeries enumerates the fire times in (downSince, now].
func missedSeries(s *Schedule, downSince, now time.Time, loc *time.Location) []time.Time {
	var out []time.Time
	// Guard against runaway series: cap enumeration.
	for i := 0; i < maxMissedSeries; i++ {
		next, err := s.nextFrom(downSince, loc)
		if err != nil || next.After(now) {
			break
		}
		out = append(out, next.UTC())
		downSince = next
		if s.Kind == ScheduleAt {
			break
		}
	}
	return out
}

// nextFrom computes the next fire after base without mutating s.
func (s *Schedule) nextFrom(base time.Time, loc *time.Location) (time.Time, error) {
	switch s.Kind {
	case ScheduleEvery:
		anchor := s.NextFireUTC
		if anchor.IsZero() {
			anchor = base
		}
		if anchor.After(base) {
			return anchor.UTC(), nil
		}
		elapsed := base.Sub(anchor)
		steps := elapsed/s.Interval + 1
		return anchor.Add(time.Duration(steps) * s.Interval).UTC(), nil
	default:
		return s.NextAfter(base, loc)
	}
}

// ParseScopeList splits a comma-separated scope list (helper for the
// schedule tool surface).
func ParseScopeList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if q := strings.TrimSpace(p); q != "" {
			out = append(out, q)
		}
	}
	return out
}
