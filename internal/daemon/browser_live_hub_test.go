package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

type liveEngineFake struct {
	observer func(browser.LiveEvent)
	frame    func(browser.LiveFrame)
	stops    int
	err      error
}

func (f *liveEngineFake) SetLiveObserver(fn func(browser.LiveEvent)) { f.observer = fn }
func (f *liveEngineFake) WatchLive(_ string, fn func(browser.LiveFrame)) (func(), error) {
	if f.err != nil {
		return nil, f.err
	}
	f.frame = fn
	return func() { f.stops++ }, nil
}

func liveHubTestBackend(t *testing.T) (*browserLiveHub, *liveEngineFake, *DaemonBackend, *telemetry.Bus) {
	t.Helper()
	engine := &liveEngineFake{}
	bus := telemetry.NewBus(nil)
	hub := newBrowserLiveHub(engine, bus)
	engine.SetLiveObserver(hub.observe)
	backend := NewDaemonBackend(nil, nil, bus)
	backend.AttachServices(BackendServices{BrowserLive: hub})
	t.Cleanup(hub.close)
	return hub, engine, backend, bus
}

func liveTestActivity(session string) browser.LiveEvent {
	return browser.LiveEvent{Type: "activity", Session: session, Tab: "tab", Kind: api.BrowserKindNavigate, Phase: api.BrowserPhaseStart, URL: "http://local.test/page", Title: "Local page"}
}

func TestBrowserLiveBindingActivityAndEnd(t *testing.T) {
	h, e, b, bus := liveHubTestBackend(t)
	now := time.Unix(100, 0)
	h.now = func() time.Time { return now }
	e.observer(browser.LiveEvent{Type: "session_opened", Session: "s"})
	e.observer(liveTestActivity("unbound"))
	h.bind("s", credentialOrigin{})
	e.observer(liveTestActivity("s"))
	if bus.Len() != 0 {
		t.Fatal("unbound session published")
	}
	h.bind("s", credentialOrigin{conversation: "c", run: "r"})
	e.observer(browser.LiveEvent{Type: "session_opened", Session: "s"})
	e.observer(browser.LiveEvent{Type: "activity", Session: "s", Kind: api.BrowserKindPage})
	if bus.Len() != 0 || h.state("c").Session != nil {
		t.Fatal("open or blank tab became visible")
	}
	ev := liveTestActivity("s")
	ev.Box = &browser.LiveRect{X: 1, Y: 2, Width: 3, Height: 4}
	e.observer(ev)
	ev.Box.X = 999
	first := h.state("c")
	if first.Session == nil || first.Session.State != api.BrowserSessionActive || first.Session.URL != ev.URL || first.Activity.Seq != 1 || first.Activity.Box.X != 1 {
		t.Fatalf("bad initial state: %+v", first)
	}
	// Returned state must not let the caller mutate the stored activity.
	first.Activity.Box.X = 77
	if h.state("c").Activity.Box.X != 1 {
		t.Fatal("state aliased activity")
	}
	h.bind("s", credentialOrigin{conversation: "other", run: "other"})
	e.observer(browser.LiveEvent{Type: "activity", Session: "s", Kind: api.BrowserKindRead, Phase: api.BrowserPhaseDone, Title: "Read page"})
	state := h.state("c")
	if state.Session.URL != ev.URL || state.Session.Title != "Read page" || state.Activity.Seq != 2 || h.state("other").Session != nil {
		t.Fatal("replayed open reset or rebound session")
	}
	for _, conversation := range []string{"other", "../c", ""} {
		_, _, err := b.WatchBrowser(conversation, "s", func(gateway.BrowserFrame) {})
		if apiErr, ok := err.(*api.Error); !ok || apiErr.Code != api.CodeUnknownTarget {
			t.Fatal("watch crossed conversation scope")
		}
	}
	stop, done, err := b.WatchBrowser("c", "s", func(gateway.BrowserFrame) {})
	if err != nil || h.sessions["s"].viewers != 1 {
		t.Fatal("watch failed")
	}
	e.observer(browser.LiveEvent{Type: "session_closed", Session: "s"})
	select {
	case <-done:
	default:
		t.Fatal("end did not close viewers")
	}
	stop()
	stop()
	if e.stops != 1 || h.sessions["s"].viewers != 0 {
		t.Fatal("stop was not idempotent")
	}
	e.observer(liveTestActivity("s"))
	e.observer(browser.LiveEvent{Type: "session_closed", Session: "s"})
	events := bus.Replay(types.Cursor{})
	if len(events) != 4 || events[0].Type != api.EventBrowserSession || events[1].Type != api.EventBrowserActivity || events[2].Type != api.EventBrowserActivity || events[3].Type != api.EventBrowserSession {
		t.Fatalf("wrong event sequence: %v", events)
	}
	for _, event := range events {
		if event.RunID == nil || *event.RunID != "r" {
			t.Fatal("missing run scope")
		}
	}
	if h.state("c").Session.State != api.BrowserSessionEnded {
		t.Fatal("ended state missing")
	}
	now = now.Add(59 * time.Second)
	if h.state("c").Session == nil {
		t.Fatal("ended state expired early")
	}
	now = now.Add(time.Second)
	if h.state("c").Session != nil || len(h.sessions) != 0 {
		t.Fatal("ended state did not expire")
	}
}

func TestBrowserLiveLastSessionAndShutdown(t *testing.T) {
	h, _, b, bus := liveHubTestBackend(t)
	for _, id := range []string{"first", "second"} {
		h.bind(id, credentialOrigin{conversation: "c", run: id})
		h.observe(liveTestActivity(id))
	}
	h.observe(browser.LiveEvent{Type: "session_closed", Session: "first"})
	if h.state("c").Session.SessionID != "second" {
		t.Fatal("older session replaced newer session")
	}
	stop, done, err := b.WatchBrowser("c", "second", func(gateway.BrowserFrame) {})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	before := bus.Len()
	h.close()
	h.close()
	h.observe(liveTestActivity("second"))
	if bus.Len() != before {
		t.Fatal("shutdown published events")
	}
	select {
	case <-done:
	default:
		t.Fatal("shutdown left viewer open")
	}
}

func TestBrowserLiveStateStrictRPC(t *testing.T) {
	h, _, b, _ := liveHubTestBackend(t)
	for _, raw := range []string{`{}`, `null`, `[]`, `{"conversation_id":"../c"}`, `{"conversation_id":"c","extra":1}`, `{"conversation_id":"c"} {}`} {
		_, err := b.Passthrough(context.Background(), api.MethodBrowserLiveState, json.RawMessage(raw))
		if apiErr, ok := err.(*api.Error); !ok || apiErr.Code != api.CodeInvalidParams {
			t.Fatalf("accepted invalid params %s", raw)
		}
	}
	read := func(backend *DaemonBackend) api.BrowserLiveStateResult {
		t.Helper()
		v, err := backend.Passthrough(context.Background(), api.MethodBrowserLiveState, json.RawMessage(`{"conversation_id":"c"}`))
		if err != nil {
			t.Fatal(err)
		}
		return v.(api.BrowserLiveStateResult)
	}
	if read(NewDaemonBackend(nil, nil, nil)).Session != nil || read(b).Session != nil {
		t.Fatal("empty state not empty")
	}
	h.bind("s", credentialOrigin{conversation: "c", run: "r"})
	h.observe(liveTestActivity("s"))
	if read(b).Session.SessionID != "s" {
		t.Fatal("RPC did not restore state")
	}
}

type liveOpenToolFake struct {
	tools.Tool
	out      tools.Result
	err      error
	onInvoke func()
}

func (t *liveOpenToolFake) Definition() tools.Definition {
	return tools.Definition{Name: "browser_open_session", Description: "Open", InputSchema: json.RawMessage(`{}`)}
}
func (t *liveOpenToolFake) Invoke(context.Context, tools.Invocation) (tools.Result, error) {
	if t.onInvoke != nil {
		t.onInvoke()
	}
	return t.out, t.err
}

func TestBrowserLiveWrapperPreservesResultAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		status       tools.ResultStatus
		err          error
		origin       bool
		bound        bool
	}{
		{"success", `{"session_id":"s", "extra":true}`, tools.StatusOK, nil, true, true},
		{"missing origin", `{"session_id":"s"}`, tools.StatusOK, nil, false, false},
		{"failure", `{"session_id":"s"}`, tools.StatusToolError, nil, true, false},
		{"error", `{"session_id":"s"}`, tools.StatusOK, errors.New("failed"), true, false},
		{"invalid output", `invalid`, tools.StatusOK, nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, e, _, bus := liveHubTestBackend(t)
			original := &liveOpenToolFake{out: tools.Result{Status: tc.status, Output: tc.output, Truncated: true, BlobRef: "blob", Sessions: []tools.SessionReceipt{{ID: "receipt"}}}, err: tc.err, onInvoke: func() { e.observer(browser.LiveEvent{Type: "session_opened", Session: "s"}) }}
			wrapper := &liveBrowserTool{Tool: original, hub: h}
			ctx := context.Background()
			if tc.origin {
				ctx = context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{conversation: "c", run: "r"})
			}
			out, err := wrapper.Invoke(ctx, tools.Invocation{Tool: "browser_open_session"})
			if !reflect.DeepEqual(out, original.out) || err != tc.err || !reflect.DeepEqual(wrapper.Definition(), original.Definition()) {
				t.Fatal("wrapper changed output, error, or definition")
			}
			if (h.sessions["s"] != nil) != tc.bound || bus.Len() != 0 {
				t.Fatal("wrong binding or open published")
			}
		})
	}
}

func TestBrowserLiveDaemonEngineWiring(t *testing.T) {
	h, e, _, bus := liveHubTestBackend(t)
	_, dt, err := buildEngine(engineWiring{Caps: capabilities.DefaultRegistry(), BrowserLive: h})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range dt.Registry.Names() {
		if len(name) > 8 && name[:8] == "browser_" {
			tool, _ := dt.Registry.Get(name)
			if _, ok := tool.(*liveBrowserTool); !ok {
				t.Fatalf("%s is not wrapped", name)
			}
		}
	}
	ctx := context.WithValue(context.Background(), credentialOriginKey{}, credentialOrigin{conversation: "daemon-conversation", run: "daemon-run"})
	out, err := dt.Exec.Execute(ctx, "browser_open_session", json.RawMessage(`{}`), tools.ExecuteOptions{IdempotencyKey: "live-open"})
	var opened struct {
		Session string `json:"session_id"`
	}
	if err != nil || out.Status != tools.StatusOK || json.Unmarshal([]byte(out.Output), &opened) != nil || opened.Session == "" {
		t.Fatal("daemon open failed")
	}
	e.observer(liveTestActivity(opened.Session))
	if h.state("daemon-conversation").Session.RunID != "daemon-run" || bus.Len() != 2 {
		t.Fatal("daemon tool did not bind origin")
	}
}

func TestBrowserLiveAssemblyWithoutLaunchingBrowser(t *testing.T) {
	s, err := Assemble(AssembleOptions{BrowserWeb: &wireengine.BrowserWebOptions{Config: config.Default(), DataDir: t.TempDir(), Available: func() error { return nil }, Warn: func(string) {}}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.BrowserWeb.Close()
	defer s.CloseBrowserLive()
	if s.BrowserLive == nil || s.engineOptions.BrowserLive != s.BrowserLive {
		t.Fatal("assembly did not wire live hub")
	}
}

func TestBrowserLiveFramesNeverEnterTelemetry(t *testing.T) {
	dir := t.TempDir()
	sink, err := telemetry.OpenSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	h, e, b, _ := liveHubTestBackend(t)
	h.bus = telemetry.NewBus(sink)
	live := h.bus.Subscribe(8)
	h.bind("s", credentialOrigin{conversation: "c", run: "r"})
	h.observe(liveTestActivity("s"))
	<-live
	<-live
	var got gateway.BrowserFrame
	stop, _, err := b.WatchBrowser("c", "s", func(f gateway.BrowserFrame) { got = f })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	frame := browser.LiveFrame{Seq: 9, Tab: "tab", JPEG: []byte("frame-only-marker"), Width: 100, Height: 80, At: time.UnixMilli(1234)}
	e.frame(frame)
	if got.Seq != 9 || got.AtMillis != 1234 || !bytes.Equal(got.JPEG, frame.JPEG) {
		t.Fatal("frame adaptation failed")
	}
	select {
	case <-live:
		t.Fatal("frame appeared on bus")
	default:
	}
	if len(h.bus.Replay(types.Cursor{})) != 2 {
		t.Fatal("frame appeared in replay")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil || bytes.Contains(raw, frame.JPEG) || bytes.Count(raw, []byte("\n")) != 2 {
		t.Fatal("frame appeared in sink")
	}
	e.err = browser.ErrUnknownLiveSession
	if _, _, err := b.WatchBrowser("c", "s", func(gateway.BrowserFrame) {}); err == nil {
		t.Fatal("engine error ignored")
	}
}

func TestBrowserLiveConcurrentActivityOrder(t *testing.T) {
	h, _, _, bus := liveHubTestBackend(t)
	h.bind("s", credentialOrigin{conversation: "c", run: "r"})
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		safego.Go(fmt.Sprintf("live-observe-%d", i), nil, func() { defer wg.Done(); h.observe(liveTestActivity("s")) })
	}
	wg.Wait()
	var seq uint64
	for _, ev := range bus.Replay(types.Cursor{}) {
		if ev.Type == api.EventBrowserActivity {
			var a api.BrowserActivityEvent
			if json.Unmarshal(ev.Payload, &a) != nil || a.Seq != seq+1 {
				t.Fatal("activity published out of order")
			}
			seq = a.Seq
		}
	}
	if seq != 40 {
		t.Fatal("missing activities")
	}
}

func TestBrowserLiveInvisibleSessionEnd(t *testing.T) {
	h, _, _, bus := liveHubTestBackend(t)
	h.bind("blank", credentialOrigin{conversation: "c", run: "r"})
	h.observe(browser.LiveEvent{Type: "session_closed", Session: "blank"})
	if bus.Len() != 0 || h.state("c").Session != nil {
		t.Fatal("a session that never showed anything must stay invisible when it ends")
	}
}

func TestBrowserLiveDaemonShutdownClosesViewers(t *testing.T) {
	h, _, b, bus := liveHubTestBackend(t)
	h.bind("s", credentialOrigin{conversation: "c", run: "r"})
	h.observe(liveTestActivity("s"))
	stop, done, err := b.WatchBrowser("c", "s", func(gateway.BrowserFrame) {})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	services, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	services.BrowserLive = h
	d := New(DefaultConfig(), Deps{Services: services})
	before := bus.Len()
	if err := d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("daemon shutdown did not terminate browser viewers")
	}
	if bus.Len() != before {
		t.Fatal("daemon shutdown published browser events")
	}
}
