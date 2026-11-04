//go:build browser && linux

package browser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

func liveFixture(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/download" {
		w.Header().Set("Content-Disposition", `attachment; filename="live-fixture.txt"`)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "local download")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><title>Live fixture</title><style>
 body { min-height:900px; background:white } .moving { width:40px;height:40px;background:blue;animation:move .4s infinite alternate }
 @keyframes move { to { transform:translateX(200px) } }
 </style><div class="moving"></div><label>Username<input id="username" autocomplete="username"></label>
 <label>Password<input id="password" type="password"></label><label>Visible<input id="visible"></label>
 <label>Long<input id="long"></label><label>Editable<div id="editable" aria-label="Editable" role="textbox" contenteditable="true"></div></label>
 <button id="click">Click</button><a href="/next">Next</a><a href="/download" download>Download</a>
 <script>window.inputs=[];document.addEventListener('input',e=>inputs.push(e.target.id));</script>`)
}
func livePageEval(t *testing.T, h *browserHarness, script string) any {
	t.Helper()
	h.e.opMu.Lock()
	defer h.e.opMu.Unlock()
	value, err := h.e.sessions[h.args["session_id"].(string)].tabs[h.args["tab_id"].(string)].page.Evaluate(script)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func liveSnapshot(t *testing.T, h *browserHarness) map[string]any {
	t.Helper()
	r := h.call("wait_ready", nil)
	if r.State != capabilities.StateCompleted && r.State != capabilities.StateInputRequired {
		t.Fatal("readiness failed")
	}
	return success(t, h.call("snapshot", nil))
}
func liveAct(t *testing.T, h *browserHarness, name, value string) capabilities.TaskResult {
	t.Helper()
	snap := liveSnapshot(t, h)
	for _, item := range snap["interactive"].([]any) {
		el := item.(map[string]any)
		if el["name"] == name {
			return h.call("act", map[string]any{"snapshot_id": snap["snapshot_id"], "ref": el["ref"], "action": "type", "value": value})
		}
	}
	t.Fatalf("target %q missing", name)
	return capabilities.TaskResult{}
}
func liveActionOK(t *testing.T, r capabilities.TaskResult) {
	t.Helper()
	if r.State != capabilities.StateCompleted && r.State != capabilities.StateInputRequired {
		t.Fatal("live action failed")
	}
}
func liveFrameFor(t *testing.T, ch <-chan LiveFrame, tab string) LiveFrame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case frame := <-ch:
			if tab == "" || frame.Tab == tab {
				return frame
			}
		case <-deadline:
			t.Fatal("no live frame for active tab")
			return LiveFrame{}
		}
	}
}
func liveWatchFrames(t *testing.T, e *Engine, session string) (chan LiveFrame, func()) {
	t.Helper()
	frames := make(chan LiveFrame, 100)
	stop, err := e.WatchLive(session, func(frame LiveFrame) {
		select {
		case frames <- frame:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return frames, stop
}
func liveDrain(ch <-chan LiveFrame) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func TestLiveWatchLifecycle(t *testing.T) {
	requireBrowser(t)
	server := httptest.NewServer(http.HandlerFunc(liveFixture))
	defer server.Close()
	e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile")})
	defer e.Close()
	events := make(chan LiveEvent, 200)
	e.SetLiveObserver(func(v LiveEvent) { events <- v })
	p := success(t, invoke(t, e, "open_session", map[string]any{}))
	session := p["session_id"].(string)
	if e.LiveWatched(session) {
		t.Fatal("capture before watch")
	}
	frames, stop := liveWatchFrames(t, e, session)
	// An empty session is a valid, idle watch target.
	select {
	case <-frames:
		t.Fatal("frame without page")
	case <-time.After(100 * time.Millisecond):
	}
	p = success(t, invoke(t, e, "tabs", map[string]any{"session_id": session, "action": "new"}))
	tab := p["tab_id"].(string)
	h := &browserHarness{t, e, map[string]any{"session_id": session, "tab_id": tab}}
	liveActionOK(t, h.call("navigate", map[string]any{"url": server.URL + "/?private=query#fragment"}))
	snap := liveSnapshot(t, h)
	for _, item := range snap["interactive"].([]any) {
		el := item.(map[string]any)
		if el["name"] == "Download" {
			success(t, h.call("download", map[string]any{"snapshot_id": snap["snapshot_id"], "ref": el["ref"]}))
		}
	}
	first := liveFrameFor(t, frames, tab)
	if !bytes.HasPrefix(first.JPEG, []byte{0xff, 0xd8}) || first.Width <= 0 || first.Height <= 0 {
		t.Fatal("invalid JPEG or dimensions")
	}
	liveDrain(frames)
	start := time.Now()
	count := 0
	timer := time.NewTimer(1400 * time.Millisecond)
counting:
	for {
		select {
		case <-frames:
			count++
		case <-timer.C:
			break counting
		}
	}
	if count < 3 || count > 16 {
		t.Fatalf("unexpected frame rate: %d frames in %s", count, time.Since(start))
	}
	stop()
	stop()
	if e.LiveWatched(session) {
		t.Fatal("last watcher did not stop")
	}
	time.Sleep(150 * time.Millisecond)
	liveDrain(frames)
	select {
	case <-frames:
		t.Fatal("frame after stop")
	case <-time.After(150 * time.Millisecond):
	}
	frames, stop = liveWatchFrames(t, e, session)
	_ = liveFrameFor(t, frames, tab) // late cached frame
	p = success(t, invoke(t, e, "tabs", map[string]any{"session_id": session, "action": "new", "url": server.URL + "/second"}))
	second := p["tab_id"].(string)
	_ = liveFrameFor(t, frames, second)
	success(t, invoke(t, e, "tabs", map[string]any{"session_id": session, "tab_id": tab, "action": "switch"}))
	_ = liveFrameFor(t, frames, tab)
	success(t, h.call("suspend_session", nil))
	if !e.LiveWatched(session) {
		t.Fatal("suspension lost viewers")
	}
	liveDrain(frames)
	select {
	case <-frames:
		t.Fatal("frame during suspension")
	case <-time.After(150 * time.Millisecond):
	}
	success(t, h.call("tabs", nil))
	_ = liveFrameFor(t, frames, tab)
	success(t, h.call("tabs", map[string]any{"action": "close"}))
	liveDrain(frames)
	select {
	case <-frames:
		t.Fatal("closed active tab still streamed")
	case <-time.After(150 * time.Millisecond):
	}
	p = success(t, invoke(t, e, "tabs", map[string]any{"session_id": session, "action": "new", "url": server.URL + "/third"}))
	_ = liveFrameFor(t, frames, p["tab_id"].(string))
	success(t, h.call("close_session", nil))
	if e.LiveWatched(session) {
		t.Fatal("session close retained viewers")
	}
	stop()
	stop()
	if _, err := e.WatchLive(session, nil); err != ErrUnknownLiveSession {
		t.Fatal("closed session remains watchable")
	}
	// All navigation and lifecycle metadata is safe and session close is observed.
	deadline := time.After(3 * time.Second)
	opened, closed, page := false, false, false
	phases := map[string]map[string]bool{}
	for !closed {
		select {
		case event := <-events:
			if strings.Contains(event.URL, "?") || strings.Contains(event.URL, "#") {
				t.Fatal("unsanitized event URL")
			}
			if event.Type == "activity" {
				if phases[event.Kind] == nil {
					phases[event.Kind] = map[string]bool{}
				}
				phases[event.Kind][event.Phase] = true
			}
			opened = opened || event.Type == "session_opened"
			closed = event.Type == "session_closed"
			page = page || (event.Kind == "page" && event.Phase == "done" && event.Title == "")
		case <-deadline:
			t.Fatal("missing session close event")
		}
	}
	if !opened || !page {
		t.Fatal("missing open or navigation observation")
	}
	for _, kind := range []string{"navigate", "wait", "read", "download"} {
		if !phases[kind]["start"] || !phases[kind]["done"] {
			t.Fatalf("missing activity phases for %s", kind)
		}
	}
	// Engine shutdown emits session_closed for sessions not explicitly closed.
	p = success(t, invoke(t, e, "open_session", map[string]any{}))
	shutdownSession := p["session_id"].(string)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	for {
		event := takeLive(t, events)
		if event.Type == "session_closed" && event.Session == shutdownSession {
			break
		}
	}

}

func TestLiveVisibleTypingAndSecrets(t *testing.T) {
	requireBrowser(t)
	server := httptest.NewServer(http.HandlerFunc(liveFixture))
	defer server.Close()
	// These sentinels are fixture values, never production credentials.
	const password = "fixture-protected-value"
	e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile"), Secrets: MemorySecrets{
		"login.username": {Value: "fixtureuser", Hosts: []string{"127.0.0.1"}},
		"login.password": {Value: password, Hosts: []string{"127.0.0.1"}},
	}})
	defer e.Close()
	var mu sync.Mutex
	var events []LiveEvent
	e.SetLiveObserver(func(v LiveEvent) { mu.Lock(); events = append(events, v); mu.Unlock() })
	h := harness(t, e, server.URL)
	session := h.args["session_id"].(string)
	// Unwatched username entry is one Fill, even with an observer installed.
	liveActionOK(t, liveAct(t, h, "Username", "secret://login.username"))
	if got := livePageEval(t, h, "() => inputs.filter(id=>id==='username').length"); fmt.Sprint(got) != "1" {
		t.Fatalf("unwatched typing count: %v (%T)", got, got)
	}
	frames, stop := liveWatchFrames(t, e, session)
	_ = liveFrameFor(t, frames, "")
	livePageEval(t, h, "() => { document.querySelector('#username').value='';inputs=[] }")
	liveActionOK(t, liveAct(t, h, "Username", "secret://login.username"))
	if got := livePageEval(t, h, "() => inputs.filter(id=>id==='username').length"); fmt.Sprint(got) != "11" {
		t.Fatal("watched username did not type key by key")
	}
	liveActionOK(t, liveAct(t, h, "Password", "secret://login.password"))
	// Ordinary text uses sequential keys; long text retains Fill behavior.
	livePageEval(t, h, "() => { inputs=[] }")
	liveActionOK(t, liveAct(t, h, "Visible", "hello"))
	if got := livePageEval(t, h, "() => inputs.filter(id=>id==='visible').length"); fmt.Sprint(got) != "5" {
		t.Fatal("plain typing did not animate")
	}
	livePageEval(t, h, "() => { inputs=[] }")
	liveActionOK(t, liveAct(t, h, "Long", strings.Repeat("x", 121)))
	if got := livePageEval(t, h, "() => inputs.filter(id=>id==='long').length"); fmt.Sprint(got) != "1" {
		t.Fatal("long typing did not Fill")
	}
	liveActionOK(t, liveAct(t, h, "Editable", "editable text"))
	if got := livePageEval(t, h, `String(document.querySelector('#editable').textContent === 'editable text')`); got != "true" {
		t.Fatal("contenteditable fallback did not Fill")
	}
	// Compare observed highlight coordinates with the actual element.
	e.opMu.Lock()
	actual, err := e.sessions[session].tabs[h.args["tab_id"].(string)].page.Locator("#username").BoundingBox()
	e.opMu.Unlock()
	if err != nil || actual == nil {
		t.Fatal("no actual element box")
	}
	mu.Lock()
	snapshot := append([]LiveEvent(nil), events...)
	mu.Unlock()
	passwordKind, boxFound := false, false
	for _, event := range snapshot {
		encoded, _ := json.Marshal(event)
		if bytes.Contains(encoded, []byte(password)) {
			t.Fatal("protected value in activity")
		}
		passwordKind = passwordKind || event.Kind == "type_password"
		if event.Kind == "type" && event.Phase == "start" && event.Box != nil {
			b := event.Box
			if b.X < actual.X+actual.Width && actual.X < b.X+b.Width && b.Y < actual.Y+actual.Height && actual.Y < b.Y+b.Height {
				boxFound = true
			}
		}
	}
	if !passwordKind || !boxFound {
		t.Fatal("missing password kind or correct highlight")
	}
	// Fill a non-username secret into a visible field, then keep the page animating.
	liveActionOK(t, liveAct(t, h, "Visible", "secret://login.password"))
	time.Sleep(150 * time.Millisecond)
	liveDrain(frames)
	select {
	case <-frames:
		t.Fatal("visible secret did not withhold frames")
	case <-time.After(350 * time.Millisecond):
	}
	// A late watcher receives only the safe cached frame, never a newly captured one.
	lateFrames, lateStop := liveWatchFrames(t, e, session)
	cached := liveFrameFor(t, lateFrames, "")
	metadata := fmt.Sprintf("%d %s %d %d %s", cached.Seq, cached.Tab, cached.Width, cached.Height, cached.At)
	if strings.Contains(metadata, password) {
		t.Fatal("protected value in frame metadata")
	}
	select {
	case <-lateFrames:
		t.Fatal("late watcher captured withheld tab")
	case <-time.After(150 * time.Millisecond):
	}
	liveActionOK(t, h.call("navigate", map[string]any{"url": server.URL + "/next"}))
	_ = liveFrameFor(t, frames, "")
	_ = liveFrameFor(t, lateFrames, "")
	lateStop()
	stop()
}

// A viewer connects as soon as the first activity is announced, so it attaches
// while navigation is still running. Closing a conversation's only session also
// stops Chrome, and the next turn relaunches it. Both must still produce frames.
func TestLiveStreamAttachedDuringNavigationAcrossRelaunch(t *testing.T) {
	requireBrowser(t)
	server := httptest.NewServer(http.HandlerFunc(liveFixture))
	defer server.Close()
	e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile")})
	defer e.Close()
	started := make(chan string, 16)
	e.SetLiveObserver(func(v LiveEvent) {
		if v.Type == "activity" && v.Kind == "navigate" && v.Phase == "start" {
			started <- v.Session
		}
	})
	for round := 0; round < 4; round++ {
		h := harness(t, e, "about:blank")
		session := h.args["session_id"].(string)
		var frames <-chan LiveFrame
		var stop func()
		attached := make(chan struct{})
		safego.Go("live test watcher", nil, func() {
			defer close(attached)
			<-started
			frames, stop = liveWatchFrames(t, e, session)
		})
		liveActionOK(t, h.call("navigate", map[string]any{"action": "go", "url": server.URL}))
		<-attached
		frame := liveFrameFor(t, frames, h.args["tab_id"].(string))
		if !bytes.HasPrefix(frame.JPEG, []byte{0xff, 0xd8}) {
			t.Fatalf("round %d: invalid frame", round)
		}
		// A viewer normally outlives the turn: the session closes under it.
		success(t, invoke(t, e, "close_session", map[string]any{"session_id": session}))
		stop()
	}
}

// The viewer remains attached across origin changes. Check rendered pixels so
// an old page's cached frame cannot satisfy the assertion for the newest page.
func TestLiveStreamAcrossOrigins(t *testing.T) {
	requireBrowser(t)
	red := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><title>Red fixture</title><style>html,body{margin:0;min-height:100vh;background:#ee1122}</style>`)
	}))
	defer red.Close()
	green := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><title>Green fixture</title><style>html,body{margin:0;min-height:100vh;background:#00cc22}</style>`)
	}))
	defer green.Close()
	e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile")})
	defer e.Close()
	h := harness(t, e, "about:blank")
	frames, _ := liveWatchFrames(t, e, h.args["session_id"].(string))
	blank := liveFrameFor(t, frames, "")
	previous := blank.Seq
	for _, fixture := range []struct {
		url   string
		green bool
	}{{red.URL, false}, {green.URL, true}} {
		began := time.Now()
		liveActionOK(t, h.call("navigate", map[string]any{"url": fixture.url}))
		deadline := time.NewTimer(10 * time.Second)
		found := false
		for !found {
			select {
			case frame := <-frames:
				if frame.Seq <= previous || frame.At.Before(began) {
					continue
				}
				img, err := jpeg.Decode(bytes.NewReader(frame.JPEG))
				if err != nil {
					t.Fatal("invalid cross-origin JPEG")
				}
				r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
				if (fixture.green && g > 40000 && r < 12000 && b < 20000) ||
					(!fixture.green && r > 50000 && g < 12000 && b < 20000) {
					previous, found = frame.Seq, true
				}
			case <-deadline.C:
				t.Fatal("no live frame showing newest origin")
			}
		}
		deadline.Stop()
	}
}

// A login that answers after a delay must not read as a failed login.
func TestLiveLoginClickWaitsForDelayedNavigation(t *testing.T) {
	requireBrowser(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/home" {
			fmt.Fprint(w, `<!doctype html><title>Home</title><h1>Signed in</h1>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Login</title><label>Password<input type="password"></label>
 <button onclick="setTimeout(()=>{location='/home'},1500)">Enter</button>`)
	}))
	defer server.Close()
	run := func(observed bool) capabilities.TaskResult {
		e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile")})
		defer e.Close()
		if observed {
			e.SetLiveObserver(func(LiveEvent) {})
		}
		h := harness(t, e, server.URL)
		snap := liveSnapshot(t, h)
		for _, item := range snap["interactive"].([]any) {
			el := item.(map[string]any)
			if el["name"] == "Enter" {
				return h.call("act", map[string]any{"snapshot_id": snap["snapshot_id"], "ref": el["ref"], "action": "click"})
			}
		}
		t.Fatal("submit button missing")
		return capabilities.TaskResult{}
	}
	if r := run(true); r.State != capabilities.StateCompleted || !strings.Contains(string(r.Payload), `"navigated":true`) {
		t.Fatalf("observed click did not wait for the delayed login: %s %s", r.State, r.Payload)
	}
	// Without an observer, the engine reports the form immediately, as before.
	if r := run(false); r.State != capabilities.StateInputRequired {
		t.Fatalf("unobserved click changed behavior: %s", r.State)
	}
}
