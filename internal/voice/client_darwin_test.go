//go:build darwin

package voice

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// Safari may record MP4/AAC while Chrome records WebM/Opus. Both browser
// formats must reach the transcription endpoint without a local audio binary.
func TestDarwinBrowserRecordingFormats(t *testing.T) {
	for _, recording := range []struct{ mime, filename, media string }{
		{"audio/mp4;codecs=mp4a.40.2", "audio.mp4", "audio/mp4"},
		{"audio/webm;codecs=opus", "audio.webm", "audio/webm"},
	} {
		t.Run(recording.media, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/audio/transcriptions" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				audio, header, err := r.FormFile("file")
				if err != nil {
					t.Error(err)
					http.Error(w, "missing audio", 400)
					return
				}
				defer audio.Close()
				data, err := io.ReadAll(audio)
				if err != nil || string(data) != "recorded audio" {
					t.Errorf("audio upload: %q, %v", data, err)
				}
				if header.Filename != recording.filename || header.Header.Get("Content-Type") != recording.media {
					t.Errorf("unexpected multipart media: %v", header.Header)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"text":"hello","language":"en"}`)
			}))
			defer server.Close()
			client := &Client{HTTP: server.Client(), Credentials: func(context.Context) (string, string, error) { return server.URL, "local-test", nil }}
			result, err := client.Transcribe(context.Background(), api.VoiceTranscribeParams{MIME: recording.mime}, []byte("recorded audio"))
			if err != nil || result.Text != "hello" {
				t.Fatalf("transcription: %+v, %v", result, err)
			}
		})
	}
}
