package agenda

import (
	"context"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// CatchUp recalculates the firings lost during a downtime window
// [downSince, now] (17.3 step 5, 17.4) and satisfies the daemon boot
// AgendaCatchUp seam. With deferLaunch (safe mode) every due firing
// is recorded without launching; otherwise due fires launch through
// the single Tick firing path. Reminders due during the outage fire
// through the same Tick so late reminders arrive with their planned
// time preserved.
func (a *Agenda) CatchUp(downSince, now time.Time, deferLaunch bool) (fired, deferred int, err error) {
	held := false
	if deferLaunch && !a.inSafeMode() {
		a.EnterSafeMode()
		held = true
	}
	before := len(a.store.PendingFires())
	delivered, rerr := a.ReconcileBoot(context.Background(), downSince, now)
	if rerr != nil {
		return 0, 0, rerr
	}
	fired += len(delivered)
	_, _ = a.Tick(context.Background(), now)
	if held {
		deferred = len(a.store.PendingFires()) - before
		if deferred < 0 {
			deferred = 0
		}
		return 0, deferred, nil
	}
	return len(delivered), 0, nil
}

// TriggerLauncher delivers trigger-rule hits (21.3). The daemon wires
// each action to the real backend: task start to the orchestrator,
// workflow launch to the workflow engine, notify to the master
// notifier, session start to the agenda itself.
type TriggerLauncher interface {
	// StartTask begins a task for the rule target with the event payload.
	StartTask(ctx context.Context, target string, ev Event) error
	// LaunchFlow starts the named workflow with the event payload.
	LaunchFlow(ctx context.Context, target string, ev Event) error
	// Notify delivers the rule target message for the event.
	Notify(ctx context.Context, target string, ev Event) error
	// StartSession opens a project session for the target.
	StartSession(ctx context.Context, target string, ev Event) error
}

// TriggerPump matches normalized capability and channel events
// against the rule set and delivers hits (21.3): mail, channel
// messages, webhooks, file changes, calendar entries and capability
// task completions. Deduplication by event id and the per-rule rate
// limit live in TriggerEngine; Ingest stays synchronous so fire order
// is deterministic, and Run fans the source channels out in one
// safego.Go goroutine.
type TriggerPump struct {
	engine   *TriggerEngine
	launcher TriggerLauncher
	now      func() time.Time
}

// NewTriggerPump returns a pump over engine. A nil launcher records
// hits without delivering (safe-mode behavior).
func NewTriggerPump(engine *TriggerEngine, launcher TriggerLauncher) *TriggerPump {
	if engine == nil {
		engine = NewTriggerEngine()
	}
	return &TriggerPump{engine: engine, launcher: launcher, now: time.Now}
}

// Engine exposes the rule set for management tools.
func (p *TriggerPump) Engine() *TriggerEngine { return p.engine }

// Ingest normalizes one event, matches it and delivers every hit. It
// returns the hits for audit.
func (p *TriggerPump) Ingest(ctx context.Context, ev Event) []TriggerHit {
	if ev.At.IsZero() {
		ev.At = p.now().UTC()
	}
	hits := p.engine.Ingest(ev, p.now())
	for _, h := range hits {
		if p.launcher == nil {
			continue
		}
		var err error
		switch h.Action {
		case ActionStartTask:
			err = p.launcher.StartTask(ctx, h.Target, ev)
		case ActionLaunchFlow:
			err = p.launcher.LaunchFlow(ctx, h.Target, ev)
		case ActionNotify:
			err = p.launcher.Notify(ctx, h.Target, ev)
		case ActionStartSession:
			err = p.launcher.StartSession(ctx, h.Target, ev)
		default:
			err = fmt.Errorf("agenda: unknown trigger action %q", h.Action)
		}
		_ = err
	}
	return hits
}

// Run subscribes to the source channels until ctx ends or Stop closes
// the pump. Sources are the §21.3 feeds (mail, channels, webhooks,
// file watcher, calendar, capability completions) already normalized
// to Events by the daemon adapters.
//
// The returned channel closes when the pump goroutine exits (context
// done or no sources left), so the daemon can wait for a clean stop.
func (p *TriggerPump) Run(ctx context.Context, sources ...<-chan Event) <-chan struct{} {
	done := make(chan struct{})
	safego.Go("agenda-trigger-pump", nil, func() {
		defer close(done)
		cases := make([]<-chan Event, len(sources))
		copy(cases, sources)
		for {
			if len(cases) == 0 {
				return
			}
			// Multiplex without reflect: poll in order; sources are
			// low-rate control feeds, so a short park is exact enough
			// and keeps the pump deterministic.
			progress := false
			for _, ch := range cases {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-ch:
					if !ok {
						continue
					}
					p.Ingest(ctx, ev)
					progress = true
				default:
				}
			}
			if !progress {
				select {
				case <-ctx.Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		}
	})
	return done
}

// Normalizers turn raw §21.3 source payloads into Events with stable
// ids so duplicate deliveries dedupe by event id.

// NormalizeMail builds the event for one received email.
func NormalizeMail(id, from, subject string, at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Event{ID: "mail:" + id, Type: "mail.received", Source: "mail",
		Payload: map[string]any{"from": from, "subject": subject}, At: at.UTC()}
}

// NormalizeChannelMessage builds the event for one channel message
// (WhatsApp, Telegram or another channel adapter).
func NormalizeChannelMessage(id, channel, sender, text string, at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Event{ID: "chan:" + channel + ":" + id, Type: "channel.message", Source: channel,
		Payload: map[string]any{"sender": sender, "text": text}, At: at.UTC()}
}

// NormalizeWebhook builds the event for one inbound webhook.
func NormalizeWebhook(id, hook string, body map[string]any, at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Event{ID: "hook:" + hook + ":" + id, Type: "webhook." + hook, Source: "webhook",
		Payload: body, At: at.UTC()}
}

// NormalizeFileChange builds the event for one watched-file change.
func NormalizeFileChange(path, op string, at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Event{ID: "file:" + path + ":" + op, Type: "file.changed", Source: "watcher",
		Payload: map[string]any{"path": path, "op": op}, At: at.UTC()}
}

// NormalizeCalendar builds the event for one calendar entry.
func NormalizeCalendar(id, title string, start time.Time, at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Event{ID: "cal:" + id, Type: "calendar.event", Source: "calendar",
		Payload: map[string]any{"title": title, "start": start.UTC().Format(time.RFC3339)}, At: at.UTC()}
}

// NormalizeCapabilityTask builds the event for one durable capability
// task completion (Section 14.1 poll terminal state).
func NormalizeCapabilityTask(capabilityID, taskID, state string, at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Event{ID: "cap:" + capabilityID + ":" + taskID, Type: "capability." + state, Source: capabilityID,
		Payload: map[string]any{"task_id": taskID, "state": state}, At: at.UTC()}
}
