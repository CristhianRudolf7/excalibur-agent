//go:build browser && linux

package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGuardedRenderer(t *testing.T) {
	requireBrowser(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<p id="result"></p><script>document.getElementById('result').textContent='rendered JavaScript content'</script>`)
	}))
	defer site.Close()
	dir := t.TempDir()
	e := New(Config{ProfileDir: dir + "/profile"})
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	// This test-only fetch permits exactly one local fixture; production uses
	// websearch.FetchRenderResource and never permits private destinations.
	body, final, err := e.Render(ctx, site.URL, func(ctx context.Context, raw string) ([]byte, string, error) {
		if raw != site.URL+"/" {
			return nil, "", fmt.Errorf("unapproved fixture")
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return b, resp.Header.Get("Content-Type"), err
	})
	if err != nil || !strings.Contains(body, ">rendered JavaScript content</p>") || !strings.HasPrefix(final, site.URL) {
		t.Fatalf("render: %v", err)
	}
}
