package browser

import (
	"bytes"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	pw "github.com/mxschmitt/playwright-go"
)

func TestLiveURLAndCleaning(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{"https://user:pass@example.test/path?q=private#token", "https://example.test/path"},
		{"http://example.test:8080/a%20b?x=1", "http://example.test:8080/a%20b"},
		{"about:blank", ""}, {"http://[invalid", ""},
	} {
		if got := liveURL(tt.raw); got != tt.want {
			t.Errorf("liveURL = %q, want %q", got, tt.want)
		}
	}
	e := New(Config{})
	e.secrets = []string{"sentinel"}
	if got := e.liveURL("https://example.test/sentinel?q=sentinel#sentinel"); got != "https://example.test/[secret]" {
		t.Fatal(got)
	}
	if got := e.liveURL("https://example.test/" + strings.Repeat("x", 1000)); len(got) > 512 {
		t.Fatal("URL exceeds byte limit")
	}
	if got := liveBytes(strings.Repeat("界", 300), 512); len(got) > 512 || !utf8.ValidString(got) {
		t.Fatal("invalid truncation")
	}
	e.secrets = append(e.secrets, strings.Repeat("z", 100))
	if got := e.liveURL("https://example.test/" + strings.Repeat("a", 470) + strings.Repeat("z", 100)); strings.Contains(got, "z") {
		t.Fatal("truncation preceded redaction")
	}
	events := make(chan LiveEvent, 2)
	e.SetLiveObserver(func(v LiveEvent) { events <- v })
	e.activity(&tab{id: "t", session: "s"}, "click", "start", strings.Repeat("界", 100)+"sentinel", nil)
	v := takeLive(t, events)
	if utf8.RuneCountInString(v.Target) != 80 || strings.Contains(v.Target, "sentinel") {
		t.Fatal("target cleaning or truncation")
	}
}

func TestLiveTypingTable(t *testing.T) {
	for _, tt := range []struct {
		name, value                        string
		field, animate, password, withhold bool
	}{
		{"plain", "hello", false, true, false, false},
		{"empty", "", false, false, false, false},
		{"unicode", strings.Repeat("界", 120), false, true, false, false},
		{"long", strings.Repeat("x", 121), false, false, false, false},
		{"password", "hello", true, false, true, false},
		{"username", "secret://login.username", false, true, false, false},
		{"email", "secret://login.email", false, true, false, false},
		{"username password field", "secret://login.username", true, false, true, false},
		{"secret password field", "secret://login.password", true, false, true, false},
		{"secret visible", "secret://login.password", false, false, true, true},
		{"dash username", "secret://login-username", false, false, true, true},
		{"dash email", "secret://login-email", false, false, true, true},
		{"last suffix", "secret://login.username.other", false, false, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := typingPlan(tt.value, tt.field)
			if p.animate != tt.animate || p.password != tt.password || p.withhold != tt.withhold {
				t.Fatalf("unexpected plan: %+v", p)
			}
			if n := utf8.RuneCountInString(tt.value); p.animate && (p.delay > 45*time.Millisecond || p.delay*time.Duration(n) > 2*time.Second) {
				t.Fatal("typing budget exceeded")
			}
		})
	}
}

type liveCommand struct {
	name   string
	at     time.Time
	params map[string]any
}
type fakeLiveCDP struct {
	mu       sync.Mutex
	handler  func(map[string]any)
	commands chan liveCommand
	capture  string
	send     func(string, map[string]any) (any, error)
	detach   func() error
}

func (f *fakeLiveCDP) On(name string, handler any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = handler.(func(map[string]any))
}
func (f *fakeLiveCDP) Send(name string, params map[string]any) (any, error) {
	f.commands <- liveCommand{name, time.Now(), params}
	if f.send != nil {
		return f.send(name, params)
	}
	return f.reply(name)
}

func (f *fakeLiveCDP) reply(name string) (any, error) {
	switch name {
	case "Page.captureScreenshot":
		f.mu.Lock()
		defer f.mu.Unlock()
		return map[string]any{"data": f.capture}, nil
	case "Page.getLayoutMetrics":
		return map[string]any{"cssLayoutViewport": map[string]any{"clientWidth": float64(640), "clientHeight": float64(480)}}, nil
	}
	return nil, nil
}
func (f *fakeLiveCDP) Detach() error {
	f.commands <- liveCommand{name: "detach", at: time.Now()}
	if f.detach != nil {
		return f.detach()
	}
	return nil
}
func (f *fakeLiveCDP) frame(data []byte, id int) {
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	handler(map[string]any{"data": base64.StdEncoding.EncodeToString(data), "sessionId": id, "metadata": map[string]any{"deviceWidth": float64(640), "deviceHeight": float64(480)}})
}
func takeLive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("live callback timeout")
		var zero T
		return zero
	}
}
func liveCommandNamed(t *testing.T, f *fakeLiveCDP, name string) liveCommand {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case command := <-f.commands:
			if command.name == name {
				return command
			}
		case <-timer.C:
			t.Fatalf("live command %s timeout", name)
			return liveCommand{}
		}
	}
}

func fakeLiveEngine() (*Engine, *fakeLiveCDP, *liveSession, *livePage) {
	e := New(Config{})
	f := &fakeLiveCDP{commands: make(chan liveCommand, 100), capture: base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xd9})}
	p := &livePage{session: "s", tab: "t"}
	s := &liveSession{source: p, open: func() (liveCDP, error) { return f, nil }, watchers: map[uint64]func(LiveFrame){}}
	e.live.sessions = map[string]*liveSession{"s": s}
	return e, f, s, p
}

func TestLivePacerCacheWithholdingAndStop(t *testing.T) {
	e, f, s, p := fakeLiveEngine()
	frames := make(chan LiveFrame, 20)
	// Watch and stop must remain usable while an action owns opMu.
	e.opMu.Lock()
	defer e.opMu.Unlock()
	stop, err := e.WatchLive("s", func(v LiveFrame) { frames <- v })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if !e.LiveWatched("s") {
		t.Fatal("missing watcher")
	}
	liveCommandNamed(t, f, "Page.startScreencast")
	data := []byte{0xff, 0xd8, 0xff, 0xd9}
	before := time.Now()
	f.frame(data, 1)
	frame := takeLive(t, frames)
	data[0] = 0
	if frame.JPEG[0] != 0xff || frame.Width != 640 || frame.Height != 480 || frame.Tab != "t" || frame.Seq != 1 {
		t.Fatal("invalid frame")
	}
	ack := liveCommandNamed(t, f, "Page.screencastFrameAck")
	if ack.at.Sub(before) < 90*time.Millisecond || ack.params["sessionId"] != 1 {
		t.Fatal("early or wrong acknowledgement")
	}
	cached := make(chan LiveFrame, 2)
	lateStop, err := e.WatchLive("s", func(v LiveFrame) { cached <- v })
	if err != nil {
		t.Fatal(err)
	}
	if got := takeLive(t, cached); got.Seq != frame.Seq || got.JPEG[0] != 0xff {
		t.Fatal("late joiner cache")
	}
	lateStop()
	lateStop()
	p.withheld.Store(true)
	f.frame([]byte{1, 2, 3}, 2)
	liveCommandNamed(t, f, "Page.screencastFrameAck")
	select {
	case <-frames:
		t.Fatal("withheld frame delivered")
	default:
	}
	e.live.mu.Lock()
	cachedSeq := s.cached.Seq
	e.live.mu.Unlock()
	if cachedSeq != frame.Seq {
		t.Fatal("withheld frame replaced cache")
	}
	p.withheld.Store(false)
	f.frame(make([]byte, (1<<20)+1), 3)
	liveCommandNamed(t, f, "Page.screencastFrameAck")
	select {
	case <-frames:
		t.Fatal("oversized frame delivered")
	default:
	}
	f.frame([]byte{0xff, 0xd8}, 4)
	if next := takeLive(t, frames); next.Seq != 2 {
		t.Fatal("sequence did not resume")
	}
	liveCommandNamed(t, f, "Page.screencastFrameAck")
	stop()
	stop()
	if e.LiveWatched("s") {
		t.Fatal("watcher survived stop")
	}
	liveCommandNamed(t, f, "Page.stopScreencast")
	liveCommandNamed(t, f, "detach")
}

func TestLiveInitialFrameAndPanicIsolation(t *testing.T) {
	e, f, _, _ := fakeLiveEngine()
	stop, err := e.WatchLive("s", func(LiveFrame) { panic("callback failure") })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	frames := make(chan LiveFrame, 2)
	stop2, err := e.WatchLive("s", func(v LiveFrame) { frames <- v })
	if err != nil {
		t.Fatal(err)
	}
	defer stop2()
	frame := takeLive(t, frames)
	if frame.Width != 640 || frame.Height != 480 || len(frame.JPEG) != 4 {
		t.Fatal("invalid initial capture")
	}
	liveCommandNamed(t, f, "Page.getLayoutMetrics")
}

func TestLiveInitialFrameWithheld(t *testing.T) {
	e, f, _, p := fakeLiveEngine()
	p.withheld.Store(true)
	stop, err := e.WatchLive("s", func(LiveFrame) { t.Error("withheld initial capture") })
	if err != nil {
		t.Fatal(err)
	}
	liveCommandNamed(t, f, "Page.startScreencast")
	time.Sleep(600 * time.Millisecond)
	stop()
	for {
		command := takeLive(t, f.commands)
		if command.name == "Page.captureScreenshot" {
			t.Fatal("capture while withheld")
		}
		if command.name == "detach" {
			break
		}
	}
}

func TestLiveUnknownSessionAndSilentClosure(t *testing.T) {
	e, f, _, _ := fakeLiveEngine()
	if _, err := e.WatchLive("missing", nil); !errors.Is(err, ErrUnknownLiveSession) {
		t.Fatal(err)
	}
	stop, err := e.WatchLive("s", nil)
	if err != nil {
		t.Fatal(err)
	}
	liveCommandNamed(t, f, "Page.startScreencast")
	e.opMu.Lock()
	e.refreshLive()
	e.opMu.Unlock()
	liveCommandNamed(t, f, "detach")
	if e.LiveWatched("s") {
		t.Fatal("closed session still watched")
	}
	stop()
	stop()
	if _, err := e.WatchLive("s", nil); !errors.Is(err, ErrUnknownLiveSession) {
		t.Fatal(err)
	}
}

func TestLiveObserverNilAndReentry(t *testing.T) {
	e := New(Config{})
	// A nil observer must never inspect a page, even on completion.
	e.activity(&tab{}, "read", "done", "", nil)
	e.emitLive(LiveEvent{Type: "activity"})
	if len(e.live.events) != 0 || e.live.dispatching {
		t.Fatal("nil observer queued work")
	}
	events := make(chan LiveEvent, 3)
	e.SetLiveObserver(func(v LiveEvent) { e.SetLiveObserver(nil); events <- v })
	e.emitLive(LiveEvent{Type: "session_opened", Session: "s"})
	if got := takeLive(t, events); got.Type != "session_opened" {
		t.Fatal("event missing")
	}
	for _, state := range []capabilities.TaskState{capabilities.StateCompleted, capabilities.StateInputRequired} {
		if livePhase(capabilities.TaskResult{State: state}) != "done" {
			t.Fatal("blocked must be done")
		}
	}
	if livePhase(capabilities.TaskResult{State: capabilities.StateFailed}) != "failed" {
		t.Fatal("hard failure must fail")
	}
}

type liveURLPage struct {
	pw.Page
	raw string
}

func (p liveURLPage) URL() string { return p.raw }

func TestLiveDoneMetadataAndImmutableCache(t *testing.T) {
	e := New(Config{})
	e.secrets = []string{"sentinel"}
	events := make(chan LiveEvent, 2)
	e.SetLiveObserver(func(v LiveEvent) { events <- v })
	e.activity(&tab{session: "s", id: "t", title: "sentinel" + strings.Repeat("界", 220), page: liveURLPage{raw: "https://user:pass@example.test/sentinel?hidden#hidden"}}, "read", "done", "sentinel", nil)
	event := takeLive(t, events)
	if event.URL != "https://example.test/[secret]" || event.Target != "[secret]" || utf8.RuneCountInString(event.Title) != 200 || strings.Contains(event.Title, "sentinel") {
		t.Fatal("unsafe or unbounded completion metadata")
	}
	stream := &liveStream{}
	s := &liveSession{stream: stream}
	p := &livePage{tab: "t"}
	jpeg := []byte{0xff, 0xd8}
	e.deliverLive(s, p, stream, jpeg, 640, 480)
	jpeg[0] = 0
	if s.cached.JPEG[0] != 0xff {
		t.Fatal("cache shares mutable capture bytes")
	}
}

func TestLiveFallbackAndScreencastShareFrameBudget(t *testing.T) {
	e, f, _, _ := fakeLiveEngine()
	frames := make(chan LiveFrame, 2)
	stop, err := e.WatchLive("s", func(v LiveFrame) { frames <- v })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	first := takeLive(t, frames)
	f.frame([]byte{0xff, 0xd8}, 1)
	second := takeLive(t, frames)
	if second.At.Sub(first.At) < 90*time.Millisecond {
		t.Fatal("fallback and screencast frames burst above frame budget")
	}
}

func fastLiveEngine() (*Engine, *fakeLiveCDP, *liveSession, *livePage) {
	e, f, s, p := fakeLiveEngine()
	e.live.timing = liveTiming{call: 120 * time.Millisecond, initial: 20 * time.Millisecond,
		watchdog: 50 * time.Millisecond, stale: 200 * time.Millisecond,
		backoff: 20 * time.Millisecond, maxBackoff: 80 * time.Millisecond, frame: 2 * time.Millisecond}
	return e, f, s, p
}

func watchFakeLive(t *testing.T, e *Engine, s *liveSession) (chan LiveFrame, func(), *liveStream) {
	t.Helper()
	frames := make(chan LiveFrame, 100)
	stop, err := e.WatchLive("s", func(v LiveFrame) { frames <- v })
	if err != nil {
		t.Fatal(err)
	}
	e.live.mu.Lock()
	stream := s.stream
	e.live.mu.Unlock()
	t.Cleanup(func() {
		stop()
		awaitLiveFinished(t, stream, time.Second)
	})
	return frames, stop, stream
}

func awaitLiveFinished(t *testing.T, stream *liveStream, timeout time.Duration) {
	t.Helper()
	select {
	case <-stream.finished:
	case <-time.After(timeout):
		t.Fatal("stream did not finish within bounded shutdown")
	}
}

func noLiveFrame(t *testing.T, frames <-chan LiveFrame, duration time.Duration) {
	t.Helper()
	select {
	case <-frames:
		t.Fatal("unexpected live frame")
	case <-time.After(duration):
	}
}

func TestLiveOpenRetriesAndBackoff(t *testing.T) {
	for _, failures := range []int{2, 4} {
		t.Run(string(rune('0'+failures)), func(t *testing.T) {
			e, f, s, _ := fastLiveEngine()
			opened := make(chan time.Time, 10)
			var attempts atomic.Int32
			s.open = func() (liveCDP, error) {
				opened <- time.Now()
				if attempts.Add(1) <= int32(failures) {
					return nil, errors.New("fixture failure")
				}
				return f, nil
			}
			// Nothing opens until the first watcher, even with the observer nil.
			time.Sleep(10 * time.Millisecond)
			if attempts.Load() != 0 {
				t.Fatal("protocol work without a watcher")
			}
			frames, _, stream := watchFakeLive(t, e, s)
			_ = takeLive(t, frames)
			previous := takeLive(t, opened)
			want := e.live.timing.backoff
			for range failures {
				next := takeLive(t, opened)
				if next.Sub(previous) < want {
					t.Fatal("retry ignored exponential backoff")
				}
				previous, want = next, min(want*2, e.live.timing.maxBackoff)
			}
			e.live.mu.Lock()
			registered := s.stream == stream
			e.live.mu.Unlock()
			if !registered || attempts.Load() != int32(failures+1) {
				t.Fatal("retry abandoned registered stream")
			}
		})
	}
}

func TestLiveStartRetriesAndIndependentFallback(t *testing.T) {
	for _, hangs := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "hang"}[hangs], func(t *testing.T) {
			e, f, s, _ := fastLiveEngine()
			var starts atomic.Int32
			release := make(chan struct{})
			exited := make(chan struct{})
			// Release uncancellable fake Send workers after checking controller exit.
			t.Cleanup(func() { close(release) })
			f.send = func(name string, _ map[string]any) (any, error) {
				if name == "Page.startScreencast" && starts.Add(1) == 1 {
					if hangs {
						<-release
						close(exited)
					}
					return nil, errors.New("fixture start failure")
				}
				return f.reply(name)
			}
			began := time.Now()
			frames, _, _ := watchFakeLive(t, e, s)
			_ = takeLive(t, frames)
			if hangs && time.Since(began) >= e.live.timing.call {
				t.Fatal("fallback waited for start deadline")
			}
			first := liveCommandNamed(t, f, "Page.startScreencast")
			second := liveCommandNamed(t, f, "Page.startScreencast")
			minimum := e.live.timing.backoff
			if hangs {
				minimum += e.live.timing.call
			}
			if second.at.Sub(first.at) < minimum {
				t.Fatal("start retry preceded deadline or backoff")
			}
			f.frame([]byte("recovered screencast"), 1)
			_ = takeLive(t, frames)
			liveCommandNamed(t, f, "Page.screencastFrameAck")
			select {
			case <-exited:
				t.Fatal("hung fake unexpectedly completed")
			default:
			}
		})
	}
}

func TestLiveAckRetries(t *testing.T) {
	for _, hangs := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "hang"}[hangs], func(t *testing.T) {
			e, f, s, _ := fastLiveEngine()
			e.live.timing.initial = time.Second
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			var acks atomic.Int32
			f.send = func(name string, _ map[string]any) (any, error) {
				if name == "Page.screencastFrameAck" && acks.Add(1) == 1 {
					if hangs {
						<-release
					}
					return nil, errors.New("fixture ack failure")
				}
				return f.reply(name)
			}
			frames, _, _ := watchFakeLive(t, e, s)
			liveCommandNamed(t, f, "Page.startScreencast")
			f.frame([]byte("first"), 1)
			_ = takeLive(t, frames)
			liveCommandNamed(t, f, "Page.screencastFrameAck")
			liveCommandNamed(t, f, "detach")
			liveCommandNamed(t, f, "Page.startScreencast")
			liveDrainFakeFrames(frames)
			f.frame([]byte("resumed"), 2)
			for frame := takeLive(t, frames); string(frame.JPEG) != "resumed"; frame = takeLive(t, frames) {
			}
			liveCommandNamed(t, f, "Page.screencastFrameAck")
		})
	}
}

func liveDrainFakeFrames(frames <-chan LiveFrame) {
	for {
		select {
		case <-frames:
		default:
			return
		}
	}
}

func TestLiveWatchdogChangedIdenticalAndStatic(t *testing.T) {
	e, f, s, _ := fastLiveEngine()
	var opens atomic.Int32
	s.open = func() (liveCDP, error) { opens.Add(1); return f, nil }
	frames, _, _ := watchFakeLive(t, e, s)
	_ = takeLive(t, frames)
	f.mu.Lock()
	f.capture = base64.StdEncoding.EncodeToString([]byte("changed screenshot"))
	f.mu.Unlock()
	changed := takeLive(t, frames)
	if string(changed.JPEG) != "changed screenshot" {
		t.Fatal("watchdog did not capture changed page")
	}
	noLiveFrame(t, frames, 2*e.live.timing.stale)
	if opens.Load() != 1 {
		t.Fatal("idle unchanged page repeatedly restarted")
	}
	captures := 0
	for len(f.commands) > 0 {
		if command := <-f.commands; command.name == "Page.captureScreenshot" {
			captures++
		}
	}
	if captures < 3 || captures > 13 {
		t.Fatalf("unexpected watchdog screenshot budget: %d", captures)
	}
}

func TestLiveWatchdogNavigationRestartsSilentCDP(t *testing.T) {
	e, f, s, p := fastLiveEngine()
	p.location.Store("about:blank")
	opened := make(chan struct{}, 10)
	s.open = func() (liveCDP, error) { opened <- struct{}{}; return f, nil }
	frames, _, _ := watchFakeLive(t, e, s)
	liveCommandNamed(t, f, "Page.startScreencast")
	_ = takeLive(t, frames)
	_ = takeLive(t, opened)
	p.location.Store("http://fixture.invalid/next")
	_ = takeLive(t, opened)
	liveCommandNamed(t, f, "detach")
	liveCommandNamed(t, f, "Page.startScreencast")
	f.frame([]byte("new page"), 1)
	if got := takeLive(t, frames); string(got.JPEG) != "new page" {
		t.Fatal("navigation restart did not resume frames")
	}
}

func TestLiveScreenshotFailureRestarts(t *testing.T) {
	for _, mode := range []string{"error", "hang", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			e, f, s, _ := fastLiveEngine()
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			var captures atomic.Int32
			f.send = func(name string, _ map[string]any) (any, error) {
				if name == "Page.captureScreenshot" && captures.Add(1) == 1 {
					switch mode {
					case "hang":
						<-release
					case "invalid":
						return map[string]any{"data": "invalid base64"}, nil
					}
					return nil, errors.New("fixture capture failure")
				}
				return f.reply(name)
			}
			frames, _, _ := watchFakeLive(t, e, s)
			_ = takeLive(t, frames)
			liveCommandNamed(t, f, "detach")
			if captures.Load() < 2 {
				t.Fatal("screenshot failure did not retry")
			}
		})
	}
}

func TestLiveHungMetricsUsesViewport(t *testing.T) {
	e, f, s, p := fastLiveEngine()
	p.viewport.Store(uint64(320)<<32 | 240)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.send = func(name string, _ map[string]any) (any, error) {
		if name == "Page.getLayoutMetrics" {
			<-release
			return nil, nil
		}
		return f.reply(name)
	}
	frames, _, _ := watchFakeLive(t, e, s)
	if frame := takeLive(t, frames); frame.Width != 320 || frame.Height != 240 {
		t.Fatal("hung metrics prevented bounded viewport fallback")
	}
}

func TestLiveHungCallsStopAndRemoval(t *testing.T) {
	for _, method := range []string{"open", "Page.startScreencast", "Page.screencastFrameAck", "Page.captureScreenshot", "Page.getLayoutMetrics", "Page.stopScreencast", "detach"} {
		for _, remove := range []bool{false, true} {
			name := method + map[bool]string{false: "/stop", true: "/remove"}[remove]
			t.Run(name, func(t *testing.T) {
				e, f, s, _ := fastLiveEngine()
				entered, release, exited := make(chan struct{}, 1), make(chan struct{}), make(chan struct{}, 1)
				t.Cleanup(func() {
					close(release)
					_ = takeLive(t, exited)
				})
				hang := func() {
					entered <- struct{}{}
					<-release
					exited <- struct{}{}
				}
				if method == "open" {
					s.open = func() (liveCDP, error) { hang(); return f, nil }
				} else if method == "detach" {
					f.detach = func() error { hang(); return nil }
				} else {
					f.send = func(name string, _ map[string]any) (any, error) {
						if name == method {
							hang()
						}
						return f.reply(name)
					}
				}
				_, stop, stream := watchFakeLive(t, e, s)
				if method == "Page.screencastFrameAck" {
					liveCommandNamed(t, f, "Page.startScreencast")
					f.frame([]byte("first"), 1)
				}
				// Cleanup calls only begin after stopping.
				cleanup := method == "Page.stopScreencast" || method == "detach"
				if cleanup {
					liveCommandNamed(t, f, "Page.startScreencast")
				}
				if !cleanup {
					_ = takeLive(t, entered)
				}
				before := time.Now()
				if remove {
					e.opMu.Lock()
					e.refreshLive()
					e.opMu.Unlock()
				} else {
					stop()
				}
				if time.Since(before) >= e.live.timing.call/2 {
					t.Fatal("stop or removal waited for protocol call")
				}
				if cleanup {
					_ = takeLive(t, entered)
				}
				awaitLiveFinished(t, stream, 3*e.live.timing.call)
			})
		}
	}
}

func TestLiveWatchdogWithheld(t *testing.T) {
	e, f, s, p := fastLiveEngine()
	p.withheld.Store(true)
	frames, stop, stream := watchFakeLive(t, e, s)
	liveCommandNamed(t, f, "Page.startScreencast")
	f.frame([]byte("withheld"), 1)
	liveCommandNamed(t, f, "Page.screencastFrameAck")
	noLiveFrame(t, frames, 3*e.live.timing.watchdog)
	stop()
	awaitLiveFinished(t, stream, time.Second)
	for len(f.commands) > 0 {
		if command := <-f.commands; command.name == "Page.captureScreenshot" {
			t.Fatal("withheld watchdog captured a screenshot")
		}
	}
}

type liveLogBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *liveLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *liveLogBuffer) snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.String()
}

func TestLiveRetryLogsExcludePageAndErrorData(t *testing.T) {
	var logs liveLogBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	e, f, s, p := fastLiveEngine()
	p.location.Store("https://sentinel.invalid/private-page")
	var opens atomic.Int32
	s.open = func() (liveCDP, error) {
		if opens.Add(1) <= 2 {
			return nil, errors.New("RAW-ERROR https://sentinel.invalid/private-error")
		}
		return f, nil
	}
	frames, stop, stream := watchFakeLive(t, e, s)
	_ = takeLive(t, frames)
	stop()
	awaitLiveFinished(t, stream, time.Second)
	got := logs.snapshot()
	if strings.Contains(got, "sentinel") || strings.Contains(got, "RAW-ERROR") || strings.Contains(got, "https:") {
		t.Fatal("private page or raw error reached log")
	}
	if strings.Count(got, `"level":"WARN"`) != 2 || strings.Count(got, `"level":"INFO"`) != 1 ||
		strings.Count(got, `"class":"open"`) != 2 || !strings.Contains(got, `"session":"s","tab":"t"`) {
		t.Fatal("missing sanitized retry or single recovery log")
	}
}

func TestLiveLateScreenshotIsDiscarded(t *testing.T) {
	e, old, s, _ := fastLiveEngine()
	_, current, _, _ := fakeLiveEngine()
	current.capture = base64.StdEncoding.EncodeToString([]byte("current page"))
	release, exited := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); _ = takeLive(t, exited) })
	old.send = func(name string, _ map[string]any) (any, error) {
		if name == "Page.captureScreenshot" {
			<-release
			defer close(exited)
			return map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("expired page"))}, nil
		}
		return old.reply(name)
	}
	var opens atomic.Int32
	s.open = func() (liveCDP, error) {
		if opens.Add(1) == 1 {
			return old, nil
		}
		return current, nil
	}
	frames, _, _ := watchFakeLive(t, e, s)
	if got := takeLive(t, frames); string(got.JPEG) != "current page" {
		t.Fatal("expired screenshot replaced current frame")
	}
	releaseOnce.Do(func() { close(release) })
	_ = takeLive(t, exited)
	// Retired listeners also cannot deliver packets into the current stream.
	old.frame([]byte("retired screencast"), 7)
	noLiveFrame(t, frames, 2*e.live.timing.watchdog)
}

func TestLiveEngineCloseDuringHungCall(t *testing.T) {
	e, f, s, _ := fastLiveEngine()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.send = func(name string, _ map[string]any) (any, error) {
		if name == "Page.startScreencast" {
			<-release
		}
		return f.reply(name)
	}
	_, _, stream := watchFakeLive(t, e, s)
	liveCommandNamed(t, f, "Page.startScreencast")
	before := time.Now()
	if err := e.Close(); err != nil {
		t.Fatal("engine close failed")
	}
	if time.Since(before) >= e.live.timing.call/2 {
		t.Fatal("engine close waited for hung live call")
	}
	awaitLiveFinished(t, stream, 3*e.live.timing.call)
}

func TestLiveRecoveryLogsWithIdenticalCachedScreenshot(t *testing.T) {
	var logs liveLogBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	e, f, s, _ := fastLiveEngine()
	cached, _ := base64.StdEncoding.DecodeString(f.capture)
	s.cached = &LiveFrame{JPEG: cached, At: time.Now()}
	var starts atomic.Int32
	f.send = func(name string, _ map[string]any) (any, error) {
		if name == "Page.startScreencast" && starts.Add(1) == 1 {
			return nil, errors.New("fixture failure")
		}
		return f.reply(name)
	}
	frames, stop, stream := watchFakeLive(t, e, s)
	_ = takeLive(t, frames) // The existing safe cache is replayed once.
	liveCommandNamed(t, f, "Page.startScreencast")
	liveCommandNamed(t, f, "Page.startScreencast")
	noLiveFrame(t, frames, 3*e.live.timing.watchdog)
	stop()
	awaitLiveFinished(t, stream, time.Second)
	if got := logs.snapshot(); strings.Count(got, `"level":"WARN"`) != 1 || strings.Count(got, `"level":"INFO"`) != 1 {
		t.Fatal("identical valid screenshot did not record a single recovery")
	}
}
