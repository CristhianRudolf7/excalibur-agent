package agenda

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event is a normalized trigger source event (21.3): mail, channel
// message, webhook, file change, calendar entry, or capability task
// completion. Normalization keeps type, origin, payload and id.
type Event struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Source  string         `json:"source"`
	Payload map[string]any `json:"payload,omitempty"`
	At      time.Time      `json:"at_utc"`
}

// TriggerAction is what a fired rule does (21.3).
type TriggerAction string

// Trigger actions (21.3).
const (
	ActionStartTask    TriggerAction = "start_task"
	ActionLaunchFlow   TriggerAction = "launch_workflow"
	ActionNotify       TriggerAction = "notify"
	ActionStartSession TriggerAction = "start_session"
)

// TriggerFilter selects the events a rule reacts to (21.3).
type TriggerFilter struct {
	// Types and Sources accept exact values; empty means any.
	Types   []string `json:"types,omitempty"`
	Sources []string `json:"sources,omitempty"`
	// Contains matches when every entry appears (case-insensitive) in
	// the concatenated payload text.
	Contains []string `json:"contains,omitempty"`
}

// Matches reports whether the event passes the filter.
func (f TriggerFilter) Matches(e Event) bool {
	if len(f.Types) > 0 && !containsFold(f.Types, e.Type) {
		return false
	}
	if len(f.Sources) > 0 && !containsFold(f.Sources, e.Source) {
		return false
	}
	if len(f.Contains) > 0 {
		text := strings.ToLower(e.Type + " " + e.Source + " " + payloadText(e.Payload))
		for _, want := range f.Contains {
			if !strings.Contains(text, strings.ToLower(want)) {
				return false
			}
		}
	}
	return true
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

func payloadText(p map[string]any) string {
	var sb strings.Builder
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%v ", k, p[k])
	}
	return sb.String()
}

// TriggerRule is one event-trigger rule (21.3, Appendix A: triggers).
type TriggerRule struct {
	ID     string        `json:"id"`
	Filter TriggerFilter `json:"filter"`
	// Action to run when the rule fires.
	Action TriggerAction `json:"action"`
	// Target names the task, workflow or channel the action needs.
	Target string `json:"target"`
	// MaxPerHour caps firings (21.3: límite de frecuencia).
	MaxPerHour int `json:"max_per_hour"`
	// DedupWindow drops repeat deliveries of the same event id.
	DedupWindow time.Duration `json:"dedup_window"`
	// Enabled rules participate in matching; disabled ones are kept.
	Enabled bool `json:"enabled"`
}

// Validate checks a trigger rule.
func (r *TriggerRule) Validate() error {
	if r == nil {
		return fmt.Errorf("agenda: nil trigger rule")
	}
	if r.ID == "" {
		return fmt.Errorf("agenda: trigger rule needs an id")
	}
	switch r.Action {
	case ActionStartTask, ActionLaunchFlow, ActionNotify, ActionStartSession:
	default:
		return fmt.Errorf("agenda: rule %q: unknown action %q", r.ID, r.Action)
	}
	if r.Target == "" {
		return fmt.Errorf("agenda: rule %q: action needs a target", r.ID)
	}
	if r.MaxPerHour < 0 {
		return fmt.Errorf("agenda: rule %q: negative rate limit", r.ID)
	}
	return nil
}

// TriggerHit records one rule firing for audit and tests.
type TriggerHit struct {
	RuleID  string        `json:"rule_id"`
	EventID string        `json:"event_id"`
	Action  TriggerAction `json:"action"`
	Target  string        `json:"target"`
	At      time.Time     `json:"at_utc"`
}

// TriggerEngine matches normalized events against rules with
// deduplication by (rule, event id) and per-rule rate limiting (21.3).
type TriggerEngine struct {
	rules []*TriggerRule
	// seen maps ruleID+"\x00"+eventID to first-fire time for dedup.
	seen map[string]time.Time
	// hits records firing times per rule for rate limiting.
	hits map[string][]time.Time
	log  []TriggerHit
}

// NewTriggerEngine returns an empty engine.
func NewTriggerEngine() *TriggerEngine {
	return &TriggerEngine{seen: map[string]time.Time{}, hits: map[string][]time.Time{}}
}

// AddRule registers a rule (replacing any rule with the same ID).
func (e *TriggerEngine) AddRule(r *TriggerRule) error {
	if err := r.Validate(); err != nil {
		return err
	}
	for i, old := range e.rules {
		if old.ID == r.ID {
			cp := *r
			e.rules[i] = &cp
			return nil
		}
	}
	cp := *r
	e.rules = append(e.rules, &cp)
	return nil
}

// RemoveRule deletes a rule.
func (e *TriggerEngine) RemoveRule(id string) error {
	for i, r := range e.rules {
		if r.ID == id {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("agenda: unknown trigger rule %q", id)
}

// Rules returns a copy of the registered rules.
func (e *TriggerEngine) Rules() []*TriggerRule {
	out := make([]*TriggerRule, 0, len(e.rules))
	for _, r := range e.rules {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// defaultDedupWindow bounds the event-id memory when a rule sets no window.
const defaultDedupWindow = 24 * time.Hour

// Ingest normalizes one event and returns the hits it produced.
// Duplicate (rule, event id) pairs inside the rule dedup window and
// over-limit bursts produce no hits (21.3).
func (e *TriggerEngine) Ingest(ev Event, now time.Time) []TriggerHit {
	if e.seen == nil {
		e.seen = map[string]time.Time{}
	}
	if e.hits == nil {
		e.hits = map[string][]time.Time{}
	}
	e.gc(now)
	if ev.ID == "" {
		return nil
	}
	var out []TriggerHit
	for _, r := range e.rules {
		if !r.Enabled || !r.Filter.Matches(ev) {
			continue
		}
		if !e.allowRate(r, now) {
			continue
		}
		key := r.ID + "\x00" + ev.ID
		if first, ok := e.seen[key]; ok {
			if now.Sub(first) < e.dedupWindow(r) {
				continue
			}
		}
		e.seen[key] = now
		hit := TriggerHit{RuleID: r.ID, EventID: ev.ID, Action: r.Action, Target: r.Target, At: now.UTC()}
		e.hits[r.ID] = append(e.hits[r.ID], now)
		e.log = append(e.log, hit)
		out = append(out, hit)
	}
	return out
}

func (e *TriggerEngine) dedupWindow(r *TriggerRule) time.Duration {
	if r.DedupWindow > 0 {
		return r.DedupWindow
	}
	return defaultDedupWindow
}

func (e *TriggerEngine) allowRate(r *TriggerRule, now time.Time) bool {
	if r.MaxPerHour <= 0 {
		return true
	}
	cutoff := now.Add(-time.Hour)
	kept := e.hits[r.ID][:0]
	count := 0
	for _, t := range e.hits[r.ID] {
		if t.After(cutoff) {
			kept = append(kept, t)
			count++
		}
	}
	e.hits[r.ID] = kept
	return count < r.MaxPerHour
}

func (e *TriggerEngine) gc(now time.Time) {
	maxWindow := defaultDedupWindow
	for _, r := range e.rules {
		if w := e.dedupWindow(r); w > maxWindow {
			maxWindow = w
		}
	}
	for key, t := range e.seen {
		if now.Sub(t) > maxWindow {
			delete(e.seen, key)
		}
	}
}

// Hits returns the firing log.
func (e *TriggerEngine) Hits() []TriggerHit {
	return append([]TriggerHit(nil), e.log...)
}
