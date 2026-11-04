//go:build live

package daemon

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// This opt-in smoke uses a free Quick Tunnel and never calls a model provider.
func TestPublicPreviewLive(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 to exercise a real Cloudflare Quick Tunnel")
	}
	m := newPreviewManager(t.TempDir())
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := m.publish(ctx, previewRequest{Files: map[string]string{"index.html": "<!doctype html><html><body>Excalibur public preview smoke</body></html>"}, TTLSeconds: 60}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := m.client.Get(out.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "Excalibur public preview smoke") {
		t.Fatalf("public endpoint did not serve the smoke page (HTTP %d): %v", response.StatusCode, err)
	}
	if err = m.stopID(out.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.client.Get(out.LocalURL); err == nil {
		t.Fatal("owned HTTP server survived stop")
	}
}
