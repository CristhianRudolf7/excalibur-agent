package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
)

func liveContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func liveCredentials(base, key string) Credentials {
	return func(context.Context) (string, string, error) { return base + "/v1", key, nil }
}

func realtimeServer(t *testing.T, model, language string, fn func(context.Context, *websocket.Conn)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/realtime" || r.URL.RawQuery != "intent=transcription" || r.Header.Get("Authorization") != "Bearer test-primary" {
			t.Error("invalid realtime request")
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var got, want any
		_ = json.Unmarshal(raw, &got)
		expected := `{"type":"session.update","session":{"type":"transcription","audio":{"input":{"format":{"type":"audio/pcm","rate":24000},"transcription":{"model":"` + model + `","language":"` + language + `"},"turn_detection":null}}}}`
		_ = json.Unmarshal([]byte(expected), &want)
		if !reflect.DeepEqual(got, want) {
			t.Error("incorrect session.update configuration")
			return
		}
		fn(ctx, conn)
	}))
}

func upstreamWrite(ctx context.Context, conn *websocket.Conn, raw string) {
	_ = conn.Write(ctx, websocket.MessageText, []byte(raw))
}

func nextListen(t *testing.T, ctx context.Context, s *ListenSession, want ListenEvent) {
	t.Helper()
	select {
	case got, ok := <-s.Events():
		if !ok || got != want {
			t.Fatalf("event %+v, open=%v; want %+v", got, ok, want)
		}
	case <-ctx.Done():
		t.Fatal("missing listen event")
	}
}

func TestLiveListenRoundTrip(t *testing.T) {
	ctx := liveContext(t)
	server := realtimeServer(t, "gpt-realtime-whisper", "es", func(ctx context.Context, conn *websocket.Conn) {
		upstreamWrite(ctx, conn, `{"type":"session.created"}`)
		upstreamWrite(ctx, conn, `{"type":"session.updated"}`)
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg map[string]string
			if json.Unmarshal(raw, &msg) != nil {
				t.Error("invalid client event")
				return
			}
			switch msg["type"] {
			case "input_audio_buffer.append":
				audio, err := base64.StdEncoding.DecodeString(msg["audio"])
				if err != nil || !bytes.Equal(audio, []byte{1, 2, 3, 4}) {
					t.Error("audio changed")
				}
				upstreamWrite(ctx, conn, `{"type":"conversation.item.input_audio_transcription.delta","item_id":"a","delta":"Hello"}`)
			case "input_audio_buffer.commit":
				upstreamWrite(ctx, conn, `{"type":"input_audio_buffer.committed"}`)
				upstreamWrite(ctx, conn, `{"type":"conversation.item.input_audio_transcription.completed","item_id":"a","transcript":"Hello world"}`)
				// A final needs no matching delta or local transcript accumulation.
				upstreamWrite(ctx, conn, `{"type":"conversation.item.input_audio_transcription.completed","item_id":"b","transcript":"Final only"}`)
			case "input_audio_buffer.clear":
				upstreamWrite(ctx, conn, `{"type":"error","error":{"code":"input_audio_buffer_commit_empty"}}`)
			default:
				t.Error("unexpected client event")
			}
		}
	})
	defer server.Close()
	c := &Client{Credentials: liveCredentials(server.URL, "test-primary")}
	s, err := c.Listen(ctx, "bad-language")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Append([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	nextListen(t, ctx, s, ListenEvent{Type: "delta", Item: "a", Text: "Hello"})
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	nextListen(t, ctx, s, ListenEvent{Type: "final", Item: "a", Text: "Hello world"})
	nextListen(t, ctx, s, ListenEvent{Type: "final", Item: "b", Text: "Final only"})
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	nextListen(t, ctx, s, ListenEvent{Type: "final"})
}

func TestLiveListenStartupFallback(t *testing.T) {
	for _, tc := range []struct {
		name            string
		status          int
		both, same      bool
		errorType, code string
	}{
		{name: "created then quota", errorType: "insufficient_quota"},
		{name: "both exhausted", both: true, code: "credit_balance_exhausted"},
		{name: "same credential", same: true, code: "insufficient_quota"},
		{name: "handshake unauthorized", status: 401},
		{name: "handshake payment", status: 402},
		{name: "handshake forbidden", status: 403},
		{name: "handshake rate limit", status: 429},
		{name: "hard limit", code: "billing_hard_limit_reached"},
		{name: "invalid key", code: "invalid_api_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := liveContext(t)
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				primary := r.Header.Get("Authorization") == "Bearer test-primary"
				if !primary && r.Header.Get("Authorization") != "Bearer test-fallback" {
					t.Error("unexpected credential")
					return
				}
				reject := primary || tc.both
				if reject && tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				_, _, err = conn.Read(ctx)
				if err != nil {
					return
				}
				upstreamWrite(ctx, conn, `{"type":"session.created"}`)
				if reject {
					raw, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": tc.errorType, "code": tc.code, "message": "test-primary test-fallback"}})
					_ = conn.Write(ctx, websocket.MessageText, raw)
					return
				}
				upstreamWrite(ctx, conn, `{"type":"session.updated"}`)
				_, _, _ = conn.Read(ctx)
			}))
			defer server.Close()
			fallback := "test-fallback"
			if tc.same {
				fallback = "test-primary"
			}
			c := &Client{Credentials: liveCredentials(server.URL, "test-primary"), FallbackCredentials: liveCredentials(server.URL, fallback)}
			s, err := c.Listen(ctx, "es")
			if tc.both || tc.same {
				if !errors.Is(err, ErrUnavailable) {
					t.Fatalf("expected unavailable, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				_ = s.Close()
			}
			if err != nil && (strings.Contains(err.Error(), "test-primary") || strings.Contains(err.Error(), "test-fallback")) {
				t.Fatal("credential leaked")
			}
			want := int32(2)
			if tc.same {
				want = 1
			}
			if attempts.Load() != want {
				t.Fatalf("attempts %d", attempts.Load())
			}
		})
	}
}

func TestLiveListenUnavailableAndDisabled(t *testing.T) {
	disabled := false
	for _, c := range []*Client{{}, {Config: config.VoiceConfig{Enabled: &disabled}, Credentials: liveCredentials("http://unused.invalid", "test-primary")}} {
		if _, err := c.Listen(liveContext(t), "es"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable: %v", err)
		}
		if err := c.SpeakStream(liveContext(t), "Hello", func([]byte) error { t.Fatal("unexpected audio"); return nil }); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable: %v", err)
		}
	}
}

func TestLiveListenTimeoutAndStartupFailure(t *testing.T) {
	for _, mode := range []string{"timeout", "rejected", "closed", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			ctx := liveContext(t)
			server := realtimeServer(t, "custom-live", "en", func(ctx context.Context, conn *websocket.Conn) {
				upstreamWrite(ctx, conn, `{"type":"session.created"}`)
				switch mode {
				case "timeout":
					_, _, _ = conn.Read(ctx)
				case "rejected":
					upstreamWrite(ctx, conn, `{"type":"error","error":{"message":"test-primary","code":"unsupported"}}`)
				case "invalid":
					upstreamWrite(ctx, conn, `invalid`)
				}
			})
			defer server.Close()
			c := &Client{Config: config.VoiceConfig{LiveSTTModel: "custom-live"}, Credentials: liveCredentials(server.URL, "test-primary"), FallbackCredentials: func(context.Context) (string, string, error) { t.Error("unexpected fallback"); return "", "", nil }}
			s, err := c.listen(ctx, "en", 30*time.Millisecond)
			if s != nil || err == nil || errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "test-primary") {
				t.Fatalf("startup result: %v", err)
			}
		})
	}
}

func TestLiveListenMidSessionErrors(t *testing.T) {
	for _, mode := range []string{"close", "quota", "ignored"} {
		t.Run(mode, func(t *testing.T) {
			ctx := liveContext(t)
			server := realtimeServer(t, "gpt-realtime-whisper", "es", func(ctx context.Context, conn *websocket.Conn) {
				upstreamWrite(ctx, conn, `{"type":"session.updated"}`)
				if mode == "quota" {
					upstreamWrite(ctx, conn, `{"type":"error","error":{"type":"insufficient_quota","message":"test-primary"}}`)
				}
				if mode == "ignored" {
					upstreamWrite(ctx, conn, `{"type":"error","error":{"code":"other","message":"test-primary"}}`)
					upstreamWrite(ctx, conn, `{"type":"unknown"}`)
					upstreamWrite(ctx, conn, `{"type":"conversation.item.input_audio_transcription.completed","transcript":"Still open"}`)
					_, _, _ = conn.Read(ctx)
				}
			})
			defer server.Close()
			s, err := (&Client{Credentials: liveCredentials(server.URL, "test-primary")}).Listen(ctx, "es")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			want := ListenEvent{Type: "error", Code: "failed"}
			if mode == "quota" {
				want.Code = "unavailable"
			}
			if mode == "ignored" {
				want = ListenEvent{Type: "final", Text: "Still open"}
			}
			nextListen(t, ctx, s, want)
			if mode == "quota" {
				for _, send := range []func() error{func() error { return s.Append([]byte{1, 2}) }, s.Commit, s.Clear} {
					if err := send(); !errors.Is(err, ErrUnavailable) {
						t.Fatalf("quota write lost availability: %v", err)
					}
				}
			}
		})
	}
}

func TestLiveSpeechStreamingAndAlignment(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "defaults", true: "override"}[override], func(t *testing.T) {
			ctx := liveContext(t)
			first := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/audio/speech" || r.Header.Get("Authorization") != "Bearer test-primary" {
					t.Error("incorrect speech request")
					return
				}
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				wantVoice, wantModel := "marin", "gpt-4o-mini-tts"
				if override {
					wantVoice, wantModel = "cedar", "custom-tts"
				}
				if body["voice"] != wantVoice || body["model"] != wantModel || body["response_format"] != "pcm" || body["input"] != "Hello" || body["instructions"] != "Speak naturally in the language of the text." {
					t.Error("incorrect speech configuration")
				}
				_, _ = w.Write([]byte{1, 2, 3})
				w.(http.Flusher).Flush()
				select {
				case <-first:
				case <-r.Context().Done():
					return
				}
				_, _ = w.Write([]byte{4, 5, 6, 7})
				w.(http.Flusher).Flush()
				_, _ = w.Write([]byte{8})
			}))
			defer server.Close()
			c := &Client{Credentials: liveCredentials(server.URL, "test-primary"), HTTP: &http.Client{Timeout: 90 * time.Second}}
			if override {
				c.Config.LiveVoice = "cedar"
				c.Config.TTSModel = "custom-tts"
			}
			if c.liveHTTP(5*time.Minute).Timeout != 5*time.Minute {
				t.Fatal("stream uses short timeout")
			}
			var got []byte
			err := c.SpeakStream(ctx, "Hello", func(p []byte) error {
				if len(p)%2 != 0 {
					t.Error("unaligned PCM")
				}
				if len(got) == 0 {
					close(first)
				}
				got = append(got, p...)
				return nil
			})
			if err != nil || !bytes.Equal(got, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
				t.Fatalf("stream result %v, audio %v", err, got)
			}
		})
	}
}

func TestLiveSpeechFallback(t *testing.T) {
	for _, status := range []int{401, 402, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				if r.Header.Get("Authorization") == "Bearer test-primary" {
					w.WriteHeader(status)
					_, _ = io.WriteString(w, "test-primary")
					return
				}
				if r.Header.Get("Authorization") != "Bearer test-fallback" {
					t.Error("incorrect fallback credential")
				}
				_, _ = w.Write([]byte{1, 2})
			}))
			defer server.Close()
			c := &Client{Credentials: liveCredentials(server.URL, "test-primary"), FallbackCredentials: liveCredentials(server.URL, "test-fallback")}
			var got []byte
			if err := c.SpeakStream(liveContext(t), "Hello", func(p []byte) error { got = append(got, p...); return nil }); err != nil {
				t.Fatal(err)
			}
			if attempts.Load() != 2 || !bytes.Equal(got, []byte{1, 2}) {
				t.Fatal("fallback failed")
			}
		})
	}
}

func TestLiveSpeechCancellationAndWriteError(t *testing.T) {
	for _, mode := range []string{"cancel", "writer"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(liveContext(t))
			defer cancel()
			ended := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(ended)
				_, _ = w.Write([]byte{1, 2})
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			c := &Client{Credentials: liveCredentials(server.URL, "test-primary"), FallbackCredentials: func(context.Context) (string, string, error) { t.Error("fallback after audio"); return "", "", nil }}
			writerErr := errors.New("writer stopped")
			err := c.SpeakStream(ctx, "Hello", func([]byte) error {
				if mode == "cancel" {
					cancel()
					return nil
				}
				return writerErr
			})
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			if mode == "writer" && !errors.Is(err, writerErr) {
				t.Fatalf("writer error: %v", err)
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("upstream kept reading")
			}
		})
	}
}

func TestLiveSpeechInvalidAndRejected(t *testing.T) {
	ctx := liveContext(t)
	for _, text := range []string{"", " ", strings.Repeat("a", 4097), string([]byte{255})} {
		if err := (&Client{}).SpeakStream(ctx, text, func([]byte) error { return nil }); err == nil || errors.Is(err, ErrUnavailable) {
			t.Fatal("invalid text accepted")
		}
	}
	for _, mode := range []string{"rejected", "empty", "odd", "redirect", "both", "same"} {
		t.Run(mode, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				switch mode {
				case "odd":
					_, _ = w.Write([]byte{1})
				case "redirect":
					w.Header().Set("Location", "/must-not-redirect")
					w.WriteHeader(307)
				case "rejected":
					w.WriteHeader(500)
					_, _ = io.WriteString(w, "test-primary")
				case "both", "same":
					w.WriteHeader(401)
				}
			}))
			defer server.Close()
			c := &Client{Credentials: liveCredentials(server.URL, "test-primary"), FallbackCredentials: liveCredentials(server.URL, "test-fallback")}
			if mode == "same" {
				c.FallbackCredentials = c.Credentials
			}
			err := c.SpeakStream(ctx, "Hello", func([]byte) error { return nil })
			if err == nil || strings.Contains(err.Error(), "test-primary") {
				t.Fatalf("incorrect error: %v", err)
			}
			want := int32(1)
			if mode == "both" {
				want = 2
			}
			if attempts.Load() != want {
				t.Fatal("incorrect attempt count")
			}
		})
	}
}

func TestLiveListenTLSAndRedirectRefusal(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "TLS", true: "redirect"}[redirect], func(t *testing.T) {
			ctx := liveContext(t)
			var attempts atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				if r.URL.Path != "/v1/realtime" || r.Header.Get("Authorization") != "Bearer test-primary" {
					t.Error("incorrect TLS endpoint")
					return
				}
				if redirect {
					w.Header().Set("Location", "/redirect-target")
					w.WriteHeader(307)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				_, _, err = conn.Read(ctx)
				if err != nil {
					return
				}
				// The service may send events larger than the websocket library's default.
				upstreamWrite(ctx, conn, `{"type":"unknown","padding":"`+strings.Repeat("a", 128<<10)+`"}`)
				upstreamWrite(ctx, conn, `{"type":"session.updated"}`)
				_, _, _ = conn.Read(ctx)
			}))
			defer server.Close()
			c := &Client{Credentials: liveCredentials(server.URL, "test-primary"), HTTP: server.Client()}
			s, err := c.Listen(ctx, "es")
			if redirect {
				if err == nil || errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "test-primary") {
					t.Fatalf("redirect result: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				_ = s.Close()
			}
			if attempts.Load() != 1 {
				t.Fatal("redirect followed")
			}
		})
	}
}

func TestLiveSpeechReadFailureDoesNotRetry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte{1, 2})
		w.(http.Flusher).Flush()
	}))
	defer server.Close()
	c := &Client{Credentials: liveCredentials(server.URL, "test-primary"), FallbackCredentials: liveCredentials(server.URL, "test-fallback")}
	var got []byte
	err := c.SpeakStream(liveContext(t), "Hello", func(p []byte) error { got = append(got, p...); return nil })
	if err == nil || strings.Contains(err.Error(), "test-primary") || attempts.Load() != 1 || !bytes.Equal(got, []byte{1, 2}) {
		t.Fatalf("partial stream result: %v", err)
	}
}
