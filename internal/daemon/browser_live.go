package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type browserLiveEngine interface {
	SetLiveObserver(func(browser.LiveEvent))
	WatchLive(session string, fn func(browser.LiveFrame)) (stop func(), err error)
}

type browserLiveSession struct {
	conversation, run string
	visible, ended    bool
	url, title        string
	activity          *api.BrowserActivityEvent
	endedAt           time.Time
	order, seq        uint64
	done              chan struct{}
	viewers           int
}

// browserLiveHub contains metadata only. Images go directly from the engine to
// the gateway callback, never through the bus or durable storage.
type browserLiveHub struct {
	mu       sync.Mutex
	publish  sync.Mutex
	sessions map[string]*browserLiveSession
	engine   browserLiveEngine
	bus      *telemetry.Bus
	now      func() time.Time
	order    uint64
	closed   bool
}

func newBrowserLiveHub(engine browserLiveEngine, bus *telemetry.Bus) *browserLiveHub {
	return &browserLiveHub{engine: engine, bus: bus, now: time.Now, sessions: make(map[string]*browserLiveSession)}
}

// expireLocked is deliberately lazy: ended sessions need no timers or workers.
func (h *browserLiveHub) expireLocked() {
	for id, s := range h.sessions {
		if s.ended && h.now().Sub(s.endedAt) >= time.Minute {
			delete(h.sessions, id)
		}
	}
}

func (h *browserLiveHub) bind(session string, origin credentialOrigin) {
	if h == nil || session == "" || origin.conversation == "" || origin.run == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expireLocked()
	if h.closed || h.sessions[session] != nil {
		return
	}
	h.sessions[session] = &browserLiveSession{conversation: origin.conversation, run: origin.run, done: make(chan struct{})}
}

func browserSessionEvent(id string, s *browserLiveSession) *api.BrowserSessionEvent {
	state := api.BrowserSessionActive
	if s.ended {
		state = api.BrowserSessionEnded
	}
	return &api.BrowserSessionEvent{ConversationID: s.conversation, RunID: s.run, SessionID: id, State: state, URL: s.url, Title: s.title}
}

func (h *browserLiveHub) observe(ev browser.LiveEvent) {
	// Serialize updates and publication together so simultaneous callbacks cannot
	// publish one session's activity sequence out of order. Publish never owns mu.
	h.publish.Lock()
	defer h.publish.Unlock()
	h.mu.Lock()
	h.expireLocked()
	s := h.sessions[ev.Session]
	if h.closed || s == nil || s.ended {
		h.mu.Unlock()
		return
	}
	var session *api.BrowserSessionEvent
	var activity *api.BrowserActivityEvent
	switch ev.Type {
	case "activity":
		if !s.visible && ev.Kind == api.BrowserKindPage && ev.URL == "" {
			h.mu.Unlock()
			return
		}
		if ev.URL != "" {
			s.url = ev.URL
		}
		if ev.Title != "" {
			s.title = ev.Title
		}
		if !s.visible {
			s.visible = true
			h.order++
			s.order = h.order
			session = browserSessionEvent(ev.Session, s)
		}
		s.seq++
		activity = &api.BrowserActivityEvent{ConversationID: s.conversation, RunID: s.run, SessionID: ev.Session, TabID: ev.Tab, Seq: s.seq, Phase: ev.Phase, Kind: ev.Kind, Target: ev.Target, URL: ev.URL, Title: ev.Title}
		if ev.Box != nil {
			activity.Box = &api.BrowserRect{X: ev.Box.X, Y: ev.Box.Y, Width: ev.Box.Width, Height: ev.Box.Height}
		}
		s.activity = activity
	case "session_closed":
		s.ended, s.endedAt = true, h.now()
		close(s.done)
		// A session that never showed anything stays invisible when it ends.
		if s.visible {
			session = browserSessionEvent(ev.Session, s)
		}
	}
	run := types.RunID(s.run)
	h.mu.Unlock()
	if h.bus != nil {
		if session != nil {
			_, _ = h.bus.Publish(api.EventBrowserSession, &run, nil, session)
		}
		if activity != nil {
			_, _ = h.bus.Publish(api.EventBrowserActivity, &run, nil, activity)
		}
	}
}

func (h *browserLiveHub) state(conversation string) api.BrowserLiveStateResult {
	out := api.BrowserLiveStateResult{}
	if h == nil {
		return out
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expireLocked()
	var last uint64
	for id, s := range h.sessions {
		if s.conversation != conversation || !s.visible || s.order <= last {
			continue
		}
		last = s.order
		out.Session = browserSessionEvent(id, s)
		activity := *s.activity
		if activity.Box != nil {
			box := *activity.Box
			activity.Box = &box
		}
		out.Activity = &activity
	}
	return out
}

func (h *browserLiveHub) close() {
	if h == nil {
		return
	}
	h.publish.Lock()
	defer h.publish.Unlock()
	h.mu.Lock()
	h.closed = true
	for _, s := range h.sessions {
		if !s.ended {
			close(s.done)
			s.ended, s.endedAt = true, h.now()
		}
	}
	h.mu.Unlock()
}

// liveBrowserTool preserves the wrapped definition and complete result. The
// open event precedes binding; only subsequent activities make a session visible.
type liveBrowserTool struct {
	tools.Tool
	hub *browserLiveHub
}

func (t *liveBrowserTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	out, err := t.Tool.Invoke(ctx, inv)
	if err == nil && out.Status == tools.StatusOK && inv.Tool == "browser_open_session" {
		var opened struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal([]byte(out.Output), &opened) == nil {
			origin, _ := ctx.Value(credentialOriginKey{}).(credentialOrigin)
			t.hub.bind(opened.SessionID, origin)
		}
	}
	return out, err
}

func (b *DaemonBackend) browserLiveOp(_ context.Context, raw json.RawMessage) (any, error) {
	var p api.BrowserLiveStateParams
	if strictLibraryParams(raw, &p) != nil || !artifactScopePattern.MatchString(p.ConversationID) {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "invalid browser live parameters"}
	}
	return b.liveBrowserHub().state(p.ConversationID), nil
}

func (b *DaemonBackend) liveBrowserHub() *browserLiveHub {
	if b.Services == nil {
		return nil
	}
	return b.Services.BrowserLive
}

// WatchBrowser enforces conversation ownership before attaching an engine
// watcher. Its callback is memory-only; frames are never telemetry events.
func (b *DaemonBackend) WatchBrowser(conversationID, sessionID string, fn func(gateway.BrowserFrame)) (func(), <-chan struct{}, error) {
	unknown := &api.Error{Code: api.CodeUnknownTarget, Message: "unknown browser session"}
	h := b.liveBrowserHub()
	if h == nil || !artifactScopePattern.MatchString(conversationID) {
		return nil, nil, unknown
	}
	h.mu.Lock()
	h.expireLocked()
	s := h.sessions[sessionID]
	if h.closed || s == nil || s.conversation != conversationID || s.ended || !s.visible {
		h.mu.Unlock()
		return nil, nil, unknown
	}
	s.viewers++
	h.mu.Unlock()
	stop, err := h.engine.WatchLive(sessionID, func(f browser.LiveFrame) {
		fn(gateway.BrowserFrame{Seq: f.Seq, Tab: f.Tab, JPEG: f.JPEG, Width: f.Width, Height: f.Height, AtMillis: f.At.UnixMilli()})
	})
	var once sync.Once
	release := func() {
		once.Do(func() {
			if stop != nil {
				stop()
			}
			h.mu.Lock()
			s.viewers--
			h.mu.Unlock()
		})
	}
	if err != nil {
		release()
		return nil, nil, unknown
	}
	return release, s.done, nil
}

var _ gateway.BrowserLiveBackend = (*DaemonBackend)(nil)

// CloseBrowserLive terminates viewers without publishing shutdown events.
func (s *Services) CloseBrowserLive() {
	if s != nil {
		s.BrowserLive.close()
	}
}
