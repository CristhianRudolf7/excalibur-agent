package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

type testVoiceAction struct {
	kind string
	pcm  []byte
}
type testVoiceSession struct {
	events  chan VoiceListenEvent
	actions chan testVoiceAction
	closed  chan struct{}
	once    sync.Once
}

func newTestVoiceSession() *testVoiceSession {
	return &testVoiceSession{events: make(chan VoiceListenEvent, 8), actions: make(chan testVoiceAction, 8), closed: make(chan struct{})}
}
func (s *testVoiceSession) Append(p []byte) error {
	s.actions <- testVoiceAction{"append", append([]byte(nil), p...)}
	return nil
}
func (s *testVoiceSession) Commit() error                   { s.actions <- testVoiceAction{kind: "commit"}; return nil }
func (s *testVoiceSession) Clear() error                    { s.actions <- testVoiceAction{kind: "clear"}; return nil }
func (s *testVoiceSession) Events() <-chan VoiceListenEvent { return s.events }
func (s *testVoiceSession) Close() error                    { s.once.Do(func() { close(s.closed) }); return nil }

type testVoiceBackend struct {
	*MemoryBackend
	sessions  chan *testVoiceSession
	languages chan string
	listen    func(context.Context, string) (VoiceListenSession, error)
	speak     func(context.Context, string, func([]byte) error) error
}

func (b *testVoiceBackend) VoiceListen(ctx context.Context, language string) (VoiceListenSession, error) {
	b.languages <- language
	if b.listen != nil {
		return b.listen(ctx, language)
	}
	s := newTestVoiceSession()
	b.sessions <- s
	return s, nil
}
func (b *testVoiceBackend) VoiceSpeak(ctx context.Context, text string, write func([]byte) error) error {
	if b.speak != nil {
		return b.speak(ctx, text, write)
	}
	return write([]byte{1, 2, 3, 4})
}
func newTestVoiceBackend() *testVoiceBackend {
	return &testVoiceBackend{MemoryBackend: NewMemoryBackend(nil), sessions: make(chan *testVoiceSession, 8), languages: make(chan string, 8)}
}

func voiceTestServer(t *testing.T, b *testVoiceBackend) (*Server, string, context.Context) {
	t.Helper()
	gate := New(DefaultConfig(), b, nil, telemetry.NewBus(nil))
	server := httptest.NewServer(gate)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(func() { cancel(); _ = gate.Stop(context.Background()); server.Close() })
	return gate, "ws" + strings.TrimPrefix(server.URL, "http"), ctx
}
func voiceDial(t *testing.T, ctx context.Context, base, path string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}
func voiceWrite(t *testing.T, ctx context.Context, conn *websocket.Conn, raw string) {
	t.Helper()
	if err := conn.Write(ctx, websocket.MessageText, []byte(raw)); err != nil {
		t.Fatal(err)
	}
}
func voiceReadJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, want string) {
	t.Helper()
	typ, raw, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("missing text: type=%v, error=%v", typ, err)
	}
	var got, expected any
	if json.Unmarshal(raw, &got) != nil || json.Unmarshal([]byte(want), &expected) != nil {
		t.Fatal("invalid reply JSON")
	}
	gotRaw, _ := json.Marshal(got)
	expectedRaw, _ := json.Marshal(expected)
	if !bytes.Equal(gotRaw, expectedRaw) {
		t.Fatalf("reply %s; want %s", gotRaw, expectedRaw)
	}
}
func voiceReadAudio(t *testing.T, ctx context.Context, conn *websocket.Conn, id uint32, want []byte) {
	t.Helper()
	typ, raw, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || len(raw) < 4 {
		t.Fatalf("invalid PCM frame: %v", err)
	}
	if binary.BigEndian.Uint32(raw[:4]) != id || !bytes.Equal(raw[4:], want) {
		t.Fatal("incorrect PCM frame")
	}
}
func voiceWaitClosed(t *testing.T, ctx context.Context, s *testVoiceSession) {
	t.Helper()
	select {
	case <-s.closed:
	case <-ctx.Done():
		t.Fatal("session leaked")
	}
}

func TestVoiceListenSocketRoundTrip(t *testing.T) {
	b := newTestVoiceBackend()
	_, base, ctx := voiceTestServer(t, b)
	conn := voiceDial(t, ctx, base, "/ws/voice/listen?language=bad")
	voiceReadJSON(t, ctx, conn, `{"type":"ready"}`)
	session := <-b.sessions
	if language := <-b.languages; language != "es" {
		t.Fatalf("language: %s", language)
	}
	pcm := make([]byte, 64<<10)
	pcm[0] = 1
	pcm[len(pcm)-1] = 2
	if err := conn.Write(ctx, websocket.MessageBinary, pcm); err != nil {
		t.Fatal(err)
	}
	action := <-session.actions
	if action.kind != "append" || !bytes.Equal(action.pcm, pcm) {
		t.Fatal("audio changed")
	}
	voiceWrite(t, ctx, conn, `{"type":"commit"}`)
	if (<-session.actions).kind != "commit" {
		t.Fatal("commit missing")
	}
	voiceWrite(t, ctx, conn, `{"type":"clear"}`)
	if (<-session.actions).kind != "clear" {
		t.Fatal("clear missing")
	}
	session.events <- VoiceListenEvent{Type: "delta", Item: "a", Text: "Hello"}
	session.events <- VoiceListenEvent{Type: "final", Item: "a", Text: "Hello world"}
	session.events <- VoiceListenEvent{Type: "final"}
	voiceReadJSON(t, ctx, conn, `{"type":"delta","item":"a","text":"Hello"}`)
	voiceReadJSON(t, ctx, conn, `{"type":"final","item":"a","text":"Hello world"}`)
	voiceReadJSON(t, ctx, conn, `{"type":"final","item":"","text":""}`)
	_ = conn.CloseNow()
	voiceWaitClosed(t, ctx, session)
}

func TestVoiceListenFailureAndNormalClose(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		startup    bool
		err        error
		event      VoiceListenEvent
	}{
		{name: "unavailable startup", startup: true, err: ErrVoiceUnavailable, code: "unavailable"},
		{name: "failed startup", startup: true, err: errors.New("private upstream message"), code: "failed"},
		{name: "midcall unavailable", event: VoiceListenEvent{Type: "error", Code: "unavailable", Text: "private upstream message"}, code: "unavailable"},
		{name: "midcall closed", event: VoiceListenEvent{Type: "closed"}, code: "failed"},
		{name: "midcall failed", event: VoiceListenEvent{Type: "error", Code: "unexpected", Text: "private upstream message"}, code: "failed"},
		{name: "event channel closed", code: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newTestVoiceBackend()
			if tc.startup {
				b.listen = func(context.Context, string) (VoiceListenSession, error) { return nil, tc.err }
			}
			_, base, ctx := voiceTestServer(t, b)
			conn := voiceDial(t, ctx, base, "/ws/voice/listen?language=en")
			if !tc.startup {
				voiceReadJSON(t, ctx, conn, `{"type":"ready"}`)
				s := <-b.sessions
				if tc.event.Type == "" {
					close(s.events)
				} else {
					s.events <- tc.event
				}
			}
			voiceReadJSON(t, ctx, conn, `{"type":"error","code":"`+tc.code+`"}`)
			_, _, err := conn.Read(ctx)
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("closure: %v", err)
			}
			if (<-b.languages) != "en" {
				t.Fatal("valid language changed")
			}
		})
	}
}

func TestVoiceSocketCapsAndOrigins(t *testing.T) {
	for _, path := range []string{"/ws/voice/listen", "/ws/voice/speak"} {
		t.Run(path, func(t *testing.T) {
			b := newTestVoiceBackend()
			_, base, ctx := voiceTestServer(t, b)
			_, resp, err := websocket.Dial(ctx, base+path, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://other.invalid"}}})
			if err == nil || resp == nil || resp.StatusCode != 403 {
				t.Fatal("cross-origin accepted")
			}
			first := voiceDial(t, ctx, base, path)
			if strings.HasSuffix(path, "listen") {
				voiceReadJSON(t, ctx, first, `{"type":"ready"}`)
			}
			second := voiceDial(t, ctx, base, path)
			if strings.HasSuffix(path, "listen") {
				voiceReadJSON(t, ctx, second, `{"type":"ready"}`)
			}
			_, resp, err = websocket.Dial(ctx, base+path, nil)
			if err == nil || resp == nil || resp.StatusCode != 429 {
				t.Fatal("cap not enforced")
			}
			_ = first.CloseNow()
			_ = second.CloseNow()
		})
	}
}

func TestVoiceUnsupportedMethodAndLoopback(t *testing.T) {
	gate := New(DefaultConfig(), NewMemoryBackend(nil), nil, nil)
	for _, path := range []string{"/ws/voice/listen", "/ws/voice/speak"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		gate.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("unsupported: %d", w.Code)
		}
		w = httptest.NewRecorder()
		r.RemoteAddr = "203.0.113.1:1234"
		gate.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("remote: %d", w.Code)
		}
		gateWithVoice := New(DefaultConfig(), newTestVoiceBackend(), nil, nil)
		w = httptest.NewRecorder()
		r = httptest.NewRequest("POST", path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		gateWithVoice.ServeHTTP(w, r)
		if w.Code != 405 {
			t.Fatalf("method: %d", w.Code)
		}
	}
}

func TestVoiceListenDisconnectDuringStartup(t *testing.T) {
	b := newTestVoiceBackend()
	stopped := make(chan struct{})
	started := make(chan struct{})
	b.listen = func(ctx context.Context, _ string) (VoiceListenSession, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}
	_, base, ctx := voiceTestServer(t, b)
	conn := voiceDial(t, ctx, base, "/ws/voice/listen")
	<-started
	_ = conn.CloseNow()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("startup leaked")
	}
}

func TestVoiceListenBadMessagesAndReadLimit(t *testing.T) {
	for _, tc := range []struct{ name, message string }{
		{"invalid JSON", "bad"},
		{"unknown command", `{"type":"unknown"}`},
		{"oversized message", strings.Repeat("a", (64<<10)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.message
			b := newTestVoiceBackend()
			_, base, ctx := voiceTestServer(t, b)
			conn := voiceDial(t, ctx, base, "/ws/voice/listen")
			voiceReadJSON(t, ctx, conn, `{"type":"ready"}`)
			s := <-b.sessions
			_ = conn.Write(ctx, websocket.MessageText, []byte(msg))
			if len(msg) <= 64<<10 {
				voiceReadJSON(t, ctx, conn, `{"type":"error","code":"failed"}`)
			}
			_, _, err := conn.Read(ctx)
			if err == nil {
				t.Fatal("bad message accepted")
			}
			voiceWaitClosed(t, ctx, s)
		})
	}
}

func TestVoiceSpeakRoundTripAndErrors(t *testing.T) {
	b := newTestVoiceBackend()
	_, base, ctx := voiceTestServer(t, b)
	conn := voiceDial(t, ctx, base, "/ws/voice/speak")
	voiceWrite(t, ctx, conn, `{"type":"speak","id":7,"text":"Hello"}`)
	voiceReadAudio(t, ctx, conn, 7, []byte{1, 2, 3, 4})
	voiceReadJSON(t, ctx, conn, `{"type":"done","id":7}`)
	for _, msg := range []string{`{"type":"speak","id":8,"text":""}`, `{"type":"unknown","id":8}`, `{"type":"speak","id":8,"text":`, `{"type":"speak","id":8,"text":12}`} {
		voiceWrite(t, ctx, conn, msg)
		// Unmarshal cannot recover an id from syntactically invalid JSON.
		id := "8"
		if strings.HasSuffix(msg, `"text":`) {
			id = "0"
		}
		voiceReadJSON(t, ctx, conn, `{"type":"error","id":`+id+`,"code":"failed"}`)
	}
	for _, msg := range []string{`{"type":"speak","id":0,"text":"Hello"}`, `{"type":"speak","id":-1,"text":"Hello"}`, `{"type":"speak","id":1.2,"text":"Hello"}`, `{"type":"speak","id":4294967296,"text":"Hello"}`} {
		voiceWrite(t, ctx, conn, msg)
		voiceReadJSON(t, ctx, conn, `{"type":"error","id":0,"code":"failed"}`)
	}
	voiceWrite(t, ctx, conn, `{"type":"speak","id":9,"text":"`+strings.Repeat("a", 4097)+`"}`)
	voiceReadJSON(t, ctx, conn, `{"type":"error","id":9,"code":"failed"}`)
}

func TestVoiceSpeakCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "replace"} {
		t.Run(mode, func(t *testing.T) {
			b := newTestVoiceBackend()
			stopped := make(chan struct{})
			staleResult := make(chan error, 1)
			b.speak = func(ctx context.Context, text string, write func([]byte) error) error {
				if text == "second" {
					return write([]byte{3, 4})
				}
				if err := write([]byte{1, 2}); err != nil {
					return err
				}
				<-ctx.Done()
				// A backend callback racing with cancellation must not reach the socket.
				staleResult <- write([]byte{9, 9})
				close(stopped)
				return ctx.Err()
			}
			_, base, ctx := voiceTestServer(t, b)
			conn := voiceDial(t, ctx, base, "/ws/voice/speak")
			voiceWrite(t, ctx, conn, `{"type":"speak","id":1,"text":"first"}`)
			voiceReadAudio(t, ctx, conn, 1, []byte{1, 2})
			if mode == "cancel" {
				voiceWrite(t, ctx, conn, `{"type":"cancel","id":1}`)
			} else {
				voiceWrite(t, ctx, conn, `{"type":"speak","id":2,"text":"second"}`)
			}
			voiceReadJSON(t, ctx, conn, `{"type":"done","id":1}`)
			select {
			case <-stopped:
			case <-ctx.Done():
				t.Fatal("synthesis leaked")
			}
			if !errors.Is(<-staleResult, context.Canceled) {
				t.Fatal("stale write accepted")
			}
			if mode == "cancel" {
				voiceWrite(t, ctx, conn, `{"type":"speak","id":2,"text":"second"}`)
			}
			voiceReadAudio(t, ctx, conn, 2, []byte{3, 4})
			voiceReadJSON(t, ctx, conn, `{"type":"done","id":2}`)
		})
	}
}

func TestVoiceSpeakUnavailableAndFailed(t *testing.T) {
	for _, err := range []error{ErrVoiceUnavailable, errors.New("private upstream message")} {
		b := newTestVoiceBackend()
		b.speak = func(context.Context, string, func([]byte) error) error { return err }
		_, base, ctx := voiceTestServer(t, b)
		conn := voiceDial(t, ctx, base, "/ws/voice/speak")
		voiceWrite(t, ctx, conn, `{"type":"speak","id":7,"text":"Hello"}`)
		voiceReadJSON(t, ctx, conn, `{"type":"error","id":7,"code":"`+voiceErrorCode(err)+`"}`)
	}
}

func TestVoiceShutdownClosesSessions(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			b := newTestVoiceBackend()
			stopped := make(chan struct{})
			started := make(chan struct{})
			b.speak = func(ctx context.Context, _ string, _ func([]byte) error) error {
				close(started)
				<-ctx.Done()
				close(stopped)
				return ctx.Err()
			}
			gate, base, ctx := voiceTestServer(t, b)
			listen := voiceDial(t, ctx, base, "/ws/voice/listen")
			voiceReadJSON(t, ctx, listen, `{"type":"ready"}`)
			s := <-b.sessions
			speak := voiceDial(t, ctx, base, "/ws/voice/speak")
			voiceWrite(t, ctx, speak, `{"type":"speak","id":1,"text":"Hello"}`)
			<-started
			if mode == "shutdown" {
				if err := gate.Stop(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				_ = listen.CloseNow()
				_ = speak.CloseNow()
			}
			voiceWaitClosed(t, ctx, s)
			select {
			case <-stopped:
			case <-ctx.Done():
				t.Fatal("synthesis leaked")
			}
			if mode == "shutdown" {
				if _, _, err := listen.Read(ctx); err == nil {
					t.Fatal("listen survived shutdown")
				}
				if _, _, err := speak.Read(ctx); err == nil {
					t.Fatal("speak survived shutdown")
				}
			}
		})
	}
}

func TestVoiceLeavesEventSocketUnaffected(t *testing.T) {
	b := newTestVoiceBackend()
	gate, base, ctx := voiceTestServer(t, b)
	listen := voiceDial(t, ctx, base, "/ws/voice/listen")
	voiceReadJSON(t, ctx, listen, `{"type":"ready"}`)
	speak := voiceDial(t, ctx, base, "/ws/voice/speak")
	events := voiceDial(t, ctx, base, "/ws")
	voiceWrite(t, ctx, events, `{"jsonrpc":"2.0","id":1,"method":"run.list","params":{}}`)
	typ, raw, err := events.Read(ctx)
	if err != nil || typ != websocket.MessageText || !bytes.Contains(raw, []byte(`"result"`)) {
		t.Fatalf("event socket failed: %v, %s", err, raw)
	}
	voiceWrite(t, ctx, speak, `{"type":"speak","id":1,"text":"Hello"}`)
	voiceReadAudio(t, ctx, speak, 1, []byte{1, 2, 3, 4})
	voiceReadJSON(t, ctx, speak, `{"type":"done","id":1}`)
	session := <-b.sessions
	session.events <- VoiceListenEvent{Type: "final", Text: "Private transient text"}
	voiceReadJSON(t, ctx, listen, `{"type":"final","item":"","text":"Private transient text"}`)
	if gate.bus.Len() != 0 {
		t.Fatal("voice entered event history")
	}
}

func TestVoiceSlowClientEndsOnlyItsSession(t *testing.T) {
	b := newTestVoiceBackend()
	stopped := make(chan struct{})
	b.speak = func(ctx context.Context, _ string, write func([]byte) error) error {
		defer close(stopped)
		pcm := make([]byte, 64<<10)
		for {
			if err := write(pcm); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	_, base, _ := voiceTestServer(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	listen := voiceDial(t, ctx, base, "/ws/voice/listen")
	voiceReadJSON(t, ctx, listen, `{"type":"ready"}`)
	session := <-b.sessions
	stalled := voiceDial(t, ctx, base, "/ws/voice/speak")
	voiceWrite(t, ctx, stalled, `{"type":"speak","id":1,"text":"Hello"}`)
	// With no reader, bounded websocket writes must time out and cancel synthesis.
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("stalled client did not stop")
	}
	session.events <- VoiceListenEvent{Type: "final", Text: "Still listening"}
	voiceReadJSON(t, ctx, listen, `{"type":"final","item":"","text":"Still listening"}`)
}
