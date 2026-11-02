package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRenderResourcesRetainReaderPolicy(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	for _, raw := range []string{server.URL, "file:///etc/passwd", "https://tbench.ai/task", "http://169.254.169.254/latest/meta-data"} {
		if _, _, err := FetchRenderResource(context.Background(), raw, "Excalibur test"); err == nil {
			t.Fatal("allowed forbidden render resource")
		}
	}
	if hits.Load() != 0 {
		t.Fatal("renderer dialed loopback")
	}
}
