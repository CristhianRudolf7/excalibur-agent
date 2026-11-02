package voice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{Credentials: func(context.Context) (string, string, error) { return server.URL + "/v1", "test-audio-key", nil }}
}

func TestAudioRequests(t *testing.T) {
	calls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-audio-key" || r.Method != "POST" {
			t.Error("missing request authorization or method")
		}
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			if r.FormValue("model") != "custom-stt" || r.FormValue("language") != "en" || r.FormValue("response_format") != "json" {
				t.Error("incorrect transcription fields")
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if string(data) != "audio bytes" || header.Filename != "audio.webm" || header.Header.Get("Content-Type") != "audio/webm" {
				t.Error("incorrect audio part")
			}
			_, _ = io.WriteString(w, `{"text":"Hello","language":"en","duration":1.25}`)
		case "/v1/audio/speech":
			var p map[string]string
			if json.NewDecoder(r.Body).Decode(&p) != nil {
				t.Error("invalid speech JSON")
			}
			if p["input"] != "Hello" || p["model"] != "custom-tts" || p["voice"] != "coral" || p["response_format"] != "wav" {
				t.Error("incorrect speech fields")
			}
			_, _ = io.WriteString(w, "wave bytes")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	client.Config = config.VoiceConfig{STTModel: "custom-stt", TTSModel: "custom-tts", Voice: "coral"}
	result, err := client.Transcribe(context.Background(), api.VoiceTranscribeParams{MIME: "audio/webm; codecs=opus", Language: "en"}, []byte("audio bytes"))
	if err != nil || result.Text != "Hello" || result.Language != "en" || result.DurationMS != 1250 {
		t.Fatalf("transcription: %+v %v", result, err)
	}
	data, media, err := client.Speak(context.Background(), api.VoiceSpeakParams{Text: "Hello", Format: "wav"})
	if err != nil || string(data) != "wave bytes" || media != "audio/wav" || calls != 2 {
		t.Fatalf("speech: %q %q %v", data, media, err)
	}
}

func TestAudioDefaultsAndLimits(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var p map[string]string
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p["model"] != "gpt-4o-mini-tts" || p["voice"] != "alloy" || p["response_format"] != "mp3" {
			t.Error("incorrect defaults")
		}
		_, _ = io.WriteString(w, "audio")
	})
	s := client.Status(context.Background())
	if !s.Available || s.STTModel != "gpt-4o-mini-transcribe" || len(s.Voices) < 13 {
		t.Fatalf("status %+v", s)
	}
	if _, _, err := client.Speak(context.Background(), api.VoiceSpeakParams{Text: strings.Repeat("é", 4096)}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []api.VoiceSpeakParams{{}, {Text: " "}, {Text: strings.Repeat("é", 4097)}, {Text: "ok", Format: "flac"}} {
		if _, _, err := client.Speak(context.Background(), p); err == nil {
			t.Fatal("accepted invalid text/format")
		}
	}
	for _, audio := range [][]byte{nil, make([]byte, MaxAudioBytes+1)} {
		if _, err := client.Transcribe(context.Background(), api.VoiceTranscribeParams{MIME: "audio/wav"}, audio); err == nil {
			t.Fatal("accepted invalid audio size")
		}
	}
	if _, err := client.Transcribe(context.Background(), api.VoiceTranscribeParams{MIME: "text/plain"}, []byte("audio")); err == nil {
		t.Fatal("accepted MIME")
	}
	client.Credentials = nil
	if s := client.Status(context.Background()); s.Available || s.Reason == "" || s.Voices == nil {
		t.Fatalf("no-key status %+v", s)
	}
	disabled := false
	client.Config.Enabled = &disabled
	if s := client.Status(context.Background()); s.Available || s.Reason != "voice: disabled" {
		t.Fatalf("disabled status %+v", s)
	}
}

func TestAudioErrorsCancellationAndRedirection(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://test-audio-key.invalid")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "test-audio-key")
			})
			_, _, err := client.Speak(context.Background(), api.VoiceSpeakParams{Text: "Hello"})
			if err == nil || strings.Contains(err.Error(), "test-audio-key") {
				t.Fatalf("unsafe error %v", err)
			}
		})
	}
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "not JSON") })
	if _, err := client.Transcribe(context.Background(), api.VoiceTranscribeParams{MIME: "audio/wav"}, []byte("audio")); err == nil {
		t.Fatal("accepted bad JSON")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := client.Speak(ctx, api.VoiceSpeakParams{Text: "Hello"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation %v", err)
	}
	client.Credentials = func(context.Context) (string, string, error) { return "", "", errors.New("test-audio-key") }
	if s := client.Status(context.Background()); s.Available || strings.Contains(s.Reason, "test-audio-key") {
		t.Fatalf("unsafe credentials status %+v", s)
	}
	client = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	})
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := client.Speak(ctx, api.VoiceSpeakParams{Text: "Hello"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline %v", err)
	}
}

func TestModelCredentialsResolveAtBoundary(t *testing.T) {
	for _, name := range []string{"EXCALIBUR_PROVIDER", "EXCALIBUR_API_KEY", "EXCALIBUR_API_KEY_SECRET", "EXCALIBUR_BASE_URL", "EXCALIBUR_CREDENTIALS_FILE", "OPENAI_API_KEY", "OPENAI_KEY", "EXCALIBUR_MODEL"} {
		t.Setenv(name, "")
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"file-key","base_url":"https://audio.example/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.ModelsConfig{Provider: "openai", CredentialsFile: path, APIKeyEnv: "AUDIO_TEST_KEY"}
	t.Setenv("AUDIO_TEST_KEY", "")
	resolve := ModelCredentials(cfg, nil)
	if base, key, err := resolve(context.Background()); err != nil || key != "file-key" || base != "https://audio.example/v1" {
		t.Fatal("file resolution failed")
	}
	t.Setenv("AUDIO_TEST_KEY", "env-key")
	if _, key, err := resolve(context.Background()); err != nil || key != "env-key" {
		t.Fatal("environment resolution failed")
	}
	cfg.APIKeySecret = "secret://audio"
	calls := 0
	resolve = ModelCredentials(cfg, func(ctx context.Context, name string) (string, error) {
		calls++
		if name != "secret://audio" {
			t.Error("secret reference changed")
		}
		return "vault-key", nil
	})
	for i := 0; i < 2; i++ {
		if _, key, err := resolve(context.Background()); err != nil || key != "vault-key" {
			t.Fatal("vault resolution failed")
		}
	}
	if calls != 2 {
		t.Fatal("credentials were cached")
	}
}
