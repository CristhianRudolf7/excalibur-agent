package automations

import (
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
)

func validateSchedule(s Schedule) error {
	if _, err := agenda.LoadZone(s.Timezone); err != nil {
		return err
	}
	switch s.Kind {
	case ScheduleOnce:
		if s.At == nil || s.At.IsZero() {
			return fmt.Errorf("once schedule requires at")
		}
	case ScheduleInterval:
		if s.EveryMinutes < 5 || s.EveryMinutes > int((time.Duration(1<<63-1)/time.Minute)) {
			return fmt.Errorf("interval every_minutes must be at least 5 and fit a duration")
		}
	case ScheduleDaily, ScheduleWeekly, ScheduleMonthly:
		if len(s.Time) != 5 {
			return fmt.Errorf("time must match HH:MM")
		}
		if _, err := time.Parse("15:04", s.Time); err != nil {
			return fmt.Errorf("time must match HH:MM")
		}
		if s.Kind == ScheduleWeekly {
			if len(s.Weekdays) == 0 {
				return fmt.Errorf("weekly schedule requires weekdays")
			}
			for _, d := range s.Weekdays {
				if d < 0 || d > 6 {
					return fmt.Errorf("weekdays must be in 0..6")
				}
			}
		}
		if s.Kind == ScheduleMonthly && (s.DayOfMonth < 1 || s.DayOfMonth > 31) {
			return fmt.Errorf("day_of_month must be in 1..31")
		}
	case ScheduleCron:
		if len(strings.Fields(s.Cron)) != 5 {
			return fmt.Errorf("cron requires 5 fields")
		}
		if _, err := agenda.ParseCron(s.Cron); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown schedule kind %q", s.Kind)
	}
	return nil
}

// NextRun returns the first slot strictly after after, in UTC. A nil slot
// means a once schedule has already elapsed. Daily gap times advance to the
// first valid wall minute; overlap times use the first occurrence only.
func NextRun(s Schedule, after time.Time) (*time.Time, error) {
	if err := validateSchedule(s); err != nil {
		return nil, err
	}
	loc, _ := agenda.LoadZone(s.Timezone)
	var next time.Time
	switch s.Kind {
	case ScheduleOnce:
		if !s.At.After(after) {
			return nil, nil
		}
		next = s.At.UTC()
	case ScheduleInterval:
		next = after.Add(time.Duration(s.EveryMinutes) * time.Minute).UTC()
	case ScheduleCron:
		c, _ := agenda.ParseCron(s.Cron)
		n, err := c.Next(after, loc)
		if err != nil {
			return nil, err
		}
		next = n.UTC()
	default:
		clock, _ := time.Parse("15:04", s.Time)
		wall := after.In(loc)
		// Iterate calendar dates in UTC so offset changes never skip a date.
		date := time.Date(wall.Year(), wall.Month(), wall.Day(), 0, 0, 0, 0, time.UTC)
		for i := 0; i < 370; i++ {
			d := date.AddDate(0, 0, i)
			if s.Kind == ScheduleWeekly {
				match := false
				for _, day := range s.Weekdays {
					if int(d.Weekday()) == day {
						match = true
					}
				}
				if !match {
					continue
				}
			}
			if s.Kind == ScheduleMonthly {
				last := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
				day := min(s.DayOfMonth, last)
				if d.Day() != day {
					continue
				}
			}
			candidate := agenda.ResolveWallTime(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, loc)
			if candidate.After(after) {
				next = candidate
				break
			}
		}
		if next.IsZero() {
			return nil, fmt.Errorf("schedule has no next slot")
		}
	}
	return &next, nil
}

// nextScheduled distinguishes a pending past once slot from an already
// consumed once slot, including manual execution before the specified time.
func nextScheduled(s Schedule, now time.Time, consumed bool) (*time.Time, error) {
	if s.Kind == ScheduleOnce && consumed {
		return nil, nil
	}
	next, err := NextRun(s, now)
	if err == nil && next == nil && s.Kind == ScheduleOnce {
		at := s.At.UTC()
		next = &at
	}
	return next, err
}
