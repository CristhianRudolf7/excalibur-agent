package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/voice"
)

func audioRPC(t *testing.T, g *gateway.Server, method string, params any, out any) *api.Error {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("POST", "/rpc", bytes.NewReader(raw)))
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *api.Error      `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error == nil && out != nil {
		if err := json.Unmarshal(reply.Result, out); err != nil {
			t.Fatal(err)
		}
	}
	return reply.Error
}

func TestVoiceGatewayBlobRoundTrip(t *testing.T) {
	audio := []byte("synthesized audio")
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/speech":
			_, _ = w.Write(audio)
		case "/v1/audio/transcriptions":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			got, _ := io.ReadAll(f)
			if !bytes.Equal(got, audio) {
				t.Error("audio changed")
			}
			_, _ = io.WriteString(w, `{"text":"Hello","language":"en","duration":2}`)
		default:
			t.Error("unexpected path")
		}
	}))
	defer remote.Close()
	client := &voice.Client{Credentials: func(context.Context) (string, string, error) { return remote.URL + "/v1", "test-key", nil }}
	blobs := gateway.NewMemoryBlobs()
	backend := NewDaemonBackend(nil, nil, nil)
	backend.AttachServices(BackendServices{Voice: &VoiceRoutes{Client: client, Blobs: blobs}})
	cfg := gateway.DefaultConfig()
	cfg.AllowRemote = true
	g := gateway.New(cfg, backend, blobs, nil)
	var status api.VoiceStatusResult
	if err := audioRPC(t, g, api.MethodVoiceStatus, map[string]any{}, &status); err != nil || !status.Available {
		t.Fatalf("status %+v %v", status, err)
	}
	var spoken api.VoiceSpeakResult
	if err := audioRPC(t, g, api.MethodVoiceSpeak, api.VoiceSpeakParams{Text: "Hello"}, &spoken); err != nil {
		t.Fatal(err)
	}
	if spoken.MIME != "audio/mpeg" || len(spoken.AudioBlob) != 64 {
		t.Fatalf("speech %+v", spoken)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "/blobs/"+spoken.AudioBlob+"?mime=audio/mpeg", nil))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), audio) || w.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatal("speech blob unavailable")
	}
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/blobs", bytes.NewReader(audio))
	req.Header.Set("Content-Type", "audio/mpeg")
	g.ServeHTTP(w, req)
	var upload struct {
		AudioBlob string `json:"audio_blob"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &upload) != nil || upload.AudioBlob != spoken.AudioBlob {
		t.Fatalf("upload %d %s", w.Code, w.Body.String())
	}
	var transcript api.VoiceTranscribeResult
	if err := audioRPC(t, g, api.MethodVoiceTranscribe, api.VoiceTranscribeParams{AudioBlob: upload.AudioBlob, MIME: "audio/mpeg"}, &transcript); err != nil || transcript.Text != "Hello" || transcript.DurationMS != 2000 {
		t.Fatalf("transcript %+v %v", transcript, err)
	}
	for _, tc := range []struct {
		method string
		params any
		code   int
	}{
		{api.MethodVoiceSpeak, api.VoiceSpeakParams{Text: strings.Repeat("a", 4097)}, api.CodeInvalidParams},
		{api.MethodVoiceTranscribe, api.VoiceTranscribeParams{AudioBlob: "bad", MIME: "audio/mpeg"}, api.CodeInvalidParams},
		{api.MethodVoiceTranscribe, api.VoiceTranscribeParams{AudioBlob: strings.Repeat("0", 64), MIME: "audio/mpeg"}, api.CodeUnknownTarget},
	} {
		if err := audioRPC(t, g, tc.method, tc.params, nil); err == nil || err.Code != tc.code {
			t.Fatalf("invalid request: %v", err)
		}
	}
	client.Credentials = nil
	if err := audioRPC(t, g, api.MethodVoiceStatus, nil, &status); err != nil || status.Available || status.Reason == "" {
		t.Fatalf("no-key status %+v %v", status, err)
	}
}

func TestVoiceUnwiredStatus(t *testing.T) {
	cfg := gateway.DefaultConfig()
	cfg.AllowRemote = true
	g := gateway.New(cfg, NewDaemonBackend(nil, nil, nil), nil, nil)
	var status api.VoiceStatusResult
	if err := audioRPC(t, g, api.MethodVoiceStatus, nil, &status); err != nil || status.Available || status.Reason == "" {
		t.Fatalf("unwired status %+v %v", status, err)
	}
}
