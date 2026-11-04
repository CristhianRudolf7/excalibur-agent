package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/voice"
)

func TestVoiceLiveAdapterUnavailable(t *testing.T) {
	disabled := false
	for _, tc := range []struct {
		name   string
		routes *VoiceRoutes
		attach bool
	}{
		{name: "no services"},
		{name: "no routes", attach: true},
		{name: "no client", attach: true, routes: &VoiceRoutes{}},
		{name: "no credentials", attach: true, routes: &VoiceRoutes{Client: &voice.Client{}}},
		{name: "disabled", attach: true, routes: &VoiceRoutes{Client: &voice.Client{Config: config.VoiceConfig{Enabled: &disabled}, Credentials: func(context.Context) (string, string, error) {
			t.Fatal("disabled voice resolved credentials")
			return "", "", nil
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewDaemonBackend(nil, nil, nil)
			if tc.attach {
				b.AttachServices(BackendServices{Voice: tc.routes})
			}
			if _, err := b.VoiceListen(context.Background(), "es"); !errors.Is(err, gateway.ErrVoiceUnavailable) {
				t.Fatalf("listen unavailable mapping: %v", err)
			}
			if err := b.VoiceSpeak(context.Background(), "Hello", func([]byte) error { t.Fatal("unexpected audio"); return nil }); !errors.Is(err, gateway.ErrVoiceUnavailable) {
				t.Fatalf("speech unavailable mapping: %v", err)
			}
		})
	}
}

func TestVoiceLiveAdapterRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	actions := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/speech":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["input"] != "Hello" || body["response_format"] != "pcm" {
				t.Error("incorrect speech request")
			}
			_, _ = w.Write([]byte{1, 2, 3, 4})
		case "/v1/realtime":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			_, _, err = conn.Read(ctx)
			if err != nil {
				return
			}
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"session.updated"}`))
			for {
				_, raw, err := conn.Read(ctx)
				if err != nil {
					return
				}
				var msg map[string]string
				_ = json.Unmarshal(raw, &msg)
				actions <- msg["type"]
				if msg["type"] == "input_audio_buffer.append" {
					got, _ := base64.StdEncoding.DecodeString(msg["audio"])
					if !bytes.Equal(got, []byte{1, 2}) {
						t.Error("adapter changed audio")
					}
				}
				if msg["type"] == "input_audio_buffer.commit" {
					_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.delta","item_id":"a","delta":"Hello"}`))
					_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","item_id":"a","transcript":"Hello world"}`))
				}
			}
		default:
			t.Error("unexpected endpoint")
		}
	}))
	defer server.Close()
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{Voice: &VoiceRoutes{Client: &voice.Client{Credentials: func(context.Context) (string, string, error) { return server.URL + "/v1", "test-key", nil }}}})
	s, err := b.VoiceListen(ctx, "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Append([]byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"input_audio_buffer.append", "input_audio_buffer.commit", "input_audio_buffer.clear"} {
		select {
		case got := <-actions:
			if got != want {
				t.Fatalf("action %s", got)
			}
		case <-ctx.Done():
			t.Fatal("missing action")
		}
	}
	for _, want := range []gateway.VoiceListenEvent{{Type: "delta", Item: "a", Text: "Hello"}, {Type: "final", Item: "a", Text: "Hello world"}} {
		select {
		case got := <-s.Events():
			if got != want {
				t.Fatalf("event %+v", got)
			}
		case <-ctx.Done():
			t.Fatal("missing event")
		}
	}
	var pcm []byte
	if err := b.VoiceSpeak(ctx, "Hello", func(p []byte) error { pcm = append(pcm, p...); return nil }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pcm, []byte{1, 2, 3, 4}) {
		t.Fatal("adapter changed speech")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-s.Events():
		if ok {
			t.Fatal("adapter events still open")
		}
	case <-ctx.Done():
		t.Fatal("adapter leaked")
	}
}

func TestVoiceLiveAdapterProviderErrors(t *testing.T) {
	for _, status := range []int{401, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			b := NewDaemonBackend(nil, nil, nil)
			b.AttachServices(BackendServices{Voice: &VoiceRoutes{Client: &voice.Client{Credentials: func(context.Context) (string, string, error) { return server.URL, "test-key", nil }}}})
			_, listenErr := b.VoiceListen(context.Background(), "es")
			speechErr := b.VoiceSpeak(context.Background(), "Hello", func([]byte) error { return nil })
			for _, err := range []error{listenErr, speechErr} {
				if err == nil || errors.Is(err, gateway.ErrVoiceUnavailable) != (status == 401) {
					t.Fatalf("provider error mapping: %v", err)
				}
			}
		})
	}
}

func TestVoiceLiveAdapterPreservesMidCallQuota(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"session.updated"}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":{"type":"insufficient_quota"}}`))
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{Voice: &VoiceRoutes{Client: &voice.Client{Credentials: func(context.Context) (string, string, error) { return server.URL, "test-key", nil }}}})
	s, err := b.VoiceListen(ctx, "es")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case event := <-s.Events():
		if event.Type != "error" || event.Code != "unavailable" {
			t.Fatalf("quota event: %+v", event)
		}
	case <-ctx.Done():
		t.Fatal("missing quota event")
	}
	for _, send := range []func() error{func() error { return s.Append([]byte{1, 2}) }, s.Commit, s.Clear} {
		if err := send(); !errors.Is(err, gateway.ErrVoiceUnavailable) {
			t.Fatalf("quota mapping: %v", err)
		}
	}
}
