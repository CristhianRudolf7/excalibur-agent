package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	pw "github.com/mxschmitt/playwright-go"
)

// ErrUnknownLiveSession identifies a watch request for a session that no longer exists.
var ErrUnknownLiveSession = errors.New("browser: unknown live session")

// LiveRect is a viewport-relative rectangle in CSS pixels.
type LiveRect struct{ X, Y, Width, Height float64 }

// LiveEvent describes browser lifecycle and activity without typed values.
type LiveEvent struct {
	Type, Session, Tab, Phase, Kind, Target, URL, Title string
	Box                                                 *LiveRect
}

// LiveFrame contains immutable JPEG bytes shared by the current viewers.
type LiveFrame struct {
	Seq           uint64
	Tab           string
	JPEG          []byte
	Width, Height int
	At            time.Time
}

type liveCDP interface {
	On(string, any)
	Send(string, map[string]any) (any, error)
	Detach() error
}

type livePage struct {
	page         pw.Page
	session, tab string
	withheld     atomic.Bool
	closed       atomic.Bool
	viewport     atomic.Uint64
	location     atomic.Value // main-frame URL; compared locally, never logged
	listen       sync.Once
}

type liveSession struct {
	source   *livePage
	open     func() (liveCDP, error)
	watchers map[uint64]func(LiveFrame)
	stream   *liveStream
	previous <-chan struct{}
	cached   *LiveFrame
	seq      uint64
}

type liveState struct {
	mu          sync.Mutex
	sessions    map[string]*liveSession
	pages       []*livePage
	observer    func(LiveEvent)
	events      []LiveEvent
	dispatching bool
	next        uint64
	timing      liveTiming
}

type livePacket struct {
	data          string
	id            any
	width, height int
}

type liveStream struct {
	finished chan struct{}
	done     chan struct{}
}

// Zero values use production deadlines. Tests configure these before watching.
type liveTiming struct {
	call, initial, watchdog, stale, backoff, maxBackoff, frame time.Duration
}

func (t liveTiming) defaults() liveTiming {
	values := []*time.Duration{&t.call, &t.initial, &t.watchdog, &t.stale, &t.backoff, &t.maxBackoff, &t.frame}
	defaults := []time.Duration{3 * time.Second, 500 * time.Millisecond, 1500 * time.Millisecond, 6 * time.Second, 250 * time.Millisecond, 2 * time.Second, 100 * time.Millisecond}
	for i, v := range values {
		if *v <= 0 {
			*v = defaults[i]
		}
	}
	return t
}

type liveFeed struct {
	mu      sync.Mutex
	pending []livePacket
	wake    chan struct{}
	closed  bool
}

var (
	errLiveTimeout = errors.New("browser: live protocol deadline")
	errLiveStopped = errors.New("browser: live stream stopped")
	errLiveCapture = errors.New("browser: invalid live screenshot")
)

type liveResult[T any] struct {
	value T
	err   error
}

// liveCall never waits for a late result. CDP has no per-call cancellation, so
// its worker can only leave Send when the transport returns or disconnects.
func liveCall[T any](done <-chan struct{}, timeout time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	select {
	case <-done:
		return zero, errLiveStopped
	default:
	}
	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	result := make(chan liveResult[T], 1)
	safego.Go("browser live protocol", nil, func() {
		// Do not let safego log a protocol panic containing private page data.
		defer func() {
			if recover() != nil {
				result <- liveResult[T]{err: errors.New("browser: live protocol panic")}
			}
		}()
		v, err := fn()
		result <- liveResult[T]{v, err}
	})
	select {
	case <-done:
		return zero, errLiveStopped
	case <-timer.C:
		return zero, errLiveTimeout
	case r := <-result:
		// Do not accept completion that raced cancellation or the deadline.
		select {
		case <-done:
			return zero, errLiveStopped
		case <-timer.C:
			return zero, errLiveTimeout
		default:
			if !time.Now().Before(deadline) {
				return zero, errLiveTimeout
			}
			return r.value, r.err
		}
	}
}

// SetLiveObserver installs a non-blocking callback. Nil disables events.
func (e *Engine) SetLiveObserver(fn func(LiveEvent)) {
	e.live.mu.Lock()
	e.live.observer = fn
	pages := append([]*livePage(nil), e.live.pages...)
	e.live.mu.Unlock()
	if fn != nil {
		for _, p := range pages {
			e.listenLive(p)
		}
	}
}

func (e *Engine) hasLiveObserver() bool {
	e.live.mu.Lock()
	defer e.live.mu.Unlock()
	return e.live.observer != nil
}

// emitLive queues ordered callbacks so observers can re-enter the engine. No
// callback runs under an engine mutex or on the Playwright dispatcher.
func (e *Engine) emitLive(v LiveEvent) {
	e.live.mu.Lock()
	defer e.live.mu.Unlock()
	if e.live.observer == nil {
		return
	}
	e.live.events = append(e.live.events, v)
	if e.live.dispatching {
		return
	}
	e.live.dispatching = true
	safego.Go("browser live observer", nil, func() {
		for {
			e.live.mu.Lock()
			if len(e.live.events) == 0 {
				e.live.dispatching = false
				e.live.mu.Unlock()
				return
			}
			v, fn := e.live.events[0], e.live.observer
			e.live.events[0] = LiveEvent{}
			e.live.events = e.live.events[1:]
			e.live.mu.Unlock()
			if fn != nil {
				liveCallback(func() { fn(v) })
			}
		}
	})
}

// A callback panic must neither stop the stream nor log potentially private data.
func liveCallback(fn func()) { defer func() { _ = recover() }(); fn() }

// WatchLive streams the active tab until its idempotent stop is called.
func (e *Engine) WatchLive(session string, fn func(LiveFrame)) (func(), error) {
	e.live.mu.Lock()
	s := e.live.sessions[session]
	if s == nil {
		e.live.mu.Unlock()
		return nil, ErrUnknownLiveSession
	}
	e.live.next++
	id := e.live.next
	s.watchers[id] = fn
	cached := s.cached
	source := s.source
	e.startLiveLocked(s)
	e.live.mu.Unlock()
	if source != nil {
		e.listenLive(source)
	}
	if cached != nil && fn != nil {
		liveCallback(func() { fn(*cached) })
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			e.live.mu.Lock()
			defer e.live.mu.Unlock()
			delete(s.watchers, id)
			if len(s.watchers) == 0 {
				stopLiveLocked(s)
			}
		})
	}, nil
}

// LiveWatched does not wait for a browser operation to complete.
func (e *Engine) LiveWatched(session string) bool {
	e.live.mu.Lock()
	defer e.live.mu.Unlock()
	s := e.live.sessions[session]
	return s != nil && len(s.watchers) != 0
}

func stopLiveLocked(s *liveSession) {
	if s.stream != nil {
		close(s.stream.done)
		s.previous = s.stream.finished
		s.stream = nil
	}
}

func (e *Engine) startLiveLocked(s *liveSession) {
	if s.stream != nil || s.source == nil || s.open == nil || len(s.watchers) == 0 {
		return
	}
	stream := &liveStream{finished: make(chan struct{}), done: make(chan struct{})}
	s.stream = stream
	source, open, previous := s.source, s.open, s.previous
	safego.Go("browser live frames", nil, func() {
		defer close(stream.finished)
		defer func() { _ = recover() }()
		if previous != nil {
			select {
			case <-previous:
			case <-stream.done:
				return
			}
		}
		select {
		case <-stream.done:
			return
		default:
		}
		e.runLive(s, source, stream, open)
	})
}

// refreshLive is called only under opMu; WatchLive only reads this shadow map.
func (e *Engine) refreshLive() {
	e.live.mu.Lock()
	if e.live.sessions == nil {
		e.live.sessions = map[string]*liveSession{}
	}
	var listen []*livePage
	e.live.pages = nil
	for id, session := range e.sessions {
		if e.closed {
			continue
		}
		s := e.live.sessions[id]
		if s == nil {
			s = &liveSession{watchers: map[uint64]func(LiveFrame){}}
			e.live.sessions[id] = s
		}
		for _, t := range session.tabs {
			if t.live != nil {
				e.live.pages = append(e.live.pages, t.live)
				if e.live.observer != nil || len(s.watchers) > 0 {
					listen = append(listen, t.live)
				}
			}
		}
		var source *livePage
		var open func() (liveCDP, error)
		if t := session.tabs[session.active]; !e.suspended && e.browser != nil && t != nil && t.page != nil && !t.live.closed.Load() {
			source = t.live
			if size := t.page.ViewportSize(); size != nil {
				source.viewport.Store(uint64(uint32(size.Width))<<32 | uint64(uint32(size.Height)))
			}
			browser, page := e.browser, t.page
			open = func() (liveCDP, error) { return browser.NewCDPSession(page) }
		}
		if s.source != source {
			stopLiveLocked(s)
			s.cached = nil
			s.source = source
		}
		s.open = open
		e.startLiveLocked(s)
	}
	for id, s := range e.live.sessions {
		if e.sessions[id] == nil || e.closed {
			stopLiveLocked(s)
			s.watchers = map[uint64]func(LiveFrame){}
			delete(e.live.sessions, id)
		}
	}
	e.live.mu.Unlock()
	for _, p := range listen {
		e.listenLive(p)
	}
}

// Track closure independently of Playwright's unsynchronized IsClosed field.
// This listener does no protocol work and is also needed when live viewing is off.
func (e *Engine) listenLiveClose(p *livePage) {
	p.page.OnClose(func(pw.Page) {
		p.closed.Store(true)
		e.live.mu.Lock()
		defer e.live.mu.Unlock()
		if s := e.live.sessions[p.session]; s != nil && s.source == p {
			stopLiveLocked(s)
		}
	})
}

func (e *Engine) listenLive(p *livePage) {
	if p == nil || p.page == nil {
		return
	}
	p.listen.Do(func() {
		page := p.page
		page.OnFrameNavigated(func(frame pw.Frame) {
			if frame != page.MainFrame() {
				return
			}
			p.withheld.Store(false)
			p.location.Store(frame.URL())
			if !e.hasLiveObserver() {
				return
			}
			raw := frame.URL()
			if raw == "about:blank" {
				return
			}
			e.emitLive(LiveEvent{Type: "activity", Session: p.session, Tab: p.tab, Kind: "page", Phase: "done", URL: e.liveURL(raw)})
		})

	})
}

// runLive owns retries for the entire watch, rather than one CDP connection.
func (e *Engine) runLive(s *liveSession, p *livePage, stream *liveStream, open func() (liveCDP, error)) {
	timing := e.live.timing.defaults()
	backoff, failed := timing.backoff, false
	var lastFrame time.Time
	e.live.mu.Lock()
	if s.cached != nil {
		lastFrame = s.cached.At
	}
	e.live.mu.Unlock()
	recovered := func() {
		if failed {
			slog.Info("browser live stream recovered", "session", p.session, "tab", p.tab)
			failed = false
		}
		backoff = timing.backoff
	}
	for !p.closed.Load() {
		select {
		case <-stream.done:
			return
		default:
		}
		initialDeadline := time.Now().Add(timing.initial)
		cdp, err := liveCall(stream.done, timing.call, open)
		class := "open"
		if err == nil {
			attemptTiming := timing
			attemptTiming.initial = max(time.Nanosecond, time.Until(initialDeadline))
			class, err = e.runLiveCDP(s, p, stream, cdp, attemptTiming, &lastFrame, recovered)
		}
		if errors.Is(err, errLiveStopped) || p.closed.Load() {
			return
		}
		if errors.Is(err, errLiveTimeout) {
			class = "timeout"
		}
		// Never attach raw protocol errors, URLs, titles or frame content.
		slog.Warn("browser live stream retry", "session", p.session, "tab", p.tab, "class", class)
		failed = true
		if !liveDelay(stream.done, backoff) {
			return
		}
		backoff = min(backoff*2, timing.maxBackoff)
	}
}

// Cleanup shares one deadline so a dead Chrome cannot turn two bounded calls
// into a six-second shutdown. Detach is attempted even when stop times out.
func closeLiveCDP(cdp liveCDP, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	_, _ = liveCall(nil, timeout, func() (any, error) { return cdp.Send("Page.stopScreencast", nil) })
	_, _ = liveCall(nil, max(time.Nanosecond, time.Until(deadline)), func() (any, error) { return nil, cdp.Detach() })
}

func (e *Engine) runLiveCDP(s *liveSession, p *livePage, stream *liveStream, cdp liveCDP, timing liveTiming, lastFrame *time.Time, recovered func()) (string, error) {
	feed := &liveFeed{wake: make(chan struct{}, 1)}
	attemptDone := make(chan struct{})
	defer func() {
		close(attemptDone)
		feed.mu.Lock()
		feed.closed, feed.pending = true, nil
		feed.mu.Unlock()
		closeLiveCDP(cdp, timing.call)
	}()
	cdp.On("Page.screencastFrame", func(v map[string]any) {
		// Do not decode or send protocol commands on the dispatcher. A retired
		// connection cannot enqueue packets into its successor's feed.
		meta, _ := v["metadata"].(map[string]any)
		data, _ := v["data"].(string)
		packet := livePacket{data: data, id: v["sessionId"], width: liveNumber(meta["deviceWidth"]), height: liveNumber(meta["deviceHeight"])}
		feed.mu.Lock()
		defer feed.mu.Unlock()
		if feed.closed {
			return
		}
		feed.pending = append(feed.pending, packet)
		select {
		case feed.wake <- struct{}{}:
		default:
		}
	})
	// Start runs independently: a stuck start must not postpone the first
	// screenshot once open has supplied a usable CDP session.
	started := make(chan error, 1)
	safego.Go("browser live start", nil, func() {
		_, err := liveCall(attemptDone, timing.call, func() (any, error) {
			return cdp.Send("Page.startScreencast", map[string]any{"format": "jpeg", "quality": 60, "maxWidth": 1280, "maxHeight": 800})
		})
		started <- err
	})
	initial := time.NewTimer(timing.initial)
	defer initial.Stop()
	watchdog := time.NewTimer(timing.watchdog)
	defer watchdog.Stop()
	lastCast, lastURL := time.Now(), p.location.Load()
	received, running := false, false
	for {
		select {
		case <-stream.done:
			return "", errLiveStopped
		case err := <-started:
			if err != nil {
				return "start", err
			}
			started, running = nil, true
		case <-initial.C:
			if !received && !p.withheld.Load() {
				_, err := e.captureLiveFrame(s, p, stream, cdp, timing, lastFrame)
				if err != nil {
					return "screenshot", err
				}
				if running {
					recovered()
				}
				watchdog.Reset(timing.watchdog)
			}
		case <-watchdog.C:
			if p.closed.Load() {
				return "", errLiveStopped
			}
			if time.Since(*lastFrame) >= timing.watchdog && !p.withheld.Load() {
				_, err := e.captureLiveFrame(s, p, stream, cdp, timing, lastFrame)
				if err != nil {
					return "screenshot", err
				}
				if running {
					recovered()
				}
			}
			if time.Since(lastCast) >= timing.stale && p.location.Load() != lastURL {
				return "timeout", errLiveTimeout
			}
			watchdog.Reset(timing.watchdog)
		case <-feed.wake:
			feed.mu.Lock()
			packets := feed.pending
			feed.pending = nil
			feed.mu.Unlock()
			for _, packet := range packets {
				lastCast, lastURL = time.Now(), p.location.Load()
				if !liveDelay(stream.done, max(0, timing.frame-time.Since(*lastFrame))) {
					return "", errLiveStopped
				}
				if len(packet.data) <= base64.StdEncoding.EncodedLen(1<<20) && !p.withheld.Load() {
					jpeg, err := base64.StdEncoding.DecodeString(packet.data)
					if err == nil && len(jpeg) > 0 && len(jpeg) <= 1<<20 && e.deliverLive(s, p, stream, jpeg, packet.width, packet.height) {
						received = true
						*lastFrame = time.Now()
						watchdog.Reset(timing.watchdog)
					}
				}
				// Delaying even the first ack prevents a two-frame burst.
				if !liveDelay(stream.done, timing.frame) {
					return "", errLiveStopped
				}
				_, err := liveCall(stream.done, timing.call, func() (any, error) {
					return cdp.Send("Page.screencastFrameAck", map[string]any{"sessionId": packet.id})
				})
				if err != nil {
					return "ack", err
				}
				if running {
					recovered()
				}
			}
		}
	}
}

func liveNumber(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func liveDelay(done <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-done:
		return false
	case <-timer.C:
		return true
	}
}

func (e *Engine) captureLiveFrame(s *liveSession, p *livePage, stream *liveStream, cdp liveCDP, timing liveTiming, lastFrame *time.Time) (bool, error) {
	result, err := liveCall(stream.done, timing.call, func() (any, error) {
		return cdp.Send("Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 60})
	})
	if err != nil {
		return false, err
	}
	object, _ := result.(map[string]any)
	data, _ := object["data"].(string)
	if len(data) > base64.StdEncoding.EncodedLen(1<<20) {
		return false, errLiveCapture
	}
	jpeg, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(jpeg) == 0 || len(jpeg) > 1<<20 {
		return false, errLiveCapture
	}
	e.live.mu.Lock()
	identical := s.cached != nil && bytes.Equal(s.cached.JPEG, jpeg)
	e.live.mu.Unlock()
	if identical || p.withheld.Load() {
		return false, nil
	}
	width, height := 0, 0
	metrics, err := liveCall(stream.done, timing.call, func() (any, error) { return cdp.Send("Page.getLayoutMetrics", nil) })
	if errors.Is(err, errLiveStopped) {
		return false, err
	}
	if err == nil {
		object, _ := metrics.(map[string]any)
		viewport, _ := object["cssLayoutViewport"].(map[string]any)
		width, height = liveNumber(viewport["clientWidth"]), liveNumber(viewport["clientHeight"])
	}
	if width == 0 || height == 0 {
		size := p.viewport.Load()
		width, height = int(size>>32), int(uint32(size))
	}
	if !liveDelay(stream.done, max(0, timing.frame-time.Since(*lastFrame))) {
		return false, errLiveStopped
	}
	delivered := e.deliverLive(s, p, stream, jpeg, width, height)
	if delivered {
		*lastFrame = time.Now()
	}
	return delivered, nil
}

func (e *Engine) deliverLive(s *liveSession, p *livePage, stream *liveStream, jpeg []byte, width, height int) bool {
	e.live.mu.Lock()
	if s.stream != stream || p.withheld.Load() || p.closed.Load() {
		e.live.mu.Unlock()
		return false
	}
	s.seq++
	frame := LiveFrame{Seq: s.seq, Tab: p.tab, JPEG: append([]byte(nil), jpeg...), Width: width, Height: height, At: time.Now()}
	s.cached = &frame
	callbacks := make([]func(LiveFrame), 0, len(s.watchers))
	for _, fn := range s.watchers {
		if fn != nil {
			callbacks = append(callbacks, fn)
		}
	}
	e.live.mu.Unlock()
	for _, fn := range callbacks {
		liveCallback(func() { fn(frame) })
	}
	return true
}

func liveURL(raw string) string { return liveBytes(parseLiveURL(raw), 512) }
func parseLiveURL(raw string) string {
	if raw == "about:blank" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = nil, "", "", "", false
	return u.String()
}
func liveBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
func (e *Engine) liveURL(raw string) string { return liveBytes(e.clean(parseLiveURL(raw)), 512) }

// typingPlan is independent of watching: the caller applies it only to watched
// actions, leaving routine replay and unwatched Fill behavior untouched.
type liveTyping struct {
	animate, password, withhold bool
	delay                       time.Duration
}

func typingPlan(value string, isPassword bool) liveTyping {
	secret := strings.HasPrefix(value, "secret://")
	suffix := value[strings.LastIndex(value, ".")+1:]
	username := secret && (suffix == "username" || suffix == "email")
	protected := isPassword || (secret && !username)
	n := utf8.RuneCountInString(value)
	animate := !protected && (username || (n > 0 && n <= 120))
	delay := typingDelay(value)
	return liveTyping{animate: animate, password: protected, withhold: secret && !username && !isPassword, delay: delay}
}

const passwordFieldScript = `el => el.type === 'password' || String(el.autocomplete || '').toLowerCase().includes('password')`

func livePassword(locator pw.Locator) bool {
	value, err := locator.Evaluate(passwordFieldScript, nil, pw.LocatorEvaluateOptions{Timeout: pw.Float(1000)})
	password, ok := value.(bool)
	return err != nil || !ok || password
}
func liveBox(locator pw.Locator) *LiveRect {
	if err := locator.ScrollIntoViewIfNeeded(pw.LocatorScrollIntoViewIfNeededOptions{Timeout: pw.Float(1000)}); err != nil {
		return nil
	}
	box, err := locator.BoundingBox(pw.LocatorBoundingBoxOptions{Timeout: pw.Float(1000)})
	if err != nil || box == nil {
		return nil
	}
	return &LiveRect{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height}
}

func (e *Engine) activity(t *tab, kind, phase, target string, box *LiveRect) {
	if !e.hasLiveObserver() {
		return
	}
	v := LiveEvent{Type: "activity", Session: t.session, Tab: t.id, Kind: kind, Phase: phase, Target: boundedText(e.clean(target), 80), Box: box}
	if phase == "done" {
		v.URL = e.liveURL(t.page.URL())
		v.Title = boundedText(e.clean(t.title), 200)
	}
	e.emitLive(v)
}

func (e *Engine) observed(ctx context.Context, t *tab, a arguments, kind string, run func() capabilities.TaskResult) capabilities.TaskResult {
	if kind == "" {
		return run()
	}
	if e.hasLiveObserver() {
		v := LiveEvent{Type: "activity", Session: t.session, Tab: t.id, Kind: kind, Phase: "start"}
		if kind == "navigate" && (a.action("go") == "go" || a.action("go") == "url") {
			v.URL = e.liveURL(a.str("url"))
		}
		e.emitLive(v)
	}
	r := run()
	phase := "done"
	if r.State == capabilities.StateFailed || r.State == capabilities.StateCancelled {
		phase = "failed"
	}
	e.activity(t, kind, phase, "", nil)
	return r
}

func typingDelay(value string) time.Duration {
	delay := 45 * time.Millisecond
	if n := utf8.RuneCountInString(value); n > 0 && 2*time.Second/time.Duration(n) < delay {
		delay = 2 * time.Second / time.Duration(n)
	}
	return delay
}
func livePhase(r capabilities.TaskResult) string {
	if r.State == capabilities.StateFailed || r.State == capabilities.StateCancelled {
		return "failed"
	}
	return "done"
}

const loginSettle = 8 * time.Second

func submitsForm(action, value string) bool {
	return action == "click" || (action == "press" && value == "Enter")
}

// settleLogin gives a submitted login form time to respond. A site can take
// seconds to validate and redirect, and reporting the still-visible form at once
// reads as a failed login. It runs only on the observed assistant path and returns
// as soon as the page leaves that form, so other clicks are not delayed.
func (e *Engine) settleLogin(ctx context.Context, t *tab, before string) {
	if t.blocked != capabilities.BlockedLogin || !e.hasLiveObserver() {
		return
	}
	deadline := time.Now().Add(loginSettle)
	for time.Now().Before(deadline) && t.blocked == capabilities.BlockedLogin && t.page.URL() == before {
		if !liveDelay(ctx.Done(), 250*time.Millisecond) {
			return
		}
		e.detect(t)
	}
}
