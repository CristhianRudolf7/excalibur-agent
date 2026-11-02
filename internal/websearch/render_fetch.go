package websearch

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// FetchRenderResource fetches bounded public resources for the trusted browser
// renderer. Chrome receives fulfilled responses and never dials their hosts.
// Redirects and DNS rebinding use exactly the reader's guarded transport.
func FetchRenderResource(ctx context.Context, raw, userAgent string) ([]byte, string, error) {
	if _, err := validateURL(raw); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", errors.New("websearch: invalid resource")
	}
	req.Header.Set("User-Agent", userAgent)
	client := newReaderClient(15 * time.Second)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", errors.New("websearch: resource refused or unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", errors.New("websearch: resource unavailable")
	}
	body, err := readBounded(resp.Body, 5<<20)
	return body, resp.Header.Get("Content-Type"), err
}
