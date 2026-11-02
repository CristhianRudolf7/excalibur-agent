package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type apiProvider struct {
	name      string
	key       KeyResolver
	client    *http.Client
	limits    Limits
	userAgent string
}

func (p *apiProvider) Name() string { return p.name }
func freshness(s string) string {
	switch strings.ToLower(s) {
	case "day", "d", "pd", "24h":
		return "day"
	case "week", "w", "pw", "7d":
		return "week"
	case "month", "m", "pm", "30d":
		return "month"
	case "year", "y", "py", "365d":
		return "year"
	}
	return ""
}
func (p *apiProvider) Search(ctx context.Context, q string, o tools.SearchOptions) ([]tools.SearchResult, error) {
	fail := func(status int, transient, missing bool) *ProviderError {
		return &ProviderError{Provider: p.name, Status: status, Transient: transient, MissingKey: missing}
	}
	if p.key == nil {
		return nil, fail(0, false, true)
	}
	key, err := p.key(ctx)
	if err != nil || strings.TrimSpace(key) == "" {
		return nil, fail(0, false, true)
	}
	var req *http.Request
	if p.name == "tavily" {
		body := map[string]any{"query": q, "max_results": count(o.K), "search_depth": "basic", "topic": "general"}
		if f := freshness(o.Freshness); f != "" {
			body["time_range"] = f
		}
		if o.Lang != "" {
			body["language"] = o.Lang
		}
		if len(o.Domains) > 0 {
			body["include_domains"] = o.Domains
		}
		raw, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", bytes.NewReader(raw))
	} else {
		if len(o.Domains) > 0 {
			parts := []string{}
			for _, d := range o.Domains {
				parts = append(parts, "site:"+d)
			}
			q += " (" + strings.Join(parts, " OR ") + ")"
		}
		v := url.Values{"q": {q}, "count": {strconv.Itoa(count(o.K))}, "result_filter": {"web"}}
		if f := freshness(o.Freshness); f != "" {
			v.Set("freshness", map[string]string{"day": "pd", "week": "pw", "month": "pm", "year": "py"}[f])
		}
		if o.Lang != "" {
			v.Set("search_lang", o.Lang)
		}
		req, err = http.NewRequestWithContext(ctx, "GET", "https://api.search.brave.com/res/v1/web/search?"+v.Encode(), nil)
	}
	if err != nil {
		return nil, fail(0, false, false)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", p.userAgent)
	if p.name == "tavily" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("X-Subscription-Token", key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fail(0, true, false)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := fail(resp.StatusCode, resp.StatusCode == 429 || resp.StatusCode >= 500, false)
		e.RetryAfter = retryAfter(resp.Header.Get("Retry-After"))
		return nil, e
	}
	raw, err := readBounded(resp.Body, p.limits.ResponseBytes)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var readErr *bodyReadError
		return nil, fail(0, errors.As(err, &readErr) && readErr.timeout, false)
	}
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Results []struct {
			Title, URL, Content string
			PublishedDate       string `json:"published_date"`
		}
		Web struct {
			Results []struct {
				Title, URL, Description, Age string
				PageAge                      string `json:"page_age"`
			}
		}
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Error) > 0 || (p.name == "tavily" && envelope.Results == nil) {
		return nil, fail(0, false, false)
	}
	out := []tools.SearchResult{}
	clean := func(s string) string { return strings.ReplaceAll(s, key, "[redacted]") }
	for _, r := range envelope.Results {
		out = append(out, tools.SearchResult{Title: clean(r.Title), URL: clean(r.URL), Snippet: clean(r.Content), PublishedAt: clean(r.PublishedDate)})
	}
	for _, r := range envelope.Web.Results {
		date := r.PageAge
		if date == "" {
			date = r.Age
		}
		out = append(out, tools.SearchResult{Title: clean(r.Title), URL: clean(r.URL), Snippet: clean(r.Description), PublishedAt: clean(date)})
	}
	return filtered(out, o.K), nil
}

// bodyReadError preserves timeout classification without exposing transport details.
type bodyReadError struct{ timeout bool }

func (e *bodyReadError) Error() string { return "websearch: response read failed" }

func readBounded(r io.Reader, n int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		var timeout net.Error
		return nil, &bodyReadError{timeout: errors.As(err, &timeout) && timeout.Timeout()}
	}
	if int64(len(b)) > n {
		return nil, errors.New("websearch: response too large")
	}
	return b, nil
}
func retryAfter(s string) time.Duration {
	if n, e := strconv.ParseInt(s, 10, 32); e == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, e := http.ParseTime(s); e == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
