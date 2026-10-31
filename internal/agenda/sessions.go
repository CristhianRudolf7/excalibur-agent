package agenda

import (
	"fmt"
	"time"
)

// Multi-day work sessions (17.6). Long N3 initiatives never live in one
// context compacted again and again: work splits into sessions, each a
// fresh execution compiled from the DB, chained by the agenda when
// dependencies complete, at a scheduled time, or in the low-activity
// window when running in background.

// HeartbeatTimeout is the 17.6 watchdog threshold: an execution in
// "running" with no heartbeat for 10 minutes and no registered long
// tool is declared stuck (Appendix B).
const HeartbeatTimeout = 10 * time.Minute

// SessionState tracks one initiative session (17.6).
type SessionState string

// Session states.
const (
	SessionScheduled SessionState = "scheduled"
	SessionRunning   SessionState = "running"
	SessionHandoff   SessionState = "handoff"
	SessionDone      SessionState = "done"
	SessionStuck     SessionState = "stuck"
)

// Session is one work session of a long initiative (17.6/21.7
// project-session): milestone or budget slice with startup commands,
// fresh-context compilation and handoff notes for the next session.
type Session struct {
	ID          string       `json:"id"`
	Initiative  string       `json:"initiative"`
	Milestone   string       `json:"milestone"`
	State       SessionState `json:"state"`
	BudgetUSD   float64      `json:"budget_usd"`
	WindowStart time.Time    `json:"window_start_utc,omitempty"`
	// StartupCmds are the project boot commands rerun every session
	// (the Anthropic long-running pattern of 17.6).
	StartupCmds []string `json:"startup_cmds,omitempty"`
	// Handoff carries plan state, evidence and continuation notes for
	// the next session.
	Handoff string `json:"handoff,omitempty"`
	// Heartbeat is the last liveness renewal.
	Heartbeat time.Time `json:"heartbeat_utc"`
	// LongTool is set while a registered long tool runs (exempt from
	// the heartbeat watchdog).
	LongTool string `json:"long_tool,omitempty"`
}

// Beat renews the session heartbeat.
func (s *Session) Beat(now time.Time) {
	s.Heartbeat = now.UTC()
}

// Stuck reports whether the watchdog must declare the session stuck:
// running, no heartbeat for 10 minutes, no registered long tool
// (17.6). Stuck sessions are interrupted, reconciled as in 17.3 and
// resumed or paused.
func (s *Session) Stuck(now time.Time) bool {
	if s.State != SessionRunning {
		return false
	}
	if s.LongTool != "" {
		return false
	}
	if s.Heartbeat.IsZero() {
		return true
	}
	return now.UTC().Sub(s.Heartbeat) > HeartbeatTimeout
}

// VerifyStartup describes the mandatory session start: verify state
// with quick checks before advancing, to catch regressions slipped in
// between sessions (17.6 step 4).
type StartupVerification struct {
	// Commands rerun at session start (tests and quick checks).
	Commands []string `json:"commands"`
	// RegressionFound is set when the checks fail.
	RegressionFound bool `json:"regression_found"`
	// Detail carries the failing check evidence.
	Detail string `json:"detail,omitempty"`
}

// SessionChain links the sessions of one initiative (17.6): sessions
// complete in dependency order, each compiled fresh from the DB with
// no inherited history.
type SessionChain struct {
	Initiative string     `json:"initiative"`
	Sessions   []*Session `json:"sessions"`
}

// Next returns the first session ready to run: scheduled with met
// dependencies (all earlier sessions done or handed off). Handed-off
// sessions count as satisfied and are skipped.
func (c *SessionChain) Next() *Session {
	for i, s := range c.Sessions {
		if s.State == SessionDone || s.State == SessionHandoff {
			continue
		}
		if s.State != SessionScheduled {
			return nil
		}
		ready := true
		for _, prev := range c.Sessions[:i] {
			if prev.State != SessionDone && prev.State != SessionHandoff {
				ready = false
				break
			}
		}
		if ready {
			return s
		}
	}
	return nil
}

// HandOff closes a session and leaves state for the next one (17.6
// steps 2-3).
func (c *SessionChain) HandOff(id, notes string, now time.Time) error {
	for _, s := range c.Sessions {
		if s.ID == id {
			if s.State != SessionRunning {
				return fmt.Errorf("agenda: cannot hand off session %q in %s", id, s.State)
			}
			s.State = SessionHandoff
			s.Handoff = notes
			s.Heartbeat = now.UTC()
			return nil
		}
	}
	return fmt.Errorf("agenda: unknown session %q", id)
}
