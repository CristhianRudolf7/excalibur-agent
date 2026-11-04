package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type liveBackendFake struct {
	*MemoryBackend
	mu       sync.Mutex
	next     int
	watchers map[int]func(BrowserFrame)
	done     chan struct{}
	stops    int
	initial  []BrowserFrame
}

func newLiveBackendFake(bus *telemetry.Bus) *liveBackendFake {
	return &liveBackendFake{MemoryBackend: NewMemoryBackend(bus), watchers: make(map[int]func(BrowserFrame)), done: make(chan struct{})}
}

func (b *liveBackendFake) WatchBrowser(conversation, session string, fn func(BrowserFrame)) (func(), <-chan struct{}, error) {
	if conversation != "conversation" || session != "session" {
		return nil, nil, &api.Error{Code: api.CodeUnknownTarget, Message: "unknown browser session"}
	}
	b.mu.Lock()
	b.next++
	id := b.next
	b.watchers[id] = fn
	b.mu.Unlock()
	for _, f := range b.initial {
		fn(f)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.watchers, id)
			b.stops++
			b.mu.Unlock()
		})
	}, b.done, nil
}

func (b *liveBackendFake) emit(f BrowserFrame) {
	b.mu.Lock()
	callbacks := make([]func(BrowserFrame), 0, len(b.watchers))
	for _, fn := range b.watchers {
		callbacks = append(callbacks, fn)
	}
	b.mu.Unlock()
	for _, fn := range callbacks {
		fn(f)
	}
}

func (b *liveBackendFake) counts() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.watchers), b.stops
}

func browserLiveTestServer(t *testing.T) (*Server, *liveBackendFake, string, context.Context) {
	t.Helper()
	bus := telemetry.NewBus(nil)
	backend := newLiveBackendFake(bus)
	gate := New(DefaultConfig(), backend, nil, bus)
	server := httptest.NewServer(gate)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(func() { cancel(); _ = gate.Stop(context.Background()); server.Close() })
	return gate, backend, "ws" + strings.TrimPrefix(server.URL, "http"), ctx
}

func browserLiveDial(t *testing.T, ctx context.Context, base string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, base+"/ws/browser?conversation_id=conversation&session_id=session", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func browserLiveRead(t *testing.T, ctx context.Context, conn *websocket.Conn) (BrowserFrame, []byte) {
	t.Helper()
	typ, raw, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || len(raw) < 4 {
		t.Fatalf("invalid binary frame: type=%v, error=%v", typ, err)
	}
	n := int(binary.BigEndian.Uint32(raw[:4]))
	if n > len(raw)-4 {
		t.Fatal("invalid header length")
	}
	var header struct {
		Seq    uint64 `json:"seq"`
		Tab    string `json:"tab_id"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		At     int64  `json:"at"`
	}
	if err := json.Unmarshal(raw[4:4+n], &header); err != nil {
		t.Fatal(err)
	}
	return BrowserFrame{Seq: header.Seq, Tab: header.Tab, Width: header.Width, Height: header.Height, AtMillis: header.At}, raw[4+n:]
}

func awaitBrowserLive(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-deadline.C:
			t.Fatal("browser viewer did not stop")
		case <-tick.C:
		}
	}
}

func TestBrowserLiveBinaryRoundTripAndEventSocket(t *testing.T) {
	gate, backend, base, ctx := browserLiveTestServer(t)
	viewer := browserLiveDial(t, ctx, base)
	events, _, err := websocket.Dial(ctx, base+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer events.CloseNow()
	// Watch-only client messages have no effects and do not break the stream.
	if err := viewer.Write(ctx, websocket.MessageText, []byte(`{"ignored":true}`)); err != nil {
		t.Fatal(err)
	}
	jpeg := []byte{0xff, 0xd8, 1, 2, 3, 0xff, 0xd9}
	backend.emit(BrowserFrame{Seq: 12, Tab: "tab-1", Width: 1280, Height: 720, AtMillis: 1793000000000, JPEG: jpeg})
	frame, image := browserLiveRead(t, ctx, viewer)
	if frame.Seq != 12 || frame.Tab != "tab-1" || frame.Width != 1280 || frame.Height != 720 || frame.AtMillis != 1793000000000 || !bytes.Equal(image, jpeg) {
		t.Fatal("binary framing changed the header or JPEG")
	}
	if gate.bus.Len() != 0 || len(gate.replay(types.Cursor{}, nil)) != 0 {
		t.Fatal("frames entered event history")
	}
	// The event socket continues serving RPC while the frame socket is open,
	// and again after a browser session ends.
	call := func() {
		t.Helper()
		if err := events.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":"live","method":"run.list","params":{}}`)); err != nil {
			t.Fatal(err)
		}
		_, raw, err := events.Read(ctx)
		var reply api.Response
		if err != nil || json.Unmarshal(raw, &reply) != nil || reply.Error != nil || string(reply.ID) != `"live"` {
			t.Fatal("browser stream affected event RPC")
		}
	}
	call()
	if err := events.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":"subscribe","method":"conversation.subscribe","params":{"types":["browser.activity"]}}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := events.Read(ctx); err != nil {
		t.Fatal("event subscription failed")
	}
	if _, err := gate.PublishEvent(api.EventBrowserActivity, nil, nil, map[string]string{"kind": api.BrowserKindRead}); err != nil {
		t.Fatal(err)
	}
	_, raw, err := events.Read(ctx)
	var notification struct {
		Method string      `json:"method"`
		Params types.Event `json:"params"`
	}
	if err != nil || json.Unmarshal(raw, &notification) != nil || notification.Method != "event" || notification.Params.Type != api.EventBrowserActivity {
		t.Fatal("browser frame socket affected event delivery")
	}
	close(backend.done)
	_, _, err = viewer.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure || !strings.Contains(err.Error(), "ended") {
		t.Fatalf("wrong session end: %v", err)
	}
	call()
	awaitBrowserLive(t, func() bool { n, stopped := backend.counts(); return n == 0 && stopped == 1 })
}

func TestBrowserLiveViewerCapAndDisconnect(t *testing.T) {
	_, backend, base, ctx := browserLiveTestServer(t)
	viewers := make([]*websocket.Conn, 4)
	for i := range viewers {
		viewers[i] = browserLiveDial(t, ctx, base)
	}
	_, resp, err := websocket.Dial(ctx, base+"/ws/browser?conversation_id=conversation&session_id=session", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatal("fifth viewer was not rejected")
	}
	if n, _ := backend.counts(); n != 4 {
		t.Fatal("rejected viewer attached an engine watcher")
	}
	_ = viewers[0].CloseNow()
	awaitBrowserLive(t, func() bool { n, stopped := backend.counts(); return n == 3 && stopped == 1 })
	browserLiveDial(t, ctx, base)
	for _, c := range viewers[1:] {
		_ = c.CloseNow()
	}
	awaitBrowserLive(t, func() bool { n, stopped := backend.counts(); return n == 1 && stopped == 4 })
}

func TestBrowserLiveUnknownAndUnsupported(t *testing.T) {
	_, _, base, ctx := browserLiveTestServer(t)
	for _, query := range []string{"", "?conversation_id=conversation", "?conversation_id=other&session_id=session", "?conversation_id=conversation&session_id=unknown"} {
		_, resp, err := websocket.Dial(ctx, base+"/ws/browser"+query, nil)
		if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown viewer request not rejected: %s", query)
		}
	}
	unsupported := httptest.NewServer(New(DefaultConfig(), nil, nil, nil))
	defer unsupported.Close()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(unsupported.URL, "http")+"/ws/browser?conversation_id=conversation&session_id=session", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatal("backend without live interface was not rejected")
	}
}

func TestBrowserLiveOriginAndLoopback(t *testing.T) {
	gate, backend, base, ctx := browserLiveTestServer(t)
	_, resp, err := websocket.Dial(ctx, base+"/ws/browser?conversation_id=conversation&session_id=session", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://other.invalid"}}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatal("cross-origin browser viewer accepted")
	}
	awaitBrowserLive(t, func() bool { n, stopped := backend.counts(); return n == 0 && stopped == 1 })
	// An explicitly matching origin is accepted by the default origin check.
	conn, _, err := websocket.Dial(ctx, base+"/ws/browser?conversation_id=conversation&session_id=session", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http" + strings.TrimPrefix(base, "ws")}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.CloseNow()
	req := httptest.NewRequest(http.MethodGet, "/ws/browser?conversation_id=conversation&session_id=session", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	recorder := httptest.NewRecorder()
	gate.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatal("non-loopback peer accepted")
	}
}

func TestBrowserLiveMailboxNewestWithStalledReader(t *testing.T) {
	// Freeze consumption completely: callback delivery must stay non-blocking,
	// consume constant queue space, and retain only the latest frame.
	mailbox := newBrowserFrameMailbox()
	for i := 1; i <= 10000; i++ {
		mailbox.offer(BrowserFrame{Seq: uint64(i)})
	}
	if len(mailbox.ch) != 1 || (<-mailbox.ch).Seq != 10000 {
		t.Fatal("stalled reader accumulated stale frames")
	}
	// A backend can synchronously replay frames before Accept starts the socket
	// writer; the real gateway must send only the newest cached frame.
	_, backend, base, ctx := browserLiveTestServer(t)
	backend.initial = []BrowserFrame{{Seq: 1}, {Seq: 2}, {Seq: 3}}
	viewer := browserLiveDial(t, ctx, base)
	frame, _ := browserLiveRead(t, ctx, viewer)
	if frame.Seq != 3 {
		t.Fatal("gateway delivered stale cached frame")
	}
}

func TestBrowserLiveMailboxConcurrentProducers(t *testing.T) {
	mailbox := newBrowserFrameMailbox()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		safego.Go("live-mailbox-test", nil, func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mailbox.offer(BrowserFrame{})
			}
		})
	}
	wg.Wait()
	mailbox.offer(BrowserFrame{Seq: 1001})
	if len(mailbox.ch) != 1 || (<-mailbox.ch).Seq != 1001 {
		t.Fatal("concurrent callbacks lost latest frame")
	}
}

func TestBrowserLiveReadLimitStopsWatcher(t *testing.T) {
	_, backend, base, ctx := browserLiveTestServer(t)
	viewer := browserLiveDial(t, ctx, base)
	_ = viewer.Write(ctx, websocket.MessageBinary, bytes.Repeat([]byte{'x'}, 1025))
	_, _, err := viewer.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("read limit was not enforced: %v", err)
	}
	awaitBrowserLive(t, func() bool { n, stopped := backend.counts(); return n == 0 && stopped == 1 })
}

func TestBrowserLiveServerStopClosesHijackedSocket(t *testing.T) {
	bus := telemetry.NewBus(nil)
	backend := newLiveBackendFake(bus)
	gate := New(DefaultConfig(), backend, nil, bus)
	addr, err := gate.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	viewer := browserLiveDial(t, ctx, "ws://"+addr)
	if err := gate.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := viewer.Read(ctx); err == nil {
		t.Fatal("shutdown left hijacked socket open")
	}
	awaitBrowserLive(t, func() bool { n, stopped := backend.counts(); return n == 0 && stopped == 1 })
}
